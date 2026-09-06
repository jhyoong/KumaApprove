package service

// Service is the core interface that all service plugins implement.
type Service interface {
	Name() string
	Actions() []ActionDefinition
	Execute(action string, args map[string]string) (*Result, error)
}

// ActionDefinition describes an action a service can perform,
// including its default approval tier.
type ActionDefinition struct {
	Name        string
	DefaultTier string
	Description string
	Params      []ParamDef
}

// ParamDef describes a parameter accepted by an action.
type ParamDef struct {
	Name        string
	Required    bool
	Description string
}

// Result holds the output of a service action execution.
type Result struct {
	Data any
}

// Enricher is an optional interface that services can implement to resolve
// opaque resource IDs into human-readable details for approval messages.
type Enricher interface {
	EnrichDetails(action string, args map[string]string) (map[string]string, error)
}
