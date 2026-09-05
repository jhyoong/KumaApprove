package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jhyoong/KumaApprove/internal/approval"
	"github.com/jhyoong/KumaApprove/internal/service"
)

type fakeService struct {
	name    string
	actions []service.ActionDefinition
	called  string
	args    map[string]string
}

func (f *fakeService) Name() string                    { return f.name }
func (f *fakeService) Actions() []service.ActionDefinition { return f.actions }
func (f *fakeService) Execute(action string, args map[string]string) (*service.Result, error) {
	f.called = action
	f.args = args
	return &service.Result{Data: map[string]string{"status": "ok"}}, nil
}

func TestRouterDispatch(t *testing.T) {
	svc := &fakeService{
		name: "gmail",
		actions: []service.ActionDefinition{
			{Name: "list", DefaultTier: "auto", Description: "List messages"},
		},
	}

	reg := service.NewRegistry()
	reg.Register(svc)

	r := NewRouter(RouterConfig{
		Registry: reg,
	})

	result, err := r.Dispatch("gmail", "list", "user@example.com", map[string]string{"limit": "20"})
	if err != nil {
		t.Fatal(err)
	}
	if svc.called != "list" {
		t.Fatalf("expected list, got %s", svc.called)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestRouterUnknownService(t *testing.T) {
	reg := service.NewRegistry()
	r := NewRouter(RouterConfig{
		Registry: reg,
	})

	_, err := r.Dispatch("unknown", "list", "user@example.com", nil)
	if err == nil {
		t.Fatal("expected error for unknown service")
	}
}

func TestRouterDenyTier(t *testing.T) {
	svc := &fakeService{
		name: "gmail",
		actions: []service.ActionDefinition{
			{Name: "list", DefaultTier: "auto", Description: "List messages"},
		},
	}

	reg := service.NewRegistry()
	reg.Register(svc)

	r := NewRouter(RouterConfig{
		Registry:      reg,
		TierOverrides: map[string]string{"gmail:list": "deny"},
	})

	_, err := r.Dispatch("gmail", "list", "user@example.com", nil)
	if err == nil {
		t.Fatal("expected error for denied action")
	}
	if !strings.Contains(err.Error(), "denied by policy") {
		t.Fatalf("expected 'denied by policy' in error, got: %s", err.Error())
	}
}

type fakeApprover struct {
	approved bool
	err      error
	called   bool
}

func (f *fakeApprover) RequestApproval(_ context.Context, _ approval.ApprovalRequest) (approval.ApprovalResult, error) {
	f.called = true
	if f.err != nil {
		return approval.ApprovalResult{}, f.err
	}
	return approval.ApprovalResult{Approved: f.approved, Message: "test"}, nil
}

func TestRouterApproveApproved(t *testing.T) {
	svc := &fakeService{
		name: "gmail",
		actions: []service.ActionDefinition{
			{Name: "send", DefaultTier: "approve", Description: "Send email"},
		},
	}
	reg := service.NewRegistry()
	reg.Register(svc)

	approver := &fakeApprover{approved: true}
	r := NewRouter(RouterConfig{
		Registry: reg,
		Approver: approver,
	})

	result, err := r.Dispatch("gmail", "send", "user@example.com", map[string]string{"to": "a@b.com"})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if !approver.called {
		t.Fatal("expected approver to be called")
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if svc.called != "send" {
		t.Fatalf("expected send, got %s", svc.called)
	}
}

func TestRouterApproveRejected(t *testing.T) {
	svc := &fakeService{
		name: "gmail",
		actions: []service.ActionDefinition{
			{Name: "send", DefaultTier: "approve", Description: "Send email"},
		},
	}
	reg := service.NewRegistry()
	reg.Register(svc)

	approver := &fakeApprover{approved: false}
	r := NewRouter(RouterConfig{
		Registry: reg,
		Approver: approver,
	})

	_, err := r.Dispatch("gmail", "send", "user@example.com", nil)
	if err == nil {
		t.Fatal("expected error for rejected approval")
	}
	if !strings.Contains(err.Error(), "rejected") {
		t.Fatalf("expected 'rejected' in error, got: %s", err.Error())
	}
}

func TestRouterApproveError(t *testing.T) {
	svc := &fakeService{
		name: "gmail",
		actions: []service.ActionDefinition{
			{Name: "send", DefaultTier: "approve", Description: "Send email"},
		},
	}
	reg := service.NewRegistry()
	reg.Register(svc)

	approver := &fakeApprover{err: fmt.Errorf("network error")}
	r := NewRouter(RouterConfig{
		Registry: reg,
		Approver: approver,
	})

	_, err := r.Dispatch("gmail", "send", "user@example.com", nil)
	if err == nil {
		t.Fatal("expected error for approval failure")
	}
	if !strings.Contains(err.Error(), "approval failed") {
		t.Fatalf("expected 'approval failed' in error, got: %s", err.Error())
	}
}

func TestRouterApproveNoApprover(t *testing.T) {
	svc := &fakeService{
		name: "gmail",
		actions: []service.ActionDefinition{
			{Name: "send", DefaultTier: "approve", Description: "Send email"},
		},
	}
	reg := service.NewRegistry()
	reg.Register(svc)

	r := NewRouter(RouterConfig{
		Registry: reg,
		Approver: nil,
	})

	_, err := r.Dispatch("gmail", "send", "user@example.com", nil)
	if err == nil {
		t.Fatal("expected error when no approver configured")
	}
	if !strings.Contains(err.Error(), "no approver configured") {
		t.Fatalf("expected 'no approver configured' in error, got: %s", err.Error())
	}
}

func TestRouterErrorType(t *testing.T) {
	reg := service.NewRegistry()
	r := NewRouter(RouterConfig{Registry: reg})

	_, err := r.Dispatch("nonexistent", "list", "", nil)
	if err == nil {
		t.Fatal("expected error")
	}

	var re *RouterError
	if !errors.As(err, &re) {
		t.Fatalf("expected *RouterError, got %T: %v", err, err)
	}
	if re.Code == "" {
		t.Fatal("expected non-empty error code")
	}
}

func TestRouterErrorCodes(t *testing.T) {
	tests := []struct {
		name         string
		service      string
		action       string
		args         map[string]string
		setupReg     func() *service.Registry
		approver     *fakeApprover
		tierOverride map[string]string
		wantCode     string
	}{
		{
			name:    "unknown service",
			service: "bogus", action: "list",
			setupReg: func() *service.Registry { return service.NewRegistry() },
			wantCode: "INVALID_ARGS",
		},
		{
			name:    "unknown action",
			service: "gmail", action: "bogus",
			setupReg: func() *service.Registry {
				reg := service.NewRegistry()
				reg.Register(&fakeService{
					name:    "gmail",
					actions: []service.ActionDefinition{{Name: "list", DefaultTier: "auto"}},
				})
				return reg
			},
			wantCode: "INVALID_ARGS",
		},
		{
			name:    "denied by policy",
			service: "gmail", action: "list",
			setupReg: func() *service.Registry {
				reg := service.NewRegistry()
				reg.Register(&fakeService{
					name:    "gmail",
					actions: []service.ActionDefinition{{Name: "list", DefaultTier: "auto"}},
				})
				return reg
			},
			tierOverride: map[string]string{"gmail:list": "deny"},
			wantCode:     "DENIED_BY_POLICY",
		},
		{
			name:    "no approver configured",
			service: "gmail", action: "send",
			setupReg: func() *service.Registry {
				reg := service.NewRegistry()
				reg.Register(&fakeService{
					name:    "gmail",
					actions: []service.ActionDefinition{{Name: "send", DefaultTier: "approve"}},
				})
				return reg
			},
			wantCode: "NO_APPROVER",
		},
		{
			name:    "approval rejected",
			service: "gmail", action: "send",
			setupReg: func() *service.Registry {
				reg := service.NewRegistry()
				reg.Register(&fakeService{
					name:    "gmail",
					actions: []service.ActionDefinition{{Name: "send", DefaultTier: "approve"}},
				})
				return reg
			},
			approver: &fakeApprover{approved: false},
			wantCode: "APPROVAL_REJECTED",
		},
		{
			name:    "approval error",
			service: "gmail", action: "send",
			setupReg: func() *service.Registry {
				reg := service.NewRegistry()
				reg.Register(&fakeService{
					name:    "gmail",
					actions: []service.ActionDefinition{{Name: "send", DefaultTier: "approve"}},
				})
				return reg
			},
			approver: &fakeApprover{err: fmt.Errorf("network down")},
			wantCode: "APPROVAL_ERROR",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := RouterConfig{
				Registry:      tt.setupReg(),
				TierOverrides: tt.tierOverride,
			}
			if tt.approver != nil {
				cfg.Approver = tt.approver
			}
			r := NewRouter(cfg)

			_, err := r.Dispatch(tt.service, tt.action, "user@test.com", tt.args)
			if err == nil {
				t.Fatal("expected error")
			}

			var re *RouterError
			if !errors.As(err, &re) {
				t.Fatalf("expected *RouterError, got %T: %v", err, err)
			}
			if re.Code != tt.wantCode {
				t.Fatalf("expected code %s, got %s (msg: %s)", tt.wantCode, re.Code, re.Message)
			}
		})
	}
}
