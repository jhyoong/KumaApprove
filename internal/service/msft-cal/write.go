package msftcal

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/jhyoong/KumaApprove/internal/service"
)

// createEvent creates a new calendar event via the Microsoft Graph API.
// Required params: title, start, end. Optional: description, location.
func (s *MsftCalService) createEvent(args map[string]string) (*service.Result, error) {
	title := args["title"]
	if title == "" {
		return nil, fmt.Errorf("missing required parameter: title")
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
		"subject": title,
		"start": map[string]string{
			"dateTime": start,
			"timeZone": "UTC",
		},
		"end": map[string]string{
			"dateTime": end,
			"timeZone": "UTC",
		},
	}

	if desc, ok := args["description"]; ok && desc != "" {
		body["body"] = map[string]string{
			"content": desc,
		}
	}

	if loc, ok := args["location"]; ok && loc != "" {
		body["location"] = map[string]string{
			"displayName": loc,
		}
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshaling event body: %w", err)
	}

	endpoint := fmt.Sprintf("%s/v1.0/me/events", s.baseURL)
	req, err := s.authedRequest(http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("creating event: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("create event: status %d", resp.StatusCode)
	}

	var event graphEvent
	if err := json.NewDecoder(resp.Body).Decode(&event); err != nil {
		return nil, fmt.Errorf("decoding created event: %w", err)
	}

	detail := eventToDetail(event)
	return &service.Result{Data: detail}, nil
}

// updateEvent updates an existing calendar event via the Microsoft Graph API.
// Required param: event-id. Optional: title, start, end, description, location.
// At least one field besides event-id must be provided.
func (s *MsftCalService) updateEvent(args map[string]string) (*service.Result, error) {
	eventID := args["event-id"]
	if eventID == "" {
		return nil, fmt.Errorf("missing required parameter: event-id")
	}

	body := make(map[string]any)

	if title, ok := args["title"]; ok && title != "" {
		body["subject"] = title
	}
	if start, ok := args["start"]; ok && start != "" {
		body["start"] = map[string]string{
			"dateTime": start,
			"timeZone": "UTC",
		}
	}
	if end, ok := args["end"]; ok && end != "" {
		body["end"] = map[string]string{
			"dateTime": end,
			"timeZone": "UTC",
		}
	}
	if desc, ok := args["description"]; ok && desc != "" {
		body["body"] = map[string]string{
			"content": desc,
		}
	}
	if loc, ok := args["location"]; ok && loc != "" {
		body["location"] = map[string]string{
			"displayName": loc,
		}
	}

	if len(body) == 0 {
		return nil, fmt.Errorf("no fields to update")
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshaling update body: %w", err)
	}

	endpoint := fmt.Sprintf("%s/v1.0/me/events/%s", s.baseURL, eventID)
	req, err := s.authedRequest(http.MethodPatch, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("updating event %s: %w", eventID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("update event %s: status %d", eventID, resp.StatusCode)
	}

	var event graphEvent
	if err := json.NewDecoder(resp.Body).Decode(&event); err != nil {
		return nil, fmt.Errorf("decoding updated event %s: %w", eventID, err)
	}

	detail := eventToDetail(event)
	return &service.Result{Data: detail}, nil
}

// deleteEvent deletes a calendar event via the Microsoft Graph API.
// Required param: event-id.
func (s *MsftCalService) deleteEvent(args map[string]string) (*service.Result, error) {
	eventID := args["event-id"]
	if eventID == "" {
		return nil, fmt.Errorf("missing required parameter: event-id")
	}

	endpoint := fmt.Sprintf("%s/v1.0/me/events/%s", s.baseURL, eventID)
	req, err := s.authedRequest(http.MethodDelete, endpoint, nil)
	if err != nil {
		return nil, err
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("deleting event %s: %w", eventID, err)
	}
	defer resp.Body.Close()

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
