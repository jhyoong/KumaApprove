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
	// Privileged marks an action that will run as root.
	Privileged bool
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

// Tracer sends plain-text messages verbatim, with no markup interpretation:
// trace events (one per approval or denial outcome when tracing is enabled)
// and alerts such as AUTH_EXPIRED.
type Tracer interface {
	SendTrace(text string) error
}
