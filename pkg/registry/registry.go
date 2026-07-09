// Package registry is a tiny, generic, compile-time plugin table.
//
// It is the backbone of Praetor's "add a variant = drop a file" seams: a plugin
// file registers itself into a package-level Registry in its init(), and the
// consuming code looks the plugin up by name at runtime. Registration is
// append-only and happens at process start; there is deliberately no runtime
// mutation, no Deregister, and no dynamic loading (see
// docs/modularity-plugin-architecture.md for why Go's dynamic-plugin options are
// avoided). The first user is pkg/notify's notification backends.
package registry

import (
	"fmt"
	"sort"
	"sync"
)

// Registry is a named, type-safe table of plugins keyed by a stable string.
// Registration is expected at init time; lookups at request time.
type Registry[T any] struct {
	kind string
	mu   sync.RWMutex
	m    map[string]T
}

// New returns an empty Registry. kind is used only in panic/error messages.
func New[T any](kind string) *Registry[T] {
	return &Registry[T]{kind: kind, m: make(map[string]T)}
}

// Register adds v under name. A duplicate name is a programmer error (two
// plugins claiming the same key), so it panics at init — failing the build's
// smoke test rather than surfacing at runtime.
func (r *Registry[T]) Register(name string, v T) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.m[name]; dup {
		panic(fmt.Sprintf("%s: duplicate registration %q", r.kind, name))
	}
	r.m[name] = v
}

// Get returns the plugin registered under name, and whether it was found.
func (r *Registry[T]) Get(name string) (T, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.m[name]
	return v, ok
}

// Names returns the registered names, sorted, so callers (and API responses)
// have a stable order.
func (r *Registry[T]) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.m))
	for k := range r.m {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}
