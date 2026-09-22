package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/jhyoong/KumaApprove/internal/credstore"
)

// CredentialStore abstracts credential storage so that GoogleAuth can be tested
// with a fake store. It mirrors the Get/Put methods of credstore.Store.
type CredentialStore interface {
	Get(key string) (credstore.Credential, error)
	Put(key string, cred credstore.Credential) error
}

// AuthExpiredError indicates that a token refresh failed and the user
// must re-authorize via the OAuth flow.
type AuthExpiredError struct {
	Service string
	Account string
	Err     error
}

func (e *AuthExpiredError) Error() string {
	return fmt.Sprintf("auth expired for %s:%s: %v", e.Service, e.Account, e.Err)
}

func (e *AuthExpiredError) Unwrap() error {
	return e.Err
}

// GoogleAuth handles Google OAuth2 token retrieval, refresh, and the
// interactive browser-based authorization flow.
type GoogleAuth struct {
	ClientID      string
	ClientSecret  string
	TokenURL      string
	DeviceCodeURL string
	RelayURL      string
	Store         CredentialStore
}

var defaultScopes = map[string][]string{
	"gmail": {
		"https://www.googleapis.com/auth/gmail.readonly",
		"https://www.googleapis.com/auth/gmail.send",
		"https://www.googleapis.com/auth/gmail.compose",
		"https://www.googleapis.com/auth/gmail.modify",
	},
	"gcal": {
		"https://www.googleapis.com/auth/calendar",
		"https://www.googleapis.com/auth/calendar.events",
	},
}

// GetToken returns a valid access token for the given service and account.
// If the stored token is expired, it refreshes it automatically.
func (g *GoogleAuth) GetToken(service, account string) (string, error) {
	key := service + ":" + account
	cred, err := g.Store.Get(key)
	if err != nil {
		return "", fmt.Errorf("no credentials for %s: %w", key, err)
	}

	if !tokenExpired(cred.Expiry) {
		return cred.AccessToken, nil
	}

	newToken, newExpiry, err := g.refreshToken(cred.RefreshToken)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[AUTH] Token refresh failed: %v\n", err)
		token, reAuthErr := g.reAuth(service, account)
		if reAuthErr != nil {
			return "", &AuthExpiredError{Service: service, Account: account, Err: fmt.Errorf("refresh failed: %w; re-auth failed: %w", err, reAuthErr)}
		}
		return token, nil
	}

	cred.AccessToken = newToken
	cred.Expiry = newExpiry
	if err := g.Store.Put(key, cred); err != nil {
		return "", fmt.Errorf("saving refreshed token: %w", err)
	}

	return newToken, nil
}

func (g *GoogleAuth) refreshToken(refreshToken string) (string, string, error) {
	tokenURL := g.TokenURL
	if tokenURL == "" {
		tokenURL = "https://oauth2.googleapis.com/token"
	}

	params := url.Values{
		"client_id":     {g.ClientID},
		"client_secret": {g.ClientSecret},
		"refresh_token": {refreshToken},
		"grant_type":    {"refresh_token"},
	}

	resp, err := http.PostForm(tokenURL, params)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	var result struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		Error       string `json:"error"`
		ErrorDesc   string `json:"error_description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", "", err
	}
	if result.Error != "" {
		return "", "", fmt.Errorf("%s: %s", result.Error, result.ErrorDesc)
	}

	expiry := time.Now().Add(time.Duration(result.ExpiresIn) * time.Second).Format(time.RFC3339)
	return result.AccessToken, expiry, nil
}

// deviceFlowServices lists services whose scopes Google allows in the
// device-authorization flow (RFC 8628). Gmail scopes are prohibited
// by Google in device flow — Gmail re-auth requires the browser-based
// OAuth flow.
var deviceFlowServices = map[string]bool{
	"gcal": true,
}

type deviceCodeResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURL string `json:"verification_url"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
	Error           string `json:"error"`
	ErrorDesc       string `json:"error_description"`
}

func (g *GoogleAuth) requestDeviceCode(service string) (deviceCodeResponse, error) {
	scopes, ok := defaultScopes[service]
	if !ok {
		return deviceCodeResponse{}, fmt.Errorf("unknown service: %s", service)
	}

	deviceCodeURL := g.DeviceCodeURL
	if deviceCodeURL == "" {
		deviceCodeURL = "https://oauth2.googleapis.com/device/code"
	}

	params := url.Values{
		"client_id": {g.ClientID},
		"scope":     {strings.Join(scopes, " ")},
	}

	resp, err := http.PostForm(deviceCodeURL, params)
	if err != nil {
		return deviceCodeResponse{}, err
	}
	defer resp.Body.Close()

	var result deviceCodeResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return deviceCodeResponse{}, err
	}
	if result.Error != "" {
		return deviceCodeResponse{}, fmt.Errorf("device code request: %s: %s", result.Error, result.ErrorDesc)
	}

	return result, nil
}

func (g *GoogleAuth) pollDeviceToken(deviceCode string, interval, expiresIn int) (string, string, string, error) {
	tokenURL := g.TokenURL
	if tokenURL == "" {
		tokenURL = "https://oauth2.googleapis.com/token"
	}

	if interval < 1 {
		interval = 5
	}

	deadline := time.Now().Add(time.Duration(expiresIn) * time.Second)

	for {
		if time.Now().After(deadline) {
			return "", "", "", fmt.Errorf("device flow timed out")
		}

		time.Sleep(time.Duration(interval) * time.Second)

		params := url.Values{
			"client_id":     {g.ClientID},
			"client_secret": {g.ClientSecret},
			"device_code":   {deviceCode},
			"grant_type":    {"urn:ietf:params:oauth:grant-type:device_code"},
		}

		resp, err := http.PostForm(tokenURL, params)
		if err != nil {
			return "", "", "", err
		}

		var result struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
			ExpiresIn    int    `json:"expires_in"`
			Error        string `json:"error"`
		}
		json.NewDecoder(resp.Body).Decode(&result)
		resp.Body.Close()

		switch result.Error {
		case "":
			expiry := time.Now().Add(time.Duration(result.ExpiresIn) * time.Second).Format(time.RFC3339)
			return result.AccessToken, result.RefreshToken, expiry, nil
		case "authorization_pending":
			continue
		case "slow_down":
			interval += 5
			continue
		case "access_denied":
			return "", "", "", fmt.Errorf("user denied access")
		default:
			return "", "", "", fmt.Errorf("device flow error: %s", result.Error)
		}
	}
}

func (g *GoogleAuth) runDeviceFlow(service, account string) (string, error) {
	if !deviceFlowServices[service] {
		return "", fmt.Errorf("device flow not supported for %s (scopes prohibited by Google); re-run: kuma-approve auth %s %s", service, service, account)
	}

	resp, err := g.requestDeviceCode(service)
	if err != nil {
		return "", err
	}
	if resp.DeviceCode == "" {
		return "", fmt.Errorf("empty device code in response")
	}

	fmt.Fprintf(os.Stderr, "[AUTH_DEVICE_FLOW] Verification URL: %s\n", resp.VerificationURL)
	fmt.Fprintf(os.Stderr, "[AUTH_DEVICE_FLOW] User Code: %s\n", resp.UserCode)
	fmt.Fprintf(os.Stderr, "[AUTH_DEVICE_FLOW] Waiting for approval (expires in %ds)...\n", resp.ExpiresIn)

	token, refresh, expiry, err := g.pollDeviceToken(resp.DeviceCode, resp.Interval, resp.ExpiresIn)
	if err != nil {
		return "", err
	}

	key := service + ":" + account
	if err := g.Store.Put(key, credstore.Credential{
		AccessToken:  token,
		RefreshToken: refresh,
		Expiry:       expiry,
	}); err != nil {
		return "", fmt.Errorf("saving device flow token: %w", err)
	}

	return token, nil
}

func (g *GoogleAuth) runRelayFlow(service, account string) (string, error) {
	if g.RelayURL == "" {
		return "", fmt.Errorf("relay URL not configured")
	}

	scopes, ok := defaultScopes[service]
	if !ok {
		return "", fmt.Errorf("unknown service: %s", service)
	}

	sessionBytes := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, sessionBytes); err != nil {
		return "", fmt.Errorf("generating session ID: %w", err)
	}
	sessionID := hex.EncodeToString(sessionBytes)

	redirectURI := strings.TrimRight(g.RelayURL, "/") + "/callback"

	authURL := fmt.Sprintf(
		"https://accounts.google.com/o/oauth2/v2/auth?client_id=%s&redirect_uri=%s&response_type=code&scope=%s&access_type=offline&prompt=consent&state=%s",
		url.QueryEscape(g.ClientID),
		url.QueryEscape(redirectURI),
		url.QueryEscape(strings.Join(scopes, " ")),
		url.QueryEscape(sessionID),
	)

	fmt.Fprintf(os.Stderr, "[AUTH_RELAY] Authorize at: %s\n", authURL)
	fmt.Fprintf(os.Stderr, "[AUTH_RELAY] Waiting for authorization (timeout 5m)...\n")

	pollURL := fmt.Sprintf("%s/poll?session=%s", strings.TrimRight(g.RelayURL, "/"), url.QueryEscape(sessionID))
	deadline := time.Now().Add(5 * time.Minute)

	for {
		if time.Now().After(deadline) {
			return "", fmt.Errorf("relay flow timed out after 5 minutes")
		}

		time.Sleep(5 * time.Second)

		resp, err := http.Get(pollURL)
		if err != nil {
			continue
		}

		var result struct {
			Status string `json:"status"`
			Code   string `json:"code"`
		}
		json.NewDecoder(resp.Body).Decode(&result)
		resp.Body.Close()

		if result.Status == "complete" && result.Code != "" {
			token, refresh, expiry, err := g.exchangeCode(result.Code, redirectURI)
			if err != nil {
				return "", fmt.Errorf("exchanging relay code: %w", err)
			}

			key := service + ":" + account
			if err := g.Store.Put(key, credstore.Credential{
				AccessToken:  token,
				RefreshToken: refresh,
				Expiry:       expiry,
			}); err != nil {
				return "", fmt.Errorf("saving relay flow token: %w", err)
			}

			return token, nil
		}
	}
}

func (g *GoogleAuth) reAuth(service, account string) (string, error) {
	if deviceFlowServices[service] {
		token, err := g.runDeviceFlow(service, account)
		if err == nil {
			return token, nil
		}
		fmt.Fprintf(os.Stderr, "[AUTH] Device flow failed: %v\n", err)
	}

	if g.RelayURL != "" {
		token, err := g.runRelayFlow(service, account)
		if err == nil {
			return token, nil
		}
		fmt.Fprintf(os.Stderr, "[AUTH] Relay flow failed: %v\n", err)
	}

	fmt.Fprintf(os.Stderr, "[AUTH] Falling back to browser OAuth...\n")

	if err := g.RunOAuthFlow(service, account); err != nil {
		return "", err
	}
	key := service + ":" + account
	cred, err := g.Store.Get(key)
	if err != nil {
		return "", err
	}
	return cred.AccessToken, nil
}

// RunOAuthFlow starts the interactive browser-based OAuth2 flow for the
// given service and account. It opens a browser, waits for the callback,
// exchanges the authorization code for tokens, and stores them.
func (g *GoogleAuth) RunOAuthFlow(service, account string) error {
	scopes, ok := defaultScopes[service]
	if !ok {
		return fmt.Errorf("unknown service: %s", service)
	}

	listenAddr := "127.0.0.1:0"
	if p, err := strconv.Atoi(os.Getenv("KUMA_OAUTH_PORT")); err == nil && p > 0 {
		listenAddr = fmt.Sprintf("0.0.0.0:%d", p)
	}
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return fmt.Errorf("starting callback server: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/callback", port)

	authURL := fmt.Sprintf(
		"https://accounts.google.com/o/oauth2/v2/auth?client_id=%s&redirect_uri=%s&response_type=code&scope=%s&access_type=offline&prompt=consent",
		url.QueryEscape(g.ClientID),
		url.QueryEscape(redirectURI),
		url.QueryEscape(strings.Join(scopes, " ")),
	)

	fmt.Printf("Opening browser for OAuth authorization...\n")
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

	token, refresh, expiry, err := g.exchangeCode(code, redirectURI)
	if err != nil {
		return fmt.Errorf("exchanging code: %w", err)
	}

	key := service + ":" + account
	return g.Store.Put(key, credstore.Credential{
		AccessToken:  token,
		RefreshToken: refresh,
		Expiry:       expiry,
	})
}

func (g *GoogleAuth) exchangeCode(code, redirectURI string) (string, string, string, error) {
	tokenURL := g.TokenURL
	if tokenURL == "" {
		tokenURL = "https://oauth2.googleapis.com/token"
	}

	params := url.Values{
		"client_id":     {g.ClientID},
		"client_secret": {g.ClientSecret},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"grant_type":    {"authorization_code"},
	}

	resp, err := http.PostForm(tokenURL, params)
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

// tokenExpired returns true if the given expiry time (RFC3339) is within
// 5 minutes of now or already past.
func tokenExpired(expiry string) bool {
	t, err := time.Parse(time.RFC3339, expiry)
	if err != nil {
		return true
	}
	return time.Now().After(t.Add(-5 * time.Minute))
}

func openBrowser(url string) {
	switch runtime.GOOS {
	case "darwin":
		exec.Command("open", url).Start()
	case "linux":
		exec.Command("xdg-open", url).Start()
	}
}
