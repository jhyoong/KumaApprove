package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jhyoong/KumaApprove/internal/credstore"
)

type MicrosoftAuth struct {
	ClientID string
	TenantID string
	TokenURL string
	Store    CredentialStore
}

var microsoftScopes = map[string][]string{
	"outlook": {
		"Mail.Read",
		"Mail.Send",
		"Mail.ReadWrite",
		"offline_access",
	},
	"msft-cal": {
		"Calendars.Read",
		"Calendars.ReadWrite",
		"offline_access",
	},
}

func (m *MicrosoftAuth) tenantID() string {
	if m.TenantID != "" {
		return m.TenantID
	}
	return "consumers"
}

func (m *MicrosoftAuth) tokenURL() string {
	if m.TokenURL != "" {
		return m.TokenURL
	}
	return fmt.Sprintf("https://login.microsoftonline.com/%s/oauth2/v2.0/token", m.tenantID())
}

func (m *MicrosoftAuth) authorizeURL() string {
	return fmt.Sprintf("https://login.microsoftonline.com/%s/oauth2/v2.0/authorize", m.tenantID())
}

// msCallbackPort is the fixed loopback port used for the OAuth redirect.
// Microsoft matches the redirect URI against the app registration exactly --
// scheme, host, port and path must all match -- so the port cannot be
// ephemeral the way it is for Google. This URI must be registered under
// "Mobile & desktop applications" in the Azure AD app registration.
const msCallbackPort = 8400

// MicrosoftCallbackRedirectURI returns the redirect URI sent to Microsoft.
// It must use the literal host "localhost"; Microsoft rejects loopback IPs
// such as 127.0.0.1 for this app type (AADSTS50011). The returned URI has
// to be registered verbatim in the Azure AD app registration.
func MicrosoftCallbackRedirectURI() string {
	return fmt.Sprintf("http://localhost:%d/callback", msCallbackPort)
}

// listenLoopback binds msCallbackPort dual-stack so the callback arrives
// whether the browser resolves localhost to 127.0.0.1 or ::1, falling back
// to IPv4-only when IPv6 is unavailable.
func listenLoopback(port int) (net.Listener, error) {
	l, err := net.Listen("tcp", fmt.Sprintf("[::]:%d", port))
	if err == nil {
		return l, nil
	}
	v4, v4err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if v4err != nil {
		return nil, fmt.Errorf("binding callback port %d: %w (dual-stack attempt: %v)", port, v4err, err)
	}
	return v4, nil
}

// oauthState returns a random state value used to reject forged callbacks.
func oauthState() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generating OAuth state: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// callbackResult carries the outcome of the browser redirect to RunOAuthFlow.
type callbackResult struct {
	code  string
	state string
	err   error
}

// handleCallback validates the redirect query and reports on ch. Microsoft
// puts the failure reason in error/error_description, so those are surfaced
// verbatim rather than collapsed into "no authorization code received".
func handleCallback(w http.ResponseWriter, r *http.Request, ch chan<- callbackResult) {
	q := r.URL.Query()

	if oauthErr := q.Get("error"); oauthErr != "" {
		ch <- callbackResult{err: fmt.Errorf("%s: %s", oauthErr, q.Get("error_description"))}
		fmt.Fprintf(w, "Authorization failed: %s. You may close this tab.", oauthErr)
		return
	}

	code := q.Get("code")
	if code == "" {
		ch <- callbackResult{err: fmt.Errorf("no authorization code received")}
		fmt.Fprint(w, "Error: no authorization code. Close this tab.")
		return
	}

	ch <- callbackResult{code: code, state: q.Get("state")}
	fmt.Fprint(w, "Authorization successful. You may close this tab.")
}

func (m *MicrosoftAuth) GetToken(service, account string) (string, error) {
	key := service + ":" + account
	cred, err := m.Store.Get(key)
	if err != nil {
		return "", fmt.Errorf("no credentials for %s: %w", key, err)
	}

	if !tokenExpired(cred.Expiry) {
		return cred.AccessToken, nil
	}

	newToken, newRefresh, newExpiry, err := m.refreshToken(cred.RefreshToken)
	if err != nil {
		return "", &AuthExpiredError{Service: service, Account: account, Err: err}
	}

	cred.AccessToken = newToken
	cred.RefreshToken = newRefresh
	cred.Expiry = newExpiry
	if err := m.Store.Put(key, cred); err != nil {
		return "", fmt.Errorf("saving refreshed token: %w", err)
	}

	return newToken, nil
}

func (m *MicrosoftAuth) refreshToken(refreshToken string) (string, string, string, error) {
	params := url.Values{
		"client_id":     {m.ClientID},
		"refresh_token": {refreshToken},
		"grant_type":    {"refresh_token"},
	}

	resp, err := http.PostForm(m.tokenURL(), params)
	if err != nil {
		return "", "", "", err
	}
	defer resp.Body.Close()

	var result struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		Error        string `json:"error"`
		ErrorDesc    string `json:"error_description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", "", "", err
	}
	if result.Error != "" {
		return "", "", "", fmt.Errorf("%s: %s", result.Error, result.ErrorDesc)
	}

	expiry := time.Now().Add(time.Duration(result.ExpiresIn) * time.Second).Format(time.RFC3339)
	return result.AccessToken, result.RefreshToken, expiry, nil
}

func (m *MicrosoftAuth) RunOAuthFlow(service, account string) error {
	scopes, ok := microsoftScopes[service]
	if !ok {
		return fmt.Errorf("unknown service: %s", service)
	}

	listener, err := listenLoopback(msCallbackPort)
	if err != nil {
		return fmt.Errorf("starting callback server: %w", err)
	}
	redirectURI := MicrosoftCallbackRedirectURI()

	state, err := oauthState()
	if err != nil {
		listener.Close()
		return err
	}

	authURL := fmt.Sprintf(
		"%s?client_id=%s&redirect_uri=%s&response_type=code&scope=%s&response_mode=query&state=%s",
		m.authorizeURL(),
		url.QueryEscape(m.ClientID),
		url.QueryEscape(redirectURI),
		url.QueryEscape(strings.Join(scopes, " ")),
		url.QueryEscape(state),
	)

	fmt.Printf("Opening browser for Microsoft authorization...\n")
	fmt.Printf("If the browser does not open, visit:\n%s\n\n", authURL)
	openBrowser(authURL)

	resultCh := make(chan callbackResult, 4)

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		handleCallback(w, r, resultCh)
	})

	server := &http.Server{Handler: mux}
	go server.Serve(listener)

	code, err := waitForAuthCode(server, resultCh, state)
	if err != nil {
		return err
	}

	token, refresh, expiry, err := m.exchangeCode(code, redirectURI)
	if err != nil {
		return fmt.Errorf("exchanging code: %w", err)
	}

	key := service + ":" + account
	return m.Store.Put(key, credstore.Credential{
		AccessToken:  token,
		RefreshToken: refresh,
		Expiry:       expiry,
	})
}

// waitForAuthCode blocks until the browser callback delivers an authorization
// code whose state matches, or until the flow times out. Callbacks with a
// mismatched state are ignored: the callback port is fixed, so a stale tab
// from an earlier attempt can hit the server between retries.
func waitForAuthCode(server *http.Server, resultCh <-chan callbackResult, state string) (string, error) {
	timeout := time.After(5 * time.Minute)
	for {
		select {
		case res := <-resultCh:
			if res.err != nil {
				server.Shutdown(context.Background())
				return "", res.err
			}
			if res.state != state {
				continue
			}
			server.Shutdown(context.Background())
			return res.code, nil
		case <-timeout:
			server.Shutdown(context.Background())
			return "", fmt.Errorf("OAuth flow timed out after 5 minutes")
		}
	}
}

func (m *MicrosoftAuth) exchangeCode(code, redirectURI string) (string, string, string, error) {
	params := url.Values{
		"client_id":    {m.ClientID},
		"code":         {code},
		"redirect_uri": {redirectURI},
		"grant_type":   {"authorization_code"},
	}

	resp, err := http.PostForm(m.tokenURL(), params)
	if err != nil {
		return "", "", "", err
	}
	defer resp.Body.Close()

	var result struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		Error        string `json:"error"`
		ErrorDesc    string `json:"error_description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", "", "", err
	}
	if result.Error != "" {
		return "", "", "", fmt.Errorf("%s: %s", result.Error, result.ErrorDesc)
	}

	expiry := time.Now().Add(time.Duration(result.ExpiresIn) * time.Second).Format(time.RFC3339)
	return result.AccessToken, result.RefreshToken, expiry, nil
}
