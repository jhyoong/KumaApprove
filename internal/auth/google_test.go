package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jhyoong/KumaApprove/internal/credstore"
)

type fakeStore struct {
	creds map[string]credstore.Credential
}

func newFakeStore() *fakeStore {
	return &fakeStore{creds: make(map[string]credstore.Credential)}
}
func (f *fakeStore) Get(key string) (credstore.Credential, error) {
	c, ok := f.creds[key]
	if !ok {
		return credstore.Credential{}, fmt.Errorf("not found: %s", key)
	}
	return c, nil
}
func (f *fakeStore) Put(key string, c credstore.Credential) error {
	f.creds[key] = c
	return nil
}

func TestTokenNeedsRefresh(t *testing.T) {
	past := time.Now().Add(-1 * time.Hour).Format(time.RFC3339)
	future := time.Now().Add(1 * time.Hour).Format(time.RFC3339)

	if !tokenExpired(past) {
		t.Fatal("past time should be expired")
	}
	if tokenExpired(future) {
		t.Fatal("future time should not be expired")
	}
}

func TestRefreshToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"access_token": "new-access-token",
			"expires_in":   3600,
			"token_type":   "Bearer",
		})
	}))
	defer server.Close()

	store := newFakeStore()
	store.Put("gmail:test@gmail.com", credstore.Credential{
		AccessToken:  "old-token",
		RefreshToken: "refresh-token-123",
		Expiry:       time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
	})

	provider := &GoogleAuth{
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		TokenURL:     server.URL,
		Store:        store,
	}

	token, err := provider.GetToken("gmail", "test@gmail.com")
	if err != nil {
		t.Fatal(err)
	}
	if token != "new-access-token" {
		t.Fatalf("expected new-access-token, got %s", token)
	}

	updated, _ := store.Get("gmail:test@gmail.com")
	if updated.AccessToken != "new-access-token" {
		t.Fatal("store should have been updated")
	}
}

func TestGetTokenStillValid(t *testing.T) {
	store := newFakeStore()
	store.Put("gmail:test@gmail.com", credstore.Credential{
		AccessToken:  "valid-token",
		RefreshToken: "refresh-123",
		Expiry:       time.Now().Add(1 * time.Hour).Format(time.RFC3339),
	})

	provider := &GoogleAuth{
		Store: store,
	}

	token, err := provider.GetToken("gmail", "test@gmail.com")
	if err != nil {
		t.Fatal(err)
	}
	if token != "valid-token" {
		t.Fatalf("expected valid-token, got %s", token)
	}
}

func TestGetTokenAllReAuthFails(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"error":             "invalid_grant",
			"error_description": "Token has been expired or revoked.",
		})
	}))
	defer tokenServer.Close()

	// Occupy a port so RunOAuthFlow fails immediately
	blocker, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	blockerPort := blocker.Addr().(*net.TCPAddr).Port
	defer blocker.Close()
	os.Setenv("KUMA_OAUTH_PORT", strconv.Itoa(blockerPort))
	defer os.Unsetenv("KUMA_OAUTH_PORT")

	store := newFakeStore()
	store.Put("gcal:test@gmail.com", credstore.Credential{
		AccessToken:  "expired-token",
		RefreshToken: "revoked-refresh-token",
		Expiry:       time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
	})

	provider := &GoogleAuth{
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		TokenURL:     tokenServer.URL,
		Store:        store,
	}

	_, err = provider.GetToken("gcal", "test@gmail.com")
	if err == nil {
		t.Fatal("expected AuthExpiredError when all re-auth methods fail")
	}

	var authErr *AuthExpiredError
	if !errors.As(err, &authErr) {
		t.Fatalf("expected AuthExpiredError, got %T: %v", err, err)
	}
}

func TestReAuthGmailUsesBrowserOAuth(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.FormValue("grant_type") == "authorization_code" {
			json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "browser-oauth-token",
				"refresh_token": "browser-oauth-refresh",
				"expires_in":    3600,
			})
			return
		}
		t.Fatalf("unexpected grant_type: %s", r.FormValue("grant_type"))
	}))
	defer tokenServer.Close()

	// Find a free port for the OAuth callback server
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()

	os.Setenv("KUMA_OAUTH_PORT", strconv.Itoa(port))
	defer os.Unsetenv("KUMA_OAUTH_PORT")

	store := newFakeStore()
	provider := &GoogleAuth{
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		TokenURL:     tokenServer.URL,
		Store:        store,
	}

	type result struct {
		token string
		err   error
	}
	done := make(chan result, 1)

	go func() {
		token, err := provider.reAuth("gmail", "test@gmail.com")
		done <- result{token, err}
	}()

	// Wait for RunOAuthFlow to start listening
	time.Sleep(300 * time.Millisecond)

	// Simulate the browser callback
	callbackURL := fmt.Sprintf("http://127.0.0.1:%d/callback?code=test-auth-code", port)
	resp, err := http.Get(callbackURL)
	if err != nil {
		t.Fatalf("failed to hit callback: %v", err)
	}
	resp.Body.Close()

	r := <-done
	if r.err != nil {
		t.Fatalf("reAuth failed: %v", r.err)
	}
	if r.token != "browser-oauth-token" {
		t.Fatalf("expected browser-oauth-token, got %s", r.token)
	}

	cred, _ := store.Get("gmail:test@gmail.com")
	if cred.RefreshToken != "browser-oauth-refresh" {
		t.Fatal("store should have the refresh token from browser OAuth")
	}
}

func TestRunRelayFlowSuccess(t *testing.T) {
	// Mock relay server: returns pending once, then complete with auth code.
	relayAttempt := 0
	relayServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		relayAttempt++
		w.Header().Set("Content-Type", "application/json")
		if relayAttempt < 2 {
			json.NewEncoder(w).Encode(map[string]string{"status": "pending"})
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "complete", "code": "relay-auth-code"})
	}))
	defer relayServer.Close()

	// Mock token server: exchanges the relay auth code for tokens.
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.FormValue("code") != "relay-auth-code" {
			t.Fatalf("expected relay-auth-code, got %s", r.FormValue("code"))
		}
		json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "relay-access-token",
			"refresh_token": "relay-refresh-token",
			"expires_in":    3600,
		})
	}))
	defer tokenServer.Close()

	store := newFakeStore()
	provider := &GoogleAuth{
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		RelayURL:     relayServer.URL,
		TokenURL:     tokenServer.URL,
		Store:        store,
	}

	token, err := provider.runRelayFlow("gmail", "test@gmail.com")
	if err != nil {
		t.Fatalf("relay flow failed: %v", err)
	}
	if token != "relay-access-token" {
		t.Fatalf("expected relay-access-token, got %s", token)
	}

	cred, _ := store.Get("gmail:test@gmail.com")
	if cred.RefreshToken != "relay-refresh-token" {
		t.Fatal("store should have the refresh token from relay flow")
	}
}

func TestRunRelayFlowNotifiesURL(t *testing.T) {
	relayServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "complete", "code": "relay-auth-code"})
	}))
	defer relayServer.Close()

	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "relay-access-token",
			"refresh_token": "relay-refresh-token",
			"expires_in":    3600,
		})
	}))
	defer tokenServer.Close()

	var sent []string
	provider := &GoogleAuth{
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		RelayURL:     relayServer.URL,
		TokenURL:     tokenServer.URL,
		Store:        newFakeStore(),
		Notify: func(text string) error {
			sent = append(sent, text)
			return errors.New("telegram down")
		},
	}

	// A Notify failure must not abort the flow.
	if err := provider.RunRelayFlow("gmail", "test@gmail.com"); err != nil {
		t.Fatalf("relay flow failed: %v", err)
	}
	if len(sent) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(sent))
	}
	for _, want := range []string{"response_type=code", "redirect_uri=" + url.QueryEscape(relayServer.URL+"/callback"), "state="} {
		if !strings.Contains(sent[0], want) {
			t.Errorf("notification missing %q: %s", want, sent[0])
		}
	}
}

func TestReAuthRelayFailureSkipsBrowserOAuth(t *testing.T) {
	relayServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "complete", "code": "bad-code"})
	}))
	defer relayServer.Close()

	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"error": "invalid_grant", "error_description": "Bad code"})
	}))
	defer tokenServer.Close()

	// Occupy the callback port so a browser OAuth fallback would fail with a
	// different, recognisable error instead of blocking for 5 minutes.
	blocker, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Close()
	os.Setenv("KUMA_OAUTH_PORT", strconv.Itoa(blocker.Addr().(*net.TCPAddr).Port))
	defer os.Unsetenv("KUMA_OAUTH_PORT")

	provider := &GoogleAuth{
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		RelayURL:     relayServer.URL,
		TokenURL:     tokenServer.URL,
		Store:        newFakeStore(),
	}

	_, err = provider.reAuth("gmail", "test@gmail.com")
	if err == nil {
		t.Fatal("expected relay failure to be returned")
	}
	if !strings.Contains(err.Error(), "exchanging relay code") {
		t.Fatalf("expected relay error without browser fallback, got: %v", err)
	}
}

func TestRunRelayFlowSkippedWhenNotConfigured(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.FormValue("grant_type") == "authorization_code" {
			json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "browser-token",
				"refresh_token": "browser-refresh",
				"expires_in":    3600,
			})
			return
		}
		t.Fatalf("unexpected grant_type: %s", r.FormValue("grant_type"))
	}))
	defer tokenServer.Close()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()

	os.Setenv("KUMA_OAUTH_PORT", strconv.Itoa(port))
	defer os.Unsetenv("KUMA_OAUTH_PORT")

	store := newFakeStore()
	provider := &GoogleAuth{
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		TokenURL:     tokenServer.URL,
		Store:        store,
	}

	type result struct {
		token string
		err   error
	}
	done := make(chan result, 1)

	go func() {
		token, err := provider.reAuth("gmail", "test@gmail.com")
		done <- result{token, err}
	}()

	time.Sleep(300 * time.Millisecond)

	callbackURL := fmt.Sprintf("http://127.0.0.1:%d/callback?code=browser-code", port)
	resp, err := http.Get(callbackURL)
	if err != nil {
		t.Fatalf("failed to hit callback: %v", err)
	}
	resp.Body.Close()

	r := <-done
	if r.err != nil {
		t.Fatalf("reAuth failed: %v", r.err)
	}
	if r.token != "browser-token" {
		t.Fatalf("expected browser-token, got %s", r.token)
	}
}
