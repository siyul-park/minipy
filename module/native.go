package module

import (
	"fmt"

	"github.com/siyul-park/minipy/ast"
	"github.com/siyul-park/minipy/token"
	"github.com/siyul-park/minipy/types"

	vmtypes "github.com/siyul-park/minivm/types"
)

// CheckFunc type-checks a native call given its argument expressions.
type CheckFunc func(c Checker, args []ast.Expr, pos token.Pos) types.Type

// EmitFunc lowers a native call given its argument expressions.
type EmitFunc func(e Emitter, args []ast.Expr)

// ValueFunc produces the optional runtime value of a native symbol.
type ValueFunc func(r Runtime) vmtypes.Value

// funcSymbol is a Symbol assembled from type-check and emit callbacks.
type funcSymbol struct {
	name  string
	check CheckFunc
	emit  EmitFunc
}

// valueSymbol adds a runtime value to a funcSymbol.
type valueSymbol struct {
	*funcSymbol
	value ValueFunc
}

var (
	_ Symbol        = (*funcSymbol)(nil)
	_ RuntimeSymbol = (*valueSymbol)(nil)
)

// nativeModule is a Module backed by an in-memory symbol table.
type nativeModule struct {
	name    string
	symbols map[string]Symbol
	names   []string
}

var _ Module = (*nativeModule)(nil)

// NewSymbol builds a Symbol from its type-check, emit, and optional runtime
// value behaviors.
func NewSymbol(name string, check CheckFunc, emit EmitFunc, value ValueFunc) Symbol {
	if name == "" {
		panic("module: empty symbol name")
	}
	if check == nil {
		panic("module: nil check function for " + name)
	}
	if emit == nil {
		panic("module: nil emit function for " + name)
	}
	symbol := &funcSymbol{name: name, check: check, emit: emit}
	if value == nil {
		return symbol
	}
	return &valueSymbol{funcSymbol: symbol, value: value}
}

// NewNative builds a native Module from its symbols, preserving registration
// order. Duplicate names panic because silently replacing extension behavior is
// a configuration error.
func NewNative(name string, symbols ...Symbol) Module {
	if name == "" {
		panic("module: empty module name")
	}
	m := &nativeModule{
		name:    name,
		symbols: make(map[string]Symbol, len(symbols)),
		names:   make([]string, 0, len(symbols)),
	}
	for _, symbol := range symbols {
		if symbol == nil {
			panic("module: nil symbol in " + name)
		}
		symbolName := symbol.Name()
		if _, exists := m.symbols[symbolName]; exists {
			panic(fmt.Sprintf("module: duplicate symbol %s.%s", name, symbolName))
		}
		m.symbols[symbolName] = symbol
		m.names = append(m.names, symbolName)
	}
	return m
}

func (s *funcSymbol) Name() string { return s.name }

func (s *funcSymbol) Check(c Checker, args []ast.Expr, pos token.Pos) types.Type {
	return s.check(c, args, pos)
}

func (s *funcSymbol) Emit(e Emitter, args []ast.Expr) { s.emit(e, args) }

func (s *valueSymbol) Value(r Runtime) vmtypes.Value { return s.value(r) }

func (m *nativeModule) Name() string { return m.name }

func (m *nativeModule) Symbol(name string) (Symbol, bool) {
	s, ok := m.symbols[name]
	return s, ok
}

func (m *nativeModule) Names() []string {
	return append([]string(nil), m.names...)
}
