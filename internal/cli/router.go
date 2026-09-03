package cli

import "fmt"

type Servicer interface {
	Name() string
	HasAction(action string) bool
	Execute(action string, args map[string]string) (any, error)
}

type Router struct {
	services map[string]Servicer
}

func NewRouter() *Router {
	return &Router{services: make(map[string]Servicer)}
}

func (r *Router) Register(svc Servicer) {
	r.services[svc.Name()] = svc
}

func (r *Router) Dispatch(service, action string, args map[string]string) (any, error) {
	svc, ok := r.services[service]
	if !ok {
		return nil, fmt.Errorf("unknown service: %s", service)
	}
	if !svc.HasAction(action) {
		return nil, fmt.Errorf("unknown action %s for service %s", action, service)
	}
	return svc.Execute(action, args)
}

func (r *Router) ServiceNames() []string {
	names := make([]string, 0, len(r.services))
	for name := range r.services {
		names = append(names, name)
	}
	return names
}
