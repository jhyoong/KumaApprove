package auth

import (
	"context"
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
	ClientID     string
	ClientSecret string
	TenantID     string
	TokenURL     string
	Store        CredentialStore
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
		"client_secret": {m.ClientSecret},
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

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("starting callback server: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/callback", port)

	authURL := fmt.Sprintf(
		"%s?client_id=%s&redirect_uri=%s&response_type=code&scope=%s&response_mode=query",
		m.authorizeURL(),
		url.QueryEscape(m.ClientID),
		url.QueryEscape(redirectURI),
		url.QueryEscape(strings.Join(scopes, " ")),
	)

	fmt.Printf("Opening browser for Microsoft authorization...\n")
	fmt.Printf("If the browser does not open, visit:\n%s\n\n", authURL)
	openBrowser(authURL)

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		if code == "" {
			errCh <- fmt.Errorf("no authorization code received")
			fmt.Fprint(w, "Error: no authorization code. Close this tab.")
			return
		}
		codeCh <- code
		fmt.Fprint(w, "Authorization successful. You may close this tab.")
	})

	server := &http.Server{Handler: mux}
	go server.Serve(listener)

	var code string
	select {
	case code = <-codeCh:
	case err := <-errCh:
		server.Shutdown(context.Background())
		return err
	case <-time.After(5 * time.Minute):
		server.Shutdown(context.Background())
		return fmt.Errorf("OAuth flow timed out after 5 minutes")
	}
	server.Shutdown(context.Background())

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

func (m *MicrosoftAuth) exchangeCode(code, redirectURI string) (string, string, string, error) {
	params := url.Values{
		"client_id":     {m.ClientID},
		"client_secret": {m.ClientSecret},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"grant_type":    {"authorization_code"},
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
