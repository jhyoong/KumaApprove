package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jhyoong/KumaApprove/internal/credstore"
)

func TestMicrosoftGetTokenStillValid(t *testing.T) {
	store := newFakeStore()
	store.Put("outlook:test@outlook.com", credstore.Credential{
		AccessToken:  "valid-ms-token",
		RefreshToken: "refresh-123",
		Expiry:       time.Now().Add(1 * time.Hour).Format(time.RFC3339),
	})

	provider := &MicrosoftAuth{
		Store: store,
	}

	token, err := provider.GetToken("outlook", "test@outlook.com")
	if err != nil {
		t.Fatal(err)
	}
	if token != "valid-ms-token" {
		t.Fatalf("expected valid-ms-token, got %s", token)
	}
}

func TestMicrosoftRefreshToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parsing form: %v", err)
		}
		if r.FormValue("grant_type") != "refresh_token" {
			t.Errorf("expected grant_type=refresh_token, got %s", r.FormValue("grant_type"))
		}
		if r.FormValue("refresh_token") != "old-refresh-token" {
			t.Errorf("expected old refresh token, got %s", r.FormValue("refresh_token"))
		}
		json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "new-ms-access-token",
			"refresh_token": "new-ms-refresh-token",
			"expires_in":    3600,
			"token_type":    "Bearer",
		})
	}))
	defer server.Close()

	store := newFakeStore()
	store.Put("outlook:test@outlook.com", credstore.Credential{
		AccessToken:  "expired-token",
		RefreshToken: "old-refresh-token",
		Expiry:       time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
	})

	provider := &MicrosoftAuth{
		ClientID:     "ms-client-id",
		ClientSecret: "ms-client-secret",
		TenantID:     "consumers",
		TokenURL:     server.URL,
		Store:        store,
	}

	token, err := provider.GetToken("outlook", "test@outlook.com")
	if err != nil {
		t.Fatal(err)
	}
	if token != "new-ms-access-token" {
		t.Fatalf("expected new-ms-access-token, got %s", token)
	}

	// Verify refresh token was rotated in the store.
	updated, _ := store.Get("outlook:test@outlook.com")
	if updated.AccessToken != "new-ms-access-token" {
		t.Fatal("access token should have been updated in store")
	}
	if updated.RefreshToken != "new-ms-refresh-token" {
		t.Fatal("refresh token should have been rotated in store")
	}
}

func TestMicrosoftGetTokenMissing(t *testing.T) {
	store := newFakeStore()
	provider := &MicrosoftAuth{Store: store}

	_, err := provider.GetToken("outlook", "nobody@outlook.com")
	if err == nil {
		t.Fatal("expected error for missing credentials")
	}
}

func TestMicrosoftRefreshFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"error":             "invalid_grant",
			"error_description": "token has been revoked",
		})
	}))
	defer server.Close()

	store := newFakeStore()
	store.Put("outlook:test@outlook.com", credstore.Credential{
		AccessToken:  "expired-token",
		RefreshToken: "revoked-refresh-token",
		Expiry:       time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
	})

	provider := &MicrosoftAuth{
		ClientID:     "ms-client-id",
		ClientSecret: "ms-client-secret",
		TenantID:     "consumers",
		TokenURL:     server.URL,
		Store:        store,
	}

	_, err := provider.GetToken("outlook", "test@outlook.com")
	if err == nil {
		t.Fatal("expected AuthExpiredError")
	}
	if _, ok := err.(*AuthExpiredError); !ok {
		t.Fatalf("expected *AuthExpiredError, got %T: %v", err, err)
	}
}
