package gmail

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSendEmail(t *testing.T) {
	var receivedBody string

	mux := http.NewServeMux()
	mux.HandleFunc("/gmail/v1/users/me/messages/send", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}

		auth := r.Header.Get("Authorization")
		if auth != "Bearer fake-token-12345" {
			t.Errorf("expected Bearer token, got %s", auth)
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("reading body: %v", err)
		}
		receivedBody = string(body)

		json.NewEncoder(w).Encode(map[string]any{
			"id":       "sent-msg-1",
			"threadId": "thread-1",
		})
	})

	ts := httptest.NewServer(mux)
	defer ts.Close()

	svc := newTestService(ts.URL)
	result, err := svc.Execute("send", map[string]string{
		"to":      "recipient@example.com",
		"subject": "Test Subject",
		"body":    "Hello, this is a test email.",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the response contains the sent message ID.
	data, ok := result.Data.(map[string]string)
	if !ok {
		t.Fatalf("expected map[string]string, got %T", result.Data)
	}
	if data["id"] != "sent-msg-1" {
		t.Errorf("expected id=sent-msg-1, got %s", data["id"])
	}

	// Verify the request body contained a raw email.
	if receivedBody == "" {
		t.Fatal("expected non-empty request body")
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(receivedBody), &payload); err != nil {
		t.Fatalf("request body is not valid JSON: %v", err)
	}
	rawEncoded, ok := payload["raw"].(string)
	if !ok || rawEncoded == "" {
		t.Fatal("expected 'raw' field in request body")
	}

	// Decode the raw email and verify it contains the expected content.
	decoded, err := base64.URLEncoding.DecodeString(rawEncoded)
	if err != nil {
		// Try without padding.
		decoded, err = base64.RawURLEncoding.DecodeString(rawEncoded)
		if err != nil {
			t.Fatalf("decoding raw email: %v", err)
		}
	}
	rawEmail := string(decoded)
	if !strings.Contains(rawEmail, "To: recipient@example.com") {
		t.Errorf("raw email missing To header, got:\n%s", rawEmail)
	}
	if !strings.Contains(rawEmail, "Subject: Test Subject") {
		t.Errorf("raw email missing Subject header, got:\n%s", rawEmail)
	}
	if !strings.Contains(rawEmail, "Hello, this is a test email.") {
		t.Errorf("raw email missing body, got:\n%s", rawEmail)
	}
}

func TestSendMissingTo(t *testing.T) {
	svc := newTestService("http://unused")

	_, err := svc.Execute("send", map[string]string{
		"subject": "Test",
		"body":    "Test body",
	})
	if err == nil {
		t.Fatal("expected error for missing --to")
	}
	if !strings.Contains(err.Error(), "to") {
		t.Errorf("error should mention 'to', got: %s", err.Error())
	}
}

func TestSendMissingSubject(t *testing.T) {
	svc := newTestService("http://unused")

	_, err := svc.Execute("send", map[string]string{
		"to":   "recipient@example.com",
		"body": "Test body",
	})
	if err == nil {
		t.Fatal("expected error for missing --subject")
	}
	if !strings.Contains(err.Error(), "subject") {
		t.Errorf("error should mention 'subject', got: %s", err.Error())
	}
}

func TestCreateDraft(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/gmail/v1/users/me/drafts", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}

		auth := r.Header.Get("Authorization")
		if auth != "Bearer fake-token-12345" {
			t.Errorf("expected Bearer token, got %s", auth)
		}

		// Verify the request body has message.raw.
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("reading body: %v", err)
		}
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("parsing body: %v", err)
		}
		msg, ok := payload["message"].(map[string]any)
		if !ok {
			t.Fatalf("expected message object in body, got %v", payload)
		}
		if _, ok := msg["raw"].(string); !ok {
			t.Fatal("expected raw field in message")
		}

		json.NewEncoder(w).Encode(map[string]any{
			"id": "draft-1",
			"message": map[string]any{
				"id": "draft-msg-1",
			},
		})
	})

	ts := httptest.NewServer(mux)
	defer ts.Close()

	svc := newTestService(ts.URL)
	result, err := svc.Execute("draft", map[string]string{
		"to":      "recipient@example.com",
		"subject": "Draft Subject",
		"body":    "Draft body content.",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, ok := result.Data.(map[string]string)
	if !ok {
		t.Fatalf("expected map[string]string, got %T", result.Data)
	}
	if data["draft_id"] != "draft-1" {
		t.Errorf("expected draft_id=draft-1, got %s", data["draft_id"])
	}
}

func TestReplyToMessage(t *testing.T) {
	mux := http.NewServeMux()

	// Mock GET original message to fetch threading info.
	mux.HandleFunc("/gmail/v1/users/me/messages/orig-msg-1", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET for original message, got %s", r.Method)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"id":       "orig-msg-1",
			"threadId": "thread-100",
			"payload": map[string]any{
				"headers": []map[string]string{
					{"name": "From", "value": "sender@example.com"},
					{"name": "Subject", "value": "Original Subject"},
					{"name": "Message-ID", "value": "<original-message-id@example.com>"},
				},
			},
		})
	})

	// Mock POST send for the reply.
	mux.HandleFunc("/gmail/v1/users/me/messages/send", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("reading body: %v", err)
		}

		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("parsing body: %v", err)
		}

		// Verify threadId is included.
		if payload["threadId"] != "thread-100" {
			t.Errorf("expected threadId=thread-100, got %v", payload["threadId"])
		}

		// Verify the raw email contains reply headers.
		rawEncoded, ok := payload["raw"].(string)
		if !ok {
			t.Fatal("expected raw field")
		}
		decoded, err := base64.RawURLEncoding.DecodeString(rawEncoded)
		if err != nil {
			t.Fatalf("decoding raw: %v", err)
		}
		rawEmail := string(decoded)

		if !strings.Contains(rawEmail, "In-Reply-To: <original-message-id@example.com>") {
			t.Errorf("expected In-Reply-To header, got:\n%s", rawEmail)
		}
		if !strings.Contains(rawEmail, "References: <original-message-id@example.com>") {
			t.Errorf("expected References header, got:\n%s", rawEmail)
		}
		if !strings.Contains(rawEmail, "Re: Original Subject") {
			t.Errorf("expected Re: prefix on Subject, got:\n%s", rawEmail)
		}
		if !strings.Contains(rawEmail, "To: sender@example.com") {
			t.Errorf("expected reply to original sender, got:\n%s", rawEmail)
		}

		json.NewEncoder(w).Encode(map[string]any{
			"id":       "reply-msg-1",
			"threadId": "thread-100",
		})
	})

	ts := httptest.NewServer(mux)
	defer ts.Close()

	svc := newTestService(ts.URL)
	result, err := svc.Execute("reply", map[string]string{
		"id":   "orig-msg-1",
		"body": "Thanks for your email!",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, ok := result.Data.(map[string]string)
	if !ok {
		t.Fatalf("expected map[string]string, got %T", result.Data)
	}
	if data["id"] != "reply-msg-1" {
		t.Errorf("expected id=reply-msg-1, got %s", data["id"])
	}
}
