package gmail

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

// GmailService implements the service.Service interface for Gmail operations.
type GmailService struct {
	tokenProvider TokenProvider
	account       string
	baseURL       string
	httpClient    *http.Client
}

// New creates a new GmailService with the given token provider and account.
func New(tokenProvider TokenProvider, account string) *GmailService {
	return &GmailService{
		tokenProvider: tokenProvider,
		account:       account,
		baseURL:       "https://gmail.googleapis.com",
		httpClient:    http.DefaultClient,
	}
}

// Name returns the service name.
func (g *GmailService) Name() string { return "gmail" }

// Actions returns the action definitions for the Gmail service.
func (g *GmailService) Actions() []service.ActionDefinition {
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
				{Name: "query", Required: true, Description: "Gmail search query"},
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
			DefaultTier: approval.TierAuto,
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
func (g *GmailService) Execute(action string, args map[string]string) (*service.Result, error) {
	switch action {
	case "list":
		return g.list(args)
	case "get":
		return g.get(args)
	case "search":
		return g.search(args)
	case "send":
		return g.send(args)
	case "reply":
		return g.reply(args)
	case "draft":
		return g.draft(args)
	default:
		return nil, fmt.Errorf("unknown action: %s", action)
	}
}

// authedRequest creates an HTTP request with the Bearer token header set.
func (g *GmailService) authedRequest(method, url string, body io.Reader) (*http.Request, error) {
	token, err := g.tokenProvider.GetToken("gmail", g.account)
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
