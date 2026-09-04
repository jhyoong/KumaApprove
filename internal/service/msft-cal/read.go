package msftcal

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
	Subject string `json:"subject"`
	Start   string `json:"start"`
	End     string `json:"end"`
}

// EventDetail holds the full content of a calendar event.
type EventDetail struct {
	ID          string     `json:"id"`
	Subject     string     `json:"subject"`
	Description string     `json:"description"`
	Location    string     `json:"location"`
	Start       string     `json:"start"`
	End         string     `json:"end"`
	Organizer   string     `json:"organizer"`
	Attendees   []Attendee `json:"attendees"`
}

// Attendee represents a single event attendee.
type Attendee struct {
	Email          string `json:"email"`
	ResponseStatus string `json:"responseStatus"`
}

// graphEventsResponse represents the Microsoft Graph API list events response.
type graphEventsResponse struct {
	Value []graphEvent `json:"value"`
}

// graphEvent represents a single Microsoft Graph API event.
type graphEvent struct {
	ID          string `json:"id"`
	Subject     string `json:"subject"`
	BodyPreview string `json:"bodyPreview"`
	Start       struct {
		DateTime string `json:"dateTime"`
		TimeZone string `json:"timeZone"`
	} `json:"start"`
	End struct {
		DateTime string `json:"dateTime"`
		TimeZone string `json:"timeZone"`
	} `json:"end"`
	Location struct {
		DisplayName string `json:"displayName"`
	} `json:"location"`
	Organizer struct {
		EmailAddress struct {
			Address string `json:"address"`
		} `json:"emailAddress"`
	} `json:"organizer"`
	Attendees []struct {
		EmailAddress struct {
			Address string `json:"address"`
		} `json:"emailAddress"`
		Status struct {
			Response string `json:"response"`
		} `json:"status"`
	} `json:"attendees"`
}

// list fetches calendar events. If date is provided, filters to that day.
func (s *MsftCalService) list(args map[string]string) (*service.Result, error) {
	endpoint := fmt.Sprintf("%s/v1.0/me/events", s.baseURL)
	params := url.Values{}
	params.Set("$orderby", "start/dateTime")
	params.Set("$top", "50")

	dateStr := args["date"]
	if dateStr != "" {
		d, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			return nil, fmt.Errorf("invalid date format (expected YYYY-MM-DD): %w", err)
		}
		dayStart := d.Format("2006-01-02") + "T00:00:00"
		dayEnd := d.AddDate(0, 0, 1).Format("2006-01-02") + "T00:00:00"
		filter := fmt.Sprintf("start/dateTime ge '%s' and start/dateTime lt '%s'", dayStart, dayEnd)
		params.Set("$filter", filter)
	}

	fullURL := endpoint + "?" + params.Encode()

	req, err := s.authedRequest(http.MethodGet, fullURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("listing events: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list events: status %d", resp.StatusCode)
	}

	var eventsResp graphEventsResponse
	if err := json.NewDecoder(resp.Body).Decode(&eventsResp); err != nil {
		return nil, fmt.Errorf("decoding events response: %w", err)
	}

	summaries := make([]EventSummary, len(eventsResp.Value))
	for i, event := range eventsResp.Value {
		summaries[i] = EventSummary{
			ID:      event.ID,
			Subject: event.Subject,
			Start:   event.Start.DateTime,
			End:     event.End.DateTime,
		}
	}

	return &service.Result{Data: summaries}, nil
}

// getEvent fetches a single event by ID. Requires the "event-id" parameter.
func (s *MsftCalService) getEvent(args map[string]string) (*service.Result, error) {
	eventID, ok := args["event-id"]
	if !ok || eventID == "" {
		return nil, fmt.Errorf("missing required parameter: event-id")
	}

	endpoint := fmt.Sprintf("%s/v1.0/me/events/%s", s.baseURL, eventID)
	req, err := s.authedRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("getting event %s: %w", eventID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get event %s: status %d", eventID, resp.StatusCode)
	}

	var event graphEvent
	if err := json.NewDecoder(resp.Body).Decode(&event); err != nil {
		return nil, fmt.Errorf("decoding event %s: %w", eventID, err)
	}

	detail := eventToDetail(event)
	return &service.Result{Data: detail}, nil
}

// eventToDetail converts a graphEvent API response to an EventDetail.
func eventToDetail(event graphEvent) EventDetail {
	attendees := make([]Attendee, len(event.Attendees))
	for i, a := range event.Attendees {
		attendees[i] = Attendee{
			Email:          a.EmailAddress.Address,
			ResponseStatus: a.Status.Response,
		}
	}

	return EventDetail{
		ID:          event.ID,
		Subject:     event.Subject,
		Description: event.BodyPreview,
		Location:    event.Location.DisplayName,
		Start:       event.Start.DateTime,
		End:         event.End.DateTime,
		Organizer:   event.Organizer.EmailAddress.Address,
		Attendees:   attendees,
	}
}
