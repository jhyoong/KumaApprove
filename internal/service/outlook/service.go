package outlook

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

// OutlookService implements the service.Service interface for Outlook operations.
type OutlookService struct {
	tokenProvider TokenProvider
	account       string
	baseURL       string
	httpClient    *http.Client
}

// New creates a new OutlookService with the given token provider and account.
func New(tokenProvider TokenProvider, account string) *OutlookService {
	return &OutlookService{
		tokenProvider: tokenProvider,
		account:       account,
		baseURL:       "https://graph.microsoft.com",
		httpClient:    http.DefaultClient,
	}
}

// Name returns the service name.
func (o *OutlookService) Name() string { return "outlook" }

// Actions returns the action definitions for the Outlook service.
func (o *OutlookService) Actions() []service.ActionDefinition {
	return []service.ActionDefinition{
		{
			Name:        "list",
			DefaultTier: approval.TierAuto,
			Description: "List recent messages",
			Params: []service.ParamDef{
				{Name: "limit", Required: false, Description: "Max messages to return (default 20)"},
			},
		},
		{
			Name:        "get",
			DefaultTier: approval.TierAuto,
			Description: "Get a message by ID",
			Params: []service.ParamDef{
				{Name: "id", Required: true, Description: "Message ID"},
			},
		},
		{
			Name:        "search",
			DefaultTier: approval.TierAuto,
			Description: "Search messages by query",
			Params: []service.ParamDef{
				{Name: "query", Required: true, Description: "Search query"},
				{Name: "limit", Required: false, Description: "Max messages to return (default 20)"},
			},
		},
		{
			Name:        "send",
			DefaultTier: approval.TierApprove,
			Description: "Send a new email",
			Params: []service.ParamDef{
				{Name: "to", Required: true, Description: "Recipient email"},
				{Name: "subject", Required: true, Description: "Email subject"},
				{Name: "body", Required: true, Description: "Email body"},
			},
		},
		{
			Name:        "reply",
			DefaultTier: approval.TierApprove,
			Description: "Reply to a message",
			Params: []service.ParamDef{
				{Name: "id", Required: true, Description: "Message ID to reply to"},
				{Name: "body", Required: true, Description: "Reply body"},
			},
		},
		{
			Name:        "draft",
			DefaultTier: approval.TierApprove,
			Description: "Create a draft email",
			Params: []service.ParamDef{
				{Name: "to", Required: true, Description: "Recipient email"},
				{Name: "subject", Required: true, Description: "Email subject"},
				{Name: "body", Required: true, Description: "Email body"},
			},
		},
	}
}

// Execute dispatches the given action to the appropriate handler method.
func (o *OutlookService) Execute(action string, args map[string]string) (*service.Result, error) {
	switch action {
	case "list":
		return o.list(args)
	case "get":
		return o.get(args)
	case "search":
		return o.search(args)
	case "send":
		return nil, fmt.Errorf("not implemented")
	case "reply":
		return nil, fmt.Errorf("not implemented")
	case "draft":
		return nil, fmt.Errorf("not implemented")
	default:
		return nil, fmt.Errorf("unknown action: %s", action)
	}
}

// authedRequest creates an HTTP request with the Bearer token header set.
func (o *OutlookService) authedRequest(method, url string, body io.Reader) (*http.Request, error) {
	token, err := o.tokenProvider.GetToken("outlook", o.account)
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
