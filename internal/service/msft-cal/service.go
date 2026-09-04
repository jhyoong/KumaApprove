package msftcal

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

// MsftCalService implements the service.Service interface for Microsoft Calendar operations.
type MsftCalService struct {
	tokenProvider TokenProvider
	account       string
	baseURL       string
	httpClient    *http.Client
}

// New creates a new MsftCalService with the given token provider and account.
func New(tokenProvider TokenProvider, account string) *MsftCalService {
	return &MsftCalService{
		tokenProvider: tokenProvider,
		account:       account,
		baseURL:       "https://graph.microsoft.com",
		httpClient:    http.DefaultClient,
	}
}

// Name returns the service name.
func (s *MsftCalService) Name() string { return "msft-cal" }

// Actions returns the action definitions for the Microsoft Calendar service.
func (s *MsftCalService) Actions() []service.ActionDefinition {
	return []service.ActionDefinition{
		{
			Name:        "list",
			DefaultTier: approval.TierAuto,
			Description: "List calendar events",
			Params: []service.ParamDef{
				{Name: "date", Required: false, Description: "Date in YYYY-MM-DD format (default: no filter)"},
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
				{Name: "subject", Required: true, Description: "Event subject"},
				{Name: "start", Required: true, Description: "Start time (RFC3339)"},
				{Name: "end", Required: true, Description: "End time (RFC3339)"},
				{Name: "body", Required: false, Description: "Event body"},
				{Name: "location", Required: false, Description: "Event location"},
			},
		},
		{
			Name:        "update",
			DefaultTier: approval.TierApprove,
			Description: "Update an existing event",
			Params: []service.ParamDef{
				{Name: "event-id", Required: true, Description: "Event ID"},
				{Name: "subject", Required: false, Description: "Event subject"},
				{Name: "start", Required: false, Description: "Start time (RFC3339)"},
				{Name: "end", Required: false, Description: "End time (RFC3339)"},
				{Name: "body", Required: false, Description: "Event body"},
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
func (s *MsftCalService) Execute(action string, args map[string]string) (*service.Result, error) {
	switch action {
	case "list":
		return s.list(args)
	case "get":
		return s.getEvent(args)
	case "create":
		return s.createEvent(args)
	case "update":
		return s.updateEvent(args)
	case "delete":
		return s.deleteEvent(args)
	default:
		return nil, fmt.Errorf("unknown action: %s", action)
	}
}

// authedRequest creates an HTTP request with the Bearer token header set.
func (s *MsftCalService) authedRequest(method, url string, body io.Reader) (*http.Request, error) {
	token, err := s.tokenProvider.GetToken("msft-cal", s.account)
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
