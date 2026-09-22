package auth

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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
			"error": "access_denied",
		})
	}))
	defer server.Close()

	provider := &GoogleAuth{
		ClientID:      "client-id",
		DeviceCodeURL: server.URL,
	}

	resp, err := provider.requestDeviceCode("gmail")
	if err != nil {
		t.Fatal("expected no transport error")
	}
	if resp.DeviceCode != "" {
		t.Fatalf("expected empty device code, got %s", resp.DeviceCode)
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
