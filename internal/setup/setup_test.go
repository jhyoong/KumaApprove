package setup

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jhyoong/KumaApprove/internal/config"
	"github.com/jhyoong/KumaApprove/internal/credstore"
)

func TestReadLine(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("hello\n"))
	got := readLine(reader, "prompt: ")
	if got != "hello" {
		t.Errorf("readLine() = %q, want %q", got, "hello")
	}
}

func TestReadLineTrimsWhitespace(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("  hello  \n"))
	got := readLine(reader, "prompt: ")
	if got != "hello" {
		t.Errorf("readLine() = %q, want %q", got, "hello")
	}
}

func TestValidateTelegramBotValid(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
			"result": map[string]any{
				"id":       123,
				"is_bot":   true,
				"username": "test_bot",
			},
		})
	}))
	defer server.Close()

	err := validateTelegramBot("fake-token", server.URL)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestValidateTelegramBotInvalid(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"ok":          false,
			"description": "Unauthorized",
		})
	}))
	defer server.Close()

	err := validateTelegramBot("bad-token", server.URL)
	if err == nil {
		t.Fatal("expected error for invalid bot token")
	}
}

func TestValidateTelegramNotConfigured(t *testing.T) {
	cfg := config.Config{}
	status := validateTelegram(cfg, "")
	if status.configured {
		t.Fatal("expected not configured")
	}
}

func TestValidateTelegramValid(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": map[string]any{"id": 1}})
	}))
	defer server.Close()

	cfg := config.Config{
		Telegram: config.TelegramConfig{BotToken: "test-token", ChatID: "12345"},
	}
	status := validateTelegram(cfg, server.URL)
	if !status.configured || !status.valid {
		t.Fatalf("expected configured+valid, got configured=%v valid=%v reason=%s", status.configured, status.valid, status.reason)
	}
	if status.detail != "chat ID: 12345" {
		t.Fatalf("expected detail 'chat ID: 12345', got %q", status.detail)
	}
}

func TestValidateTelegramInvalid(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"ok": false, "description": "Unauthorized"})
	}))
	defer server.Close()

	cfg := config.Config{
		Telegram: config.TelegramConfig{BotToken: "bad-token", ChatID: "12345"},
	}
	status := validateTelegram(cfg, server.URL)
	if !status.configured {
		t.Fatal("expected configured")
	}
	if status.valid {
		t.Fatal("expected invalid")
	}
	if status.reason == "" {
		t.Fatal("expected a reason")
	}
}

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

func TestValidateGoogleNotConfigured(t *testing.T) {
	cfg := config.Config{}
	store := newFakeStore()
	status := validateGoogle(cfg, store, "")
	if status.configured {
		t.Fatal("expected not configured")
	}
}

func TestValidateGoogleValid(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"access_token": "new-token",
			"expires_in":   3600,
		})
	}))
	defer server.Close()

	store := newFakeStore()
	store.Put("gmail:user@gmail.com", credstore.Credential{
		AccessToken:  "old-token",
		RefreshToken: "refresh-123",
		Expiry:       time.Now().Add(1 * time.Hour).Format(time.RFC3339),
	})
	store.Put("gcal:user@gmail.com", credstore.Credential{
		AccessToken:  "old-token",
		RefreshToken: "refresh-456",
		Expiry:       time.Now().Add(1 * time.Hour).Format(time.RFC3339),
	})

	cfg := config.Config{
		GoogleOAuth: config.GoogleOAuthConfig{ClientID: "cid", ClientSecret: "csec"},
		Accounts:    map[string][]string{"gmail": {"user@gmail.com"}, "gcal": {"user@gmail.com"}},
	}
	status := validateGoogle(cfg, store, server.URL)
	if !status.configured || !status.valid {
		t.Fatalf("expected configured+valid, got configured=%v valid=%v reason=%s", status.configured, status.valid, status.reason)
	}
}

func TestValidateGoogleExpiredToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"error":             "invalid_grant",
			"error_description": "Token has been revoked",
		})
	}))
	defer server.Close()

	store := newFakeStore()
	store.Put("gmail:user@gmail.com", credstore.Credential{
		AccessToken:  "old-token",
		RefreshToken: "bad-refresh",
		Expiry:       time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
	})

	cfg := config.Config{
		GoogleOAuth: config.GoogleOAuthConfig{ClientID: "cid", ClientSecret: "csec"},
		Accounts:    map[string][]string{"gmail": {"user@gmail.com"}, "gcal": {"user@gmail.com"}},
	}
	status := validateGoogle(cfg, store, server.URL)
	if !status.configured {
		t.Fatal("expected configured")
	}
	if status.valid {
		t.Fatal("expected invalid")
	}
}

func TestValidateGoogleNoCredentials(t *testing.T) {
	store := newFakeStore()
	cfg := config.Config{
		GoogleOAuth: config.GoogleOAuthConfig{ClientID: "cid", ClientSecret: "csec"},
		Accounts:    map[string][]string{"gmail": {"user@gmail.com"}},
	}
	status := validateGoogle(cfg, store, "")
	if !status.configured {
		t.Fatal("expected configured")
	}
	if status.valid {
		t.Fatal("expected invalid when no credentials in store")
	}
}

func TestValidateMicrosoftNotConfigured(t *testing.T) {
	cfg := config.Config{}
	store := newFakeStore()
	status := validateMicrosoft(cfg, store, "")
	if status.configured {
		t.Fatal("expected not configured")
	}
}

func TestValidateMicrosoftValid(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "new-token",
			"refresh_token": "new-refresh",
			"expires_in":    3600,
		})
	}))
	defer server.Close()

	store := newFakeStore()
	store.Put("outlook:user@outlook.com", credstore.Credential{
		AccessToken:  "old-token",
		RefreshToken: "refresh-123",
		Expiry:       time.Now().Add(1 * time.Hour).Format(time.RFC3339),
	})
	store.Put("msft-cal:user@outlook.com", credstore.Credential{
		AccessToken:  "old-token",
		RefreshToken: "refresh-456",
		Expiry:       time.Now().Add(1 * time.Hour).Format(time.RFC3339),
	})

	cfg := config.Config{
		MicrosoftOAuth: config.MicrosoftOAuthConfig{ClientID: "cid", TenantID: "consumers"},
		Accounts:       map[string][]string{"outlook": {"user@outlook.com"}, "msft-cal": {"user@outlook.com"}},
	}
	status := validateMicrosoft(cfg, store, server.URL)
	if !status.configured || !status.valid {
		t.Fatalf("expected configured+valid, got configured=%v valid=%v reason=%s", status.configured, status.valid, status.reason)
	}
}

func TestValidateMicrosoftExpiredToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"error":             "invalid_grant",
			"error_description": "token revoked",
		})
	}))
	defer server.Close()

	store := newFakeStore()
	store.Put("outlook:user@outlook.com", credstore.Credential{
		AccessToken:  "old-token",
		RefreshToken: "bad-refresh",
		Expiry:       time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
	})

	cfg := config.Config{
		MicrosoftOAuth: config.MicrosoftOAuthConfig{ClientID: "cid", TenantID: "consumers"},
		Accounts:       map[string][]string{"outlook": {"user@outlook.com"}},
	}
	status := validateMicrosoft(cfg, store, server.URL)
	if !status.configured {
		t.Fatal("expected configured")
	}
	if status.valid {
		t.Fatal("expected invalid")
	}
}
