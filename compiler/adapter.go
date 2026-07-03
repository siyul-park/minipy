package compiler

import (
	"io"

	"github.com/siyul-park/minipy/ast"
	"github.com/siyul-park/minipy/module"
	"github.com/siyul-park/minipy/token"
	"github.com/siyul-park/minipy/types"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/interp"
	vmtypes "github.com/siyul-park/minivm/types"
)

// This file adapts the concrete checker, Compiler, and nativeRuntime to the
// narrow module.Checker, module.Emitter, and module.Runtime interfaces so native
// modules can drive type-checking and code generation without depending on
// compiler internals.

var (
	_ module.Checker = (*checker)(nil)
	_ module.Emitter = (*Compiler)(nil)
	_ module.Runtime = (*nativeRuntime)(nil)
)

// Check type-checks a sub-expression and returns its type.
func (c *checker) Check(e ast.Expr) types.Type { return c.expr(e) }

// Type returns the already-recorded type of an expression.
func (c *checker) Type(e ast.Expr) types.Type { return c.types[e] }

// SetType records the resolved type of an expression.
func (c *checker) SetType(e ast.Expr, t types.Type) { c.types[e] = t }

// ResolveType interprets an expression as a type annotation.
func (c *checker) ResolveType(e ast.Expr) types.Type { return c.resolveType(e) }

// Error reports a static error.
func (c *checker) Error(pos token.Pos, code token.Code, format string, args ...any) {
	c.errs.Add(pos, code, format, args...)
}

// Emit appends a single instruction.
func (c *Compiler) Emit(op instr.Opcode, operands ...uint64) { c.emit(op, operands...) }

// Expr lowers a sub-expression, leaving its value on the stack.
func (c *Compiler) Expr(e ast.Expr) { c.expr(e) }

// Type returns the recorded type of an expression.
func (c *Compiler) Type(e ast.Expr) types.Type { return c.types[e] }

// ConstGet pushes a constant value.
func (c *Compiler) ConstGet(v vmtypes.Value) { c.constGet(v) }

// TypeIndex interns a runtime type and returns its index.
func (c *Compiler) TypeIndex(t types.Type) uint64 { return c.typeIndex(t) }

// CallHost emits a call to a value-returning host function.
func (c *Compiler) CallHost(fn *interp.HostFunction) { c.callHost(fn) }

// CallHostVoid emits a call to a void host function.
func (c *Compiler) CallHostVoid(fn *interp.HostFunction) { c.callHostVoid(fn) }

// Host returns the runtime-bound host function for a native symbol.
func (c *Compiler) Host(module, symbol string) *interp.HostFunction {
	return c.nativeHost(module, symbol)
}

// Label allocates a fresh branch target.
func (c *Compiler) Label() instr.Label { return c.label() }

// Bind binds a label to the current position.
func (c *Compiler) Bind(l instr.Label) { c.bind(l) }

// Br emits an unconditional branch.
func (c *Compiler) Br(l instr.Label) { c.br(l) }

// BrIf emits a conditional branch consuming the top of stack.
func (c *Compiler) BrIf(l instr.Label) { c.brIf(l) }

// Tmp reserves a temporary slot and returns its index.
func (c *Compiler) Tmp() int { return c.tmp() }

// Out returns the writer bound to native symbols' runtime values.
func (rt *nativeRuntime) Out() io.Writer { return rt.out }
