package compiler

import (
	"errors"
	"fmt"
	"math"

	"github.com/siyul-park/minipy/ast"
	"github.com/siyul-park/minipy/hostabi"
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

// tryRegion is a protected region awaiting its entry depth, which is only
// final once the frame it belongs to has finished lowering (see flushTries).
type tryRegion struct {
	start, end, catch instr.Label
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
	alt       bool // '#': 0b/0o/0x prefix on b/o/x/X
	group     byte // ',' or '_' thousands separator, or 0
	width     int
	precision int  // -1 when omitted
	typ       byte // 'd' 'f' 's' ... or 0
}

const omittedSliceBound = math.MinInt64

var (
	ellipsisValue      = vmtypes.NewStruct(types.Ellipsis.VM().(*vmtypes.StructType))
	errListIndexValue  = errors.New("list.index value not found")
	errListRemoveValue = errors.New("list.remove(x): x not in list")
	errListSliceLength = errors.New("list slice assignment length mismatch")
	errExtendedSlice   = errors.New("extended slice assignment is not supported")
	errSliceStep       = errors.New("slice step cannot be zero")
	errDictKeyError    = errors.New("KeyError")
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
	prog    *program.Builder
	code    target
	reg     *module.Registry
	native  *nativeRuntime
	dynamic bool
	names   []string
	consts  []vmtypes.Value
	scratch []vmtypes.Type
	base    int

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
	nameNative map[*ast.Name]module.Symbol
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
	boxed map[*local]bool

	// scratch-slot reuse: protected disables it for a frame containing a try
	// region, free lists the released pool indices per kind, live the indices
	// the open scopes hold, and open the live-stack mark each scope was entered
	// at.
	protected bool
	free      map[vmtypes.Kind][]int
	live      []int
	open      []int

	// control-flow stacks
	loops   []loopLabels
	finally []finallyFrame
	excepts []int
	tries   []tryRegion

	err error
}

// newLowerer creates a lowerer over a fresh builder, seeded with the checked
// module's symbol tables. Compiler.Compile calls this once per Compile call.
func newLowerer(b *program.Builder, checked *checkedProgram, native *nativeRuntime) *lowerer {
	c := &lowerer{
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
		nameNative: checked.nameNative,
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
		boxed:      map[*local]bool{},
	}
	c.names = make([]string, len(checked.globals))
	for name, global := range checked.globals {
		c.names[global.index] = name
	}
	return c
}

// lower emits every top-level statement of entry, declares the global slot
// table, and assembles the finished (unoptimized, unverified) program.
func (c *lowerer) lower() (*program.Program, error) {
	c.protected = containsTry(c.entry.ast.Body)
	c.module(c.entry)
	if c.err != nil {
		return nil, c.err
	}
	c.flushTries()
	c.prog.Locals(c.scratch...)
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
	if len(operands) > 0 {
		slot := int(operands[0])
		switch op {
		case instr.GLOBAL_GET:
			c.loadSlot(slot)
			return
		case instr.GLOBAL_SET:
			c.storeSlot(slot)
			return
		case instr.GLOBAL_TEE:
			c.teeSlot(slot)
			return
		}
	}
	c.code.emit(op, operands...)
}

func (c *lowerer) loadSlot(slot int) {
	if slot >= len(c.names) {
		c.code.emit(instr.LOCAL_GET, uint64(slot-len(c.names)))
		return
	}
	if !c.dynamic {
		c.code.emit(instr.GLOBAL_GET, uint64(slot))
		return
	}
	name := c.names[slot]
	if name == "" {
		c.fail(fmt.Errorf("load dynamic slot %d: missing name", slot))
		return
	}
	c.loadName(name)
}

func (c *lowerer) storeSlot(slot int) {
	if slot >= len(c.names) {
		c.code.emit(instr.LOCAL_SET, uint64(slot-len(c.names)))
		return
	}
	if !c.dynamic {
		c.code.emit(instr.GLOBAL_SET, uint64(slot))
		return
	}
	name := c.names[slot]
	if name == "" {
		c.fail(fmt.Errorf("store dynamic slot %d: missing name", slot))
		return
	}
	c.storeName(name)
}

func (c *lowerer) teeSlot(slot int) {
	if slot >= len(c.names) {
		c.code.emit(instr.LOCAL_TEE, uint64(slot-len(c.names)))
		return
	}
	if !c.dynamic {
		c.code.emit(instr.GLOBAL_TEE, uint64(slot))
		return
	}
	c.code.emit(instr.DUP)
	c.storeSlot(slot)
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

// tryRegion records a protected region. Its entry depth is the frame's total
// local count, which is not final until the whole body is lowered — scratch
// slots are discovered while emitting — and the builder captures the depth at
// declaration time, so the region is buffered here and declared by flushTries
// once the frame closes.
func (c *lowerer) tryRegion(start, end, catch instr.Label) {
	if c.failed() {
		return
	}
	c.tries = append(c.tries, tryRegion{start: start, end: end, catch: catch})
}

// flushTries declares every buffered region in recording order, which is
// innermost-first: an inner region completes while the outer one is still
// lowering its body.
func (c *lowerer) flushTries() {
	if c.failed() {
		return
	}
	depth := c.frameDepth()
	for _, r := range c.tries {
		c.code.try(r.start, r.end, r.catch, depth)
	}
	c.tries = nil
}

// frameDepth is the frame's total local count: its named region plus every
// scratch slot reserved while lowering it.
func (c *lowerer) frameDepth() int {
	return c.frameBase() + len(c.scratch)
}

func (c *lowerer) constGet(v vmtypes.Value) {
	if c.dynamic {
		c.emit(instr.UPVAL_GET, uint64(c.constantBase()+c.constant(v)))
		return
	}
	c.emit(instr.CONST_GET, uint64(c.prog.Const(v)))
}

// constant interns a dynamic-code constant into the capture list. Name strings
// are the only constants that repeat — every other constant is built fresh per
// use — so interning them keeps the capture list from growing per occurrence.
func (c *lowerer) constant(v vmtypes.Value) int {
	if name, ok := v.(vmtypes.String); ok {
		for index, existing := range c.consts {
			if existing == name {
				return index
			}
		}
	}
	index := len(c.consts)
	c.consts = append(c.consts, v)
	return index
}

func (c *lowerer) constantBase() int {
	base := 2
	if c.current != nil {
		base += len(c.current.capOrder)
	}
	return base
}

func (c *lowerer) typeIndex(t types.Type) uint64 {
	return uint64(c.prog.Type(t.VM()))
}

// tmp reserves a scratch slot of type t in the frame currently being lowered
// and returns its slot id. Scratch slots are frame locals, not module globals:
// a global is shared by every activation, so a recursive call would clobber the
// scratch a suspended activation still holds. Slot ids continue past the named
// region so emit can tell the two apart; frameBase places them after the
// frame's own params and named locals.
//
// The declared type matters: LOCAL_GET pushes the slot's declared kind, so a
// slot read back for scalar arithmetic must be declared with that kind or
// verification rejects the program.
func (c *lowerer) tmp(t vmtypes.Type) int {
	declared := slotType(t)
	kind := declared.Kind()
	idx, reused := c.reuse(kind)
	if !reused {
		idx = len(c.scratch)
		c.scratch = append(c.scratch, declared)
	}
	c.live = append(c.live, idx)
	return len(c.names) + c.frameBase() + idx
}

// reuse takes a released slot of the given kind, if one is available. Kind is
// the only thing a slot's declaration says, so any released slot of the same
// kind serves.
func (c *lowerer) reuse(kind vmtypes.Kind) (int, bool) {
	pool := c.free[kind]
	if len(pool) == 0 {
		return 0, false
	}
	idx := pool[len(pool)-1]
	c.free[kind] = pool[:len(pool)-1]
	return idx, true
}

// enterScratch opens a scratch scope; leaveScratch releases every slot reserved
// since the matching enterScratch back to the free lists. A scratch slot lives
// exactly as long as the scope that reserved it, so scopes nest with the
// lowering: a statement's slots stay live while its nested statements run, and
// sibling statements reuse the same pool entries instead of growing the frame.
// reusable reports whether slots reserved by the statement about to be lowered
// may be released when it ends. A frame containing a protected region opts out
// entirely: entering a handler resumes at a point where the slots the guarded
// code reserved are still live, and the handler's entry depth is a single
// frame-wide number, so there is no point at which a released slot is provably
// dead. Such a frame keeps one slot per site, as before.
func (c *lowerer) reusable() bool {
	return !c.protected
}

func (c *lowerer) enterScratch() {
	c.open = append(c.open, len(c.live))
}

func (c *lowerer) leaveScratch() {
	if len(c.open) == 0 {
		return
	}
	mark := c.open[len(c.open)-1]
	c.open = c.open[:len(c.open)-1]
	if c.free == nil {
		c.free = map[vmtypes.Kind][]int{}
	}
	for _, idx := range c.live[mark:] {
		kind := c.scratch[idx].Kind()
		c.free[kind] = append(c.free[kind], idx)
	}
	c.live = c.live[:mark]
}

// frameBase is the first LOCAL_* index available to scratch slots: the params
// and named locals of the function being lowered, or the captured namespace
// slots of a dynamic unit. The module entry frame has neither, so it is 0.
func (c *lowerer) frameBase() int { return c.base }

// slotType canonicalizes a value's type to the one declaration a scratch slot
// keeps per kind. Only the kind is observable through a slot, and erasing
// concrete reference types keeps calls through a scratch slot as indeterminate
// to the verifier as they are today.
func slotType(t vmtypes.Type) vmtypes.Type {
	switch t.Kind() {
	case vmtypes.KindI1:
		return vmtypes.TypeI1
	case vmtypes.KindI8:
		return vmtypes.TypeI8
	case vmtypes.KindI32:
		return vmtypes.TypeI32
	case vmtypes.KindI64:
		return vmtypes.TypeI64
	case vmtypes.KindF32:
		return vmtypes.TypeF32
	case vmtypes.KindF64:
		return vmtypes.TypeF64
	default:
		return vmtypes.TypeRef
	}
}

// globalTable declares a slot type for every module global so the interpreter
// can size its global table and GLOBAL_* passes verification. GLOBAL_GET pushes
// the declared type, so each global carries its checked type; a dynamic type
// and a function binding both stay references, which is what their values are.
// Scratch temporaries are not here at all — they are frame locals (see tmp).
func (c *lowerer) globalTable() []vmtypes.Type {
	table := make([]vmtypes.Type, len(c.names))
	for i := range table {
		table[i] = vmtypes.TypeRef
	}
	for _, g := range c.globals {
		if g.index < len(table) && g.typ != types.Invalid {
			table[g.index] = slotType(hostabi.VMParamType(g.typ))
		}
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
	c.constGet(function)
	c.emit(instr.CALL)
}

// callHostVoid emits a call to a void host function, padding a REF_NULL so the
// expression still leaves exactly one value on the stack.
func (c *lowerer) callHostVoid(function *interp.HostFunction) {
	if function == nil || c.failed() {
		return
	}
	c.constGet(function)
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

// ConstGet emits a CONST_GET instruction for a constant pool value.
func (c *lowerer) ConstGet(v vmtypes.Value) { c.constGet(v) }

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

// slotFor reserves a scratch slot able to hold the checked value of e.
func (c *lowerer) slotFor(e ast.Expr) int {
	t, ok := c.types[e]
	if !ok || t == nil {
		return c.tmp(vmtypes.TypeRef)
	}
	return c.tmp(hostabi.VMParamType(t))
}

// Tmp reserves a scratch slot for a value of minivm type t.
func (c *lowerer) Tmp(t vmtypes.Type) int { return c.tmp(t) }

var _ module.Emitter = (*lowerer)(nil)
