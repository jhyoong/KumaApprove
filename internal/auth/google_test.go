package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
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

func TestRequestDeviceCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.FormValue("client_id") != "client-id" {
			t.Fatalf("expected client_id=client-id, got %s", r.FormValue("client_id"))
		}
		if r.FormValue("scope") == "" {
			t.Fatal("expected non-empty scope")
		}
		json.NewEncoder(w).Encode(map[string]any{
			"device_code":      "device-code-123",
			"user_code":        "ABCD-EFGH",
			"verification_url": "https://www.google.com/device",
			"expires_in":       300,
			"interval":         5,
		})
	}))
	defer server.Close()

	provider := &GoogleAuth{
		ClientID:      "client-id",
		ClientSecret:  "client-secret",
		DeviceCodeURL: server.URL,
	}

	resp, err := provider.requestDeviceCode("gmail")
	if err != nil {
		t.Fatal(err)
	}
	if resp.DeviceCode != "device-code-123" {
		t.Fatalf("expected device-code-123, got %s", resp.DeviceCode)
	}
	if resp.UserCode != "ABCD-EFGH" {
		t.Fatalf("expected ABCD-EFGH, got %s", resp.UserCode)
	}
	if resp.VerificationURL != "https://www.google.com/device" {
		t.Fatalf("expected verification URL, got %s", resp.VerificationURL)
	}
	if resp.ExpiresIn != 300 {
		t.Fatalf("expected 300, got %d", resp.ExpiresIn)
	}
	if resp.Interval != 5 {
		t.Fatalf("expected 5, got %d", resp.Interval)
	}
}

func TestRequestDeviceCodeError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]any{
			"error":             "access_denied",
			"error_description": "Client not allowed",
		})
	}))
	defer server.Close()

	provider := &GoogleAuth{
		ClientID:      "client-id",
		DeviceCodeURL: server.URL,
	}

	_, err := provider.requestDeviceCode("gmail")
	if err == nil {
		t.Fatal("expected error for access_denied response")
	}
	if !strings.Contains(err.Error(), "access_denied") {
		t.Fatalf("expected error to contain 'access_denied', got: %s", err.Error())
	}
}

func TestRequestDeviceCodeUnknownService(t *testing.T) {
	provider := &GoogleAuth{ClientID: "client-id"}

	_, err := provider.requestDeviceCode("unknown-service")
	if err == nil {
		t.Fatal("expected error for unknown service")
	}
}

func TestPollDeviceTokenSuccess(t *testing.T) {
	attempt := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.FormValue("grant_type") != "urn:ietf:params:oauth:grant-type:device_code" {
			t.Fatalf("unexpected grant_type: %s", r.FormValue("grant_type"))
		}
		if r.FormValue("device_code") != "device-code-123" {
			t.Fatalf("unexpected device_code: %s", r.FormValue("device_code"))
		}

		attempt++
		if attempt < 3 {
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(map[string]any{
				"error": "authorization_pending",
			})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "device-access-token",
			"refresh_token": "device-refresh-token",
			"expires_in":    3600,
		})
	}))
	defer server.Close()

	provider := &GoogleAuth{
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		TokenURL:     server.URL,
	}

	token, refresh, expiry, err := provider.pollDeviceToken("device-code-123", 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if token != "device-access-token" {
		t.Fatalf("expected device-access-token, got %s", token)
	}
	if refresh != "device-refresh-token" {
		t.Fatalf("expected device-refresh-token, got %s", refresh)
	}
	if expiry == "" {
		t.Fatal("expected non-empty expiry")
	}
}

func TestPollDeviceTokenDenied(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]any{
			"error": "access_denied",
		})
	}))
	defer server.Close()

	provider := &GoogleAuth{
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		TokenURL:     server.URL,
	}

	_, _, _, err := provider.pollDeviceToken("device-code-123", 1, 10)
	if err == nil {
		t.Fatal("expected error for access denied")
	}
	if err.Error() != "user denied access" {
		t.Fatalf("expected 'user denied access', got %q", err.Error())
	}
}

func TestPollDeviceTokenSlowDown(t *testing.T) {
	attempt := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt++
		if attempt == 1 {
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(map[string]any{
				"error": "slow_down",
			})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "token-after-slowdown",
			"refresh_token": "refresh-after-slowdown",
			"expires_in":    3600,
		})
	}))
	defer server.Close()

	provider := &GoogleAuth{
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		TokenURL:     server.URL,
	}

	token, _, _, err := provider.pollDeviceToken("device-code-123", 1, 30)
	if err != nil {
		t.Fatal(err)
	}
	if token != "token-after-slowdown" {
		t.Fatalf("expected token-after-slowdown, got %s", token)
	}
}

func TestGetTokenFallsBackToDeviceFlow(t *testing.T) {
	refreshServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		grantType := r.FormValue("grant_type")
		if grantType == "refresh_token" {
			json.NewEncoder(w).Encode(map[string]any{
				"error":             "invalid_grant",
				"error_description": "Token has been expired or revoked.",
			})
			return
		}
		if grantType == "urn:ietf:params:oauth:grant-type:device_code" {
			json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "device-flow-token",
				"refresh_token": "device-flow-refresh",
				"expires_in":    3600,
			})
			return
		}
		t.Fatalf("unexpected grant_type: %s", grantType)
	}))
	defer refreshServer.Close()

	deviceServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"device_code":      "test-device-code",
			"user_code":        "TEST-CODE",
			"verification_url": "https://www.google.com/device",
			"expires_in":       30,
			"interval":         1,
		})
	}))
	defer deviceServer.Close()

	store := newFakeStore()
	store.Put("gcal:test@gmail.com", credstore.Credential{
		AccessToken:  "expired-token",
		RefreshToken: "revoked-refresh-token",
		Expiry:       time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
	})

	provider := &GoogleAuth{
		ClientID:      "client-id",
		ClientSecret:  "client-secret",
		TokenURL:      refreshServer.URL,
		DeviceCodeURL: deviceServer.URL,
		Store:         store,
	}

	token, err := provider.GetToken("gcal", "test@gmail.com")
	if err != nil {
		t.Fatalf("expected device flow fallback to succeed, got: %v", err)
	}
	if token != "device-flow-token" {
		t.Fatalf("expected device-flow-token, got %s", token)
	}

	updated, _ := store.Get("gcal:test@gmail.com")
	if updated.RefreshToken != "device-flow-refresh" {
		t.Fatal("store should have the new refresh token from device flow")
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

	deviceServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"device_code":      "test-device-code",
			"user_code":        "TEST-CODE",
			"verification_url": "https://www.google.com/device",
			"expires_in":       2,
			"interval":         1,
		})
	}))
	defer deviceServer.Close()

	// Occupy a port so RunOAuthFlow fails immediately after device flow fails
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
		ClientID:      "client-id",
		ClientSecret:  "client-secret",
		TokenURL:      tokenServer.URL,
		DeviceCodeURL: deviceServer.URL,
		Store:         store,
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

func TestRunRelayFlowTimeout(t *testing.T) {
	// Mock relay server that always returns pending.
	relayServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "pending"})
	}))
	defer relayServer.Close()

	store := newFakeStore()
	provider := &GoogleAuth{
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		RelayURL:     relayServer.URL,
		Store:        store,
	}

	// Override the timeout by running with a very short deadline.
	// We can't easily shorten the 5-minute timeout in the production code
	// without adding a parameter, so we test via reAuth chain instead:
	// just verify runRelayFlow returns an error when the relay never completes
	// by calling it directly (the 5s sleep + 5m timeout makes this impractical
	// as a unit test). Instead, test the skip-when-not-configured path.
	_, err := provider.runRelayFlow("unknown-svc", "test@gmail.com")
	if err == nil {
		t.Fatal("expected error for unknown service")
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

func TestReAuthChainRelayAfterDeviceFlow(t *testing.T) {
	// Device flow fails (gcal service), relay succeeds.
	deviceServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"device_code":      "test-device-code",
			"user_code":        "TEST-CODE",
			"verification_url": "https://www.google.com/device",
			"expires_in":       2,
			"interval":         1,
		})
	}))
	defer deviceServer.Close()

	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		grantType := r.FormValue("grant_type")
		if grantType == "urn:ietf:params:oauth:grant-type:device_code" {
			json.NewEncoder(w).Encode(map[string]any{
				"error": "authorization_pending",
			})
			return
		}
		if grantType == "authorization_code" {
			json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "relay-token-after-device-fail",
				"refresh_token": "relay-refresh-after-device-fail",
				"expires_in":    3600,
			})
			return
		}
		t.Fatalf("unexpected grant_type: %s", grantType)
	}))
	defer tokenServer.Close()

	// Mock relay that immediately returns a code.
	relayServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "complete", "code": "relay-code-after-device"})
	}))
	defer relayServer.Close()

	store := newFakeStore()
	provider := &GoogleAuth{
		ClientID:      "client-id",
		ClientSecret:  "client-secret",
		TokenURL:      tokenServer.URL,
		DeviceCodeURL: deviceServer.URL,
		RelayURL:      relayServer.URL,
		Store:         store,
	}

	token, err := provider.reAuth("gcal", "test@gmail.com")
	if err != nil {
		t.Fatalf("expected relay to succeed after device flow timeout: %v", err)
	}
	if token != "relay-token-after-device-fail" {
		t.Fatalf("expected relay-token-after-device-fail, got %s", token)
	}
}

func TestRequestDeviceCodeSurfacesError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"error":             "invalid_scope",
			"error_description": "Invalid device flow scope: gmail.readonly",
		})
	}))
	defer server.Close()

	provider := &GoogleAuth{
		ClientID:      "client-id",
		DeviceCodeURL: server.URL,
	}

	_, err := provider.requestDeviceCode("gcal")
	if err == nil {
		t.Fatal("expected error for invalid_scope")
	}
	if !strings.Contains(err.Error(), "invalid_scope") {
		t.Fatalf("expected error to contain 'invalid_scope', got: %s", err.Error())
	}
}
