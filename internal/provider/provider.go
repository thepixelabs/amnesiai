// Package provider defines the Provider interface and a global registry
// for AI coding assistant configuration providers (claude, gemini, copilot, codex).
package provider

import (
	"fmt"
	"sort"
	"sync"
)

// Factory lazily constructs a provider instance.
type Factory func() (Provider, error)

// DiffEntry describes a single file difference between two snapshots.
type DiffEntry struct {
	Path   string // absolute path of the file
	Status string // "added" | "modified" | "deleted" | "unchanged"
	Before []byte // content in the old snapshot (nil if added)
	After  []byte // content in the current state (nil if deleted)
}

// Provider is the interface every AI assistant configuration provider must implement.
type Provider interface {
	// Name returns the short identifier for this provider (e.g. "claude").
	Name() string

	// Discover returns the absolute paths this provider manages.
	Discover() ([]string, error)

	// Read returns the current on-disk state as a map of path to content bytes.
	Read() (map[string][]byte, error)

	// Diff compares the current on-disk state to a previously saved snapshot.
	Diff(snapshot map[string][]byte) ([]DiffEntry, error)

	// Restore writes a snapshot back to disk, creating directories as needed.
	Restore(snapshot map[string][]byte) error
}

// registry holds all registered providers.
var (
	registryMu sync.RWMutex
	registry   = make(map[string]Factory)
)

// Register adds a provider to the global registry.
// It panics if a provider with the same name is already registered.
func Register(p Provider) {
	RegisterFactory(p.Name(), func() (Provider, error) {
		return p, nil
	})
}

// RegisterFactory adds a provider factory to the global registry.
// It panics if a provider with the same name is already registered.
func RegisterFactory(name string, factory Factory) {
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, exists := registry[name]; exists {
		panic(fmt.Sprintf("provider %q already registered", name))
	}
	registry[name] = factory
}

// Get returns a registered provider by name, or an error if not found.
func Get(name string) (Provider, error) {
	registryMu.RLock()
	factory, ok := registry[name]
	registryMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("provider %q not registered", name)
	}

	p, err := factory()
	if err != nil {
		return nil, fmt.Errorf("provider %q is unavailable: %w", name, err)
	}
	return p, nil
}

// All returns all registered providers as a name-keyed map.
func All() map[string]Provider {
	registryMu.RLock()
	factories := make(map[string]Factory, len(registry))
	for k, v := range registry {
		factories[k] = v
	}
	registryMu.RUnlock()

	out := make(map[string]Provider, len(factories))
	for k, factory := range factories {
		p, err := factory()
		if err != nil {
			continue
		}
		out[k] = p
	}
	return out
}

// Names returns the sorted list of registered provider names.
func Names() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// GetMultiple returns providers matching the requested names.
// Returns an error if any requested name is not registered.
func GetMultiple(names []string) ([]Provider, error) {
	providers := make([]Provider, 0, len(names))
	for _, name := range names {
		p, err := Get(name)
		if err != nil {
			return nil, err
		}
		providers = append(providers, p)
	}
	return providers, nil
}
