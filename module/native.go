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

// NativeSymbol is a native callable assembled from checker, emitter, and
// optional runtime-value behaviors.
type NativeSymbol struct {
	name  string
	check CheckFunc
	emit  EmitFunc
	value ValueFunc
}

// NativeConstant is a native symbol that represents a module-level constant
// value. It satisfies both Symbol and ConstantSymbol. Calling it directly is
// a static error; its Emit function emits the constant's inline value.
type NativeConstant struct {
	name string
	typ  types.Type
	emit EmitFunc
}

// NativeModule is an immutable in-memory module whose symbol order is the
// registration order.
type NativeModule struct {
	name    string
	symbols map[string]Symbol
	names   []string
}

var (
	_ RuntimeSymbol  = (*NativeSymbol)(nil)
	_ Module         = (*NativeModule)(nil)
	_ ConstantSymbol = (*NativeConstant)(nil)
)

// NewSymbol builds a native symbol from its type-check, emit, and optional
// runtime-value behaviors.
func NewSymbol(name string, check CheckFunc, emit EmitFunc, value ValueFunc) *NativeSymbol {
	if name == "" {
		panic("module: empty symbol name")
	}
	if check == nil {
		panic("module: nil check function for " + name)
	}
	if emit == nil {
		panic("module: nil emit function for " + name)
	}
	return &NativeSymbol{name: name, check: check, emit: emit, value: value}
}

// NewConstant builds a native constant symbol with a static type and an emit
// function that pushes the constant's value onto the stack.
func NewConstant(name string, typ types.Type, emit EmitFunc) *NativeConstant {
	if name == "" {
		panic("module: empty constant name")
	}
	if typ == nil {
		panic("module: nil type for constant " + name)
	}
	if emit == nil {
		panic("module: nil emit function for constant " + name)
	}
	return &NativeConstant{name: name, typ: typ, emit: emit}
}

// NewNative builds a native module from its symbols, preserving registration
// order. Invalid definitions panic because this constructor is used to declare
// static native catalogues during startup.
func NewNative(name string, symbols ...Symbol) *NativeModule {
	if name == "" {
		panic("module: empty module name")
	}
	m := &NativeModule{
		name:    name,
		symbols: make(map[string]Symbol, len(symbols)),
		names:   make([]string, 0, len(symbols)),
	}
	for _, symbol := range symbols {
		if symbol == nil {
			panic("module: nil symbol in " + name)
		}
		symbolName := symbol.Name()
		if symbolName == "" {
			panic("module: empty symbol name in " + name)
		}
		if _, exists := m.symbols[symbolName]; exists {
			panic(fmt.Sprintf("module: duplicate symbol %s.%s", name, symbolName))
		}
		m.symbols[symbolName] = symbol
		m.names = append(m.names, symbolName)
	}
	return m
}

// Name returns the registered symbol name.
func (s *NativeSymbol) Name() string { return s.name }

// Check applies the symbol's static call rule.
func (s *NativeSymbol) Check(c Checker, args []ast.Expr, pos token.Pos) types.Type {
	return s.check(c, args, pos)
}

// Emit lowers a checked call to the symbol.
func (s *NativeSymbol) Emit(e Emitter, args []ast.Expr) { s.emit(e, args) }

// RuntimeValue returns the symbol's runtime value and whether one is present.
func (s *NativeSymbol) RuntimeValue(r Runtime) (vmtypes.Value, bool) {
	if s.value == nil {
		return nil, false
	}
	value := s.value(r)
	return value, value != nil
}

// Name returns the registered module name.
func (m *NativeModule) Name() string { return m.name }

// Symbol looks up a symbol by name.
func (m *NativeModule) Symbol(name string) (Symbol, bool) {
	symbol, ok := m.symbols[name]
	return symbol, ok
}

// Names returns symbol names in registration order.
func (m *NativeModule) Names() []string {
	return append([]string(nil), m.names...)
}

// Name returns the registered constant name.
func (c *NativeConstant) Name() string { return c.name }

// Check rejects direct calls to a constant symbol.
func (c *NativeConstant) Check(ch Checker, args []ast.Expr, pos token.Pos) types.Type {
	ch.Error(pos, token.TypeMismatch, "%s is not callable", c.name)
	return types.Invalid
}

// Emit emits the constant's inline value.
func (c *NativeConstant) Emit(e Emitter, args []ast.Expr) { c.emit(e, args) }

// ConstantType returns the static type of the constant.
func (c *NativeConstant) ConstantType() types.Type { return c.typ }
