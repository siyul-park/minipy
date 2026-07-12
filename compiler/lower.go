package compiler

import (
	"errors"
	"fmt"
	"math"

	"github.com/siyul-park/minipy/ast"
	"github.com/siyul-park/minipy/module"
	"github.com/siyul-park/minipy/types"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/interp"
	"github.com/siyul-park/minivm/program"
	vmtypes "github.com/siyul-park/minivm/types"
)

// loopLabels are the branch targets for the loop currently being lowered: cont
// for `continue` (re-test for while, the increment step for range-for) and brk
// for `break` (past any else block).
type loopLabels struct {
	cont instr.Label
	brk  instr.Label
}

type finallyFrame struct {
	emit func()
}

type target struct {
	emit  func(instr.Opcode, ...uint64)
	label func() instr.Label
	bind  func(instr.Label)
	br    func(instr.Label)
	brIf  func(instr.Label)
	try   func(instr.Label, instr.Label, instr.Label, int)
}

// formatSpec is a parsed Python mini format spec:
// [[fill]align][sign]['0'][width]['.'precision][type].
type formatSpec struct {
	fill      byte
	align     byte // '<' '>' '^' '=' or 0
	sign      byte // '+' '-' ' ' or 0
	zero      bool
	width     int
	precision int  // -1 when omitted
	typ       byte // 'd' 'f' 's' ... or 0
}

const omittedSliceBound = math.MinInt64

var (
	ellipsisValue      = vmtypes.NewStruct(types.Ellipsis.VM().(*vmtypes.StructType))
	errListIndexValue  = errors.New("list.index value not found")
	errListSliceLength = errors.New("list slice assignment length mismatch")
)

// trapClasses maps the VM trap codes minipy classifies into dedicated
// exception types, with the fixed message each bare sentinel error in
// minivm's interp package renders as. Anything else (host-function errors,
// future trap kinds) falls through to excInstance for its dynamic message.
var trapClasses = []struct {
	code    vmtypes.ErrorCode
	class   string
	message string
}{
	{interp.TrapCodeDivideByZero, "ZeroDivisionError", "divide by zero"},
	{interp.TrapCodeIndexOutOfRange, "IndexError", "index out of range"},
	{interp.TrapCodeTypeMismatch, "TypeError", "type mismatch"},
}

func mainTarget(b *program.Builder) target {
	return target{
		emit:  func(op instr.Opcode, operands ...uint64) { b.Emit(op, operands...) },
		label: b.Label,
		bind:  func(l instr.Label) { b.Bind(l) },
		br:    func(l instr.Label) { b.Br(l) },
		brIf:  func(l instr.Label) { b.BrIf(l) },
		try:   func(start, end, catch instr.Label, depth int) { b.Try(start, end, catch, depth) },
	}
}

func fnTarget(b *vmtypes.FunctionBuilder) target {
	return target{
		emit:  func(op instr.Opcode, operands ...uint64) { b.Emit(instr.New(op, operands...)) },
		label: b.Label,
		bind:  func(l instr.Label) { b.Bind(l) },
		br:    func(l instr.Label) { b.Br(l) },
		brIf:  func(l instr.Label) { b.BrIf(l) },
		try:   func(start, end, catch instr.Label, depth int) { b.Try(start, end, catch, depth) },
	}
}

// lowerer lowers a checked module into a minivm program. It is created fresh
// for each Compiler.Compile call from the checker's output and is also copied
// (via child) to lower nested function and specialization bodies.
type lowerer struct {
	// infrastructure
	prog   *program.Builder
	code   target
	reg    *module.Registry
	native *nativeRuntime

	// checker-produced metadata
	entry      *moduleInfo
	types      map[ast.Expr]types.Type
	globals    map[string]*global
	functions  map[string]*function
	classes    map[string]*class
	aliasDecls map[*ast.AnnAssign]bool
	modules    map[string]*moduleInfo
	mod        *moduleInfo
	attrSym    map[*ast.Attribute]string
	attrMod    map[*ast.Attribute]string
	attrNative map[*ast.Attribute]module.Symbol
	lambdas    map[*ast.LambdaExpr]*function
	genExprs   map[*ast.GeneratorExp]*function
	callSpec   map[*ast.CallExpr]*specialization
	callArgs   map[*ast.CallExpr][]ast.Expr
	lenDunder  map[*ast.CallExpr]bool

	// lowering-owned phase state
	emitted  map[*moduleInfo]bool
	specs    map[*specialization]int
	building map[*specialization]bool

	// current-function state
	locals  map[string]*local
	current *function

	// allocation counters
	temps map[string]int
	next  int
	boxed map[*local]bool

	// control-flow stacks
	loops   []loopLabels
	finally []finallyFrame
	excepts []int

	err error
}

// newLowerer creates a lowerer over a fresh builder, seeded with the checked
// module's symbol tables. Compiler.Compile calls this once per Compile call.
func newLowerer(b *program.Builder, checked *checkedProgram, native *nativeRuntime) *lowerer {
	return &lowerer{
		prog:       b,
		code:       mainTarget(b),
		entry:      checked.entry,
		types:      checked.types,
		globals:    checked.globals,
		functions:  checked.functions,
		classes:    checked.classes,
		aliasDecls: checked.aliasDecls,
		modules:    checked.modules,
		attrSym:    checked.attrSym,
		attrMod:    checked.attrMod,
		attrNative: checked.attrNative,
		reg:        checked.reg,
		lambdas:    checked.lambdas,
		genExprs:   checked.genExprs,
		callSpec:   checked.callSpec,
		callArgs:   checked.callArgs,
		lenDunder:  checked.lenDunder,
		emitted:    map[*moduleInfo]bool{},
		specs:      map[*specialization]int{},
		building:   map[*specialization]bool{},
		temps:      map[string]int{},
		native:     native,
		next:       len(checked.globals),
		boxed:      map[*local]bool{},
	}
}

// lower emits every top-level statement of entry, declares the global slot
// table, and assembles the finished (unoptimized, unverified) program.
func (c *lowerer) lower() (*program.Program, error) {
	c.module(c.entry)
	if c.err != nil {
		return nil, c.err
	}
	c.prog.Globals(c.globalTable()...)
	prog, err := c.prog.Build()
	if err != nil {
		return nil, fmt.Errorf("assemble program: %w", err)
	}
	return prog, nil
}

// fail records err as the lowering failure if none has been recorded yet.
// Only the first failure is kept.
func (c *lowerer) fail(err error) {
	if c.err == nil {
		c.err = err
	}
}

// failed reports whether a lowering failure has already been recorded.
func (c *lowerer) failed() bool {
	return c.err != nil
}

func (c *lowerer) emit(op instr.Opcode, operands ...uint64) {
	if c.failed() {
		return
	}
	c.code.emit(op, operands...)
}

func (c *lowerer) label() instr.Label {
	if c.failed() {
		return 0
	}
	return c.code.label()
}

func (c *lowerer) bind(l instr.Label) {
	if c.failed() {
		return
	}
	c.code.bind(l)
}

func (c *lowerer) br(l instr.Label) {
	if c.failed() {
		return
	}
	c.code.br(l)
}

func (c *lowerer) brIf(l instr.Label) {
	if c.failed() {
		return
	}
	c.code.brIf(l)
}

func (c *lowerer) tryRegion(start, end, catch instr.Label, depth int) {
	if c.failed() {
		return
	}
	c.code.try(start, end, catch, depth)
}

func (c *lowerer) tryDepth() int {
	if c.current == nil {
		return 0
	}
	return len(c.current.params) + len(c.current.order)
}

func (c *lowerer) constGet(v vmtypes.Value) {
	c.emit(instr.CONST_GET, uint64(c.prog.Const(v)))
}

func (c *lowerer) typeIndex(t types.Type) uint64 {
	return uint64(c.prog.Type(t.VM()))
}

func (c *lowerer) tmp() int {
	idx := c.next
	c.next++
	return idx
}

// globalTable declares a fixed slot type for every global the module uses so
// the interpreter can size its global table and GLOBAL_* passes verification.
// Every slot is a reference: scratch slots from tmp carry no static type and are
// reused for values of different kinds, and a reference slot round-trips any
// boxed value under the interpreter's retain rules, so one uniform declaration
// stays correct where a per-slot precise kind could not.
func (c *lowerer) globalTable() []vmtypes.Type {
	table := make([]vmtypes.Type, c.next)
	for i := range table {
		table[i] = vmtypes.TypeRef
	}
	return table
}

// module lowers every top-level statement. The entry function terminates by
// running off the end of its code (the VM has no entry-frame RETURN), so a
// trailing NOP gives any control-flow merge label bound at the very end a valid
// landing instruction — branch targets must stay within the code (analysis
// rejects a jump to len(code)).
func (c *lowerer) module(mod *moduleInfo) {
	if c.failed() {
		return
	}
	c.buildCallSpecs(c.callSpec)
	c.emitModule(mod)
	c.emit(instr.NOP)
}

func (c *lowerer) emitModule(mod *moduleInfo) {
	if c.failed() {
		return
	}
	if mod == nil || c.emitted[mod] || mod.native {
		return
	}
	c.emitted[mod] = true
	prev := c.mod
	c.mod = mod
	c.block(mod.ast.Body)
	c.mod = prev
}

// block lowers a statement sequence (a module body or a compound block).
func (c *lowerer) block(body []ast.Stmt) {
	if c.failed() {
		return
	}
	for _, s := range body {
		c.stmt(s)
		if iff, ok := s.(*ast.If); ok && len(iff.Orelse) == 0 && blockReturns(iff.Body) {
			if known, truth := c.truth(iff.Cond); known && truth {
				return
			}
		}
	}
}

// truth mirrors checker.truth for codegen. Specialized
// function bodies may leave impossible branches unchecked, so lowering must
// prune those same branches instead of compiling expressions with no type table
// entries.
func (c *lowerer) truth(cond ast.Expr) (known bool, truth bool) {
	return fold(cond, c.typ, func(e ast.Expr) types.Type { return c.types[e] })
}

// callHost emits a call to a value-returning host function.
func (c *lowerer) callHost(function *interp.HostFunction) {
	if function == nil || c.failed() {
		return
	}
	c.emit(instr.CONST_GET, uint64(c.prog.Const(function)))
	c.emit(instr.CALL)
}

// callHostVoid emits a call to a void host function, padding a REF_NULL so the
// expression still leaves exactly one value on the stack.
func (c *lowerer) callHostVoid(function *interp.HostFunction) {
	if function == nil || c.failed() {
		return
	}
	c.emit(instr.CONST_GET, uint64(c.prog.Const(function)))
	c.emit(instr.CALL)
	c.emit(instr.REF_NULL)
}

// Emit appends a single instruction.
func (c *lowerer) Emit(op instr.Opcode, operands ...uint64) { c.emit(op, operands...) }

// Expr lowers a sub-expression, leaving its value on the stack.
func (c *lowerer) Expr(e ast.Expr) { c.expr(e) }

// Type returns the recorded type of an expression.
func (c *lowerer) Type(e ast.Expr) types.Type { return c.types[e] }

// TypeIndex interns a runtime type and returns its index.
func (c *lowerer) TypeIndex(t types.Type) uint64 { return c.typeIndex(t) }

// CallHost emits a call to a value-returning host function.
func (c *lowerer) CallHost(fn *interp.HostFunction) { c.callHost(fn) }

// CallHostVoid emits a call to a void host function.
func (c *lowerer) CallHostVoid(fn *interp.HostFunction) { c.callHostVoid(fn) }

// Host returns the runtime-bound host function for a native symbol.
func (c *lowerer) Host(module, symbol string) *interp.HostFunction {
	return c.nativeHost(module, symbol)
}

// Runtime returns the runtime resources bound to native symbols.
func (c *lowerer) Runtime() module.Runtime { return c.native }

// Label allocates a fresh branch target.
func (c *lowerer) Label() instr.Label { return c.label() }

// Bind binds a label to the current position.
func (c *lowerer) Bind(l instr.Label) { c.bind(l) }

// Br emits an unconditional branch.
func (c *lowerer) Br(l instr.Label) { c.br(l) }

// BrIf emits a conditional branch consuming the top of stack.
func (c *lowerer) BrIf(l instr.Label) { c.brIf(l) }

// Tmp reserves a temporary slot and returns its index.
func (c *lowerer) Tmp() int { return c.tmp() }

var _ module.Emitter = (*lowerer)(nil)
