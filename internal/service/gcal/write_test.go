package gcal

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCreateEvent(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/calendar/v3/calendars/primary/events", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}

		auth := r.Header.Get("Authorization")
		if auth != "Bearer fake-token-12345" {
			t.Errorf("expected Bearer token, got %s", auth)
		}

		contentType := r.Header.Get("Content-Type")
		if contentType != "application/json" {
			t.Errorf("expected application/json content type, got %s", contentType)
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("reading request body: %v", err)
		}
		defer r.Body.Close()

		if !strings.Contains(string(body), "Team Planning") {
			t.Errorf("expected request body to contain summary 'Team Planning', got %s", string(body))
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"id":      "new-evt-1",
			"summary": "Team Planning",
			"start":   map[string]string{"dateTime": "2024-03-01T10:00:00Z"},
			"end":     map[string]string{"dateTime": "2024-03-01T11:00:00Z"},
			"status":  "confirmed",
		})
	})

	ts := httptest.NewServer(mux)
	defer ts.Close()

	svc := newTestService(ts.URL)
	result, err := svc.Execute("create", map[string]string{
		"summary": "Team Planning",
		"start":   "2024-03-01T10:00:00Z",
		"end":     "2024-03-01T11:00:00Z",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	detail, ok := result.Data.(EventDetail)
	if !ok {
		t.Fatalf("expected EventDetail, got %T", result.Data)
	}

	if detail.ID != "new-evt-1" {
		t.Errorf("expected new-evt-1, got %s", detail.ID)
	}
	if detail.Summary != "Team Planning" {
		t.Errorf("expected Team Planning, got %s", detail.Summary)
	}
}

func TestCreateMissingTitle(t *testing.T) {
	svc := newTestService("http://unused")

	_, err := svc.Execute("create", map[string]string{
		"start": "2024-03-01T10:00:00Z",
		"end":   "2024-03-01T11:00:00Z",
	})
	if err == nil {
		t.Fatal("expected error for missing summary")
	}
	if err.Error() != "missing required parameter: summary" {
		t.Errorf("unexpected error message: %s", err.Error())
	}
}

func TestUpdateEvent(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/calendar/v3/calendars/primary/events/evt-update-1", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("expected PATCH, got %s", r.Method)
		}

		auth := r.Header.Get("Authorization")
		if auth != "Bearer fake-token-12345" {
			t.Errorf("expected Bearer token, got %s", auth)
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("reading request body: %v", err)
		}
		defer r.Body.Close()

		if !strings.Contains(string(body), "Updated Title") {
			t.Errorf("expected request body to contain 'Updated Title', got %s", string(body))
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"id":      "evt-update-1",
			"summary": "Updated Title",
			"start":   map[string]string{"dateTime": "2024-03-01T10:00:00Z"},
			"end":     map[string]string{"dateTime": "2024-03-01T11:00:00Z"},
			"status":  "confirmed",
		})
	})

	ts := httptest.NewServer(mux)
	defer ts.Close()

	svc := newTestService(ts.URL)
	result, err := svc.Execute("update", map[string]string{
		"event-id": "evt-update-1",
		"summary":  "Updated Title",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	detail, ok := result.Data.(EventDetail)
	if !ok {
		t.Fatalf("expected EventDetail, got %T", result.Data)
	}

	if detail.ID != "evt-update-1" {
		t.Errorf("expected evt-update-1, got %s", detail.ID)
	}
	if detail.Summary != "Updated Title" {
		t.Errorf("expected Updated Title, got %s", detail.Summary)
	}
}

func TestDeleteEvent(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/calendar/v3/calendars/primary/events/evt-delete-1", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}

		auth := r.Header.Get("Authorization")
		if auth != "Bearer fake-token-12345" {
			t.Errorf("expected Bearer token, got %s", auth)
		}

		w.WriteHeader(http.StatusNoContent)
	})

	ts := httptest.NewServer(mux)
	defer ts.Close()

	svc := newTestService(ts.URL)
	result, err := svc.Execute("delete", map[string]string{
		"event-id": "evt-delete-1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, ok := result.Data.(map[string]string)
	if !ok {
		t.Fatalf("expected map[string]string, got %T", result.Data)
	}

	if data["status"] != "deleted" {
		t.Errorf("expected status=deleted, got %s", data["status"])
	}
	if data["event-id"] != "evt-delete-1" {
		t.Errorf("expected event-id=evt-delete-1, got %s", data["event-id"])
	}
}

func TestDeleteMissingEventID(t *testing.T) {
	svc := newTestService("http://unused")

	_, err := svc.Execute("delete", map[string]string{})
	if err == nil {
		t.Fatal("expected error for missing event-id")
	}
	if err.Error() != "missing required parameter: event-id" {
		t.Errorf("unexpected error message: %s", err.Error())
	}
}
