// Package module defines the module system shared by the compiler and the
// native-module packages (builtins, operator, and future stdlib modules).
//
// A Module exports named Symbols. Native modules implement their symbols in Go
// (inline bytecode lowering and/or host functions); source modules are compiled
// from minipy. Symbols interact with the compiler only through the narrow
// Checker, Emitter, and Runtime interfaces, so native-module packages never
// depend on concrete compiler internals.
package module

import (
	"io"

	"github.com/siyul-park/minipy/ast"
	"github.com/siyul-park/minipy/token"
	"github.com/siyul-park/minipy/types"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/interp"
	vmtypes "github.com/siyul-park/minivm/types"
)

// Module is a named unit that exports symbols. Both native modules (implemented
// in Go) and source modules (compiled from minipy) satisfy it.
type Module interface {
	Name() string
	Symbol(name string) (Symbol, bool)
	Names() []string
}

// Symbol is one exported callable of a module. It is argument-list shaped rather
// than call-expression shaped so a single symbol serves both `f(a, b)` calls and
// `a op b` operator syntax.
type Symbol interface {
	Name() string
	Check(c Checker, args []ast.Expr, pos token.Pos) types.Type
	Emit(e Emitter, args []ast.Expr)
	Value(r Runtime) vmtypes.Value
}

// Checker is the type-checking surface a Symbol may use during static analysis.
type Checker interface {
	// Check type-checks a sub-expression and returns its type.
	Check(ast.Expr) types.Type
	// Type returns the already-recorded type of an expression.
	Type(ast.Expr) types.Type
	// SetType records the resolved type of an expression.
	SetType(ast.Expr, types.Type)
	// ResolveType interprets an expression as a type annotation.
	ResolveType(ast.Expr) types.Type
	// Error reports a static error.
	Error(pos token.Pos, code token.Code, format string, args ...any)
}

// Emitter is the code-generation surface a Symbol may use during lowering.
type Emitter interface {
	// Emit appends a single instruction.
	Emit(op instr.Opcode, operands ...uint64)
	// Expr lowers a sub-expression, leaving its value on the stack.
	Expr(ast.Expr)
	// Type returns the recorded type of an expression.
	Type(ast.Expr) types.Type
	// ConstGet pushes a constant value.
	ConstGet(v vmtypes.Value)
	// TypeIndex interns a runtime type and returns its index.
	TypeIndex(t types.Type) uint64
	// CallHost emits a call to a value-returning host function.
	CallHost(fn *interp.HostFunction)
	// CallHostVoid emits a call to a void host function.
	CallHostVoid(fn *interp.HostFunction)
	// Host returns the runtime-bound host function for a native symbol.
	Host(module, symbol string) *interp.HostFunction
	// Label allocates a fresh branch target.
	Label() instr.Label
	// Bind binds a label to the current position.
	Bind(l instr.Label)
	// Br emits an unconditional branch.
	Br(l instr.Label)
	// BrIf emits a conditional branch consuming the top of stack.
	BrIf(l instr.Label)
	// Tmp reserves a temporary slot and returns its index.
	Tmp() int
}

// Runtime is the execution-time surface a Symbol may use when producing its
// runtime value (for example a host function bound to the program's output).
type Runtime interface {
	Out() io.Writer
}
