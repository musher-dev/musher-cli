package harness

import (
	"fmt"
	"os/exec"
	"sort"
	"sync"
)

// Provider represents a registered harness provider.
type Provider struct {
	Spec *Spec

	// Available reports whether the harness binary is installed on PATH.
	// Lazily evaluated on first call and cached.
	Available func() bool
}

// Registry is a thread-safe map of harness providers keyed by name.
type Registry struct {
	mu        sync.RWMutex
	providers map[string]*Provider
}

// NewRegistry creates an empty provider registry.
func NewRegistry() *Registry {
	return &Registry{
		providers: make(map[string]*Provider),
	}
}

// Register adds a provider to the registry.  Panics if a provider with the
// same name is already registered, to catch duplicate registration at startup.
func (r *Registry) Register(prov *Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.providers[prov.Spec.Name]; exists {
		panic(fmt.Sprintf("harness provider %q registered twice", prov.Spec.Name))
	}

	r.providers[prov.Spec.Name] = prov
}

// Get returns the provider with the given name, or false if not found.
func (r *Registry) Get(name string) (*Provider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	prov, ok := r.providers[name]

	return prov, ok
}

// List returns all registered providers sorted by name.
func (r *Registry) List() []*Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]*Provider, 0, len(r.providers))
	for _, prov := range r.providers {
		result = append(result, prov)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Spec.Name < result[j].Spec.Name
	})

	return result
}

// Available returns providers whose binary is installed on PATH.
func (r *Registry) Available() []*Provider {
	all := r.List()
	result := make([]*Provider, 0, len(all))

	for _, prov := range all {
		if prov.Available != nil && prov.Available() {
			result = append(result, prov)
		}
	}

	return result
}

// Names returns sorted names of all registered providers.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.providers))
	for name := range r.providers {
		names = append(names, name)
	}

	sort.Strings(names)

	return names
}

// DefaultAvailableFunc returns a function that checks whether binary is on PATH.
func DefaultAvailableFunc(binary string) func() bool {
	var (
		once   sync.Once
		result bool
	)

	return func() bool {
		once.Do(func() {
			_, err := exec.LookPath(binary)
			result = err == nil
		})

		return result
	}
}
