package msftcal

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
	return "fake-ms-token-12345", nil
}

// newTestService creates a MsftCalService pointed at the given test server URL.
func newTestService(serverURL string) *MsftCalService {
	return &MsftCalService{
		tokenProvider: &fakeTokenProvider{},
		account:       "test@example.com",
		baseURL:       serverURL,
		httpClient:    http.DefaultClient,
	}
}

func TestListEvents(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/v1.0/me/events", func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer fake-ms-token-12345" {
			t.Errorf("expected Bearer token, got %s", auth)
		}

		orderby := r.URL.Query().Get("$orderby")
		if orderby != "start/dateTime" {
			t.Errorf("expected $orderby=start/dateTime, got %s", orderby)
		}

		top := r.URL.Query().Get("$top")
		if top != "50" {
			t.Errorf("expected $top=50, got %s", top)
		}

		json.NewEncoder(w).Encode(map[string]any{
			"value": []map[string]any{
				{
					"id":      "evt1",
					"subject": "Team Standup",
					"start":   map[string]string{"dateTime": "2024-01-15T09:00:00", "timeZone": "UTC"},
					"end":     map[string]string{"dateTime": "2024-01-15T09:30:00", "timeZone": "UTC"},
				},
				{
					"id":      "evt2",
					"subject": "Lunch Meeting",
					"start":   map[string]string{"dateTime": "2024-01-15T12:00:00", "timeZone": "UTC"},
					"end":     map[string]string{"dateTime": "2024-01-15T13:00:00", "timeZone": "UTC"},
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

	events, ok := result.Data.([]EventSummary)
	if !ok {
		t.Fatalf("expected []EventSummary, got %T", result.Data)
	}

	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}

	if events[0].Subject != "Team Standup" {
		t.Errorf("expected Team Standup, got %s", events[0].Subject)
	}
	if events[0].Start != "2024-01-15T09:00:00" {
		t.Errorf("expected 2024-01-15T09:00:00, got %s", events[0].Start)
	}

	if events[1].Subject != "Lunch Meeting" {
		t.Errorf("expected Lunch Meeting, got %s", events[1].Subject)
	}
}

func TestListEventsWithDate(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/v1.0/me/events", func(w http.ResponseWriter, r *http.Request) {
		filter := r.URL.Query().Get("$filter")
		if filter == "" {
			t.Error("expected $filter to be set when date is provided")
		}
		if !strings.Contains(filter, "2024-01-15T00:00:00") {
			t.Errorf("expected filter to contain date start, got %s", filter)
		}
		if !strings.Contains(filter, "2024-01-16T00:00:00") {
			t.Errorf("expected filter to contain next day, got %s", filter)
		}

		json.NewEncoder(w).Encode(map[string]any{
			"value": []map[string]any{
				{
					"id":      "evt1",
					"subject": "Morning Meeting",
					"start":   map[string]string{"dateTime": "2024-01-15T09:00:00", "timeZone": "UTC"},
					"end":     map[string]string{"dateTime": "2024-01-15T10:00:00", "timeZone": "UTC"},
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

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	if events[0].Subject != "Morning Meeting" {
		t.Errorf("expected Morning Meeting, got %s", events[0].Subject)
	}
}

func TestGetEvent(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/v1.0/me/events/evt1", func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer fake-ms-token-12345" {
			t.Errorf("expected Bearer token, got %s", auth)
		}

		json.NewEncoder(w).Encode(map[string]any{
			"id":          "evt1",
			"subject":     "Project Review",
			"bodyPreview": "Quarterly review meeting",
			"location":    map[string]string{"displayName": "Conference Room A"},
			"start":       map[string]string{"dateTime": "2024-01-15T14:00:00", "timeZone": "UTC"},
			"end":         map[string]string{"dateTime": "2024-01-15T15:00:00", "timeZone": "UTC"},
			"organizer": map[string]any{
				"emailAddress": map[string]string{"address": "organizer@example.com"},
			},
			"attendees": []map[string]any{
				{
					"emailAddress": map[string]string{"address": "alice@example.com"},
					"status":       map[string]string{"response": "accepted"},
				},
				{
					"emailAddress": map[string]string{"address": "bob@example.com"},
					"status":       map[string]string{"response": "tentative"},
				},
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
	if event.Subject != "Project Review" {
		t.Errorf("expected Project Review, got %s", event.Subject)
	}
	if event.Location != "Conference Room A" {
		t.Errorf("expected Conference Room A, got %s", event.Location)
	}
	if event.Description != "Quarterly review meeting" {
		t.Errorf("expected Quarterly review meeting, got %s", event.Description)
	}
	if event.Organizer != "organizer@example.com" {
		t.Errorf("expected organizer@example.com, got %s", event.Organizer)
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

func TestGetEventMissingID(t *testing.T) {
	svc := newTestService("http://unused")

	_, err := svc.Execute("get", map[string]string{})
	if err == nil {
		t.Fatal("expected error for missing event-id")
	}
	if err.Error() != "missing required parameter: event-id" {
		t.Errorf("unexpected error message: %s", err.Error())
	}
}

func TestUnknownAction(t *testing.T) {
	svc := newTestService("http://unused")
	_, err := svc.Execute("nonexistent", nil)
	if err == nil {
		t.Fatal("expected error for unknown action")
	}
	if !strings.Contains(err.Error(), "unknown action") {
		t.Fatalf("expected 'unknown action' error, got: %s", err)
	}
}
