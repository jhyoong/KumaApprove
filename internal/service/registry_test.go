package service

import "testing"

type mockService struct {
	name    string
	actions []ActionDefinition
}

func (m *mockService) Name() string                { return m.name }
func (m *mockService) Actions() []ActionDefinition { return m.actions }
func (m *mockService) Execute(action string, args map[string]string) (*Result, error) {
	return &Result{Data: map[string]string{"mock": "data"}}, nil
}

func TestRegistryRegisterAndLookup(t *testing.T) {
	reg := NewRegistry()
	svc := &mockService{
		name: "gmail",
		actions: []ActionDefinition{
			{Name: "list", DefaultTier: "auto", Description: "List emails"},
			{Name: "send", DefaultTier: "approve", Description: "Send email"},
		},
	}
	reg.Register(svc)

	got := reg.Get("gmail")
	if got == nil {
		t.Fatal("expected to find gmail service")
	}
	if got.Name() != "gmail" {
		t.Fatalf("expected gmail, got %s", got.Name())
	}
}

func TestRegistryGetUnknown(t *testing.T) {
	reg := NewRegistry()
	got := reg.Get("nonexistent")
	if got != nil {
		t.Fatal("expected nil for unknown service")
	}
}

func TestRegistryListServices(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&mockService{name: "gmail"})
	reg.Register(&mockService{name: "gcal"})

	names := reg.Names()
	if len(names) != 2 {
		t.Fatalf("expected 2, got %d", len(names))
	}
}

func TestActionDefinitionTier(t *testing.T) {
	ad := ActionDefinition{
		Name:        "send",
		DefaultTier: "approve",
		Description: "Send an email",
		Params: []ParamDef{
			{Name: "to", Required: true, Description: "Recipient"},
			{Name: "subject", Required: true, Description: "Subject line"},
		},
	}
	if ad.DefaultTier != "approve" {
		t.Fatalf("expected approve tier, got %s", ad.DefaultTier)
	}
	if len(ad.Params) != 2 {
		t.Fatalf("expected 2 params, got %d", len(ad.Params))
	}
}

func TestRegistryGetAction(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&mockService{
		name: "gmail",
		actions: []ActionDefinition{
			{Name: "list", DefaultTier: "auto"},
			{Name: "send", DefaultTier: "approve"},
		},
	})

	ad, err := reg.GetAction("gmail", "list")
	if err != nil {
		t.Fatal(err)
	}
	if ad.DefaultTier != "auto" {
		t.Fatalf("expected auto, got %s", ad.DefaultTier)
	}

	_, err = reg.GetAction("gmail", "nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown action")
	}
}
