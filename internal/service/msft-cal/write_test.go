package msftcal

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

	mux.HandleFunc("/v1.0/me/events", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}

		ct := r.Header.Get("Content-Type")
		if ct != "application/json" {
			t.Errorf("expected Content-Type application/json, got %s", ct)
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("failed to read body: %v", err)
		}

		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("failed to unmarshal body: %v", err)
		}

		subject, ok := payload["subject"].(string)
		if !ok || subject != "Team Meeting" {
			t.Errorf("expected subject 'Team Meeting', got %v", payload["subject"])
		}

		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{
			"id":      "new-event-1",
			"subject": "Team Meeting",
			"start":   map[string]string{"dateTime": "2024-06-01T10:00:00", "timeZone": "UTC"},
			"end":     map[string]string{"dateTime": "2024-06-01T11:00:00", "timeZone": "UTC"},
		})
	})

	ts := httptest.NewServer(mux)
	defer ts.Close()

	svc := newTestService(ts.URL)
	result, err := svc.Execute("create", map[string]string{
		"title": "Team Meeting",
		"start": "2024-06-01T10:00:00",
		"end":   "2024-06-01T11:00:00",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	detail, ok := result.Data.(EventDetail)
	if !ok {
		t.Fatalf("expected EventDetail, got %T", result.Data)
	}

	if detail.ID != "new-event-1" {
		t.Errorf("expected ID new-event-1, got %s", detail.ID)
	}
}

func TestCreateEventMissingTitle(t *testing.T) {
	svc := newTestService("http://unused")

	_, err := svc.Execute("create", map[string]string{
		"start": "2024-06-01T10:00:00",
		"end":   "2024-06-01T11:00:00",
	})
	if err == nil {
		t.Fatal("expected error for missing title")
	}
	if !strings.Contains(err.Error(), "title") {
		t.Errorf("expected error to mention 'title', got: %s", err.Error())
	}
}

func TestUpdateEvent(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/v1.0/me/events/event1", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("expected PATCH, got %s", r.Method)
		}

		ct := r.Header.Get("Content-Type")
		if ct != "application/json" {
			t.Errorf("expected Content-Type application/json, got %s", ct)
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("failed to read body: %v", err)
		}

		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("failed to unmarshal body: %v", err)
		}

		subject, ok := payload["subject"].(string)
		if !ok || subject != "Updated Meeting" {
			t.Errorf("expected subject 'Updated Meeting', got %v", payload["subject"])
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"id":      "event1",
			"subject": "Updated Meeting",
			"start":   map[string]string{"dateTime": "2024-06-01T10:00:00", "timeZone": "UTC"},
			"end":     map[string]string{"dateTime": "2024-06-01T11:00:00", "timeZone": "UTC"},
		})
	})

	ts := httptest.NewServer(mux)
	defer ts.Close()

	svc := newTestService(ts.URL)
	result, err := svc.Execute("update", map[string]string{
		"event-id": "event1",
		"title":    "Updated Meeting",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	detail, ok := result.Data.(EventDetail)
	if !ok {
		t.Fatalf("expected EventDetail, got %T", result.Data)
	}

	if detail.ID != "event1" {
		t.Errorf("expected ID event1, got %s", detail.ID)
	}
	if detail.Subject != "Updated Meeting" {
		t.Errorf("expected subject Updated Meeting, got %s", detail.Subject)
	}
}

func TestUpdateEventNoFields(t *testing.T) {
	svc := newTestService("http://unused")

	_, err := svc.Execute("update", map[string]string{
		"event-id": "event1",
	})
	if err == nil {
		t.Fatal("expected error when no fields to update")
	}
	if !strings.Contains(err.Error(), "no fields") {
		t.Errorf("expected error to mention 'no fields', got: %s", err.Error())
	}
}

func TestDeleteEvent(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/v1.0/me/events/event1", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}

		w.WriteHeader(http.StatusNoContent)
	})

	ts := httptest.NewServer(mux)
	defer ts.Close()

	svc := newTestService(ts.URL)
	result, err := svc.Execute("delete", map[string]string{
		"event-id": "event1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, ok := result.Data.(map[string]string)
	if !ok {
		t.Fatalf("expected map[string]string, got %T", result.Data)
	}

	if data["status"] != "deleted" {
		t.Errorf("expected status 'deleted', got %s", data["status"])
	}
	if data["event-id"] != "event1" {
		t.Errorf("expected event-id 'event1', got %s", data["event-id"])
	}
}

func TestDeleteEventMissingID(t *testing.T) {
	svc := newTestService("http://unused")

	_, err := svc.Execute("delete", map[string]string{})
	if err == nil {
		t.Fatal("expected error for missing event-id")
	}
	if !strings.Contains(err.Error(), "event-id") {
		t.Errorf("expected error to mention 'event-id', got: %s", err.Error())
	}
}
