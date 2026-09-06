package gcal

import (
	"fmt"
	"io"
	"net/http"

	"github.com/jhyoong/KumaApprove/internal/approval"
	"github.com/jhyoong/KumaApprove/internal/service"
)

// TokenProvider retrieves OAuth2 access tokens for authenticated API calls.
type TokenProvider interface {
	GetToken(service, account string) (string, error)
}

// CalendarService implements the service.Service interface for Google Calendar operations.
type CalendarService struct {
	tokenProvider TokenProvider
	account       string
	baseURL       string
	httpClient    *http.Client
}

// New creates a new CalendarService with the given token provider and account.
func New(tokenProvider TokenProvider, account string) *CalendarService {
	return &CalendarService{
		tokenProvider: tokenProvider,
		account:       account,
		baseURL:       "https://www.googleapis.com",
		httpClient:    http.DefaultClient,
	}
}

// Name returns the service name.
func (c *CalendarService) Name() string { return "gcal" }

// Actions returns the action definitions for the Calendar service.
func (c *CalendarService) Actions() []service.ActionDefinition {
	return []service.ActionDefinition{
		{
			Name:        "list",
			DefaultTier: approval.TierAuto,
			Description: "List events for a date",
			Params: []service.ParamDef{
				{Name: "date", Required: false, Description: "Date in YYYY-MM-DD format (default today)"},
			},
		},
		{
			Name:        "get",
			DefaultTier: approval.TierAuto,
			Description: "Get an event by ID",
			Params: []service.ParamDef{
				{Name: "event-id", Required: true, Description: "Event ID"},
			},
		},
		{
			Name:        "create",
			DefaultTier: approval.TierApprove,
			Description: "Create a new event",
			Params: []service.ParamDef{
				{Name: "title", Required: true, Description: "Event title"},
				{Name: "start", Required: true, Description: "Start time (RFC3339)"},
				{Name: "end", Required: true, Description: "End time (RFC3339)"},
				{Name: "description", Required: false, Description: "Event description"},
				{Name: "location", Required: false, Description: "Event location"},
			},
		},
		{
			Name:        "update",
			DefaultTier: approval.TierApprove,
			Description: "Update an existing event",
			Params: []service.ParamDef{
				{Name: "event-id", Required: true, Description: "Event ID"},
				{Name: "title", Required: false, Description: "Event title"},
				{Name: "start", Required: false, Description: "Start time (RFC3339)"},
				{Name: "end", Required: false, Description: "End time (RFC3339)"},
				{Name: "description", Required: false, Description: "Event description"},
				{Name: "location", Required: false, Description: "Event location"},
			},
		},
		{
			Name:        "delete",
			DefaultTier: approval.TierApprove,
			Description: "Delete an event",
			Params: []service.ParamDef{
				{Name: "event-id", Required: true, Description: "Event ID"},
			},
		},
	}
}

// Execute dispatches the given action to the appropriate handler method.
func (c *CalendarService) Execute(action string, args map[string]string) (*service.Result, error) {
	switch action {
	case "list":
		return c.list(args)
	case "get":
		return c.getEvent(args)
	case "create":
		return c.createEvent(args)
	case "update":
		return c.updateEvent(args)
	case "delete":
		return c.deleteEvent(args)
	default:
		return nil, fmt.Errorf("unknown action: %s", action)
	}
}

// EnrichDetails resolves opaque event IDs into human-readable details for approval messages.
func (c *CalendarService) EnrichDetails(action string, args map[string]string) (map[string]string, error) {
	if action != "delete" && action != "update" {
		return args, nil
	}
	eventID := args["event-id"]
	if eventID == "" {
		return args, nil
	}
	result, err := c.getEvent(map[string]string{"event-id": eventID})
	if err != nil {
		return nil, fmt.Errorf("enriching %s: %w", action, err)
	}
	event := result.Data.(EventDetail)
	enriched := make(map[string]string, len(args)+2)
	for k, v := range args {
		enriched[k] = v
	}
	enriched["event-title"] = event.Summary
	enriched["event-time"] = event.Start + " - " + event.End
	return enriched, nil
}

// authedRequest creates an HTTP request with the Bearer token header set.
func (c *CalendarService) authedRequest(method, url string, body io.Reader) (*http.Request, error) {
	token, err := c.tokenProvider.GetToken("gcal", c.account)
	if err != nil {
		return nil, fmt.Errorf("getting token: %w", err)
	}

	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	return req, nil
}
