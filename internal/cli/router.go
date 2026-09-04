package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/jhyoong/KumaApprove/internal/approval"
	"github.com/jhyoong/KumaApprove/internal/audit"
	"github.com/jhyoong/KumaApprove/internal/service"
)

// RouterConfig holds the dependencies for the approval-aware Router.
type RouterConfig struct {
	Registry       *service.Registry
	Approver       approval.Approver // nil if no Telegram configured
	TierOverrides  map[string]string
	Logger         *audit.Logger // nil if audit logging disabled
	TimeoutMinutes int
}

// Router dispatches service actions through the approval and audit pipeline.
type Router struct {
	config RouterConfig
}

// NewRouter creates a Router with the given configuration.
// If TimeoutMinutes is zero or negative, it defaults to 240.
func NewRouter(cfg RouterConfig) *Router {
	if cfg.TimeoutMinutes <= 0 {
		cfg.TimeoutMinutes = 240
	}
	return &Router{config: cfg}
}

// Dispatch looks up the action, resolves its approval tier, requests approval
// if needed, executes the action, and logs the outcome.
func (r *Router) Dispatch(serviceName, actionName, account string, args map[string]string) (any, error) {
	actionKey := serviceName + ":" + actionName

	// Look up service and action.
	actionDef, err := r.config.Registry.GetAction(serviceName, actionName)
	if err != nil {
		r.logAction(actionKey, account, args, "denied", "failure", "INVALID_ARGS")
		return nil, err
	}

	// Resolve tier.
	tier := approval.ResolveTier(actionKey, actionDef.DefaultTier, r.config.TierOverrides)

	// Deny tier.
	if tier == approval.TierDeny {
		r.logAction(actionKey, account, args, "denied", "failure", "DENIED_BY_POLICY")
		return nil, fmt.Errorf("action %s denied by policy", actionKey)
	}

	// Approve tier -- requires approver.
	if tier == approval.TierApprove {
		if r.config.Approver == nil {
			r.logAction(actionKey, account, args, "denied", "failure", "NO_APPROVER")
			return nil, fmt.Errorf("action %s requires approval but no approver configured", actionKey)
		}

		timeout := time.Duration(r.config.TimeoutMinutes) * time.Minute
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()

		result, err := r.config.Approver.RequestApproval(ctx, approval.ApprovalRequest{
			Action:  actionKey,
			Account: account,
			Details: args,
		})
		if err != nil {
			r.logAction(actionKey, account, args, "timeout", "failure", "APPROVAL_TIMEOUT")
			return nil, fmt.Errorf("approval failed: %w", err)
		}
		if !result.Approved {
			r.logAction(actionKey, account, args, "rejected", "failure", "APPROVAL_REJECTED")
			return nil, fmt.Errorf("action %s was rejected", actionKey)
		}
	}

	// Execute the action.
	svc := r.config.Registry.Get(serviceName)
	svcResult, err := svc.Execute(actionName, args)
	if err != nil {
		status := "auto"
		if tier == approval.TierApprove {
			status = "approved"
		}
		r.logAction(actionKey, account, args, status, "failure", "API_ERROR")
		return nil, err
	}

	status := "auto"
	if tier == approval.TierApprove {
		status = "approved"
	}
	r.logAction(actionKey, account, args, status, "success", "")

	return svcResult.Data, nil
}

// logAction writes an audit log entry if a logger is configured.
func (r *Router) logAction(action, account string, params map[string]string, status, result, errorCode string) {
	if r.config.Logger == nil {
		return
	}
	r.config.Logger.Log(audit.Entry{
		Action:    action,
		Account:   account,
		Params:    params,
		Status:    status,
		Result:    result,
		ErrorCode: errorCode,
	})
}
