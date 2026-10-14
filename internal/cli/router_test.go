package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jhyoong/KumaApprove/internal/approval"
	"github.com/jhyoong/KumaApprove/internal/audit"
	"github.com/jhyoong/KumaApprove/internal/executor"
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

func TestRouterValidateBeforeApprove(t *testing.T) {
	svc := &fakeService{
		name: "gmail",
		actions: []service.ActionDefinition{
			{
				Name:        "send",
				DefaultTier: "approve",
				Params: []service.ParamDef{
					{Name: "to", Required: true, Description: "Recipient"},
					{Name: "subject", Required: true, Description: "Subject"},
					{Name: "body", Required: true, Description: "Body"},
				},
			},
		},
	}
	reg := service.NewRegistry()
	reg.Register(svc)

	approver := &fakeApprover{approved: true}
	r := NewRouter(RouterConfig{
		Registry: reg,
		Approver: approver,
	})

	_, err := r.Dispatch("gmail", "send", "user@test.com", map[string]string{})
	if err == nil {
		t.Fatal("expected error for missing params")
	}

	var re *RouterError
	if !errors.As(err, &re) {
		t.Fatalf("expected *RouterError, got %T", err)
	}
	if re.Code != "INVALID_ARGS" {
		t.Fatalf("expected INVALID_ARGS, got %s", re.Code)
	}
	if approver.called {
		t.Fatal("approver should NOT be called when params are missing")
	}
}

func TestRouterValidatePassesWithAllParams(t *testing.T) {
	svc := &fakeService{
		name: "gmail",
		actions: []service.ActionDefinition{
			{
				Name:        "send",
				DefaultTier: "approve",
				Params: []service.ParamDef{
					{Name: "to", Required: true},
					{Name: "subject", Required: true},
					{Name: "body", Required: true},
				},
			},
		},
	}
	reg := service.NewRegistry()
	reg.Register(svc)

	approver := &fakeApprover{approved: true}
	r := NewRouter(RouterConfig{
		Registry: reg,
		Approver: approver,
	})

	_, err := r.Dispatch("gmail", "send", "user@test.com", map[string]string{
		"to":      "a@b.com",
		"subject": "Hi",
		"body":    "Hello",
	})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if !approver.called {
		t.Fatal("expected approver to be called")
	}
}

type capturingApprover struct {
	approved bool
	capture  *approval.ApprovalRequest
}

func (c *capturingApprover) RequestApproval(_ context.Context, req approval.ApprovalRequest) (approval.ApprovalResult, error) {
	*c.capture = req
	return approval.ApprovalResult{Approved: c.approved, Message: "test"}, nil
}

type fakeEnricherService struct {
	fakeService
	enrichErr    error
	enrichCalls  int
	enrichResult map[string]string
}

func (f *fakeEnricherService) EnrichDetails(action string, args map[string]string) (map[string]string, error) {
	f.enrichCalls++
	if f.enrichErr != nil {
		return nil, f.enrichErr
	}
	if f.enrichResult != nil {
		return f.enrichResult, nil
	}
	return args, nil
}

type failTwiceEnricher struct {
	fakeService
	calls int
}

func (f *failTwiceEnricher) EnrichDetails(action string, args map[string]string) (map[string]string, error) {
	f.calls++
	if f.calls <= 2 {
		return nil, fmt.Errorf("temporary failure")
	}
	enriched := make(map[string]string, len(args)+1)
	for k, v := range args {
		enriched[k] = v
	}
	enriched["event-title"] = "Retried Event"
	return enriched, nil
}

func TestRouterEnrichmentPassedToApprover(t *testing.T) {
	enriched := map[string]string{
		"event-id":    "evt1",
		"event-title": "Team Standup",
		"event-time":  "2024-01-15T09:00:00Z - 2024-01-15T09:30:00Z",
	}
	svc := &fakeEnricherService{
		fakeService: fakeService{
			name: "gcal",
			actions: []service.ActionDefinition{
				{Name: "delete", DefaultTier: "approve", Params: []service.ParamDef{
					{Name: "event-id", Required: true},
				}},
			},
		},
		enrichResult: enriched,
	}
	reg := service.NewRegistry()
	reg.Register(svc)

	var captured approval.ApprovalRequest
	approver := &capturingApprover{approved: true, capture: &captured}
	r := NewRouter(RouterConfig{
		Registry: reg,
		Approver: approver,
	})

	_, err := r.Dispatch("gcal", "delete", "user@test.com", map[string]string{"event-id": "evt1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if captured.Details["event-title"] != "Team Standup" {
		t.Fatalf("expected enriched details passed to approver, got: %v", captured.Details)
	}
	if svc.enrichCalls != 1 {
		t.Fatalf("expected 1 enrich call, got %d", svc.enrichCalls)
	}
}

func TestRouterEnrichmentFailureBlocks(t *testing.T) {
	svc := &fakeEnricherService{
		fakeService: fakeService{
			name: "gcal",
			actions: []service.ActionDefinition{
				{Name: "delete", DefaultTier: "approve", Params: []service.ParamDef{
					{Name: "event-id", Required: true},
				}},
			},
		},
		enrichErr: fmt.Errorf("API unreachable"),
	}
	reg := service.NewRegistry()
	reg.Register(svc)

	approver := &fakeApprover{approved: true}
	r := NewRouter(RouterConfig{
		Registry: reg,
		Approver: approver,
	})

	_, err := r.Dispatch("gcal", "delete", "user@test.com", map[string]string{"event-id": "evt1"})
	if err == nil {
		t.Fatal("expected error for enrichment failure")
	}

	var re *RouterError
	if !errors.As(err, &re) {
		t.Fatalf("expected *RouterError, got %T", err)
	}
	if re.Code != "ENRICHMENT_FAILED" {
		t.Fatalf("expected ENRICHMENT_FAILED, got %s", re.Code)
	}
	if svc.enrichCalls != 3 {
		t.Fatalf("expected 3 retry attempts, got %d", svc.enrichCalls)
	}
	if approver.called {
		t.Fatal("approver should NOT be called when enrichment fails")
	}
}

func TestRouterEnrichmentRetrySucceeds(t *testing.T) {
	svc := &failTwiceEnricher{
		fakeService: fakeService{
			name: "gcal",
			actions: []service.ActionDefinition{
				{Name: "delete", DefaultTier: "approve", Params: []service.ParamDef{
					{Name: "event-id", Required: true},
				}},
			},
		},
	}
	reg := service.NewRegistry()
	reg.Register(svc)

	approver := &fakeApprover{approved: true}
	r := NewRouter(RouterConfig{
		Registry: reg,
		Approver: approver,
	})

	_, err := r.Dispatch("gcal", "delete", "user@test.com", map[string]string{"event-id": "evt1"})
	if err != nil {
		t.Fatalf("expected success after retry, got: %v", err)
	}
	if !approver.called {
		t.Fatal("expected approver to be called after successful retry")
	}
	if svc.calls != 3 {
		t.Fatalf("expected 3 calls (2 failures + 1 success), got %d", svc.calls)
	}
}

func TestRouterValidateOptionalParamsNotRequired(t *testing.T) {
	svc := &fakeService{
		name: "gmail",
		actions: []service.ActionDefinition{
			{
				Name:        "list",
				DefaultTier: "auto",
				Params: []service.ParamDef{
					{Name: "limit", Required: false},
				},
			},
		},
	}
	reg := service.NewRegistry()
	reg.Register(svc)

	r := NewRouter(RouterConfig{Registry: reg})

	_, err := r.Dispatch("gmail", "list", "user@test.com", map[string]string{})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

type validatingService struct {
	fakeService
	err error
}

func (v *validatingService) Validate(_ string, _ map[string]string) error { return v.err }

func TestRouterValidatorRunsBeforeApproval(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantCode string
	}{
		{"deny list", &executor.DeniedError{Cmd: "sudo ls"}, "DENIED_BY_POLICY"},
		{"bad args", fmt.Errorf("invalid timeout"), "INVALID_ARGS"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &validatingService{
				fakeService: fakeService{
					name:    "exec",
					actions: []service.ActionDefinition{{Name: "run", DefaultTier: "approve"}},
				},
				err: tt.err,
			}
			reg := service.NewRegistry()
			reg.Register(svc)
			approver := &fakeApprover{approved: true}

			r := NewRouter(RouterConfig{Registry: reg, Approver: approver})
			_, err := r.Dispatch("exec", "run", "", map[string]string{"cmd": "x"})

			var re *RouterError
			if !errors.As(err, &re) || re.Code != tt.wantCode {
				t.Fatalf("expected code %s, got %v", tt.wantCode, err)
			}
			if approver.called {
				t.Fatal("approval was requested for a request that fails validation")
			}
			if svc.called != "" {
				t.Fatal("service executed despite failing validation")
			}
		})
	}
}

func TestRouterPrivilegedAlwaysRequiresApproval(t *testing.T) {
	newReg := func() (*service.Registry, *fakeService) {
		svc := &fakeService{
			name:    "exec",
			actions: []service.ActionDefinition{{Name: "run", DefaultTier: "auto"}},
		}
		reg := service.NewRegistry()
		reg.Register(svc)
		return reg, svc
	}
	overrides := map[string]string{"exec:run": "auto"}

	t.Run("no approver", func(t *testing.T) {
		reg, svc := newReg()
		r := NewRouter(RouterConfig{Registry: reg, TierOverrides: overrides, Privileged: true})
		_, err := r.Dispatch("exec", "run", "", map[string]string{"cmd": "x"})

		var re *RouterError
		if !errors.As(err, &re) || re.Code != "NO_APPROVER" {
			t.Fatalf("expected NO_APPROVER, got %v", err)
		}
		if svc.called != "" {
			t.Fatal("privileged action executed without approval")
		}
	})

	t.Run("rejected", func(t *testing.T) {
		reg, svc := newReg()
		approver := &fakeApprover{approved: false}
		r := NewRouter(RouterConfig{Registry: reg, TierOverrides: overrides, Approver: approver, Privileged: true})
		_, err := r.Dispatch("exec", "run", "", map[string]string{"cmd": "x"})

		var re *RouterError
		if !errors.As(err, &re) || re.Code != "APPROVAL_REJECTED" {
			t.Fatalf("expected APPROVAL_REJECTED, got %v", err)
		}
		if !approver.called || svc.called != "" {
			t.Fatal("expected approval to be requested and execution to be blocked")
		}
	})

	t.Run("approved", func(t *testing.T) {
		reg, svc := newReg()
		approver := &fakeApprover{approved: true}
		r := NewRouter(RouterConfig{Registry: reg, TierOverrides: overrides, Approver: approver, Privileged: true})
		if _, err := r.Dispatch("exec", "run", "", map[string]string{"cmd": "x"}); err != nil {
			t.Fatal(err)
		}
		if !approver.called || svc.called != "run" {
			t.Fatal("expected approval then execution")
		}
	})
}

func TestRouterAuditsNonZeroExit(t *testing.T) {
	execSvc, err := executor.New(executor.ExecConfig{})
	if err != nil {
		t.Fatal(err)
	}
	reg := service.NewRegistry()
	reg.Register(execSvc)

	logPath := filepath.Join(t.TempDir(), "audit.log")
	logger, err := audit.NewLogger(logPath)
	if err != nil {
		t.Fatal(err)
	}

	r := NewRouter(RouterConfig{Registry: reg, Approver: &fakeApprover{approved: true}, Logger: logger})
	result, err := r.Dispatch("exec", "run", "", map[string]string{"cmd": "false"})
	if err != nil {
		t.Fatal(err)
	}
	if result.(*executor.ExecResult).ExitCode == 0 {
		t.Fatal("expected a non-zero exit code in the result")
	}

	logged, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(logged), `"result":"failure"`) || !strings.Contains(string(logged), "NONZERO_EXIT") {
		t.Fatalf("expected a NONZERO_EXIT failure in the audit log, got %s", logged)
	}
}

type recordingApprover struct {
	req approval.ApprovalRequest
}

func (a *recordingApprover) RequestApproval(_ context.Context, req approval.ApprovalRequest) (approval.ApprovalResult, error) {
	a.req = req
	return approval.ApprovalResult{Approved: true}, nil
}

func TestRouterFlagsPrivilegedRequests(t *testing.T) {
	for _, privileged := range []bool{false, true} {
		t.Run(fmt.Sprintf("privileged=%v", privileged), func(t *testing.T) {
			reg := service.NewRegistry()
			reg.Register(&fakeService{
				name:    "exec",
				actions: []service.ActionDefinition{{Name: "run", DefaultTier: "approve"}},
			})
			approver := &recordingApprover{}

			r := NewRouter(RouterConfig{Registry: reg, Approver: approver, Privileged: privileged})
			if _, err := r.Dispatch("exec", "run", "", map[string]string{"cmd": "id"}); err != nil {
				t.Fatal(err)
			}
			if approver.req.Privileged != privileged {
				t.Errorf("approval request Privileged = %v, want %v", approver.req.Privileged, privileged)
			}
			if approver.req.Details["cmd"] != "id" {
				t.Errorf("expected the command in the approval details, got %v", approver.req.Details)
			}
		})
	}
}

type recordingTracer struct {
	events []string
	err    error
}

func (r *recordingTracer) SendTrace(text string) error {
	r.events = append(r.events, text)
	return r.err
}

func TestRouterTrace(t *testing.T) {
	tests := []struct {
		name      string
		tier      string
		approver  *fakeApprover
		validate  error
		wantEvent []string // substrings of the single expected event; nil means no event
	}{
		{name: "auto success is not traced", tier: "auto"},
		{name: "approved", tier: "approve", approver: &fakeApprover{approved: true},
			wantEvent: []string{"exec:run: approved, success", "cmd: id_x"}},
		{name: "rejected", tier: "approve", approver: &fakeApprover{approved: false},
			wantEvent: []string{"exec:run: rejected, failure (APPROVAL_REJECTED)", "cmd: id_x"}},
		{name: "approval error", tier: "approve", approver: &fakeApprover{err: fmt.Errorf("network down")},
			wantEvent: []string{"exec:run: error, failure (APPROVAL_ERROR)"}},
		{name: "denied by tier", tier: "deny",
			wantEvent: []string{"exec:run: denied, failure (DENIED_BY_POLICY)"}},
		{name: "denied by deny list", tier: "approve", approver: &fakeApprover{approved: true},
			validate:  &executor.DeniedError{Cmd: "id_x"},
			wantEvent: []string{"exec:run: denied, failure (DENIED_BY_POLICY)", "cmd: id_x"}},
		{name: "no approver", tier: "approve",
			wantEvent: []string{"exec:run: denied, failure (NO_APPROVER)"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := service.NewRegistry()
			reg.Register(&validatingService{
				fakeService: fakeService{
					name:    "exec",
					actions: []service.ActionDefinition{{Name: "run", DefaultTier: tt.tier}},
				},
				err: tt.validate,
			})
			tracer := &recordingTracer{}
			cfg := RouterConfig{Registry: reg, Tracer: tracer}
			if tt.approver != nil {
				cfg.Approver = tt.approver
			}

			NewRouter(cfg).Dispatch("exec", "run", "", map[string]string{"cmd": "id_x"})

			if tt.wantEvent == nil {
				if len(tracer.events) != 0 {
					t.Fatalf("expected no trace, got %q", tracer.events)
				}
				return
			}
			if len(tracer.events) != 1 {
				t.Fatalf("expected exactly one trace event, got %q", tracer.events)
			}
			event := tracer.events[0]
			if !strings.HasPrefix(event, "[trace] 20") || !strings.Contains(event, " UTC\n") {
				t.Errorf("expected a leading UTC timestamp, got %q", event)
			}
			for _, want := range tt.wantEvent {
				if !strings.Contains(event, want) {
					t.Errorf("expected %q in trace, got %q", want, event)
				}
			}
		})
	}
}

func TestRouterTraceMarksRootAndTruncates(t *testing.T) {
	reg := service.NewRegistry()
	reg.Register(&fakeService{
		name:    "exec",
		actions: []service.ActionDefinition{{Name: "run", DefaultTier: "approve"}},
	})
	tracer := &recordingTracer{}
	r := NewRouter(RouterConfig{Registry: reg, Tracer: tracer, Approver: &fakeApprover{approved: true}, Privileged: true})

	if _, err := r.Dispatch("exec", "run", "", map[string]string{"cmd": strings.Repeat("a", 1000)}); err != nil {
		t.Fatal(err)
	}
	if len(tracer.events) != 1 {
		t.Fatalf("expected one trace event, got %d", len(tracer.events))
	}
	event := tracer.events[0]
	if !strings.Contains(event, "exec:run (root): approved, success") {
		t.Errorf("expected the trace to be marked as root, got %q", event)
	}
	if !strings.Contains(event, strings.Repeat("a", maxTraceParamLen)+"...") || strings.Contains(event, strings.Repeat("a", maxTraceParamLen+1)) {
		t.Errorf("expected the command to be cut at %d characters, got %q", maxTraceParamLen, event)
	}
}

func TestRouterTraceFailureDoesNotAffectAction(t *testing.T) {
	svc := &fakeService{
		name:    "exec",
		actions: []service.ActionDefinition{{Name: "run", DefaultTier: "approve"}},
	}
	reg := service.NewRegistry()
	reg.Register(svc)
	tracer := &recordingTracer{err: fmt.Errorf("telegram down")}
	r := NewRouter(RouterConfig{Registry: reg, Tracer: tracer, Approver: &fakeApprover{approved: true}})

	if _, err := r.Dispatch("exec", "run", "", map[string]string{"cmd": "id"}); err != nil {
		t.Fatalf("a failing tracer must not fail the action: %v", err)
	}
	if svc.called != "run" {
		t.Fatal("expected the action to run")
	}
}
