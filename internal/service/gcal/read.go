package gcal

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/jhyoong/KumaApprove/internal/service"
)

// EventSummary holds a brief overview of a calendar event.
type EventSummary struct {
	ID      string `json:"id"`
	Summary string `json:"summary"`
	Start   string `json:"start"`
	End     string `json:"end"`
	Status  string `json:"status"`
}

// EventDetail holds the full content of a calendar event.
type EventDetail struct {
	ID          string     `json:"id"`
	Summary     string     `json:"summary"`
	Description string     `json:"description"`
	Location    string     `json:"location"`
	Start       string     `json:"start"`
	End         string     `json:"end"`
	Status      string     `json:"status"`
	Attendees   []Attendee `json:"attendees"`
}

// Attendee represents a single event attendee.
type Attendee struct {
	Email          string `json:"email"`
	ResponseStatus string `json:"responseStatus"`
}

// calendarEventsResponse represents the Google Calendar API list events response.
type calendarEventsResponse struct {
	Items []calendarEvent `json:"items"`
}

// calendarEvent represents a single Google Calendar API event.
type calendarEvent struct {
	ID          string `json:"id"`
	Summary     string `json:"summary"`
	Description string `json:"description"`
	Location    string `json:"location"`
	Status      string `json:"status"`
	Start       struct {
		DateTime string `json:"dateTime"`
		Date     string `json:"date"`
	} `json:"start"`
	End struct {
		DateTime string `json:"dateTime"`
		Date     string `json:"date"`
	} `json:"end"`
	Attendees []struct {
		Email          string `json:"email"`
		ResponseStatus string `json:"responseStatus"`
	} `json:"attendees"`
}

// resolveTime extracts the start or end time from the Calendar API response.
// The API returns either dateTime (timed events) or date (all-day events).
func resolveTime(dt struct {
	DateTime string `json:"dateTime"`
	Date     string `json:"date"`
}) string {
	if dt.DateTime != "" {
		return dt.DateTime
	}
	return dt.Date
}

// list fetches events for a given date. Defaults to today if no date is provided.
func (c *CalendarService) list(args map[string]string) (*service.Result, error) {
	dateStr := args["date"]

	var timeMin, timeMax string
	if dateStr != "" {
		d, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			return nil, fmt.Errorf("invalid date format (expected YYYY-MM-DD): %w", err)
		}
		timeMin = d.Format("2006-01-02") + "T00:00:00Z"
		timeMax = d.AddDate(0, 0, 1).Format("2006-01-02") + "T00:00:00Z"
	} else {
		now := time.Now()
		timeMin = now.Format("2006-01-02") + "T00:00:00Z"
		timeMax = now.AddDate(0, 0, 1).Format("2006-01-02") + "T00:00:00Z"
	}

	endpoint := fmt.Sprintf("%s/calendar/v3/calendars/primary/events", c.baseURL)
	params := url.Values{}
	params.Set("timeMin", timeMin)
	params.Set("timeMax", timeMax)
	params.Set("singleEvents", "true")
	params.Set("orderBy", "startTime")
	fullURL := endpoint + "?" + params.Encode()

	req, err := c.authedRequest(http.MethodGet, fullURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("listing events: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list events: status %d", resp.StatusCode)
	}

	var eventsResp calendarEventsResponse
	if err := json.NewDecoder(resp.Body).Decode(&eventsResp); err != nil {
		return nil, fmt.Errorf("decoding events response: %w", err)
	}

	summaries := make([]EventSummary, len(eventsResp.Items))
	for i, item := range eventsResp.Items {
		summaries[i] = EventSummary{
			ID:      item.ID,
			Summary: item.Summary,
			Start:   resolveTime(item.Start),
			End:     resolveTime(item.End),
			Status:  item.Status,
		}
	}

	return &service.Result{Data: summaries}, nil
}

// getEvent fetches a single event by ID. Requires the "event-id" parameter.
func (c *CalendarService) getEvent(args map[string]string) (*service.Result, error) {
	eventID, ok := args["event-id"]
	if !ok || eventID == "" {
		return nil, fmt.Errorf("missing required parameter: event-id")
	}

	endpoint := fmt.Sprintf("%s/calendar/v3/calendars/primary/events/%s", c.baseURL, eventID)
	req, err := c.authedRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("getting event %s: %w", eventID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get event %s: status %d", eventID, resp.StatusCode)
	}

	var event calendarEvent
	if err := json.NewDecoder(resp.Body).Decode(&event); err != nil {
		return nil, fmt.Errorf("decoding event %s: %w", eventID, err)
	}

	attendees := make([]Attendee, len(event.Attendees))
	for i, a := range event.Attendees {
		attendees[i] = Attendee{
			Email:          a.Email,
			ResponseStatus: a.ResponseStatus,
		}
	}

	detail := EventDetail{
		ID:          event.ID,
		Summary:     event.Summary,
		Description: event.Description,
		Location:    event.Location,
		Start:       resolveTime(event.Start),
		End:         resolveTime(event.End),
		Status:      event.Status,
		Attendees:   attendees,
	}

	return &service.Result{Data: detail}, nil
}
