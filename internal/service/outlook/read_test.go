package outlook

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// fakeTokenProvider returns a static token for testing.
type fakeTokenProvider struct{}

func (f *fakeTokenProvider) GetToken(service, account string) (string, error) {
	return "fake-ms-token-12345", nil
}

// newTestService creates an OutlookService pointed at the given test server URL.
func newTestService(serverURL string) *OutlookService {
	return &OutlookService{
		tokenProvider: &fakeTokenProvider{},
		account:       "test@example.com",
		baseURL:       serverURL,
		httpClient:    http.DefaultClient,
	}
}

func TestListMessages(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/v1.0/me/messages", func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer fake-ms-token-12345" {
			t.Errorf("expected Bearer token, got %s", auth)
		}
		orderby := r.URL.Query().Get("$orderby")
		if orderby != "receivedDateTime desc" {
			t.Errorf("expected $orderby=receivedDateTime desc, got %s", orderby)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"value": []map[string]any{
				{
					"id":               "msg1",
					"subject":          "First Message",
					"receivedDateTime": "2024-01-01T12:00:00Z",
					"bodyPreview":      "Hello from msg1",
					"isRead":           true,
					"from": map[string]any{
						"emailAddress": map[string]string{
							"address": "alice@example.com",
						},
					},
				},
				{
					"id":               "msg2",
					"subject":          "Second Message",
					"receivedDateTime": "2024-01-02T12:00:00Z",
					"bodyPreview":      "Hello from msg2",
					"isRead":           false,
					"from": map[string]any{
						"emailAddress": map[string]string{
							"address": "bob@example.com",
						},
					},
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
	if msgs[0].From != "alice@example.com" {
		t.Errorf("expected alice@example.com, got %s", msgs[0].From)
	}
	if msgs[0].Subject != "First Message" {
		t.Errorf("expected First Message, got %s", msgs[0].Subject)
	}
	if msgs[0].IsRead != true {
		t.Errorf("expected IsRead=true, got %v", msgs[0].IsRead)
	}

	if msgs[1].ID != "msg2" {
		t.Errorf("expected msg2, got %s", msgs[1].ID)
	}
	if msgs[1].IsRead != false {
		t.Errorf("expected IsRead=false, got %v", msgs[1].IsRead)
	}
}

func TestGetMessage(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/v1.0/me/messages/msg1", func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer fake-ms-token-12345" {
			t.Errorf("expected Bearer token, got %s", auth)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"id":               "msg1",
			"subject":          "Test Subject",
			"receivedDateTime": "2024-01-01T12:00:00Z",
			"bodyPreview":      "Preview text",
			"isRead":           true,
			"from": map[string]any{
				"emailAddress": map[string]string{
					"address": "alice@example.com",
				},
			},
			"toRecipients": []map[string]any{
				{
					"emailAddress": map[string]string{
						"address": "bob@example.com",
					},
				},
			},
			"body": map[string]string{
				"content": "Hello World",
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

	mux.HandleFunc("/v1.0/me/messages", func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer fake-ms-token-12345" {
			t.Errorf("expected Bearer token, got %s", auth)
		}
		search := r.URL.Query().Get("$search")
		if search != `"from:boss"` {
			t.Errorf("expected $search=\"from:boss\", got %s", search)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"value": []map[string]any{
				{
					"id":               "msg3",
					"subject":          "Urgent",
					"receivedDateTime": "2024-01-03T12:00:00Z",
					"bodyPreview":      "Important message",
					"isRead":           false,
					"from": map[string]any{
						"emailAddress": map[string]string{
							"address": "boss@example.com",
						},
					},
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
