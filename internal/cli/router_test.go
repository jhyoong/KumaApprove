package cli

import (
	"strings"
	"testing"

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
