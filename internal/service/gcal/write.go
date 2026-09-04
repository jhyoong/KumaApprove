package gcal

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/jhyoong/KumaApprove/internal/service"
)

// createEvent creates a new calendar event. Requires summary, start, and end parameters.
func (c *CalendarService) createEvent(args map[string]string) (*service.Result, error) {
	summary := args["summary"]
	if summary == "" {
		return nil, fmt.Errorf("missing required parameter: summary")
	}

	start := args["start"]
	if start == "" {
		return nil, fmt.Errorf("missing required parameter: start")
	}

	end := args["end"]
	if end == "" {
		return nil, fmt.Errorf("missing required parameter: end")
	}

	body := map[string]any{
		"summary": summary,
		"start":   map[string]string{"dateTime": start},
		"end":     map[string]string{"dateTime": end},
	}

	if desc := args["description"]; desc != "" {
		body["description"] = desc
	}
	if loc := args["location"]; loc != "" {
		body["location"] = loc
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encoding request body: %w", err)
	}

	endpoint := fmt.Sprintf("%s/calendar/v3/calendars/primary/events", c.baseURL)
	req, err := c.authedRequest(http.MethodPost, endpoint, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("creating event: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("create event: status %d", resp.StatusCode)
	}

	var event calendarEvent
	if err := json.NewDecoder(resp.Body).Decode(&event); err != nil {
		return nil, fmt.Errorf("decoding create response: %w", err)
	}

	detail := eventToDetail(event)
	return &service.Result{Data: detail}, nil
}

// updateEvent updates an existing calendar event. Requires event-id parameter.
func (c *CalendarService) updateEvent(args map[string]string) (*service.Result, error) {
	eventID := args["event-id"]
	if eventID == "" {
		return nil, fmt.Errorf("missing required parameter: event-id")
	}

	body := map[string]any{}

	if summary := args["summary"]; summary != "" {
		body["summary"] = summary
	}
	if start := args["start"]; start != "" {
		body["start"] = map[string]string{"dateTime": start}
	}
	if end := args["end"]; end != "" {
		body["end"] = map[string]string{"dateTime": end}
	}
	if desc := args["description"]; desc != "" {
		body["description"] = desc
	}
	if loc := args["location"]; loc != "" {
		body["location"] = loc
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encoding request body: %w", err)
	}

	endpoint := fmt.Sprintf("%s/calendar/v3/calendars/primary/events/%s", c.baseURL, eventID)
	req, err := c.authedRequest(http.MethodPatch, endpoint, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("updating event %s: %w", eventID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("update event %s: status %d", eventID, resp.StatusCode)
	}

	var event calendarEvent
	if err := json.NewDecoder(resp.Body).Decode(&event); err != nil {
		return nil, fmt.Errorf("decoding update response: %w", err)
	}

	detail := eventToDetail(event)
	return &service.Result{Data: detail}, nil
}

// deleteEvent deletes a calendar event. Requires event-id parameter.
func (c *CalendarService) deleteEvent(args map[string]string) (*service.Result, error) {
	eventID := args["event-id"]
	if eventID == "" {
		return nil, fmt.Errorf("missing required parameter: event-id")
	}

	endpoint := fmt.Sprintf("%s/calendar/v3/calendars/primary/events/%s", c.baseURL, eventID)
	req, err := c.authedRequest(http.MethodDelete, endpoint, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("deleting event %s: %w", eventID, err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		return nil, fmt.Errorf("delete event %s: status %d", eventID, resp.StatusCode)
	}

	return &service.Result{
		Data: map[string]string{
			"status":   "deleted",
			"event-id": eventID,
		},
	}, nil
}

// eventToDetail converts a calendarEvent API response to an EventDetail.
func eventToDetail(event calendarEvent) EventDetail {
	attendees := make([]Attendee, len(event.Attendees))
	for i, a := range event.Attendees {
		attendees[i] = Attendee{
			Email:          a.Email,
			ResponseStatus: a.ResponseStatus,
		}
	}

	return EventDetail{
		ID:          event.ID,
		Summary:     event.Summary,
		Description: event.Description,
		Location:    event.Location,
		Start:       resolveTime(event.Start),
		End:         resolveTime(event.End),
		Status:      event.Status,
		Attendees:   attendees,
	}
}
