package outlook

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSendEmail(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/v1.0/me/sendMail", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		auth := r.Header.Get("Authorization")
		if auth != "Bearer fake-ms-token-12345" {
			t.Errorf("expected Bearer token, got %s", auth)
		}
		ct := r.Header.Get("Content-Type")
		if ct != "application/json" {
			t.Errorf("expected Content-Type application/json, got %s", ct)
		}

		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("failed to parse request body: %v", err)
		}

		msg, ok := payload["message"].(map[string]any)
		if !ok {
			t.Fatal("expected 'message' key in payload")
		}
		if msg["subject"] != "Hello" {
			t.Errorf("expected subject Hello, got %v", msg["subject"])
		}

		bodyObj, ok := msg["body"].(map[string]any)
		if !ok {
			t.Fatal("expected 'body' key in message")
		}
		if bodyObj["contentType"] != "Text" {
			t.Errorf("expected contentType Text, got %v", bodyObj["contentType"])
		}
		if bodyObj["content"] != "Hi there" {
			t.Errorf("expected content 'Hi there', got %v", bodyObj["content"])
		}

		recipients, ok := msg["toRecipients"].([]any)
		if !ok || len(recipients) == 0 {
			t.Fatal("expected toRecipients array")
		}
		recip := recipients[0].(map[string]any)
		emailAddr := recip["emailAddress"].(map[string]any)
		if emailAddr["address"] != "bob@example.com" {
			t.Errorf("expected bob@example.com, got %v", emailAddr["address"])
		}

		w.WriteHeader(http.StatusAccepted)
	})

	ts := httptest.NewServer(mux)
	defer ts.Close()

	svc := newTestService(ts.URL)
	result, err := svc.Execute("send", map[string]string{
		"to":      "bob@example.com",
		"subject": "Hello",
		"body":    "Hi there",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, ok := result.Data.(map[string]string)
	if !ok {
		t.Fatalf("expected map[string]string, got %T", result.Data)
	}
	if data["status"] != "sent" {
		t.Errorf("expected status=sent, got %s", data["status"])
	}
}

func TestSendMissingTo(t *testing.T) {
	svc := newTestService("http://unused")

	_, err := svc.Execute("send", map[string]string{
		"subject": "Hello",
		"body":    "Hi there",
	})
	if err == nil {
		t.Fatal("expected error for missing to")
	}
	if err.Error() != "missing required parameter: to" {
		t.Errorf("unexpected error message: %s", err.Error())
	}
}

func TestSendMissingSubject(t *testing.T) {
	svc := newTestService("http://unused")

	_, err := svc.Execute("send", map[string]string{
		"to":   "bob@example.com",
		"body": "Hi there",
	})
	if err == nil {
		t.Fatal("expected error for missing subject")
	}
	if err.Error() != "missing required parameter: subject" {
		t.Errorf("unexpected error message: %s", err.Error())
	}
}

func TestReplyToMessage(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/v1.0/me/messages/orig-msg-1/reply", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		auth := r.Header.Get("Authorization")
		if auth != "Bearer fake-ms-token-12345" {
			t.Errorf("expected Bearer token, got %s", auth)
		}
		ct := r.Header.Get("Content-Type")
		if ct != "application/json" {
			t.Errorf("expected Content-Type application/json, got %s", ct)
		}

		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("failed to parse request body: %v", err)
		}
		if payload["comment"] != "reply text" {
			t.Errorf("expected comment 'reply text', got %v", payload["comment"])
		}

		w.WriteHeader(http.StatusAccepted)
	})

	ts := httptest.NewServer(mux)
	defer ts.Close()

	svc := newTestService(ts.URL)
	result, err := svc.Execute("reply", map[string]string{
		"id":   "orig-msg-1",
		"body": "reply text",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, ok := result.Data.(map[string]string)
	if !ok {
		t.Fatalf("expected map[string]string, got %T", result.Data)
	}
	if data["status"] != "replied" {
		t.Errorf("expected status=replied, got %s", data["status"])
	}
}

func TestCreateDraft(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/v1.0/me/messages", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		auth := r.Header.Get("Authorization")
		if auth != "Bearer fake-ms-token-12345" {
			t.Errorf("expected Bearer token, got %s", auth)
		}
		ct := r.Header.Get("Content-Type")
		if ct != "application/json" {
			t.Errorf("expected Content-Type application/json, got %s", ct)
		}

		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("failed to parse request body: %v", err)
		}

		if payload["subject"] != "Draft Subject" {
			t.Errorf("expected subject 'Draft Subject', got %v", payload["subject"])
		}

		bodyObj, ok := payload["body"].(map[string]any)
		if !ok {
			t.Fatal("expected 'body' key in payload")
		}
		if bodyObj["contentType"] != "Text" {
			t.Errorf("expected contentType Text, got %v", bodyObj["contentType"])
		}
		if bodyObj["content"] != "Draft body" {
			t.Errorf("expected content 'Draft body', got %v", bodyObj["content"])
		}

		recipients, ok := payload["toRecipients"].([]any)
		if !ok || len(recipients) == 0 {
			t.Fatal("expected toRecipients array")
		}
		recip := recipients[0].(map[string]any)
		emailAddr := recip["emailAddress"].(map[string]any)
		if emailAddr["address"] != "alice@example.com" {
			t.Errorf("expected alice@example.com, got %v", emailAddr["address"])
		}

		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{"id": "draft-1"})
	})

	ts := httptest.NewServer(mux)
	defer ts.Close()

	svc := newTestService(ts.URL)
	result, err := svc.Execute("draft", map[string]string{
		"to":      "alice@example.com",
		"subject": "Draft Subject",
		"body":    "Draft body",
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
