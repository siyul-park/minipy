package module

import (
	"github.com/siyul-park/minipy/ast"
	"github.com/siyul-park/minipy/token"
	"github.com/siyul-park/minipy/types"

	vmtypes "github.com/siyul-park/minivm/types"
)

// Intrinsic is the runtime value of a native symbol that has no first-class
// representation: it is inline-lowered or called directly, never used as a
// value. Its String form is diagnostic only.
type Intrinsic struct {
	Name string
}

// CheckFunc type-checks a native call given its argument expressions.
type CheckFunc func(c Checker, args []ast.Expr, pos token.Pos) types.Type

// EmitFunc lowers a native call given its argument expressions.
type EmitFunc func(e Emitter, args []ast.Expr)

// ValueFunc produces the runtime value of a native symbol.
type ValueFunc func(r Runtime) vmtypes.Value

// funcSymbol is a Symbol assembled from closures. Native-module packages may use
// it directly or provide their own Symbol implementations.
type funcSymbol struct {
	name  string
	check CheckFunc
	emit  EmitFunc
	value ValueFunc
}

var _ Symbol = (*funcSymbol)(nil)

// nativeModule is a Module backed by an in-memory symbol table.
type nativeModule struct {
	name    string
	symbols map[string]Symbol
	names   []string
}

var _ Module = (*nativeModule)(nil)

// NewSymbol builds a Symbol from its type-check, emit, and value behaviors. A nil
// value defaults to an Intrinsic marker keyed by the qualified symbol name.
func NewSymbol(module, name string, check CheckFunc, emit EmitFunc, value ValueFunc) Symbol {
	if value == nil {
		full := module + "." + name
		value = func(Runtime) vmtypes.Value { return Intrinsic{Name: full} }
	}
	return &funcSymbol{name: name, check: check, emit: emit, value: value}
}

// NewNative builds a native Module from its symbols, preserving their order for
// deterministic iteration.
func NewNative(name string, symbols ...Symbol) Module {
	m := &nativeModule{
		name:    name,
		symbols: make(map[string]Symbol, len(symbols)),
		names:   make([]string, 0, len(symbols)),
	}
	for _, s := range symbols {
		nm := s.Name()
		if _, ok := m.symbols[nm]; !ok {
			m.names = append(m.names, nm)
		}
		m.symbols[nm] = s
	}
	return m
}

func (v Intrinsic) Kind() vmtypes.Kind { return vmtypes.KindRef }
func (v Intrinsic) Type() vmtypes.Type { return vmtypes.TypeRef }
func (v Intrinsic) String() string     { return "<native " + v.Name + ">" }

func (s *funcSymbol) Name() string { return s.name }

func (s *funcSymbol) Check(c Checker, args []ast.Expr, pos token.Pos) types.Type {
	return s.check(c, args, pos)
}

func (s *funcSymbol) Emit(e Emitter, args []ast.Expr) { s.emit(e, args) }

func (s *funcSymbol) Value(r Runtime) vmtypes.Value { return s.value(r) }

func (m *nativeModule) Name() string { return m.name }

func (m *nativeModule) Symbol(name string) (Symbol, bool) {
	s, ok := m.symbols[name]
	return s, ok
}

func (m *nativeModule) Names() []string {
	return append([]string(nil), m.names...)
}
