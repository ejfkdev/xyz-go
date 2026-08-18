// Package registry holds the type-erased entries built by spec.Define and
// hands them to the transport frontends. Registration validates name
// uniqueness eagerly so collisions surface at startup, not at first call.
//
// The package also owns the process-wide default registry (Default): the
// one-main-entry program registers into it via
// spec.Command[T].RegisterDefault() and lets xyz.Main() dispatch it, with
// no registry construction or import in user code. Programs that need
// several registries (or test isolation) keep using New with the explicit
// Register / Run variants.
package registry

import (
	"fmt"
	"sort"
	"sync"

	"github.com/ejfkdev/xyz-go/spec"
)

// Default is the process-wide registry behind spec.RegisterDefault and
// xyz.Main. Programs rarely need to touch it directly.
var Default = New()

func init() {
	// 把默认注册表接到 spec 的 RegisterDefault 入口上；spec 自身不依赖本包。
	spec.DefaultRegistrar = Default
}

// Add registers an entry into Default. Name collisions are an error.
func Add(e *spec.Entry) error { return Default.Add(e) }

// Registry is the central command table shared by every frontend.
type Registry struct {
	mu      sync.RWMutex
	entries map[string]*spec.Entry
}

// New returns an empty Registry.
func New() *Registry {
	return &Registry{entries: map[string]*spec.Entry{}}
}

// Add registers an entry. Name collisions are an error.
func (r *Registry) Add(e *spec.Entry) error {
	if e == nil {
		return fmt.Errorf("registry: Add(nil)")
	}
	if e.Name == "" {
		return fmt.Errorf("registry: entry has empty name")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if old, ok := r.entries[e.Name]; ok {
		return fmt.Errorf("registry: name %q already registered (existing summary %q)", e.Name, old.Summary)
	}
	r.entries[e.Name] = e
	return nil
}

// Get returns the entry registered under name.
func (r *Registry) Get(name string) (*spec.Entry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.entries[name]
	return e, ok
}

// Names returns all registered names, sorted.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.entries))
	for n := range r.entries {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// All returns all entries sorted by name.
func (r *Registry) All() []*spec.Entry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*spec.Entry, 0, len(r.entries))
	for n := range r.entries {
		out = append(out, r.entries[n])
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
