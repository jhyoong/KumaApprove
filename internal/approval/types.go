package approval

import "context"

const (
	TierAuto    = "auto"
	TierApprove = "approve"
	TierDeny    = "deny"
)

// ApprovalRequest holds the details of an action awaiting approval.
type ApprovalRequest struct {
	RequestID string
	Action    string
	Account   string
	Details   map[string]string
}

// ApprovalResult holds the outcome of an approval decision.
type ApprovalResult struct {
	Approved bool
	Message  string
}

// Approver is the interface for approval backends (e.g. Telegram bot).
type Approver interface {
	RequestApproval(ctx context.Context, req ApprovalRequest) (ApprovalResult, error)
}
