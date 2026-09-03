package service

import (
	"fmt"
	"sort"
)

// Registry holds registered services and provides lookup by name.
type Registry struct {
	services map[string]Service
}

// NewRegistry creates an empty service registry.
func NewRegistry() *Registry {
	return &Registry{services: make(map[string]Service)}
}

// Register adds a service to the registry, keyed by its Name().
func (r *Registry) Register(svc Service) {
	r.services[svc.Name()] = svc
}

// Get returns the service with the given name, or nil if not found.
func (r *Registry) Get(name string) Service {
	return r.services[name]
}

// Names returns a sorted list of all registered service names.
func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.services))
	for n := range r.services {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// GetAction looks up a specific action within a registered service.
// Returns an error if the service or action is not found.
func (r *Registry) GetAction(serviceName, actionName string) (ActionDefinition, error) {
	svc := r.services[serviceName]
	if svc == nil {
		return ActionDefinition{}, fmt.Errorf("unknown service: %s", serviceName)
	}
	for _, a := range svc.Actions() {
		if a.Name == actionName {
			return a, nil
		}
	}
	return ActionDefinition{}, fmt.Errorf("unknown action %s for service %s", actionName, serviceName)
}
