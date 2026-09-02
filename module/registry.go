package module

import (
	"errors"
	"fmt"
	"strings"

	vmtypes "github.com/siyul-park/minivm/types"
)

// Registry is an ordered, immutable set of native modules with a designated
// fallback module for unqualified names (Python's builtins).
type Registry struct {
	modules  []Module
	byName   map[string]Module
	fallback string
}

// RegistryOption configures a Registry.
type RegistryOption func(*Registry)

// Registry catalogue failures. A caller assembling a registry from
// configuration branches on these; MustNewRegistry panics with them.
var (
	ErrNilModule            = errors.New("module: nil module")
	ErrDuplicateModule      = errors.New("module: duplicate module")
	ErrUnregisteredFallback = errors.New("module: fallback module is not registered")
)

// WithFallback designates the module consulted for unqualified names.
func WithFallback(name string) RegistryOption {
	return func(r *Registry) { r.fallback = name }
}

// NewRegistry builds a Registry from the given modules and options.
// MustNewRegistry is NewRegistry for a catalogue fixed at startup, where an
// invalid one is a programming error rather than something a caller can handle.
// It panics on the errors NewRegistry reports.
func MustNewRegistry(modules []Module, opts ...RegistryOption) *Registry {
	registry, err := NewRegistry(modules, opts...)
	if err != nil {
		panic(err)
	}
	return registry
}

// NewRegistry returns a registry over modules. It reports an invalid catalogue
// rather than panicking, because a caller can assemble one from configuration:
// a nil module, two modules claiming one name, or a fallback naming a module
// that is not registered.
func NewRegistry(modules []Module, opts ...RegistryOption) (*Registry, error) {
	r := &Registry{
		modules: append([]Module(nil), modules...),
		byName:  make(map[string]Module, len(modules)),
	}
	for _, m := range modules {
		if m == nil {
			return nil, ErrNilModule
		}
		name := m.Name()
		if _, exists := r.byName[name]; exists {
			return nil, fmt.Errorf("%w: %s", ErrDuplicateModule, name)
		}
		r.byName[name] = m
	}
	for _, opt := range opts {
		opt(r)
	}
	if r.fallback != "" && !r.Has(r.fallback) {
		return nil, fmt.Errorf("%w: %s", ErrUnregisteredFallback, r.fallback)
	}
	return r, nil
}

// Modules returns the registered modules in registration order.
func (r *Registry) Modules() []Module {
	return append([]Module(nil), r.modules...)
}

// Module looks up a module by name.
func (r *Registry) Module(name string) (Module, bool) {
	m, ok := r.byName[name]
	return m, ok
}

// Has reports whether a module with the given name is registered.
func (r *Registry) Has(name string) bool {
	_, ok := r.Module(name)
	return ok
}

// FallbackName returns the configured fallback module name.
func (r *Registry) FallbackName() string { return r.fallback }

// Fallback returns the fallback module, if configured and registered.
func (r *Registry) Fallback() (Module, bool) {
	if r.fallback == "" {
		return nil, false
	}
	return r.Module(r.fallback)
}

// Symbol looks up a qualified symbol.
func (r *Registry) Symbol(module, name string) (Symbol, bool) {
	m, ok := r.Module(module)
	if !ok {
		return nil, false
	}
	return m.Symbol(name)
}

// FallbackSymbol looks up an unqualified symbol in the fallback module.
func (r *Registry) FallbackSymbol(name string) (Symbol, bool) {
	m, ok := r.Fallback()
	if !ok {
		return nil, false
	}
	return m.Symbol(name)
}

// SymbolByKey looks up a symbol from a "module.name" qualified key.
func (r *Registry) SymbolByKey(key string) (Symbol, bool) {
	moduleName, name, ok := strings.Cut(key, ".")
	if !ok {
		return nil, false
	}
	return r.Symbol(moduleName, name)
}

// Values materializes runtime-backed symbol values against a runtime, keyed by
// module then symbol name. Inline-only symbols are omitted.
func (r *Registry) Values(rt Runtime) map[string]map[string]vmtypes.Value {
	out := make(map[string]map[string]vmtypes.Value, len(r.modules))
	for _, m := range r.modules {
		names := m.Names()
		symbols := make(map[string]vmtypes.Value, len(names))
		for _, name := range names {
			symbol, ok := m.Symbol(name)
			if !ok {
				continue
			}
			runtime, ok := symbol.(RuntimeSymbol)
			if !ok {
				continue
			}
			value, ok := runtime.RuntimeValue(rt)
			if ok {
				symbols[name] = value
			}
		}
		out[m.Name()] = symbols
	}
	return out
}
