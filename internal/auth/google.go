package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
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

// GoogleAuth handles Google OAuth2 token retrieval, refresh, and the
// interactive browser-based authorization flow.
type GoogleAuth struct {
	ClientID     string
	ClientSecret string
	TokenURL     string
	Store        CredentialStore
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
		return "", fmt.Errorf("token refresh failed for %s: %w", key, err)
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

// RunOAuthFlow starts the interactive browser-based OAuth2 flow for the
// given service and account. It opens a browser, waits for the callback,
// exchanges the authorization code for tokens, and stores them.
func (g *GoogleAuth) RunOAuthFlow(service, account string) error {
	scopes, ok := defaultScopes[service]
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
