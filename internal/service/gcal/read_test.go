package gcal

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeTokenProvider returns a static token for testing.
type fakeTokenProvider struct{}

func (f *fakeTokenProvider) GetToken(service, account string) (string, error) {
	return "fake-token-12345", nil
}

// newTestService creates a CalendarService pointed at the given test server URL.
func newTestService(serverURL string) *CalendarService {
	return &CalendarService{
		tokenProvider: &fakeTokenProvider{},
		account:       "test@example.com",
		baseURL:       serverURL,
		httpClient:    http.DefaultClient,
	}
}

func TestListEvents(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/calendar/v3/calendars/primary/events", func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer fake-token-12345" {
			t.Errorf("expected Bearer token, got %s", auth)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"items": []map[string]any{
				{
					"id":      "evt1",
					"summary": "Team Standup",
					"start":   map[string]string{"dateTime": "2024-01-15T09:00:00Z"},
					"end":     map[string]string{"dateTime": "2024-01-15T09:30:00Z"},
					"status":  "confirmed",
				},
				{
					"id":      "evt2",
					"summary": "Lunch Break",
					"start":   map[string]string{"date": "2024-01-15"},
					"end":     map[string]string{"date": "2024-01-16"},
					"status":  "confirmed",
				},
			},
		})
	})

	ts := httptest.NewServer(mux)
	defer ts.Close()

	svc := newTestService(ts.URL)
	result, err := svc.Execute("list", map[string]string{"date": "2024-01-15"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	events, ok := result.Data.([]EventSummary)
	if !ok {
		t.Fatalf("expected []EventSummary, got %T", result.Data)
	}

	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}

	if events[0].Summary != "Team Standup" {
		t.Errorf("expected Team Standup, got %s", events[0].Summary)
	}
	if events[0].Start != "2024-01-15T09:00:00Z" {
		t.Errorf("expected 2024-01-15T09:00:00Z, got %s", events[0].Start)
	}

	if events[1].Summary != "Lunch Break" {
		t.Errorf("expected Lunch Break, got %s", events[1].Summary)
	}
	// All-day event uses "date" field instead of "dateTime".
	if events[1].Start != "2024-01-15" {
		t.Errorf("expected 2024-01-15, got %s", events[1].Start)
	}
}

func TestGetEvent(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/calendar/v3/calendars/primary/events/evt1", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"id":          "evt1",
			"summary":     "Project Review",
			"description": "Quarterly review meeting",
			"location":    "Conference Room A",
			"start":       map[string]string{"dateTime": "2024-01-15T14:00:00Z"},
			"end":         map[string]string{"dateTime": "2024-01-15T15:00:00Z"},
			"status":      "confirmed",
			"attendees": []map[string]string{
				{"email": "alice@example.com", "responseStatus": "accepted"},
				{"email": "bob@example.com", "responseStatus": "tentative"},
			},
		})
	})

	ts := httptest.NewServer(mux)
	defer ts.Close()

	svc := newTestService(ts.URL)
	result, err := svc.Execute("get", map[string]string{"event-id": "evt1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	event, ok := result.Data.(EventDetail)
	if !ok {
		t.Fatalf("expected EventDetail, got %T", result.Data)
	}

	if event.ID != "evt1" {
		t.Errorf("expected evt1, got %s", event.ID)
	}
	if event.Summary != "Project Review" {
		t.Errorf("expected Project Review, got %s", event.Summary)
	}
	if event.Location != "Conference Room A" {
		t.Errorf("expected Conference Room A, got %s", event.Location)
	}
	if event.Description != "Quarterly review meeting" {
		t.Errorf("expected Quarterly review meeting, got %s", event.Description)
	}
	if len(event.Attendees) != 2 {
		t.Fatalf("expected 2 attendees, got %d", len(event.Attendees))
	}
	if event.Attendees[0].Email != "alice@example.com" {
		t.Errorf("expected alice@example.com, got %s", event.Attendees[0].Email)
	}
	if event.Attendees[0].ResponseStatus != "accepted" {
		t.Errorf("expected accepted, got %s", event.Attendees[0].ResponseStatus)
	}
	if event.Attendees[1].Email != "bob@example.com" {
		t.Errorf("expected bob@example.com, got %s", event.Attendees[1].Email)
	}
}

func TestListDefaultsToToday(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/calendar/v3/calendars/primary/events", func(w http.ResponseWriter, r *http.Request) {
		timeMin := r.URL.Query().Get("timeMin")
		if timeMin == "" {
			t.Error("expected timeMin to be set when no date provided")
		}
		// timeMin should contain T00:00:00 (start of day).
		if !strings.Contains(timeMin, "T00:00:00") {
			t.Errorf("expected timeMin to start at midnight, got %s", timeMin)
		}

		timeMax := r.URL.Query().Get("timeMax")
		if timeMax == "" {
			t.Error("expected timeMax to be set when no date provided")
		}
		if !strings.Contains(timeMax, "T23:59:59") {
			t.Errorf("expected timeMax to end at 23:59:59, got %s", timeMax)
		}

		json.NewEncoder(w).Encode(map[string]any{
			"items": []any{},
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

func TestGetMissingEventID(t *testing.T) {
	svc := newTestService("http://unused")

	_, err := svc.Execute("get", map[string]string{})
	if err == nil {
		t.Fatal("expected error for missing event-id")
	}
	if err.Error() != "missing required parameter: event-id" {
		t.Errorf("unexpected error message: %s", err.Error())
	}
}
