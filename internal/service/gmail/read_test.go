package gmail

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// fakeTokenProvider returns a static token for testing.
type fakeTokenProvider struct{}

func (f *fakeTokenProvider) GetToken(service, account string) (string, error) {
	return "fake-token-12345", nil
}

// newTestService creates a GmailService pointed at the given test server URL.
func newTestService(serverURL string) *GmailService {
	return &GmailService{
		tokenProvider: &fakeTokenProvider{},
		account:       "test@example.com",
		baseURL:       serverURL,
		httpClient:    http.DefaultClient,
	}
}

func TestListMessages(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/gmail/v1/users/me/messages", func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer fake-token-12345" {
			t.Errorf("expected Bearer token, got %s", auth)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"messages": []map[string]string{
				{"id": "msg1", "threadId": "thread1"},
				{"id": "msg2", "threadId": "thread2"},
			},
		})
	})

	mux.HandleFunc("/gmail/v1/users/me/messages/msg1", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"id":      "msg1",
			"snippet": "Hello from msg1",
			"payload": map[string]any{
				"headers": []map[string]string{
					{"name": "From", "value": "alice@example.com"},
					{"name": "Subject", "value": "First Message"},
					{"name": "Date", "value": "Mon, 1 Jan 2024 12:00:00 +0000"},
				},
			},
		})
	})

	mux.HandleFunc("/gmail/v1/users/me/messages/msg2", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"id":      "msg2",
			"snippet": "Hello from msg2",
			"payload": map[string]any{
				"headers": []map[string]string{
					{"name": "From", "value": "bob@example.com"},
					{"name": "Subject", "value": "Second Message"},
					{"name": "Date", "value": "Tue, 2 Jan 2024 12:00:00 +0000"},
				},
			},
		})
	})

	ts := httptest.NewServer(mux)
	defer ts.Close()

	svc := newTestService(ts.URL)
	result, err := svc.Execute("list", map[string]string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	msgs, ok := result.Data.([]MessageSummary)
	if !ok {
		t.Fatalf("expected []MessageSummary, got %T", result.Data)
	}

	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}

	if msgs[0].ID != "msg1" {
		t.Errorf("expected msg1, got %s", msgs[0].ID)
	}
	if msgs[1].ID != "msg2" {
		t.Errorf("expected msg2, got %s", msgs[1].ID)
	}
}

func TestGetMessage(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/gmail/v1/users/me/messages/msg1", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"id":      "msg1",
			"snippet": "Full message content",
			"payload": map[string]any{
				"headers": []map[string]string{
					{"name": "From", "value": "alice@example.com"},
					{"name": "To", "value": "bob@example.com"},
					{"name": "Subject", "value": "Test Subject"},
					{"name": "Date", "value": "Mon, 1 Jan 2024 12:00:00 +0000"},
				},
				"body": map[string]string{
					"data": "SGVsbG8gV29ybGQ",
				},
			},
		})
	})

	ts := httptest.NewServer(mux)
	defer ts.Close()

	svc := newTestService(ts.URL)
	result, err := svc.Execute("get", map[string]string{"id": "msg1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	msg, ok := result.Data.(MessageDetail)
	if !ok {
		t.Fatalf("expected MessageDetail, got %T", result.Data)
	}

	if msg.ID != "msg1" {
		t.Errorf("expected msg1, got %s", msg.ID)
	}
	if msg.Subject != "Test Subject" {
		t.Errorf("expected Test Subject, got %s", msg.Subject)
	}
	if msg.From != "alice@example.com" {
		t.Errorf("expected alice@example.com, got %s", msg.From)
	}
	if msg.To != "bob@example.com" {
		t.Errorf("expected bob@example.com, got %s", msg.To)
	}
	if msg.Body != "Hello World" {
		t.Errorf("expected Hello World, got %s", msg.Body)
	}
}

func TestSearchMessages(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/gmail/v1/users/me/messages", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		if q != "from:boss" {
			t.Errorf("expected q=from:boss, got %s", q)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"messages": []map[string]string{
				{"id": "msg3", "threadId": "thread3"},
			},
		})
	})

	mux.HandleFunc("/gmail/v1/users/me/messages/msg3", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"id":      "msg3",
			"snippet": "Important message",
			"payload": map[string]any{
				"headers": []map[string]string{
					{"name": "From", "value": "boss@example.com"},
					{"name": "Subject", "value": "Urgent"},
					{"name": "Date", "value": "Wed, 3 Jan 2024 12:00:00 +0000"},
				},
			},
		})
	})

	ts := httptest.NewServer(mux)
	defer ts.Close()

	svc := newTestService(ts.URL)
	result, err := svc.Execute("search", map[string]string{"query": "from:boss"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	msgs, ok := result.Data.([]MessageSummary)
	if !ok {
		t.Fatalf("expected []MessageSummary, got %T", result.Data)
	}

	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].From != "boss@example.com" {
		t.Errorf("expected boss@example.com, got %s", msgs[0].From)
	}
	if msgs[0].Subject != "Urgent" {
		t.Errorf("expected Urgent, got %s", msgs[0].Subject)
	}
}

func TestListMissingLimit(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/gmail/v1/users/me/messages", func(w http.ResponseWriter, r *http.Request) {
		maxResults := r.URL.Query().Get("maxResults")
		if maxResults != "20" {
			t.Errorf("expected default maxResults=20, got %s", maxResults)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"messages": []any{},
		})
	})

	ts := httptest.NewServer(mux)
	defer ts.Close()

	svc := newTestService(ts.URL)
	_, err := svc.Execute("list", map[string]string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGetMissingID(t *testing.T) {
	svc := newTestService("http://unused")

	_, err := svc.Execute("get", map[string]string{})
	if err == nil {
		t.Fatal("expected error for missing id")
	}
	if err.Error() != "missing required parameter: id" {
		t.Errorf("unexpected error message: %s", err.Error())
	}
}

func TestUnknownAction(t *testing.T) {
	svc := newTestService("http://unused")

	_, err := svc.Execute("nonexistent", map[string]string{})
	if err == nil {
		t.Fatal("expected error for unknown action")
	}
}
