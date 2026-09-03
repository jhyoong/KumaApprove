package cli

import (
	"testing"
)

type fakeService struct {
	name    string
	actions map[string]bool
	called  string
	args    map[string]string
}

func (f *fakeService) Name() string { return f.name }
func (f *fakeService) Execute(action string, args map[string]string) (any, error) {
	f.called = action
	f.args = args
	return map[string]string{"status": "ok"}, nil
}
func (f *fakeService) HasAction(action string) bool {
	return f.actions[action]
}

func TestRouterDispatch(t *testing.T) {
	svc := &fakeService{
		name:    "gmail",
		actions: map[string]bool{"list": true, "send": true},
	}

	r := NewRouter()
	r.Register(svc)

	result, err := r.Dispatch("gmail", "list", map[string]string{"limit": "20"})
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
	r := NewRouter()
	_, err := r.Dispatch("unknown", "list", nil)
	if err == nil {
		t.Fatal("expected error for unknown service")
	}
}

func TestRouterUnknownAction(t *testing.T) {
	svc := &fakeService{
		name:    "gmail",
		actions: map[string]bool{"list": true},
	}
	r := NewRouter()
	r.Register(svc)

	_, err := r.Dispatch("gmail", "unknown", nil)
	if err == nil {
		t.Fatal("expected error for unknown action")
	}
}

func TestRouterListServices(t *testing.T) {
	r := NewRouter()
	r.Register(&fakeService{name: "gmail", actions: map[string]bool{"list": true}})
	r.Register(&fakeService{name: "gcal", actions: map[string]bool{"list": true}})

	names := r.ServiceNames()
	if len(names) != 2 {
		t.Fatalf("expected 2 services, got %d", len(names))
	}
}
