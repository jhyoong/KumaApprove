package setup

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
