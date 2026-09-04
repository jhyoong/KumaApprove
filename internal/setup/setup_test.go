package setup

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jhyoong/KumaApprove/internal/config"
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
