package compiler

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/siyul-park/minipy/ast"
	"github.com/siyul-park/minipy/builtins"
	"github.com/siyul-park/minipy/hostabi"
	"github.com/siyul-park/minipy/module"
	"github.com/siyul-park/minipy/operator"
	"github.com/siyul-park/minipy/token"
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
	consts map[*interp.HostFunction]int

	// checker-produced metadata
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
func newLowerer(b *program.Builder, check *checker, native *nativeRuntime) *lowerer {
	return &lowerer{
		prog:       b,
		code:       mainTarget(b),
		types:      check.types,
		globals:    check.globals,
		functions:  check.functions,
		classes:    check.classes,
		aliasDecls: check.aliasDecls,
		modules:    check.modules,
		mod:        check.mod,
		attrSym:    check.attrSym,
		attrMod:    check.attrMod,
		attrNative: check.attrNative,
		reg:        check.reg,
		lambdas:    check.lambdas,
		genExprs:   check.genExprs,
		callSpec:   check.callSpec,
		callArgs:   check.callArgs,
		lenDunder:  check.lenDunder,
		temps:      map[string]int{},
		native:     native,
		consts:     map[*interp.HostFunction]int{},
		next:       len(check.globals),
		boxed:      map[*local]bool{},
	}
}

// lower emits every top-level statement of entry, declares the global slot
// table, and assembles the finished (unoptimized, unverified) program.
func (c *lowerer) lower(entry *moduleInfo) (*program.Program, error) {
	c.module(entry)
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
	if mod == nil || mod.emitted || mod.native {
		return
	}
	mod.emitted = true
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

func (c *lowerer) stmt(s ast.Stmt) {
	switch n := s.(type) {
	case *ast.AnnAssign:
		if c.aliasDecls[n] {
			return
		}
		if n.Value != nil {
			c.expr(n.Value)
			c.set(n.Target.Name)
		}
	case *ast.Assign:
		if name, ok := n.Target.(*ast.Name); ok {
			c.expr(n.Value)
			c.set(name.Name)
		} else {
			c.assignTarget(n.Target, n.Value)
		}
	case *ast.AugAssign:
		if name, ok := n.Target.(*ast.Name); ok {
			t := c.typ(name.Name)
			c.emitBinary(n.Op, t, c.types[n.Value],
				func() { c.get(name.Name) },
				func() { c.expr(n.Value) })
			c.set(name.Name)
		} else {
			c.augAssignAttribute(n)
		}
	case *ast.ExprStmt:
		c.expr(n.X)
		c.emit(instr.DROP)
	case *ast.If:
		c.emitIf(n)
	case *ast.While:
		c.emitWhile(n)
	case *ast.For:
		c.emitFor(n)
	case *ast.Function:
		c.functionStmt(n)
	case *ast.Class:
		c.classStmt()
	case *ast.Import:
		c.importStmt(n)
	case *ast.ImportFrom:
		c.importFromStmt(n)
	case *ast.Global, *ast.Nonlocal, *ast.TypeAlias:
		// scope declarations affect checking only
	case *ast.Return:
		c.returnStmt(n)
	case *ast.Yield:
		c.yield(n)
	case *ast.Break:
		c.inlineFinalizers()
		c.br(c.loops[len(c.loops)-1].brk)
	case *ast.Continue:
		c.inlineFinalizers()
		c.br(c.loops[len(c.loops)-1].cont)
	case *ast.Pass:
		// no-op
	case *ast.Delete:
		c.deleteStmt(n)
	case *ast.Assert:
		c.assertStmt(n)
	case *ast.Match:
		c.emitMatch(n)
	case *ast.Try:
		c.emitTry(n)
	case *ast.Raise:
		c.emitRaise(n)
	case *ast.With:
		c.emitWith(n)
	}
}

func (c *lowerer) importStmt(n *ast.Import) {
	for _, a := range n.Names {
		c.emitImportChain(a.Name)
	}
}

func (c *lowerer) importFromStmt(n *ast.ImportFrom) {
	if n.Level == 0 && n.Module == "__future__" {
		return
	}
	base := relativeBase(c.mod, n)
	if base == "" {
		return
	}
	c.emitImportChain(base)
	if len(n.Names) == 1 && n.Names[0].Name == "*" {
		return
	}
	for _, a := range n.Names {
		if sub := c.modules[base+"."+a.Name]; sub != nil {
			c.emitImportChain(sub.name)
		}
	}
}

func (c *lowerer) emitImportChain(name string) {
	parts := strings.Split(name, ".")
	for i := range parts {
		m := c.modules[strings.Join(parts[:i+1], ".")]
		c.emitModule(m)
	}
}

// deleteStmt lowers `del`. A deleted name is overwritten with minivm's
// uninitialized slot value (REF_NULL for ref kinds, the typed zero for scalars);
// dict items use MAP_DELETE, list items use the native ARRAY_DELETE (remove +
// shift) via emitArrayDelete, and attributes are zeroed in place.
func (c *lowerer) deleteStmt(n *ast.Delete) {
	for _, target := range n.Targets {
		switch t := target.(type) {
		case *ast.Name:
			c.emitZeroValue(c.typ(t.Name))
			c.set(t.Name)
		case *ast.Subscript:
			if slice, ok := t.Index.(*ast.Slice); ok {
				c.expr(t.X)
				c.sliceBound(slice.Lower)
				c.sliceBound(slice.Upper)
				c.sliceBound(slice.Step)
				c.callHost(c.listSliceDelete(c.types[t.X]))
				continue
			}
			switch c.types[t.X].(type) {
			case *types.Dict:
				c.expr(t.X)
				c.expr(t.Index)
				c.emit(instr.MAP_DELETE)
			case *types.List:
				c.expr(t.X)
				c.expr(t.Index)
				c.emitArrayDelete()
				c.emit(instr.DROP)
			}
		case *ast.Attribute:
			cls := c.types[t.X].(*types.Class)
			info := c.classes[cls.Name]
			idx := info.fieldIndex[t.Name]
			c.expr(t.X)
			c.emit(instr.I32_CONST, uint64(idx))
			c.emitZeroValue(info.fields[idx].typ)
			c.emit(instr.STRUCT_SET)
		}
	}
}

// assertStmt lowers `assert test[, msg]`: on a false test it builds an error
// payload and throws it, which unwinds to an uncaught runtime exception.
func (c *lowerer) assertStmt(n *ast.Assert) {
	c.expr(n.Test)
	ok := c.label()
	c.brIf(ok)
	if n.Msg != nil {
		c.expr(n.Msg)
	} else {
		c.constGet(vmtypes.String("AssertionError"))
	}
	c.emit(instr.I32_CONST, uint64(vmtypes.ErrorCodeNone))
	c.emit(instr.ERROR_NEW)
	c.emit(instr.THROW)
	c.bind(ok)
}

// emitMatch lowers `match`/`case` into a linear decision tree: the subject is
// evaluated once into a temp slot, then each case's pattern test branches to the
// next case on mismatch and falls through to the body on a full match.
func (c *lowerer) emitMatch(n *ast.Match) {
	subjSlot := c.tmp()
	c.expr(n.Subject)
	c.emit(instr.GLOBAL_SET, uint64(subjSlot))
	subjT := c.types[n.Subject]
	end := c.label()
	for _, cs := range n.Cases {
		next := c.label()
		c.emitPatternTest(cs.Pattern, subjSlot, subjT, next)
		if cs.Guard != nil {
			c.expr(cs.Guard)
			c.emit(instr.I32_EQZ)
			c.brIf(next)
		}
		c.block(cs.Body)
		c.br(end)
		c.bind(next)
	}
	c.bind(end)
}

func (c *lowerer) emitTry(n *ast.Try) {
	finalizer := c.finalizer(n.Finalbody)
	start := c.label()
	end := c.label()
	catch := c.label()
	after := c.label()

	c.bind(start)
	if finalizer != nil {
		c.finally = append(c.finally, finallyFrame{emit: finalizer})
	}
	c.block(n.Body)
	c.emit(instr.NOP)
	c.bind(end)
	c.block(n.Orelse)
	if finalizer != nil {
		c.finally = c.finally[:len(c.finally)-1]
		finalizer()
	}
	c.br(after)

	c.bind(catch)
	errSlot := c.tmp()
	c.emit(instr.GLOBAL_SET, uint64(errSlot))
	if len(n.Handlers) == 0 {
		if finalizer != nil {
			finalizer()
		}
		c.emit(instr.GLOBAL_GET, uint64(errSlot))
		c.emit(instr.THROW)
		c.bind(after)
		c.tryRegion(start, end, catch, c.tryDepth())
		return
	}
	instSlot := c.tmp()
	c.emitCaughtInstance(errSlot, instSlot)
	for _, h := range n.Handlers {
		next := c.label()
		if h.Type != nil {
			c.emitExceptionTest(instSlot, h.Type, next)
		}
		if h.Name != "" {
			c.emit(instr.GLOBAL_GET, uint64(instSlot))
			c.set(h.Name)
		}
		if finalizer != nil {
			c.finally = append(c.finally, finallyFrame{emit: finalizer})
		}
		c.excepts = append(c.excepts, errSlot)
		c.block(h.Body)
		c.excepts = c.excepts[:len(c.excepts)-1]
		if finalizer != nil {
			c.finally = c.finally[:len(c.finally)-1]
			finalizer()
		}
		c.br(after)
		c.bind(next)
	}
	if finalizer != nil {
		finalizer()
	}
	c.emit(instr.GLOBAL_GET, uint64(errSlot))
	c.emit(instr.THROW)
	c.bind(after)
	c.tryRegion(start, end, catch, c.tryDepth())
}

func (c *lowerer) emitTryFinally(body func(), finalizer func()) {
	start := c.label()
	end := c.label()
	catch := c.label()
	after := c.label()

	c.bind(start)
	c.finally = append(c.finally, finallyFrame{emit: finalizer})
	body()
	c.finally = c.finally[:len(c.finally)-1]
	c.emit(instr.NOP)
	c.bind(end)
	finalizer()
	c.br(after)

	c.bind(catch)
	errSlot := c.tmp()
	c.emit(instr.GLOBAL_SET, uint64(errSlot))
	finalizer()
	c.emit(instr.GLOBAL_GET, uint64(errSlot))
	c.emit(instr.THROW)

	c.bind(after)
	c.tryRegion(start, end, catch, c.tryDepth())
}

func (c *lowerer) finalizer(body []ast.Stmt) func() {
	if len(body) == 0 {
		return nil
	}
	return func() { c.block(body) }
}

func (c *lowerer) inlineFinalizers() {
	for i := len(c.finally) - 1; i >= 0; i-- {
		c.finally[i].emit()
	}
}

func (c *lowerer) emitCaughtInstance(errSlot, instSlot int) {
	trap := c.label()
	done := c.label()
	c.emit(instr.GLOBAL_GET, uint64(errSlot))
	c.emit(instr.ERROR_GET)
	c.emit(instr.DUP)
	c.emit(instr.REF_IS_NULL)
	c.brIf(trap)
	c.br(done)
	c.bind(trap)
	c.emit(instr.DROP)
	c.emitTrapInstance(errSlot)
	c.bind(done)
	c.emit(instr.GLOBAL_SET, uint64(instSlot))
}

// emitTrapInstance lowers a caught VM trap (a null-payload types.Error) into
// an exception instance. It reads the trap's numeric code natively via
// ERROR_CODE and matches it against trapClasses entirely in bytecode,
// skipping excInstance's host round trip for the traps minipy classifies most
// often; an unrecognized code still defers to the host for its message text.
func (c *lowerer) emitTrapInstance(errSlot int) {
	codeSlot := c.tmp()
	c.emit(instr.GLOBAL_GET, uint64(errSlot))
	c.emit(instr.ERROR_CODE)
	c.emit(instr.GLOBAL_SET, uint64(codeSlot))

	done := c.label()
	fallback := c.label()
	matched := make([]instr.Label, len(trapClasses))
	for i, tc := range trapClasses {
		matched[i] = c.label()
		c.emit(instr.GLOBAL_GET, uint64(codeSlot))
		c.emit(instr.I32_CONST, uint64(uint32(tc.code)))
		c.emit(instr.I32_EQ)
		c.brIf(matched[i])
	}
	c.br(fallback)
	for i, tc := range trapClasses {
		c.bind(matched[i])
		c.emitExceptionStruct(c.classes[tc.class], func() { c.constGet(vmtypes.String(tc.message)) })
		c.br(done)
	}
	c.bind(fallback)
	c.emit(instr.GLOBAL_GET, uint64(errSlot))
	c.callHost(c.exc())
	c.bind(done)
}

// emitExceptionStruct allocates a BaseException-backed struct: field 0 is the
// class id and field 1 is the message produced by pushMessage. Every exception
// class inherits the same {classID, message} layout, and runtime dispatch
// (emitExceptionClassID) only inspects the classID, not the struct's nominal type.
func (c *lowerer) emitExceptionStruct(cls *class, pushMessage func()) {
	c.emit(instr.STRUCT_NEW_DEFAULT, c.typeIndex(c.classes["BaseException"].typ))
	c.emit(instr.DUP)
	c.emit(instr.I32_CONST, 0)
	c.emit(instr.I64_CONST, uint64(cls.classID))
	c.emit(instr.STRUCT_SET)
	c.emit(instr.DUP)
	c.emit(instr.I32_CONST, 1)
	pushMessage()
	c.emit(instr.STRUCT_SET)
}

func (c *lowerer) emitExceptionTest(instSlot int, typ ast.Expr, next instr.Label) {
	info := c.classForExpr(typ)
	c.emitExceptionClassID(instSlot)
	c.emit(instr.I64_CONST, uint64(info.low))
	c.emit(instr.I64_GE_S)
	c.emitExceptionClassID(instSlot)
	c.emit(instr.I64_CONST, uint64(info.high))
	c.emit(instr.I64_LE_S)
	c.emit(instr.I32_AND)
	c.emit(instr.I32_EQZ)
	c.brIf(next)
}

func (c *lowerer) classForExpr(e ast.Expr) *class {
	switch x := e.(type) {
	case *ast.Name:
		return c.classes[c.symbol(x.Name)]
	case *ast.Attribute:
		if key := c.attrSym[x]; key != "" {
			return c.classes[key]
		}
	}
	return nil
}

func (c *lowerer) emitExceptionClassID(instSlot int) {
	c.emit(instr.GLOBAL_GET, uint64(instSlot))
	c.emit(instr.I32_CONST, 0)
	c.emit(instr.STRUCT_GET)
}

func (c *lowerer) emitRaise(n *ast.Raise) {
	if n.Cause != nil {
		c.expr(n.Cause)
		c.emit(instr.DROP)
	}
	if n.Exc == nil {
		c.emit(instr.GLOBAL_GET, uint64(c.excepts[len(c.excepts)-1]))
		c.emit(instr.THROW)
		return
	}
	if call, ok := n.Exc.(*ast.CallExpr); ok {
		if name, ok := call.Fn.(*ast.Name); ok {
			if cls := c.classes[c.symbol(name.Name)]; cls != nil && isException(cls) {
				c.emitExceptionInstance(cls, c.checkedArgs(call))
				c.emit(instr.I32_CONST, uint64(vmtypes.ErrorCodeNone))
				c.emit(instr.ERROR_NEW)
				c.emit(instr.THROW)
				return
			}
		} else if attr, ok := call.Fn.(*ast.Attribute); ok {
			if cls := c.classes[c.attrSym[attr]]; cls != nil && isException(cls) {
				c.emitExceptionInstance(cls, c.checkedArgs(call))
				c.emit(instr.I32_CONST, uint64(vmtypes.ErrorCodeNone))
				c.emit(instr.ERROR_NEW)
				c.emit(instr.THROW)
				return
			}
		}
	}
	c.expr(n.Exc)
	c.emit(instr.I32_CONST, uint64(vmtypes.ErrorCodeNone))
	c.emit(instr.ERROR_NEW)
	c.emit(instr.THROW)
}

func (c *lowerer) emitExceptionInstance(cls *class, args []ast.Expr) {
	c.emitExceptionStruct(cls, func() {
		if len(args) > 0 {
			c.expr(args[0])
		} else {
			c.constGet(vmtypes.String(""))
		}
	})
}

func (c *lowerer) emitWith(n *ast.With) {
	var emit func(int)
	emit = func(i int) {
		item := n.Items[i]
		name := c.types[item.Context].(*types.Class).Name
		ctxSlot := c.tmp()
		c.expr(item.Context)
		c.emit(instr.GLOBAL_SET, uint64(ctxSlot))
		owner, enter := c.methodOwner(name, "__enter__")
		c.emit(instr.GLOBAL_GET, uint64(ctxSlot))
		c.funcValue(enter, owner.methodBody["__enter__"])
		c.emit(instr.CALL)
		if item.OptionalVars != nil {
			c.set(item.OptionalVars.(*ast.Name).Name)
		} else {
			c.emit(instr.DROP)
		}
		exitOwner, exit := c.methodOwner(name, "__exit__")
		c.emitTryFinally(func() {
			if i+1 < len(n.Items) {
				emit(i + 1)
				return
			}
			c.block(n.Body)
		}, func() {
			c.emit(instr.GLOBAL_GET, uint64(ctxSlot))
			c.funcValue(exit, exitOwner.methodBody["__exit__"])
			c.emit(instr.CALL)
			c.emit(instr.DROP)
		})
	}
	emit(0)
}

func (c *lowerer) methodOwner(name, method string) (*class, *function) {
	for info := c.classes[name]; info != nil; info = info.base {
		if found := info.methods[method]; found != nil {
			return info, found
		}
	}
	return nil, nil
}

// emitPatternTest tests the value in global slot `slot` (static type typ) against
// p, binding captures as it goes, and branches to next on mismatch.
func (c *lowerer) emitPatternTest(p ast.Pattern, slot int, typ types.Type, next instr.Label) {
	switch pat := p.(type) {
	case *ast.WildcardPattern:
		// always matches
	case *ast.CapturePattern:
		c.bindSlot(pat.Name, slot)
	case *ast.StarPattern:
		c.bindSlot(pat.Name, slot)
	case *ast.AsPattern:
		c.emitPatternTest(pat.Pattern, slot, typ, next)
		c.bindSlot(pat.Name, slot)
	case *ast.OrPattern:
		succ := c.label()
		for _, alt := range pat.Alts {
			altNext := c.label()
			c.emitPatternTest(alt, slot, typ, altNext)
			c.br(succ)
			c.bind(altNext)
		}
		c.br(next)
		c.bind(succ)
	case *ast.ValuePattern:
		c.emit(instr.GLOBAL_GET, uint64(slot))
		c.expr(pat.Value)
		c.emit(operator.CmpOpcode(token.EQ, typ))
		c.emit(instr.I32_EQZ)
		c.brIf(next)
	case *ast.SequencePattern:
		c.emitSequenceTest(pat, slot, typ, next)
	case *ast.MappingPattern:
		c.emitMappingTest(pat, slot, typ, next)
	case *ast.ClassPattern:
		c.emitClassTest(pat, slot, typ, next)
	}
}

// bindSlot stores the value in global slot into the named capture variable.
func (c *lowerer) bindSlot(name string, slot int) {
	if name == "" || name == "_" {
		return
	}
	c.emit(instr.GLOBAL_GET, uint64(slot))
	c.set(name)
}

// childSlot extracts a sub-value of the slot value at the given index (a
// list/tuple/struct element) into a fresh temp slot and returns it.
func (c *lowerer) childSlot(parent int, index int, op instr.Opcode) int {
	child := c.tmp()
	c.emit(instr.GLOBAL_GET, uint64(parent))
	c.emit(instr.I32_CONST, uint64(index))
	c.emit(op)
	c.emit(instr.GLOBAL_SET, uint64(child))
	return child
}

func (c *lowerer) emitSequenceTest(pat *ast.SequencePattern, slot int, typ types.Type, next instr.Label) {
	switch s := typ.(type) {
	case *types.Tuple:
		if pat.Star < 0 {
			for i, e := range pat.Elems {
				child := c.childSlot(slot, i, instr.STRUCT_GET)
				c.emitPatternTest(e, child, s.Elems[i], next)
			}
			return
		}
		// Tuple length is static, so no runtime length guard is needed: the
		// checker already validated prefix+suffix against the tuple arity.
		prefix := pat.Star
		suffix := len(pat.Elems) - pat.Star - 1
		for i := 0; i < prefix; i++ {
			child := c.childSlot(slot, i, instr.STRUCT_GET)
			c.emitPatternTest(pat.Elems[i], child, s.Elems[i], next)
		}
		for j := 0; j < suffix; j++ {
			srcIdx := len(s.Elems) - suffix + j
			child := c.childSlot(slot, srcIdx, instr.STRUCT_GET)
			c.emitPatternTest(pat.Elems[prefix+1+j], child, s.Elems[srcIdx], next)
		}
		star := pat.Elems[prefix].(*ast.StarPattern)
		if star.Name != "" && star.Name != "_" {
			c.emitTupleRestList(slot, s, prefix, suffix)
			c.set(star.Name)
		}
	case *types.List:
		if pat.Star < 0 {
			c.emit(instr.GLOBAL_GET, uint64(slot))
			c.emit(instr.ARRAY_LEN)
			c.emit(instr.I32_CONST, uint64(len(pat.Elems)))
			c.emit(instr.I32_EQ)
			c.emit(instr.I32_EQZ)
			c.brIf(next)
			for i, e := range pat.Elems {
				child := c.childSlot(slot, i, instr.ARRAY_GET)
				c.emitPatternTest(e, child, s.Elem, next)
			}
			return
		}
		prefix := pat.Star
		suffix := len(pat.Elems) - pat.Star - 1
		c.emit(instr.GLOBAL_GET, uint64(slot))
		c.emit(instr.ARRAY_LEN)
		c.emit(instr.I32_CONST, uint64(prefix+suffix))
		c.emit(instr.I32_LT_S)
		c.brIf(next)
		for i := 0; i < prefix; i++ {
			child := c.childSlot(slot, i, instr.ARRAY_GET)
			c.emitPatternTest(pat.Elems[i], child, s.Elem, next)
		}
		for j := 0; j < suffix; j++ {
			child := c.tmp()
			c.emit(instr.GLOBAL_GET, uint64(slot))
			c.emit(instr.GLOBAL_GET, uint64(slot))
			c.emit(instr.ARRAY_LEN)
			c.emit(instr.I32_CONST, uint64(suffix-j))
			c.emit(instr.I32_SUB)
			c.emit(instr.ARRAY_GET)
			c.emit(instr.GLOBAL_SET, uint64(child))
			c.emitPatternTest(pat.Elems[prefix+1+j], child, s.Elem, next)
		}
		star := pat.Elems[prefix].(*ast.StarPattern)
		if star.Name != "" && star.Name != "_" {
			c.emit(instr.GLOBAL_GET, uint64(slot))
			c.emit(instr.I32_CONST, uint64(prefix))
			c.emit(instr.GLOBAL_GET, uint64(slot))
			c.emit(instr.ARRAY_LEN)
			c.emit(instr.I32_CONST, uint64(suffix))
			c.emit(instr.I32_SUB)
			c.emit(instr.ARRAY_SLICE)
			c.set(star.Name)
		}
	}
}

func (c *lowerer) emitMappingTest(pat *ast.MappingPattern, slot int, typ types.Type, next instr.Label) {
	d := typ.(*types.Dict)
	for i, keyExpr := range pat.Keys {
		child := c.tmp()
		c.emit(instr.GLOBAL_GET, uint64(slot))
		c.expr(keyExpr)
		c.emit(instr.MAP_LOOKUP)
		// MAP_LOOKUP leaves [value, present]; bring value to the top to capture it,
		// leaving the presence flag to branch on.
		c.emit(instr.SWAP)
		c.emit(instr.GLOBAL_SET, uint64(child))
		c.emit(instr.I32_EQZ)
		c.brIf(next)
		c.emitPatternTest(pat.Values[i], child, d.Value, next)
	}
	if pat.Rest != "" && pat.Rest != "_" {
		keysT := types.NewList(d.Key)
		c.emit(instr.I32_CONST, uint64(len(pat.Keys)))
		c.emit(instr.ARRAY_NEW_DEFAULT, c.typeIndex(keysT))
		for i, keyExpr := range pat.Keys {
			c.emit(instr.DUP)
			c.emit(instr.I32_CONST, uint64(i))
			c.expr(keyExpr)
			c.emit(instr.ARRAY_SET)
		}
		keysSlot := c.tmp()
		c.emit(instr.GLOBAL_SET, uint64(keysSlot))
		c.emit(instr.GLOBAL_GET, uint64(slot))
		c.emit(instr.GLOBAL_GET, uint64(keysSlot))
		c.callHost(c.dictRest(d))
		c.set(pat.Rest)
	}
}

func (c *lowerer) emitClassTest(pat *ast.ClassPattern, slot int, typ types.Type, next instr.Label) {
	cls := typ.(*types.Class)
	info := c.classes[cls.Name]
	for i, sub := range pat.Args {
		child := c.childSlot(slot, i, instr.STRUCT_GET)
		c.emitPatternTest(sub, child, info.fields[i].typ, next)
	}
	for i, kw := range pat.KwNames {
		idx := info.fieldIndex[kw]
		child := c.childSlot(slot, idx, instr.STRUCT_GET)
		c.emitPatternTest(pat.Kw[i], child, info.fields[idx].typ, next)
	}
}

func (c *lowerer) assignTarget(target ast.Expr, value ast.Expr) {
	switch t := target.(type) {
	case *ast.Subscript:
		if slice, ok := t.Index.(*ast.Slice); ok {
			c.expr(t.X)
			c.sliceBound(slice.Lower)
			c.sliceBound(slice.Upper)
			c.sliceBound(slice.Step)
			c.expr(value)
			c.callHost(c.listSliceAssign(c.types[t.X]))
			return
		}
		c.expr(t.X)
		c.expr(t.Index)
		c.expr(value)
		switch recv := c.types[t.X].(type) {
		case *types.List:
			c.emit(instr.SWAP)
			c.emit(instr.I64_TO_I32)
			c.emit(instr.SWAP)
			c.emit(instr.ARRAY_SET)
		case *types.Dict:
			c.emit(instr.MAP_SET)
		case *types.Class:
			owner, m := c.methodOwner(recv.Name, "__setitem__")
			c.funcValue(m, owner.methodBody["__setitem__"])
			c.emit(instr.CALL)
			c.emit(instr.DROP)
		default:
			panic("unsupported subscript assignment")
		}
	case *ast.TupleLit:
		c.unpackAssign(t, value)
	case *ast.Attribute:
		if key := c.attrSym[t]; key != "" {
			c.expr(value)
			c.emit(instr.GLOBAL_SET, uint64(c.globals[key].index))
			return
		}
		c.expr(t.X)
		c.emit(instr.I32_CONST, uint64(c.fieldIndex(t)))
		c.expr(value)
		c.emit(instr.STRUCT_SET)
	default:
		panic("unsupported assignment target")
	}
}

func (c *lowerer) augAssignAttribute(n *ast.AugAssign) {
	attr := n.Target.(*ast.Attribute)
	if key := c.attrSym[attr]; key != "" {
		c.emitBinary(n.Op, c.types[attr], c.types[n.Value],
			func() { c.emit(instr.GLOBAL_GET, uint64(c.globals[key].index)) },
			func() { c.expr(n.Value) })
		c.emit(instr.GLOBAL_SET, uint64(c.globals[key].index))
		return
	}
	c.emitBinary(n.Op, c.types[attr], c.types[n.Value],
		func() { c.attribute(attr) },
		func() { c.expr(n.Value) })
	c.expr(attr.X)
	c.emit(instr.SWAP)
	c.emit(instr.I32_CONST, uint64(c.fieldIndex(attr)))
	c.emit(instr.SWAP)
	c.emit(instr.STRUCT_SET)
}

func (c *lowerer) classStmt() {
	// Classes are compile-time metadata; instances are structs.
}

func (c *lowerer) unpackAssign(target *ast.TupleLit, value ast.Expr) {
	if tupleStarIndex(target) < 0 {
		if tupleValue, ok := value.(*ast.TupleLit); ok {
			for i, elem := range target.Elems {
				name := elem.(*ast.Name)
				c.expr(tupleValue.Elems[i])
				c.set(name.Name)
			}
			return
		}
	}
	c.expr(value)
	valueSlot := c.tmp()
	c.emit(instr.GLOBAL_SET, uint64(valueSlot))
	star := tupleStarIndex(target)
	if star >= 0 {
		c.unpackAssignStar(target, value, valueSlot, star)
		return
	}
	for i, elem := range target.Elems {
		name := elem.(*ast.Name)
		c.emitUnpackIndex(value, valueSlot, i)
		c.set(name.Name)
	}
}

func (c *lowerer) unpackAssignStar(target *ast.TupleLit, value ast.Expr, valueSlot int, star int) {
	suffix := len(target.Elems) - star - 1
	if _, ok := c.types[value].(*types.List); ok {
		for i, elem := range target.Elems {
			if starred, ok := elem.(*ast.Starred); ok {
				name := starred.X.(*ast.Name)
				c.emit(instr.GLOBAL_GET, uint64(valueSlot))
				c.emit(instr.I64_CONST, uint64(star))
				c.emit(instr.GLOBAL_GET, uint64(valueSlot))
				c.emit(instr.ARRAY_LEN)
				c.emit(instr.I32_TO_I64_S)
				c.emit(instr.I64_CONST, uint64(suffix))
				c.emit(instr.I64_SUB)
				c.emit(instr.I64_CONST, 1)
				c.callHost(c.arraySlice(c.types[value]))
				c.set(name.Name)
				continue
			}
			name := elem.(*ast.Name)
			idx := i
			if i > star {
				idx = -suffix + (i - star - 1)
			}
			c.emitListUnpackIndex(valueSlot, idx)
			c.set(name.Name)
		}
		return
	}
	for i, elem := range target.Elems {
		if starred, ok := elem.(*ast.Starred); ok {
			name := starred.X.(*ast.Name)
			c.emitTupleRestList(valueSlot, c.types[value].(*types.Tuple), star, suffix)
			c.set(name.Name)
			continue
		}
		name := elem.(*ast.Name)
		idx := i
		if i > star {
			idx = len(c.types[value].(*types.Tuple).Elems) - suffix + (i - star - 1)
		}
		c.emitUnpackIndex(value, valueSlot, idx)
		c.set(name.Name)
	}
}

func (c *lowerer) emitUnpackIndex(value ast.Expr, valueSlot int, idx int) {
	c.emit(instr.GLOBAL_GET, uint64(valueSlot))
	c.emit(instr.I32_CONST, uint64(idx))
	switch c.types[value].(type) {
	case *types.Tuple:
		c.emit(instr.STRUCT_GET)
	case *types.List:
		c.emit(instr.ARRAY_GET)
	default:
		panic("unsupported unpack value")
	}
}

func (c *lowerer) emitListUnpackIndex(valueSlot int, idx int) {
	c.emit(instr.GLOBAL_GET, uint64(valueSlot))
	if idx < 0 {
		c.emit(instr.GLOBAL_GET, uint64(valueSlot))
		c.emit(instr.ARRAY_LEN)
		c.emit(instr.I32_TO_I64_S)
		c.emit(instr.I64_CONST, uint64(-idx))
		c.emit(instr.I64_SUB)
		c.emit(instr.I64_TO_I32)
	} else {
		c.emit(instr.I32_CONST, uint64(idx))
	}
	c.emit(instr.ARRAY_GET)
}

func (c *lowerer) emitTupleRestList(valueSlot int, tuple *types.Tuple, star int, suffix int) {
	restLen := len(tuple.Elems) - star - suffix
	elemType := homogeneous(tuple.Elems[star : star+restLen])
	listType := types.NewList(elemType)
	c.emit(instr.I32_CONST, uint64(restLen))
	c.emit(instr.ARRAY_NEW_DEFAULT, c.typeIndex(listType))
	for i := 0; i < restLen; i++ {
		c.emit(instr.DUP)
		c.emit(instr.I32_CONST, uint64(i))
		c.emit(instr.GLOBAL_GET, uint64(valueSlot))
		c.emit(instr.I32_CONST, uint64(star+i))
		c.emit(instr.STRUCT_GET)
		c.emit(instr.ARRAY_SET)
	}
}

func (c *lowerer) get(name string) {
	if slot, ok := c.temps[name]; ok {
		c.emit(instr.GLOBAL_GET, uint64(slot))
		return
	}
	if c.locals != nil {
		if l, ok := c.locals[name]; ok {
			c.emit(instr.LOCAL_GET, uint64(l.index))
			if l.boxed {
				c.emit(instr.REF_GET)
			}
			return
		}
	}
	if c.current != nil {
		if cap, ok := c.current.captures[name]; ok {
			c.emit(instr.UPVAL_GET, uint64(cap.index))
			if cap.boxed || cap.src.boxed {
				c.emit(instr.REF_GET)
			}
			return
		}
	}
	c.emit(instr.GLOBAL_GET, uint64(c.globals[c.symbol(name)].index))
}

func (c *lowerer) set(name string) {
	if slot, ok := c.temps[name]; ok {
		c.emit(instr.GLOBAL_SET, uint64(slot))
		return
	}
	if c.locals != nil {
		if l, ok := c.locals[name]; ok {
			if l.boxed {
				if !c.boxed[l] {
					c.emit(instr.REF_NEW)
					c.emit(instr.LOCAL_SET, uint64(l.index))
					c.boxed[l] = true
					return
				}
				c.emit(instr.LOCAL_GET, uint64(l.index))
				c.emit(instr.SWAP)
				c.emit(instr.REF_SET)
				return
			}
			c.emit(instr.LOCAL_SET, uint64(l.index))
			return
		}
	}
	if c.current != nil {
		if cap, ok := c.current.captures[name]; ok {
			c.emit(instr.UPVAL_GET, uint64(cap.index))
			c.emit(instr.SWAP)
			if cap.boxed || cap.src.boxed {
				c.emit(instr.REF_SET)
			} else {
				c.emit(instr.UPVAL_SET, uint64(cap.index))
			}
			return
		}
	}
	c.emit(instr.GLOBAL_SET, uint64(c.globals[c.symbol(name)].index))
}

func (c *lowerer) symbol(name string) string {
	if c.mod != nil {
		if b, ok := c.mod.bindings[name]; ok && b.symbol != "" {
			return moduleKey(b.module, b.symbol)
		}
		if c.mod.name != "__main__" {
			key := c.mod.name + "." + name
			if _, ok := c.globals[key]; ok {
				return key
			}
			if _, ok := c.functions[key]; ok {
				return key
			}
			if _, ok := c.classes[key]; ok {
				return key
			}
		}
	}
	if _, ok := c.globals[name]; ok {
		return name
	}
	if _, ok := c.functions[name]; ok {
		return name
	}
	if _, ok := c.classes[name]; ok {
		return name
	}
	if _, ok := c.reg.FallbackSymbol(name); ok {
		return c.reg.FallbackName() + "." + name
	}
	return name
}

// narrowCast unboxes a ref-backed binding (union/Any) to the concrete type the
// checker narrowed this use to. Flow-proven narrowing (isinstance / is-None)
// recorded a concrete type on the use node while the slot stays a ref, so a
// checked REF_CAST recovers the unboxed value. No cast is emitted when the use
// itself is still dynamic or None.
func (c *lowerer) narrowCast(x *ast.Name) {
	use := c.types[x]
	if use == nil || refDynamic(use) || types.Equal(use, types.None) {
		return
	}
	if !refDynamic(c.typ(x.Name)) {
		return
	}
	c.emit(instr.REF_CAST, c.typeIndex(use))
}

// refDynamic reports whether a type is represented as minivm's dynamic ref —
// a union or Any — whose members are recovered with REF_TEST / REF_CAST.
func refDynamic(t types.Type) bool {
	if _, ok := t.(*types.Union); ok {
		return true
	}
	return types.IsAny(t)
}

func (c *lowerer) typ(name string) types.Type {
	if _, ok := c.temps[name]; ok {
		return types.Int
	}
	if c.locals != nil {
		if l, ok := c.locals[name]; ok {
			return l.typ
		}
	}
	if c.current != nil {
		if cap, ok := c.current.captures[name]; ok {
			return cap.typ
		}
	}
	return c.globals[c.symbol(name)].typ
}

// emitIf lowers `if`/`elif`/`else`: invert the condition and branch over the
// then-block to the else-block (docs/spec/05-codegen.md).
func (c *lowerer) emitIf(n *ast.If) {
	if known, truth := c.truth(n.Cond); known {
		if truth {
			c.block(n.Body)
		} else {
			c.block(n.Orelse)
		}
		return
	}
	c.expr(n.Cond)
	c.emit(instr.I32_EQZ)
	elseL := c.label()
	end := c.label()
	c.brIf(elseL)
	c.block(n.Body)
	c.br(end)
	c.bind(elseL)
	c.block(n.Orelse)
	c.bind(end)
}

// emitWhile lowers `while`: re-test at the top, run the else block on natural
// exit (not after a break). continue → top, break → past the else block.
func (c *lowerer) emitWhile(n *ast.While) {
	top := c.label()
	elseL := c.label()
	end := c.label()

	c.bind(top)
	c.expr(n.Cond)
	c.emit(instr.I32_EQZ)
	c.brIf(elseL)

	c.loops = append(c.loops, loopLabels{cont: top, brk: end})
	c.block(n.Body)
	c.loops = c.loops[:len(c.loops)-1]

	c.br(top)
	c.bind(elseL)
	c.block(n.Orelse)
	c.bind(end)
}

// emitFor lowers array-backed iterables with indexed loops and Iterator[T]
// values with the minivm coroutine/iterator protocol. continue → increment or
// resume, break → past the else block.
func (c *lowerer) emitFor(n *ast.For) {
	if c.iterates(c.types[n.Iter]) {
		c.emitIteratorFor(n, func() {
			c.iterate(n.Iter, c.types[n.Iter])
		})
		return
	}
	c.emitIterableFor(n)
}

func (c *lowerer) emitIteratorFor(n *ast.For, emitIter func()) {
	iterSlot := c.tmp()
	emitIter()
	c.emit(instr.GLOBAL_SET, uint64(iterSlot))
	top := c.label()
	cont := c.label()
	elseL := c.label()
	end := c.label()

	c.bind(top)
	c.emit(instr.GLOBAL_GET, uint64(iterSlot))
	c.emit(instr.CORO_DONE)
	c.brIf(elseL)
	c.emit(instr.GLOBAL_GET, uint64(iterSlot))
	c.emit(instr.CORO_VALUE)
	c.setLoopTarget(n.Target)

	c.loops = append(c.loops, loopLabels{cont: cont, brk: end})
	c.block(n.Body)
	c.loops = c.loops[:len(c.loops)-1]

	c.bind(cont)
	c.emitResumeIterator(iterSlot)
	c.br(top)

	c.bind(elseL)
	c.block(n.Orelse)
	c.bind(end)
}

func (c *lowerer) iterates(t types.Type) bool {
	switch t.(type) {
	case *types.Iterator, *types.Dict, *types.Set:
		return true
	default:
		return types.Equal(t, types.Str)
	}
}

func (c *lowerer) iterate(expr ast.Expr, typ types.Type) {
	if _, ok := typ.(*types.Iterator); ok {
		c.expr(expr)
		return
	}
	c.expr(expr)
	switch typ.(type) {
	case *types.Dict, *types.Set:
		c.emit(instr.MAP_ITER)
	default:
		if types.Equal(typ, types.Str) {
			c.callHost(c.strIter())
		}
	}
}

func (c *lowerer) emitIterableFor(n *ast.For) {
	iterSlot := c.tmp()
	idxSlot := c.tmp()

	c.expr(n.Iter)
	c.emit(instr.GLOBAL_SET, uint64(iterSlot))
	c.emit(instr.I64_CONST, 0)
	c.emit(instr.GLOBAL_SET, uint64(idxSlot))

	top := c.label()
	cont := c.label()
	elseL := c.label()
	end := c.label()

	c.bind(top)
	c.emit(instr.GLOBAL_GET, uint64(idxSlot))
	c.emit(instr.GLOBAL_GET, uint64(iterSlot))
	c.emit(instr.ARRAY_LEN)
	c.emit(instr.I32_TO_I64_S)
	c.emit(instr.I64_LT_S)
	c.emit(instr.I32_EQZ)
	c.brIf(elseL)

	c.emit(instr.GLOBAL_GET, uint64(iterSlot))
	c.emit(instr.GLOBAL_GET, uint64(idxSlot))
	c.emit(instr.I64_TO_I32)
	c.emit(instr.ARRAY_GET)
	if types.Equal(c.types[n.Iter], types.Bytes) {
		c.normalizeByteElem()
	}
	c.setLoopTarget(n.Target)

	c.loops = append(c.loops, loopLabels{cont: cont, brk: end})
	c.block(n.Body)
	c.loops = c.loops[:len(c.loops)-1]

	c.bind(cont)
	c.emit(instr.GLOBAL_GET, uint64(idxSlot))
	c.emit(instr.I64_CONST, 1)
	c.emit(instr.I64_ADD)
	c.emit(instr.GLOBAL_SET, uint64(idxSlot))
	c.br(top)

	c.bind(elseL)
	c.block(n.Orelse)
	c.bind(end)
}

func (c *lowerer) setLoopTarget(target ast.Expr) {
	switch t := target.(type) {
	case *ast.Name:
		c.set(t.Name)
	case *ast.TupleLit:
		for i, elem := range t.Elems {
			name := elem.(*ast.Name)
			c.emit(instr.DUP)
			c.emit(instr.I32_CONST, uint64(i))
			c.emit(instr.STRUCT_GET)
			c.set(name.Name)
		}
		c.emit(instr.DROP)
	default:
		panic("unsupported for target")
	}
}

func (c *lowerer) functionStmt(n *ast.Function) {
	info := c.function(n)
	if info == nil {
		return
	}
	if c.current == nil && info.specializable {
		// Emit each monomorphic instantiation as its own function constant; the
		// Any-typed body below still goes to the global slot as the fallback.
		for _, spec := range info.instances {
			c.buildSpec(spec)
		}
	}
	decSlots := c.emitDecoratorValues(n.DecoratorExprs)
	c.funcValue(info, n.Body)
	c.emitFunctionDecorators(decSlots)
	if c.current != nil {
		c.set(n.Name.Name)
		return
	}
	c.emit(instr.GLOBAL_SET, uint64(info.slot.index))
}

// emitDecoratorValues evaluates decorator expressions in source order, each
// into its own temp global slot, so evaluation happens exactly once and
// stays separate from application order.
func (c *lowerer) emitDecoratorValues(decorators []ast.Expr) []int {
	if len(decorators) == 0 {
		return nil
	}
	slots := make([]int, len(decorators))
	for i, dec := range decorators {
		slots[i] = c.tmp()
		c.expr(dec)
		c.emit(instr.GLOBAL_SET, uint64(slots[i]))
	}
	return slots
}

// emitFunctionDecorators applies each previously evaluated decorator to the
// undecorated function value left on the stack by funcValue, in reverse
// (bottom-to-top) order, leaving the final decorated value on the stack.
func (c *lowerer) emitFunctionDecorators(slots []int) {
	for i := len(slots) - 1; i >= 0; i-- {
		c.emit(instr.GLOBAL_GET, uint64(slots[i]))
		c.emit(instr.CALL)
	}
}

// buildFunction finalizes fb, recording rather than panicking on an assembly
// failure (unbound label, branch-offset overflow). kind is "function" or
// "specialization"; name identifies the failing function in the wrapped error.
func (c *lowerer) buildFunction(fb *vmtypes.FunctionBuilder, kind, name string) (*vmtypes.Function, bool) {
	f, err := fb.Build()
	if err != nil {
		c.fail(fmt.Errorf("build %s %s: %w", kind, name, err))
		return nil, false
	}
	return f, true
}

// buildSpec compiles one specialization to a function constant, recording its
// index on the instance. Specializations are top-level and capture nothing.
func (c *lowerer) buildSpec(spec *specialization) {
	if c.failed() || spec == nil || spec.emitted || spec.emitting {
		return
	}
	spec.emitting = true
	defer func() { spec.emitting = false }()
	c.buildCallSpecs(spec.calls)
	if c.failed() {
		return
	}

	info := spec.info
	fb := vmtypes.NewFunctionBuilder(&vmtypes.FunctionType{
		Params:  vmParams(info),
		Returns: vmReturns(info.result),
	})
	fb.WithLocals(vmLocals(info)...)

	child := c.child(fnTarget(fb), info, spec.types, spec.calls, spec.args)
	child.block(info.body)
	child.emitNoneReturn()
	c.adopt(child)

	f, ok := c.buildFunction(fb, "specialization", spec.key)
	if !ok {
		return
	}
	info.constIdx = c.prog.Const(f)
	spec.emitted = true
}

func (c *lowerer) buildCallSpecs(calls map[*ast.CallExpr]*specialization) {
	for _, spec := range calls {
		c.buildSpec(spec)
	}
}

func (c *lowerer) function(n *ast.Function) *function {
	if c.current != nil {
		return c.current.children[n.Name.Name]
	}
	return c.functions[c.symbol(n.Name.Name)]
}

func (c *lowerer) funcValue(info *function, body []ast.Stmt) {
	if c.failed() {
		return
	}
	fb := vmtypes.NewFunctionBuilder(&vmtypes.FunctionType{
		Params:  vmParams(info),
		Returns: vmReturns(info.result),
	})
	fb.WithLocals(vmLocals(info)...)
	fb.WithCaptures(vmCaps(info)...)

	child := c.child(fnTarget(fb), info, nil, nil, nil)
	child.block(body)
	child.emitNoneReturn()
	c.adopt(child)

	function, ok := c.buildFunction(fb, "function", info.name)
	if !ok {
		return
	}
	for _, name := range info.capOrder {
		cap := info.captures[name]
		c.emitCapture(cap)
	}
	c.constGet(function)
	if len(info.capOrder) > 0 {
		c.emit(instr.CLOSURE_NEW)
	}
}

// child derives a fresh lowerer for a nested function or specialization body:
// code, current, mod, and locals switch to the child's function; types,
// callSpec, and callArgs are overridden only when non-nil (specializations
// narrow them, plain nested functions inherit the parent's); loops, finally,
// excepts, temps, boxed, and err reset to fresh zero values. The caller must
// call adopt(child) once the child finishes lowering its body.
func (c *lowerer) child(code target, info *function, types map[ast.Expr]types.Type, callSpec map[*ast.CallExpr]*specialization, callArgs map[*ast.CallExpr][]ast.Expr) *lowerer {
	child := *c
	child.code = code
	child.current = info
	child.mod = info.mod
	child.locals = info.locals
	if types != nil {
		child.types = types
	}
	if callSpec != nil {
		child.callSpec = callSpec
	}
	if callArgs != nil {
		child.callArgs = callArgs
	}
	child.loops = nil
	child.finally = nil
	child.excepts = nil
	child.temps = map[string]int{}
	child.boxed = map[*local]bool{}
	child.err = nil
	return &child
}

// adopt propagates a finished child's first lowering failure and temp-slot
// high-water mark back onto the parent.
func (c *lowerer) adopt(child *lowerer) {
	if c.err == nil {
		c.err = child.err
	}
	if child.next > c.next {
		c.next = child.next
	}
}

func (c *lowerer) emitCapture(cap *capture) {
	if c.locals != nil {
		for _, l := range c.locals {
			if l == cap.src {
				c.emit(instr.LOCAL_GET, uint64(l.index))
				return
			}
		}
	}
	c.get(cap.name)
}

func (c *lowerer) returnStmt(n *ast.Return) {
	if n.Value != nil {
		c.expr(n.Value)
	} else {
		c.emit(instr.REF_NULL)
	}
	c.inlineFinalizers()
	c.emit(instr.RETURN)
}

func (c *lowerer) yield(n *ast.Yield) {
	c.yieldCore(n.Value, n.From)
	c.emit(instr.DROP)
}

func (c *lowerer) yieldExpr(n *ast.YieldExpr) {
	c.yieldCore(n.Value, n.From)
}

// yieldCore emits a yield (or yield from) and leaves the resume value on the
// stack as the expression result. For v1 the resume value type is None, so
// resumed generators observe None through the result.
func (c *lowerer) yieldCore(value ast.Expr, from bool) {
	if from {
		iterSlot := c.tmp()
		if lt, ok := c.types[value].(*types.List); ok {
			c.expr(value)
			c.callHost(c.listIter(lt))
		} else {
			c.iterate(value, c.types[value])
		}
		c.emit(instr.GLOBAL_SET, uint64(iterSlot))
		top := c.label()
		end := c.label()
		c.bind(top)
		c.emit(instr.GLOBAL_GET, uint64(iterSlot))
		c.emit(instr.CORO_DONE)
		c.brIf(end)
		c.emit(instr.GLOBAL_GET, uint64(iterSlot))
		c.emit(instr.CORO_VALUE)
		c.emit(instr.YIELD)
		c.emit(instr.DROP)
		c.emitResumeIterator(iterSlot)
		c.br(top)
		c.bind(end)
		c.emit(instr.GLOBAL_GET, uint64(iterSlot))
		c.emit(instr.DROP)
		c.emit(instr.REF_NULL)
		return
	}
	if value != nil {
		c.expr(value)
	} else {
		c.emit(instr.REF_NULL)
	}
	c.emit(instr.YIELD)
}

func (c *lowerer) emitNoneReturn() {
	c.emit(instr.REF_NULL)
	c.emit(instr.RETURN)
}

func vmParams(info *function) []vmtypes.Type {
	out := make([]vmtypes.Type, 0, len(info.params))
	for _, p := range info.params {
		out = append(out, p.typ.VM())
	}
	return out
}

func vmLocals(info *function) []vmtypes.Type {
	out := make([]vmtypes.Type, 0, len(info.order))
	for _, name := range info.order {
		l := info.locals[name]
		if l.boxed {
			out = append(out, vmtypes.TypeRef)
		} else {
			out = append(out, l.typ.VM())
		}
	}
	return out
}

func vmCaps(info *function) []vmtypes.Type {
	out := make([]vmtypes.Type, 0, len(info.capOrder))
	for _, name := range info.capOrder {
		cap := info.captures[name]
		if cap.boxed || cap.src.boxed {
			out = append(out, vmtypes.TypeRef)
		} else {
			out = append(out, cap.typ.VM())
		}
	}
	return out
}

func vmReturns(t types.Type) []vmtypes.Type {
	if types.Equal(t, types.None) {
		return []vmtypes.Type{vmtypes.TypeRef}
	}
	return []vmtypes.Type{t.VM()}
}

// expr lowers an expression, leaving exactly one value on the stack.
func (c *lowerer) expr(n ast.Expr) {
	if c.failed() {
		return
	}
	switch x := n.(type) {
	case *ast.IntLit:
		c.emit(instr.I64_CONST, uint64(x.Value))
	case *ast.FloatLit:
		c.emit(instr.F64_CONST, math.Float64bits(x.Value))
	case *ast.BoolLit:
		c.emitBool(x.Value)
	case *ast.NoneLit:
		c.emit(instr.REF_NULL)
	case *ast.EllipsisLit:
		c.constGet(ellipsisValue)
	case *ast.StrLit:
		c.constGet(vmtypes.String(x.Value))
	case *ast.BytesLit:
		c.bytesLit(x)
	case *ast.Name:
		if x.Name == "Ellipsis" && types.Equal(c.types[x], types.Ellipsis) {
			c.constGet(ellipsisValue)
		} else {
			c.get(x.Name)
			c.narrowCast(x)
		}
	case *ast.UnaryExpr:
		c.unary(x)
	case *ast.BinaryExpr:
		c.emitBinary(x.Op, c.types[x.X], c.types[x.Y],
			func() { c.expr(x.X) },
			func() { c.expr(x.Y) })
	case *ast.BoolOp:
		c.boolOp(x)
	case *ast.Compare:
		c.compare(x)
	case *ast.CallExpr:
		c.call(x)
	case *ast.LambdaExpr:
		c.lambda(x)
	case *ast.IfExp:
		c.ifExp(x)
	case *ast.NamedExpr:
		c.namedExpr(x)
	case *ast.ListLit:
		c.listLit(x)
	case *ast.DictLit:
		c.dictLit(x)
	case *ast.SetLit:
		c.setLit(x)
	case *ast.ListComp:
		c.listComp(x)
	case *ast.DictComp:
		c.dictComp(x)
	case *ast.SetComp:
		c.setComp(x)
	case *ast.YieldExpr:
		c.yieldExpr(x)
	case *ast.GeneratorExp:
		c.generatorExp(x)
	case *ast.TupleLit:
		c.tupleLit(x)
	case *ast.Subscript:
		c.subscript(x)
	case *ast.Attribute:
		c.attribute(x)
	case *ast.FString:
		c.fstringConcat(x.Parts)
	}
}

func (c *lowerer) listLit(x *ast.ListLit) {
	t := c.types[x].(*types.List)
	if hasStarredExpr(x.Elems) {
		slot := c.tmp()
		c.emit(instr.I32_CONST, 0)
		c.emit(instr.ARRAY_NEW_DEFAULT, c.typeIndex(t))
		c.emit(instr.GLOBAL_SET, uint64(slot))
		for _, elem := range x.Elems {
			if star, ok := elem.(*ast.Starred); ok {
				if tuple, ok := c.types[star.X].(*types.Tuple); ok {
					c.appendTupleToListSlot(slot, star.X, tuple)
					continue
				}
				c.emit(instr.GLOBAL_GET, uint64(slot))
				c.expr(star.X)
				c.callHost(c.listExtend(c.types[x]))
				c.emit(instr.GLOBAL_SET, uint64(slot))
				continue
			}
			c.appendListSlot(slot, func() { c.expr(elem) })
		}
		c.emit(instr.GLOBAL_GET, uint64(slot))
		return
	}
	c.emit(instr.I32_CONST, uint64(len(x.Elems)))
	c.emit(instr.ARRAY_NEW_DEFAULT, c.typeIndex(t))
	for i, elem := range x.Elems {
		c.emit(instr.DUP)
		c.emit(instr.I32_CONST, uint64(i))
		c.expr(elem)
		c.emit(instr.ARRAY_SET)
	}
}

// bytesLit allocates an array[i8] sized to the decoded byte payload and fills
// it byte by byte. It never goes through list[int] lowering: bytes is its own
// primitive VM array type (types.Bytes), not types.NewList(types.Int).
func (c *lowerer) bytesLit(x *ast.BytesLit) {
	raw := []byte(x.Value)
	c.emit(instr.I32_CONST, uint64(len(raw)))
	c.emit(instr.ARRAY_NEW_DEFAULT, c.typeIndex(types.Bytes))
	for i, b := range raw {
		c.emit(instr.DUP)
		c.emit(instr.I32_CONST, uint64(i))
		c.emit(instr.I32_CONST, uint64(b))
		c.emit(instr.ARRAY_SET)
	}
}

func (c *lowerer) dictLit(x *ast.DictLit) {
	t := c.types[x].(*types.Dict)
	if hasDictUnpack(x) {
		slot := c.tmp()
		c.emit(instr.I32_CONST, 0)
		c.emit(instr.MAP_NEW, c.typeIndex(t))
		c.emit(instr.GLOBAL_SET, uint64(slot))
		for i := range x.Keys {
			if star, ok := x.Keys[i].(*ast.Starred); ok && x.Values[i] == nil {
				c.emit(instr.GLOBAL_GET, uint64(slot))
				c.expr(star.X)
				c.callHost(c.dictMerge(c.types[x]))
				c.emit(instr.GLOBAL_SET, uint64(slot))
				continue
			}
			c.emit(instr.GLOBAL_GET, uint64(slot))
			c.expr(x.Keys[i])
			c.expr(x.Values[i])
			c.emit(instr.MAP_SET)
		}
		c.emit(instr.GLOBAL_GET, uint64(slot))
		return
	}
	for i := range x.Keys {
		c.expr(x.Keys[i])
		c.expr(x.Values[i])
	}
	c.emit(instr.I32_CONST, uint64(len(x.Keys)))
	c.emit(instr.MAP_NEW, c.typeIndex(t))
}

func hasStarredExpr(exprs []ast.Expr) bool {
	for _, expr := range exprs {
		if _, ok := expr.(*ast.Starred); ok {
			return true
		}
	}
	return false
}

func hasDictUnpack(x *ast.DictLit) bool {
	for i, key := range x.Keys {
		if _, ok := key.(*ast.Starred); ok && x.Values[i] == nil {
			return true
		}
	}
	return false
}

func (c *lowerer) appendListSlot(slot int, emitElem func()) {
	c.emit(instr.GLOBAL_GET, uint64(slot))
	emitElem()
	c.emit(instr.I32_CONST, 1)
	c.emit(instr.ARRAY_APPEND)
	c.emit(instr.DROP)
}

func (c *lowerer) appendTupleToListSlot(slot int, tupleExpr ast.Expr, tuple *types.Tuple) {
	tupleSlot := c.tmp()
	c.expr(tupleExpr)
	c.emit(instr.GLOBAL_SET, uint64(tupleSlot))
	for i := range tuple.Elems {
		idx := i
		c.appendListSlot(slot, func() {
			c.emit(instr.GLOBAL_GET, uint64(tupleSlot))
			c.emit(instr.I32_CONST, uint64(idx))
			c.emit(instr.STRUCT_GET)
		})
	}
}

func (c *lowerer) setLit(x *ast.SetLit) {
	t := c.types[x].(*types.Set)
	if hasStarredExpr(x.Elems) {
		slot := c.tmp()
		c.emit(instr.I32_CONST, 0)
		c.emit(instr.MAP_NEW, c.typeIndex(t))
		c.emit(instr.GLOBAL_SET, uint64(slot))
		for _, elem := range x.Elems {
			if star, ok := elem.(*ast.Starred); ok {
				c.emit(instr.GLOBAL_GET, uint64(slot))
				c.expr(star.X)
				c.callHost(c.dictMerge(c.types[x]))
				c.emit(instr.GLOBAL_SET, uint64(slot))
				continue
			}
			c.emit(instr.GLOBAL_GET, uint64(slot))
			c.expr(elem)
			c.emit(instr.I32_CONST, 1)
			c.emit(instr.MAP_SET)
		}
		c.emit(instr.GLOBAL_GET, uint64(slot))
		return
	}
	for _, elem := range x.Elems {
		c.expr(elem)
		c.emit(instr.I32_CONST, 1)
	}
	c.emit(instr.I32_CONST, uint64(len(x.Elems)))
	c.emit(instr.MAP_NEW, c.typeIndex(t))
}

func (c *lowerer) lambda(x *ast.LambdaExpr) {
	info := c.lambdas[x]
	if info == nil {
		return
	}
	c.funcValue(info, []ast.Stmt{&ast.Return{Base: ast.Base{Position: x.Pos()}, Value: x.Body}})
}

func (c *lowerer) listComp(x *ast.ListComp) {
	t := c.types[x].(*types.List)
	slot := c.tmp()
	c.emit(instr.I32_CONST, 0)
	c.emit(instr.ARRAY_NEW_DEFAULT, c.typeIndex(t))
	c.emit(instr.GLOBAL_SET, uint64(slot))
	c.comp(x.Clauses, func() {
		c.appendListSlot(slot, func() { c.expr(x.Elem) })
	})
	c.emit(instr.GLOBAL_GET, uint64(slot))
}

func (c *lowerer) dictComp(x *ast.DictComp) {
	t := c.types[x].(*types.Dict)
	slot := c.tmp()
	c.emit(instr.I32_CONST, 0)
	c.emit(instr.MAP_NEW, c.typeIndex(t))
	c.emit(instr.GLOBAL_SET, uint64(slot))
	c.comp(x.Clauses, func() {
		c.emit(instr.GLOBAL_GET, uint64(slot))
		c.expr(x.Key)
		c.expr(x.Value)
		c.emit(instr.MAP_SET)
	})
	c.emit(instr.GLOBAL_GET, uint64(slot))
}

func (c *lowerer) setComp(x *ast.SetComp) {
	t := c.types[x].(*types.Set)
	slot := c.tmp()
	c.emit(instr.I32_CONST, 0)
	c.emit(instr.MAP_NEW, c.typeIndex(t))
	c.emit(instr.GLOBAL_SET, uint64(slot))
	c.comp(x.Clauses, func() {
		c.emit(instr.GLOBAL_GET, uint64(slot))
		c.expr(x.Elem)
		c.emit(instr.I32_CONST, 1)
		c.emit(instr.MAP_SET)
	})
	c.emit(instr.GLOBAL_GET, uint64(slot))
}

func (c *lowerer) generatorExp(x *ast.GeneratorExp) {
	info := c.genExprs[x]
	if info == nil {
		return
	}
	c.funcValue(info, info.body)
	c.emit(instr.CALL)
}

func (c *lowerer) comp(clauses []*ast.Comprehension, body func()) {
	var emit func(int)
	emit = func(i int) {
		if i == len(clauses) {
			body()
			return
		}
		clause := clauses[i]
		targetSlot := c.tmp()
		prev, hadPrev := c.temps[clause.Target.Name]
		c.temps[clause.Target.Name] = targetSlot
		defer func() {
			if hadPrev {
				c.temps[clause.Target.Name] = prev
			} else {
				delete(c.temps, clause.Target.Name)
			}
		}()
		if c.iterates(c.types[clause.Iter]) {
			c.iteratorComp(clause, targetSlot, func() {
				c.iterate(clause.Iter, c.types[clause.Iter])
			}, func() { emit(i + 1) })
			return
		}
		c.iterComp(clause, targetSlot, func() { emit(i + 1) })
	}
	emit(0)
}
func (c *lowerer) iterComp(clause *ast.Comprehension, targetSlot int, body func()) {
	iterSlot := c.tmp()
	idxSlot := c.tmp()
	c.expr(clause.Iter)
	c.emit(instr.GLOBAL_SET, uint64(iterSlot))
	c.emit(instr.I64_CONST, 0)
	c.emit(instr.GLOBAL_SET, uint64(idxSlot))
	top := c.label()
	cont := c.label()
	end := c.label()
	c.bind(top)
	c.emit(instr.GLOBAL_GET, uint64(idxSlot))
	c.emit(instr.GLOBAL_GET, uint64(iterSlot))
	c.emit(instr.ARRAY_LEN)
	c.emit(instr.I32_TO_I64_S)
	c.emit(instr.I64_LT_S)
	c.emit(instr.I32_EQZ)
	c.brIf(end)
	c.emit(instr.GLOBAL_GET, uint64(iterSlot))
	c.emit(instr.GLOBAL_GET, uint64(idxSlot))
	c.emit(instr.I64_TO_I32)
	c.emit(instr.ARRAY_GET)
	if types.Equal(c.types[clause.Iter], types.Bytes) {
		c.normalizeByteElem()
	}
	c.emit(instr.GLOBAL_SET, uint64(targetSlot))
	c.compFilters(clause.Ifs, cont)
	body()
	c.bind(cont)
	c.emit(instr.GLOBAL_GET, uint64(idxSlot))
	c.emit(instr.I64_CONST, 1)
	c.emit(instr.I64_ADD)
	c.emit(instr.GLOBAL_SET, uint64(idxSlot))
	c.br(top)
	c.bind(end)
}

func (c *lowerer) iteratorComp(clause *ast.Comprehension, targetSlot int, emitIter func(), body func()) {
	iterSlot := c.tmp()
	emitIter()
	c.emit(instr.GLOBAL_SET, uint64(iterSlot))
	top := c.label()
	cont := c.label()
	end := c.label()
	c.bind(top)
	c.emit(instr.GLOBAL_GET, uint64(iterSlot))
	c.emit(instr.CORO_DONE)
	c.brIf(end)
	c.emit(instr.GLOBAL_GET, uint64(iterSlot))
	c.emit(instr.CORO_VALUE)
	c.emit(instr.GLOBAL_SET, uint64(targetSlot))
	c.compFilters(clause.Ifs, cont)
	body()
	c.bind(cont)
	c.emitResumeIterator(iterSlot)
	c.br(top)
	c.bind(end)
}

func (c *lowerer) compFilters(filters []ast.Expr, cont instr.Label) {
	for _, filter := range filters {
		c.expr(filter)
		c.emit(instr.I32_EQZ)
		c.brIf(cont)
	}
}

func (c *lowerer) tupleLit(x *ast.TupleLit) {
	t := c.types[x].(*types.Tuple)
	c.emit(instr.STRUCT_NEW_DEFAULT, c.typeIndex(t))
	for i, elem := range x.Elems {
		c.emit(instr.DUP)
		c.emit(instr.I32_CONST, uint64(i))
		c.expr(elem)
		c.emit(instr.STRUCT_SET)
	}
}

func (c *lowerer) subscript(x *ast.Subscript) {
	if slice, ok := x.Index.(*ast.Slice); ok {
		c.slice(x, slice)
		return
	}
	c.expr(x.X)
	c.expr(x.Index)
	switch c.types[x.X].(type) {
	case *types.List:
		c.emit(instr.I64_TO_I32)
		c.emit(instr.ARRAY_GET)
	case *types.Dict:
		c.emit(instr.MAP_GET)
	case *types.Tuple:
		c.emit(instr.I64_TO_I32)
		c.emit(instr.STRUCT_GET)
	case *types.Class:
		cls := c.types[x.X].(*types.Class)
		owner, m := c.methodOwner(cls.Name, "__getitem__")
		c.funcValue(m, owner.methodBody["__getitem__"])
		c.emit(instr.CALL)
	default:
		if types.Equal(c.types[x.X], types.Bytes) {
			c.emit(instr.I64_TO_I32)
			c.emit(instr.ARRAY_GET)
			c.normalizeByteElem()
		} else if types.Equal(c.types[x.X], types.Str) {
			c.callHost(c.strIndex()) // stack has string and index; returns one-codepoint string
		}
	}
}

// normalizeByteElem reinterprets the array[i8] element ARRAY_GET just pushed
// as an unsigned byte in 0..255 and widens it to the int (i64) that source
// bytes indexing, direct iteration, and comprehensions all expose. It is the
// single place that undoes i8's sign extension so indexing, direct for
// loops, and comprehensions stay consistent with bytesIter's unsigned view
// (builtins/host.go).
func (c *lowerer) normalizeByteElem() {
	c.emit(instr.I32_CONST, 0xff)
	c.emit(instr.I32_AND)
	c.emit(instr.I32_TO_I64_S)
}

func (c *lowerer) namedExpr(x *ast.NamedExpr) {
	c.expr(x.Value)
	c.set(x.Target.Name)
	c.get(x.Target.Name)
}

func (c *lowerer) slice(x *ast.Subscript, s *ast.Slice) {
	c.expr(x.X)
	c.sliceBound(s.Lower)
	c.sliceBound(s.Upper)
	c.sliceBound(s.Step)
	switch c.types[x.X].(type) {
	case *types.List:
		c.callHost(c.arraySlice(c.types[x.X]))
	default:
		if types.Equal(c.types[x.X], types.Bytes) {
			c.callHost(c.arraySlice(c.types[x.X]))
			return
		}
		c.callHost(c.strSlice())
	}
}

func (c *lowerer) sliceBound(x ast.Expr) {
	if x == nil {
		c.emit(instr.I64_CONST, uint64(1)<<63)
		return
	}
	c.expr(x)
}

func (c *lowerer) attribute(x *ast.Attribute) {
	if key, ok := c.attrSym[x]; ok {
		c.emit(instr.GLOBAL_GET, uint64(c.globals[key].index))
		return
	}
	if _, ok := c.attrMod[x]; ok {
		return
	}
	c.expr(x.X)
	c.emit(instr.I32_CONST, uint64(c.fieldIndex(x)))
	c.emit(instr.STRUCT_GET)
}

func (c *lowerer) fieldIndex(x *ast.Attribute) int {
	cls := c.types[x.X].(*types.Class)
	return c.classes[cls.Name].fieldIndex[x.Name]
}

// fstringConcat lowers a sequence of f-string parts into a left-associated
// STRING_CONCAT chain seeded with the empty string. It is reused both for the
// whole f-string and for a nested format spec (f"{x:{w}.{p}f}").
func (c *lowerer) fstringConcat(parts []ast.FStringPart) {
	c.constGet(vmtypes.String(""))
	for _, part := range parts {
		c.fstringPart(part)
		c.emit(instr.STRING_CONCAT)
	}
}

func (c *lowerer) fstringPart(part ast.FStringPart) {
	switch p := part.(type) {
	case *ast.FStringText:
		c.constGet(vmtypes.String(p.Value))
	case *ast.FStringExpr:
		if p.Debug != "" {
			c.constGet(vmtypes.String(p.Debug))
			c.fstringValue(p)
			c.emit(instr.STRING_CONCAT)
			return
		}
		c.fstringValue(p)
	}
}

// fstringValue lowers a replacement field to a single string on the stack,
// applying the conversion (!s/!r/!a) first and then the format spec, matching
// Python's evaluation order: expression, then any nested format-spec fields.
func (c *lowerer) fstringValue(p *ast.FStringExpr) {
	c.expr(p.Expr)
	valType := c.types[p.Expr]

	conv := p.Conversion
	// A debug field with neither conversion nor format spec defaults to repr.
	if p.Debug != "" && conv == 0 && len(p.Format) == 0 {
		conv = 'r'
	}
	switch conv {
	case 'r', 'a':
		c.callHost(c.reprHost(valType, conv == 'a'))
		valType = types.Str
	case 's':
		if !types.Equal(valType, types.Str) {
			c.callHost(c.nativeHost("builtins", "str"))
		}
		valType = types.Str
	}

	if len(p.Format) > 0 {
		if spec, ok := staticFStringFormat(p.Format); ok {
			c.constGet(vmtypes.String(spec))
		} else {
			c.fstringConcat(p.Format)
		}
		c.callHost(c.format(valType))
		return
	}

	if conv == 0 && !types.Equal(valType, types.Str) {
		c.callHost(c.nativeHost("builtins", "str"))
	}
}

func staticFStringFormat(parts []ast.FStringPart) (string, bool) {
	var builder strings.Builder
	for _, part := range parts {
		text, ok := part.(*ast.FStringText)
		if !ok {
			return "", false
		}
		builder.WriteString(text.Value)
	}
	return builder.String(), true
}

// ifExp lowers the conditional expression `body if cond else orelse`
// (docs/spec/05-codegen.md): branch to the true arm when the condition holds,
// else fall through to the false arm.
func (c *lowerer) ifExp(x *ast.IfExp) {
	c.expr(x.Cond)
	trueL := c.label()
	end := c.label()
	c.brIf(trueL)
	c.expr(x.Orelse)
	c.br(end)
	c.bind(trueL)
	c.expr(x.Body)
	c.bind(end)
}

// unary and emitBinary delegate to the operator module, the single source of
// operator lowering.

func (c *lowerer) unary(x *ast.UnaryExpr) {
	operator.EmitUnary(c, x.Op, x.X)
}

func (c *lowerer) emitBinary(op token.Type, left, right types.Type, pushLeft, pushRight func()) {
	operator.EmitBinary(c, op, left, right, pushLeft, pushRight)
}

// boolOp lowers short-circuiting `and`/`or` (docs/spec/05-codegen.md).
func (c *lowerer) boolOp(x *ast.BoolOp) {
	c.expr(x.X)
	c.emit(instr.DUP)
	if x.Op == token.AND {
		eval := c.label()
		end := c.label()
		c.brIf(eval)
		c.br(end)
		c.bind(eval)
		c.emit(instr.DROP)
		c.expr(x.Y)
		c.bind(end)
		return
	}
	end := c.label()
	c.brIf(end)
	c.emit(instr.DROP)
	c.expr(x.Y)
	c.bind(end)
}

// compare lowers a (possibly chained) comparison to an i32 result. Chained
// comparisons evaluate each source operand once, matching Python semantics.
func (c *lowerer) compare(x *ast.Compare) {
	if len(x.Ops) == 1 {
		c.expr(x.X)
		c.expr(x.Comparators[0])
		c.emitCmpStack(x.Ops[0], c.types[x.X], c.types[x.Comparators[0]])
		return
	}
	leftSlot := c.tmp()
	end := c.label()
	c.expr(x.X)
	c.emit(instr.GLOBAL_SET, uint64(leftSlot))
	left := x.X
	for i, op := range x.Ops {
		c.emit(instr.GLOBAL_GET, uint64(leftSlot))
		c.expr(x.Comparators[i])
		if i+1 < len(x.Ops) {
			c.emit(instr.DUP)
			c.emit(instr.GLOBAL_SET, uint64(leftSlot))
		}
		c.emitCmpStack(op, c.types[left], c.types[x.Comparators[i]])
		left = x.Comparators[i]
		if i+1 < len(x.Ops) {
			c.emit(instr.DUP)
			c.emit(instr.I32_EQZ)
			c.brIf(end)
			c.emit(instr.DROP)
		}
	}
	c.bind(end)
}

func (c *lowerer) emitCmpStack(op token.Type, left types.Type, right types.Type) {
	operator.EmitCompareStack(c, op, left, right)
}

func (c *lowerer) emitResumeIterator(slot int) {
	c.emit(instr.GLOBAL_GET, uint64(slot))
	c.emit(instr.REF_NULL)
	c.emit(instr.RESUME)
	c.emit(instr.DROP)
}

// call lowers a direct native, class, function, or callable-value call.
func (c *lowerer) call(x *ast.CallExpr) {
	if attr, ok := x.Fn.(*ast.Attribute); ok {
		c.methodCall(x, attr)
		return
	}
	name, isName := x.Fn.(*ast.Name)
	if isName {
		sym := c.symbol(name.Name)
		if cls, ok := c.classes[sym]; ok {
			c.construct(x, cls)
			return
		}
		if info, ok := c.functions[sym]; ok {
			for _, arg := range c.functionCallArgs(x, info) {
				c.expr(arg)
			}
			if spec := c.callSpec[x]; spec != nil {
				c.emit(instr.CONST_GET, uint64(spec.info.constIdx))
				c.emit(instr.CALL)
				return
			}
			c.emit(instr.GLOBAL_GET, uint64(info.slot.index))
			c.emit(instr.CALL)
			return
		}
		if _, ok := c.reg.SymbolByKey(sym); !ok {
			for _, arg := range x.Args {
				c.expr(arg)
			}
			c.get(name.Name)
			c.emit(instr.CALL)
			return
		}
	}
	if !isName {
		for _, arg := range x.Args {
			c.expr(arg)
		}
		c.expr(x.Fn)
		c.emit(instr.CALL)
		return
	}

	if c.lenDunder[x] {
		c.emitLenDunder(x)
		return
	}
	if sym, ok := c.reg.SymbolByKey(c.symbol(name.Name)); ok {
		sym.Emit(c, x.Args)
	}
}

// emitLenDunder lowers len(obj) to a direct obj.__len__() call and guards the
// result against negative values, raising ValueError to match CPython.
func (c *lowerer) emitLenDunder(x *ast.CallExpr) {
	arg := x.Args[0]
	cls := c.types[arg].(*types.Class)
	owner, m := c.methodOwner(cls.Name, "__len__")
	c.expr(arg)
	c.funcValue(m, owner.methodBody["__len__"])
	c.emit(instr.CALL)
	ok := c.label()
	c.emit(instr.DUP)
	c.emit(instr.I64_CONST, 0)
	c.emit(instr.I64_GE_S)
	c.brIf(ok)
	c.emit(instr.DROP)
	c.emitExceptionStruct(c.classes["ValueError"], func() {
		c.constGet(vmtypes.String("__len__() should return >= 0"))
	})
	c.emit(instr.I32_CONST, uint64(vmtypes.ErrorCodeNone))
	c.emit(instr.ERROR_NEW)
	c.emit(instr.THROW)
	c.bind(ok)
}

func (c *lowerer) nativeHost(moduleName, symbol string) *interp.HostFunction {
	fn, ok := c.native.Value(moduleName, symbol).(*interp.HostFunction)
	if !ok {
		panic("native symbol " + moduleName + "." + symbol + " is not a host function")
	}
	return fn
}

func (c *lowerer) functionCallArgs(x *ast.CallExpr, info *function) []ast.Expr {
	if args, ok := c.callArgs[x]; ok {
		return args
	}
	args := make([]ast.Expr, len(info.params))
	seen := make([]bool, len(info.params))
	positional := 0
	for _, arg := range x.Args {
		for positional < len(info.params) && info.params[positional].kind == ast.ParamKwOnly {
			positional++
		}
		if positional >= len(info.params) {
			break
		}
		args[positional] = arg
		seen[positional] = true
		positional++
	}
	for _, kw := range x.Keywords {
		if i, ok := info.paramPosition(kw.Name); ok {
			args[i] = kw.Value
			seen[i] = true
		}
	}
	for i, p := range info.params {
		if !seen[i] {
			args[i] = p.defaultValue
		}
	}
	return args
}

func (c *lowerer) construct(x *ast.CallExpr, cls *class) {
	if isException(cls) {
		c.emitExceptionInstance(cls, c.checkedArgs(x))
		return
	}
	c.emit(instr.STRUCT_NEW_DEFAULT, c.typeIndex(cls.typ))
	c.applyFieldDefaults(cls)
	args := c.checkedArgs(x)
	if init := cls.methods["__init__"]; init != nil {
		c.emit(instr.DUP)
		for _, arg := range args {
			c.expr(arg)
		}
		c.funcValue(init, cls.methodBody["__init__"])
		c.emit(instr.CALL)
		c.emit(instr.DROP)
		return
	}
	for i, arg := range args {
		c.emit(instr.DUP)
		c.emit(instr.I32_CONST, uint64(cls.fields[i].index))
		c.expr(arg)
		c.emit(instr.STRUCT_SET)
	}
}

func (c *lowerer) checkedArgs(x *ast.CallExpr) []ast.Expr {
	if args, ok := c.callArgs[x]; ok {
		return args
	}
	return x.Args
}

func (c *lowerer) applyFieldDefaults(cls *class) {
	for _, field := range cls.fields {
		if field.value == nil {
			continue
		}
		c.emit(instr.DUP)
		c.emit(instr.I32_CONST, uint64(field.index))
		c.expr(field.value)
		c.emit(instr.STRUCT_SET)
	}
}

func (c *lowerer) methodCall(x *ast.CallExpr, attr *ast.Attribute) {
	if native := c.attrNative[attr]; native != nil {
		native.Emit(c, x.Args)
		return
	}
	if key := c.attrSym[attr]; key != "" {
		if cls := c.classes[key]; cls != nil {
			c.construct(x, cls)
			return
		}
		if info := c.functions[key]; info != nil {
			for _, arg := range c.functionCallArgs(x, info) {
				c.expr(arg)
			}
			if spec := c.callSpec[x]; spec != nil {
				c.emit(instr.CONST_GET, uint64(spec.info.constIdx))
				c.emit(instr.CALL)
				return
			}
			c.emit(instr.GLOBAL_GET, uint64(info.slot.index))
			c.emit(instr.CALL)
			return
		}
	}
	recvType := c.types[attr.X]
	if cls, ok := recvType.(*types.Class); ok {
		owner, method := c.methodOwner(cls.Name, attr.Name)
		c.expr(attr.X)
		args := c.callArgs[x]
		if args == nil {
			args = x.Args
		}
		for _, arg := range args {
			c.expr(arg)
		}
		c.funcValue(method, owner.methodBody[attr.Name])
		c.emit(instr.CALL)
		return
	}
	c.expr(attr.X)
	for _, arg := range x.Args {
		c.expr(arg)
	}
	switch attr.Name {
	case "get":
		if len(x.Args) == 1 {
			c.emitZeroValue(c.types[x])
		}
		c.callHost(c.dictGet(recvType, c.types[x]))
	case "keys":
		c.emit(instr.MAP_KEYS)
	case "values":
		c.callHost(c.dictValues(recvType, c.types[x]))
	case "items":
		c.callHost(c.dictItems(recvType, c.types[x]))
	case "append":
		c.emit(instr.I32_CONST, 1)
		c.emit(instr.ARRAY_APPEND)
		c.emit(instr.DROP)
		c.emit(instr.REF_NULL)
	case "pop":
		if len(x.Args) == 0 {
			c.emit(instr.I64_CONST, ^uint64(0))
		}
		c.emitArrayDelete()
	case "index":
		c.callHost(c.listIndex(recvType))
	case "insert":
		c.emitListInsert()
		c.emit(instr.REF_NULL)
	case "extend":
		c.emitListExtend()
		c.emit(instr.REF_NULL)
	case "reverse":
		c.emitListReverse()
		c.emit(instr.REF_NULL)
	case "upper":
		c.callHost(c.strUpper())
	case "lower":
		c.callHost(c.strLower())
	case "split":
		if len(x.Args) == 0 {
			c.constGet(vmtypes.String(" "))
		}
		c.callHost(c.strSplit())
	case "join":
		c.callHost(c.strJoin())
	case "find":
		c.callHost(c.strFind())
	default:
		panic("unsupported method")
	}
}

// emitBool pushes a bool literal as an i1 value. There is no i1 const opcode,
// so the value is pushed as i32 and normalized to i1 via `!= 0`, giving the
// result KindI1 — matching comparison results so bool literals stay
// interchangeable with them (e.g. as map keys, where the verifier matches key
// kinds exactly).
func (c *lowerer) emitBool(v bool) {
	if v {
		c.emit(instr.I32_CONST, 1)
	} else {
		c.emit(instr.I32_CONST, 0)
	}
	c.emit(instr.I32_CONST, 0)
	c.emit(instr.I32_NE)
}

func (c *lowerer) emitZeroValue(t types.Type) {
	switch {
	case types.Equal(t, types.Int):
		c.emit(instr.I64_CONST, 0)
	case types.Equal(t, types.Float):
		c.emit(instr.F64_CONST, 0)
	case types.Equal(t, types.Bool):
		c.emitBool(false)
	case types.Equal(t, types.Str):
		c.constGet(vmtypes.String(""))
	default:
		c.emit(instr.REF_NULL)
	}
}

// absInt lowers abs() on an int inline: branch on the sign and negate when
// negative (the entry frame has no locals for a branchless trick).
func (c *lowerer) emitArrayDelete() {
	idxSlot := c.tmp()
	listSlot := c.tmp()
	c.emit(instr.GLOBAL_SET, uint64(idxSlot))
	c.emit(instr.GLOBAL_SET, uint64(listSlot))

	neg := c.label()
	norm := c.label()
	c.emit(instr.GLOBAL_GET, uint64(idxSlot))
	c.emit(instr.I64_CONST, 0)
	c.emit(instr.I64_LT_S)
	c.brIf(neg)
	c.br(norm)
	c.bind(neg)
	c.emit(instr.GLOBAL_GET, uint64(idxSlot))
	c.emit(instr.GLOBAL_GET, uint64(listSlot))
	c.emit(instr.ARRAY_LEN)
	c.emit(instr.I32_TO_I64_S)
	c.emit(instr.I64_ADD)
	c.emit(instr.GLOBAL_SET, uint64(idxSlot))
	c.bind(norm)

	c.emit(instr.GLOBAL_GET, uint64(listSlot))
	c.emit(instr.GLOBAL_GET, uint64(idxSlot))
	c.emit(instr.I64_TO_I32)
	c.emit(instr.ARRAY_DELETE)
}

func (c *lowerer) emitListInsert() {
	valueSlot := c.tmp()
	idxSlot := c.tmp()
	listSlot := c.tmp()
	lenSlot := c.tmp()
	iSlot := c.tmp()

	c.emit(instr.GLOBAL_SET, uint64(valueSlot))
	c.emit(instr.GLOBAL_SET, uint64(idxSlot))
	c.emit(instr.GLOBAL_SET, uint64(listSlot))

	c.emit(instr.GLOBAL_GET, uint64(listSlot))
	c.emit(instr.ARRAY_LEN)
	c.emit(instr.I32_TO_I64_S)
	c.emit(instr.GLOBAL_SET, uint64(lenSlot))

	neg := c.label()
	clampLow := c.label()
	clampHigh := c.label()
	grow := c.label()
	c.emit(instr.GLOBAL_GET, uint64(idxSlot))
	c.emit(instr.I64_CONST, 0)
	c.emit(instr.I64_LT_S)
	c.brIf(neg)
	c.br(clampLow)
	c.bind(neg)
	c.emit(instr.GLOBAL_GET, uint64(idxSlot))
	c.emit(instr.GLOBAL_GET, uint64(lenSlot))
	c.emit(instr.I64_ADD)
	c.emit(instr.GLOBAL_SET, uint64(idxSlot))

	c.bind(clampLow)
	c.emit(instr.GLOBAL_GET, uint64(idxSlot))
	c.emit(instr.I64_CONST, 0)
	c.emit(instr.I64_LT_S)
	c.emit(instr.I32_EQZ)
	c.brIf(clampHigh)
	c.emit(instr.I64_CONST, 0)
	c.emit(instr.GLOBAL_SET, uint64(idxSlot))

	c.bind(clampHigh)
	c.emit(instr.GLOBAL_GET, uint64(idxSlot))
	c.emit(instr.GLOBAL_GET, uint64(lenSlot))
	c.emit(instr.I64_GT_S)
	c.emit(instr.I32_EQZ)
	c.brIf(grow)
	c.emit(instr.GLOBAL_GET, uint64(lenSlot))
	c.emit(instr.GLOBAL_SET, uint64(idxSlot))

	c.bind(grow)
	c.emit(instr.GLOBAL_GET, uint64(listSlot))
	c.emit(instr.GLOBAL_GET, uint64(valueSlot))
	c.emit(instr.I32_CONST, 1)
	c.emit(instr.ARRAY_APPEND)
	c.emit(instr.DROP)

	c.emit(instr.GLOBAL_GET, uint64(lenSlot))
	c.emit(instr.GLOBAL_SET, uint64(iSlot))
	top := c.label()
	done := c.label()
	c.bind(top)
	c.emit(instr.GLOBAL_GET, uint64(iSlot))
	c.emit(instr.GLOBAL_GET, uint64(idxSlot))
	c.emit(instr.I64_GT_S)
	c.emit(instr.I32_EQZ)
	c.brIf(done)

	c.emit(instr.GLOBAL_GET, uint64(listSlot))
	c.emit(instr.GLOBAL_GET, uint64(iSlot))
	c.emit(instr.I64_TO_I32)
	c.emit(instr.GLOBAL_GET, uint64(listSlot))
	c.emit(instr.GLOBAL_GET, uint64(iSlot))
	c.emit(instr.I64_CONST, 1)
	c.emit(instr.I64_SUB)
	c.emit(instr.I64_TO_I32)
	c.emit(instr.ARRAY_GET)
	c.emit(instr.ARRAY_SET)

	c.emit(instr.GLOBAL_GET, uint64(iSlot))
	c.emit(instr.I64_CONST, 1)
	c.emit(instr.I64_SUB)
	c.emit(instr.GLOBAL_SET, uint64(iSlot))
	c.br(top)
	c.bind(done)

	c.emit(instr.GLOBAL_GET, uint64(listSlot))
	c.emit(instr.GLOBAL_GET, uint64(idxSlot))
	c.emit(instr.I64_TO_I32)
	c.emit(instr.GLOBAL_GET, uint64(valueSlot))
	c.emit(instr.ARRAY_SET)
}

func (c *lowerer) emitListExtend() {
	srcSlot := c.tmp()
	listSlot := c.tmp()
	lenSlot := c.tmp()
	iSlot := c.tmp()

	c.emit(instr.GLOBAL_SET, uint64(srcSlot))
	c.emit(instr.GLOBAL_SET, uint64(listSlot))
	c.emit(instr.GLOBAL_GET, uint64(srcSlot))
	c.emit(instr.ARRAY_LEN)
	c.emit(instr.I32_TO_I64_S)
	c.emit(instr.GLOBAL_SET, uint64(lenSlot))
	c.emit(instr.I64_CONST, 0)
	c.emit(instr.GLOBAL_SET, uint64(iSlot))

	top := c.label()
	done := c.label()
	c.bind(top)
	c.emit(instr.GLOBAL_GET, uint64(iSlot))
	c.emit(instr.GLOBAL_GET, uint64(lenSlot))
	c.emit(instr.I64_LT_S)
	c.emit(instr.I32_EQZ)
	c.brIf(done)
	c.emit(instr.GLOBAL_GET, uint64(listSlot))
	c.emit(instr.GLOBAL_GET, uint64(srcSlot))
	c.emit(instr.GLOBAL_GET, uint64(iSlot))
	c.emit(instr.I64_TO_I32)
	c.emit(instr.ARRAY_GET)
	c.emit(instr.I32_CONST, 1)
	c.emit(instr.ARRAY_APPEND)
	c.emit(instr.DROP)
	c.emit(instr.GLOBAL_GET, uint64(iSlot))
	c.emit(instr.I64_CONST, 1)
	c.emit(instr.I64_ADD)
	c.emit(instr.GLOBAL_SET, uint64(iSlot))
	c.br(top)
	c.bind(done)
}

func (c *lowerer) emitListReverse() {
	listSlot := c.tmp()
	iSlot := c.tmp()
	jSlot := c.tmp()
	tmpSlot := c.tmp()

	c.emit(instr.GLOBAL_SET, uint64(listSlot))
	c.emit(instr.I64_CONST, 0)
	c.emit(instr.GLOBAL_SET, uint64(iSlot))
	c.emit(instr.GLOBAL_GET, uint64(listSlot))
	c.emit(instr.ARRAY_LEN)
	c.emit(instr.I32_TO_I64_S)
	c.emit(instr.I64_CONST, 1)
	c.emit(instr.I64_SUB)
	c.emit(instr.GLOBAL_SET, uint64(jSlot))

	top := c.label()
	done := c.label()
	c.bind(top)
	c.emit(instr.GLOBAL_GET, uint64(iSlot))
	c.emit(instr.GLOBAL_GET, uint64(jSlot))
	c.emit(instr.I64_LT_S)
	c.emit(instr.I32_EQZ)
	c.brIf(done)

	c.emit(instr.GLOBAL_GET, uint64(listSlot))
	c.emit(instr.GLOBAL_GET, uint64(iSlot))
	c.emit(instr.I64_TO_I32)
	c.emit(instr.ARRAY_GET)
	c.emit(instr.GLOBAL_SET, uint64(tmpSlot))

	c.emit(instr.GLOBAL_GET, uint64(listSlot))
	c.emit(instr.GLOBAL_GET, uint64(iSlot))
	c.emit(instr.I64_TO_I32)
	c.emit(instr.GLOBAL_GET, uint64(listSlot))
	c.emit(instr.GLOBAL_GET, uint64(jSlot))
	c.emit(instr.I64_TO_I32)
	c.emit(instr.ARRAY_GET)
	c.emit(instr.ARRAY_SET)

	c.emit(instr.GLOBAL_GET, uint64(listSlot))
	c.emit(instr.GLOBAL_GET, uint64(jSlot))
	c.emit(instr.I64_TO_I32)
	c.emit(instr.GLOBAL_GET, uint64(tmpSlot))
	c.emit(instr.ARRAY_SET)

	c.emit(instr.GLOBAL_GET, uint64(iSlot))
	c.emit(instr.I64_CONST, 1)
	c.emit(instr.I64_ADD)
	c.emit(instr.GLOBAL_SET, uint64(iSlot))
	c.emit(instr.GLOBAL_GET, uint64(jSlot))
	c.emit(instr.I64_CONST, 1)
	c.emit(instr.I64_SUB)
	c.emit(instr.GLOBAL_SET, uint64(jSlot))
	c.br(top)
	c.bind(done)
}

func (c *lowerer) dictGet(receiver, result types.Type) *interp.HostFunction {
	dict := receiver.(*types.Dict)
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{receiver.VM(), dict.Key.VM(), result.VM()}, Returns: []vmtypes.Type{result.VM()}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			if val, ok := mapGet(i, params[0], params[1]); ok {
				return []vmtypes.Boxed{val}, nil
			}
			return []vmtypes.Boxed{params[2]}, nil
		},
	)
}

func (c *lowerer) dictValues(receiver, result types.Type) *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{receiver.VM()}, Returns: []vmtypes.Type{result.VM()}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			_, vals := mapEntries(i, params[0])
			return hostabi.AllocArray(i, result.VM().(*vmtypes.ArrayType), vals)
		},
	)
}

func (c *lowerer) dictItems(receiver, result types.Type) *interp.HostFunction {
	tupleType := result.(*types.List).Elem.VM().(*vmtypes.StructType)
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{receiver.VM()}, Returns: []vmtypes.Type{result.VM()}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			keys, vals := mapEntries(i, params[0])
			items := make([]vmtypes.Boxed, 0, len(keys))
			for idx := range keys {
				addr, err := i.Alloc(vmtypes.NewStruct(tupleType, keys[idx], vals[idx]))
				if err != nil {
					return nil, err
				}
				items = append(items, vmtypes.BoxRef(addr))
			}
			return hostabi.AllocArray(i, result.VM().(*vmtypes.ArrayType), items)
		},
	)
}

// dictRest returns a new dict holding receiver minus the keys in the second
// argument. It backs mapping-pattern `**rest` captures.
func (c *lowerer) dictRest(receiver types.Type) *interp.HostFunction {
	keys := types.NewList(receiver.(*types.Dict).Key)
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{receiver.VM(), keys.VM()}, Returns: []vmtypes.Type{receiver.VM()}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			src, err := i.Load(params[0].Ref())
			if err != nil {
				return nil, err
			}
			mt, ok := src.Type().(*vmtypes.MapType)
			if !ok {
				return nil, fmt.Errorf("dict rest on non-map value")
			}
			ks, vs := mapEntries(i, params[0])
			_, exclude := hostabi.ArrayElems(i, params[1])
			out := vmtypes.NewMapForType(mt, len(ks))
			for idx, k := range ks {
				skip := false
				for _, ex := range exclude {
					if hostabi.BoxedEqual(i, k, ex) {
						skip = true
						break
					}
				}
				if !skip {
					mapSet(out, k, vs[idx])
				}
			}
			addr, err := i.Alloc(out)
			if err != nil {
				return nil, err
			}
			return []vmtypes.Boxed{vmtypes.BoxRef(addr)}, nil
		},
	)
}

// mapSet inserts (key, value) into a map value, dispatching on its concrete
// representation (mirrors mapGet).
func mapSet(m vmtypes.Value, key, val vmtypes.Boxed) {
	switch mm := m.(type) {
	case *vmtypes.TypedMap[bool]:
		mm.Set(key.Bool(), val)
	case *vmtypes.TypedMap[int32]:
		mm.Set(key.I32(), val)
	case *vmtypes.TypedMap[int64]:
		mm.Set(key.I64(), val)
	case *vmtypes.TypedMap[float32]:
		mm.Set(key.F32(), val)
	case *vmtypes.TypedMap[float64]:
		mm.Set(key.F64(), val)
	case *vmtypes.Map:
		mm.Set(mapKey(key), vmtypes.MapEntry{Key: key, Value: val})
	}
}

// arraySlice builds `receiver[a:b:c]` for any array-backed VM type (list or
// bytes): it works on the generic array element view (hostabi.ArrayElems),
// so it stays receiver-agnostic and returns a freshly allocated array of the
// same VM type rather than mutating the receiver.
func (c *lowerer) arraySlice(receiver types.Type) *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{receiver.VM(), vmtypes.TypeI64, vmtypes.TypeI64, vmtypes.TypeI64}, Returns: []vmtypes.Type{receiver.VM()}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			typ, elems := hostabi.ArrayElems(i, params[0])
			indexes, err := sliceIndexes(len(elems), hostabi.LoadI64(i, params[1]), hostabi.LoadI64(i, params[2]), hostabi.LoadI64(i, params[3]))
			if err != nil {
				return nil, err
			}
			out := make([]vmtypes.Boxed, 0, len(indexes))
			for _, idx := range indexes {
				out = append(out, elems[idx])
			}
			return hostabi.AllocArray(i, typ, out)
		},
	)
}

func (c *lowerer) listSliceAssign(receiver types.Type) *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{receiver.VM(), vmtypes.TypeI64, vmtypes.TypeI64, vmtypes.TypeI64, receiver.VM()}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			typ, elems := hostabi.ArrayElems(i, params[0])
			_, values := hostabi.ArrayElems(i, params[4])
			start, stop, err := normalizeSliceRange(len(elems), hostabi.LoadI64(i, params[1]), hostabi.LoadI64(i, params[2]), hostabi.LoadI64(i, params[3]))
			if err != nil {
				return nil, err
			}
			if len(values) != stop-start {
				return nil, errListSliceLength
			}
			if err := retainBoxes(i, values); err != nil {
				return nil, err
			}
			if err := releaseBoxes(i, elems[start:stop]); err != nil {
				return nil, err
			}
			out := append([]vmtypes.Boxed(nil), elems...)
			copy(out[start:stop], values)
			if err := i.Store(params[0].Ref(), vmtypes.NewArray(typ, out...)); err != nil {
				return nil, err
			}
			return nil, nil
		},
	)
}

func (c *lowerer) listSliceDelete(receiver types.Type) *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{receiver.VM(), vmtypes.TypeI64, vmtypes.TypeI64, vmtypes.TypeI64}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			typ, elems := hostabi.ArrayElems(i, params[0])
			start, stop, err := normalizeSliceRange(len(elems), hostabi.LoadI64(i, params[1]), hostabi.LoadI64(i, params[2]), hostabi.LoadI64(i, params[3]))
			if err != nil {
				return nil, err
			}
			if err := releaseBoxes(i, elems[start:stop]); err != nil {
				return nil, err
			}
			out := append([]vmtypes.Boxed(nil), elems[:start]...)
			out = append(out, elems[stop:]...)
			if err := i.Store(params[0].Ref(), vmtypes.NewArray(typ, out...)); err != nil {
				return nil, err
			}
			return nil, nil
		},
	)
}

func retainBoxes(i *interp.Interpreter, values []vmtypes.Boxed) error {
	for _, value := range values {
		if value.Kind() == vmtypes.KindRef && value.Ref() != 0 {
			if _, err := i.Retain(value.Ref()); err != nil {
				return err
			}
		}
	}
	return nil
}

func releaseBoxes(i *interp.Interpreter, values []vmtypes.Boxed) error {
	for _, value := range values {
		if value.Kind() == vmtypes.KindRef && value.Ref() != 0 {
			if err := i.Release(value.Ref()); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *lowerer) listIndex(receiver types.Type) *interp.HostFunction {
	elem := receiver.(*types.List).Elem
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{receiver.VM(), elem.VM()}, Returns: []vmtypes.Type{vmtypes.TypeI64}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			_, elems := hostabi.ArrayElems(i, params[0])
			for idx, elem := range elems {
				if hostabi.BoxedEqual(i, elem, params[1]) {
					return []vmtypes.Boxed{vmtypes.BoxI64(int64(idx))}, nil
				}
			}
			return nil, errListIndexValue
		},
	)
}

func (c *lowerer) listExtend(receiver types.Type) *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{receiver.VM(), receiver.VM()}, Returns: []vmtypes.Type{receiver.VM()}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			typ, left := hostabi.ArrayElems(i, params[0])
			_, right := hostabi.ArrayElems(i, params[1])
			out := append(left, right...)
			return hostabi.AllocArray(i, typ, out)
		},
	)
}

func (c *lowerer) dictMerge(receiver types.Type) *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{receiver.VM(), receiver.VM()}, Returns: []vmtypes.Type{receiver.VM()}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			src, err := i.Load(params[0].Ref())
			if err != nil {
				return nil, err
			}
			mt, ok := src.Type().(*vmtypes.MapType)
			if !ok {
				return nil, fmt.Errorf("dict merge on non-map value")
			}
			leftKeys, leftVals := mapEntries(i, params[0])
			rightKeys, rightVals := mapEntries(i, params[1])
			out := vmtypes.NewMapForType(mt, len(leftKeys)+len(rightKeys))
			for idx, key := range leftKeys {
				mapSet(out, key, leftVals[idx])
			}
			for idx, key := range rightKeys {
				mapSet(out, key, rightVals[idx])
			}
			addr, err := i.Alloc(out)
			if err != nil {
				return nil, err
			}
			return []vmtypes.Boxed{vmtypes.BoxRef(addr)}, nil
		},
	)
}

func (c *lowerer) listIter(arg types.Type) *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{arg.VM()}, Returns: []vmtypes.Type{vmtypes.TypeRef}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			_, elems := hostabi.ArrayElems(i, params[0])
			addr, err := i.Alloc(hostabi.NewIterator("list.iterator", elems))
			if err != nil {
				return nil, err
			}
			return []vmtypes.Boxed{vmtypes.BoxRef(addr)}, nil
		},
	)
}

func (c *lowerer) strIter() *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{vmtypes.TypeString}, Returns: []vmtypes.Type{vmtypes.TypeRef}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			s := hostabi.LoadStr(i, params[0])
			values := make([]vmtypes.Boxed, 0, len([]rune(s)))
			for _, r := range s {
				addr, err := i.Alloc(vmtypes.String(string(r)))
				if err != nil {
					return nil, err
				}
				values = append(values, vmtypes.BoxRef(addr))
			}
			addr, err := i.Alloc(hostabi.NewIterator("str.iterator", values))
			if err != nil {
				return nil, err
			}
			return []vmtypes.Boxed{vmtypes.BoxRef(addr)}, nil
		},
	)
}

func (c *lowerer) format(t types.Type) *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{t.VM(), vmtypes.TypeString}, Returns: []vmtypes.Type{vmtypes.TypeString}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			return hostabi.AllocString(i, pyFormat(i, params[0], hostabi.LoadStr(i, params[1])))
		},
	)
}

// reprHost renders a value with repr()/ascii() rules: strings gain quotes and
// escapes, other scalars render like str(). It is static per source type, not a
// runtime __repr__ dispatch.
func (c *lowerer) reprHost(t types.Type, ascii bool) *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{t.VM()}, Returns: []vmtypes.Type{vmtypes.TypeString}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			return hostabi.AllocString(i, pyRepr(i, params[0], ascii))
		},
	)
}

func (c *lowerer) strIndex() *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{vmtypes.TypeString, vmtypes.TypeI64}, Returns: []vmtypes.Type{vmtypes.TypeString}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			s := []rune(hostabi.LoadStr(i, params[0]))
			idx := int(hostabi.LoadI64(i, params[1]))
			if idx < 0 {
				idx += len(s)
			}
			if idx < 0 || idx >= len(s) {
				return nil, interp.ErrIndexOutOfRange
			}
			return hostabi.AllocString(i, string(s[idx]))
		},
	)
}

func (c *lowerer) strUpper() *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{vmtypes.TypeString}, Returns: []vmtypes.Type{vmtypes.TypeString}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			return hostabi.AllocString(i, strings.ToUpper(hostabi.LoadStr(i, params[0])))
		},
	)
}

func (c *lowerer) strLower() *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{vmtypes.TypeString}, Returns: []vmtypes.Type{vmtypes.TypeString}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			return hostabi.AllocString(i, strings.ToLower(hostabi.LoadStr(i, params[0])))
		},
	)
}

func (c *lowerer) strFind() *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{vmtypes.TypeString, vmtypes.TypeString}, Returns: []vmtypes.Type{vmtypes.TypeI64}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			return []vmtypes.Boxed{vmtypes.BoxI64(int64(strings.Index(hostabi.LoadStr(i, params[0]), hostabi.LoadStr(i, params[1]))))}, nil
		},
	)
}

func (c *lowerer) strSlice() *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{vmtypes.TypeString, vmtypes.TypeI64, vmtypes.TypeI64, vmtypes.TypeI64}, Returns: []vmtypes.Type{vmtypes.TypeString}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			runes := []rune(hostabi.LoadStr(i, params[0]))
			indexes, err := sliceIndexes(len(runes), hostabi.LoadI64(i, params[1]), hostabi.LoadI64(i, params[2]), hostabi.LoadI64(i, params[3]))
			if err != nil {
				return nil, err
			}
			var b strings.Builder
			for _, idx := range indexes {
				b.WriteRune(runes[idx])
			}
			return hostabi.AllocString(i, b.String())
		},
	)
}

func (c *lowerer) strSplit() *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{vmtypes.TypeString, vmtypes.TypeString}, Returns: []vmtypes.Type{vmtypes.NewArrayType(vmtypes.TypeString)}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			parts := strings.Split(hostabi.LoadStr(i, params[0]), hostabi.LoadStr(i, params[1]))
			out := make([]vmtypes.Boxed, 0, len(parts))
			for _, part := range parts {
				box, err := hostabi.AllocString(i, part)
				if err != nil {
					return nil, err
				}
				out = append(out, box[0])
			}
			return hostabi.AllocArray(i, vmtypes.NewArrayType(vmtypes.TypeString), out)
		},
	)
}

func (c *lowerer) strJoin() *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{vmtypes.TypeString, vmtypes.NewArrayType(vmtypes.TypeString)}, Returns: []vmtypes.Type{vmtypes.TypeString}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			_, elems := hostabi.ArrayElems(i, params[1])
			parts := make([]string, len(elems))
			for idx, elem := range elems {
				parts[idx] = hostabi.LoadStr(i, elem)
			}
			return hostabi.AllocString(i, strings.Join(parts, hostabi.LoadStr(i, params[0])))
		},
	)
}

func (c *lowerer) exc() *interp.HostFunction {
	excType := c.classes["BaseException"].typ.VM().(*vmtypes.StructType)
	classID := func(name string) int64 { return int64(c.classes[name].classID) }
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{vmtypes.TypeRef}, Returns: []vmtypes.Type{c.classes["BaseException"].typ.VM()}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			class := classID("RuntimeError")
			message := ""
			if params[0].Kind() == vmtypes.KindRef && params[0].Ref() != 0 {
				if val, err := i.Load(params[0].Ref()); err == nil {
					if exc, ok := val.(*vmtypes.Error); ok {
						message = exc.Error()
						switch {
						case errors.Is(exc.Unwrap(), interp.ErrDivideByZero):
							class = classID("ZeroDivisionError")
						case errors.Is(exc.Unwrap(), interp.ErrIndexOutOfRange):
							class = classID("IndexError")
						case errors.Is(exc.Unwrap(), interp.ErrTypeMismatch):
							class = classID("TypeError")
						case errors.Is(exc.Unwrap(), errListIndexValue):
							class = classID("ValueError")
						case errors.Is(exc.Unwrap(), errListSliceLength):
							class = classID("ValueError")
						case errors.Is(exc.Unwrap(), builtins.ErrOrdValue):
							class = classID("ValueError")
						case errors.Is(exc.Unwrap(), builtins.ErrChrValue):
							class = classID("ValueError")
						}
					}
				}
			}
			msg, err := hostabi.AllocString(i, message)
			if err != nil {
				return nil, err
			}
			addr, err := i.Alloc(vmtypes.NewStruct(excType, vmtypes.BoxI64(class), msg[0]))
			if err != nil {
				return nil, err
			}
			return []vmtypes.Boxed{vmtypes.BoxRef(addr)}, nil
		},
	)
}

func parseFormatSpec(spec string) formatSpec {
	f := formatSpec{fill: ' ', precision: -1}
	i := 0
	// [[fill]align]
	if len(spec) >= 2 && isAlign(spec[1]) {
		f.fill, f.align = spec[0], spec[1]
		i = 2
	} else if len(spec) >= 1 && isAlign(spec[0]) {
		f.align = spec[0]
		i = 1
	}
	// [sign]
	if i < len(spec) && (spec[i] == '+' || spec[i] == '-' || spec[i] == ' ') {
		f.sign = spec[i]
		i++
	}
	// ['#'] alternate form — accepted but not applied
	if i < len(spec) && spec[i] == '#' {
		i++
	}
	// ['0'] zero-padding implies '=' alignment with '0' fill
	if i < len(spec) && spec[i] == '0' {
		f.zero = true
		if f.align == 0 {
			f.align, f.fill = '=', '0'
		}
		i++
	}
	// [width]
	for i < len(spec) && spec[i] >= '0' && spec[i] <= '9' {
		f.width = f.width*10 + int(spec[i]-'0')
		i++
	}
	// [grouping] — accepted but not applied
	if i < len(spec) && (spec[i] == ',' || spec[i] == '_') {
		i++
	}
	// ['.'precision]
	if i < len(spec) && spec[i] == '.' {
		i++
		f.precision = 0
		for i < len(spec) && spec[i] >= '0' && spec[i] <= '9' {
			f.precision = f.precision*10 + int(spec[i]-'0')
			i++
		}
	}
	// [type]
	if i < len(spec) {
		f.typ = spec[i]
	}
	return f
}

func isAlign(b byte) bool { return b == '<' || b == '>' || b == '^' || b == '=' }

// pyFormat applies a Python format spec to a boxed scalar. It supports the v1
// scalar subset: fill/alignment, sign, zero-padding, width, precision, and the
// common presentation types (d, b, o, x/X, f/F, e/E, g/G, %, s, c).
func pyFormat(i *interp.Interpreter, v vmtypes.Boxed, spec string) string {
	if spec == "" {
		return hostabi.FormatScalar(i, v)
	}
	f := parseFormatSpec(spec)
	body, sign, numeric := formatBody(i, v, f)
	return padFormat(body, sign, f, numeric)
}

// formatBody renders the value's digits/text without width padding, returning
// the unsigned body, the sign prefix, and whether the value is numeric (so the
// caller can apply '=' zero-padding between the sign and the digits).
func formatBody(i *interp.Interpreter, v vmtypes.Boxed, f formatSpec) (body, sign string, numeric bool) {
	switch f.typ {
	case 'd', 'b', 'o', 'x', 'X', 'c':
		n := hostabi.LoadI64(i, v)
		if v.Kind() == vmtypes.KindI1 {
			n = int64(v.I32())
		}
		if f.typ == 'c' {
			return string(rune(n)), "", false
		}
		return intBody(n, f)
	case 'f', 'F', 'e', 'E', 'g', 'G', '%':
		return floatBody(floatValue(i, v), f)
	case 's', 0:
		if isNumericKind(v) && f.typ == 0 {
			// No explicit type: numbers keep numeric formatting/alignment.
			if v.Kind() == vmtypes.KindF32 || v.Kind() == vmtypes.KindF64 {
				return floatBody(floatValue(i, v), f)
			}
			if v.Kind() == vmtypes.KindI64 {
				return intBody(hostabi.LoadI64(i, v), f)
			}
		}
		s := hostabi.FormatScalar(i, v)
		if f.precision >= 0 && f.precision < len(s) {
			s = s[:f.precision]
		}
		return s, "", false
	default:
		return hostabi.FormatScalar(i, v), "", false
	}
}

func intBody(n int64, f formatSpec) (body, sign string, numeric bool) {
	sign = signPrefix(n < 0, f)
	if n < 0 {
		n = -n
	}
	switch f.typ {
	case 'b':
		body = strconv.FormatInt(n, 2)
	case 'o':
		body = strconv.FormatInt(n, 8)
	case 'x':
		body = strconv.FormatInt(n, 16)
	case 'X':
		body = strings.ToUpper(strconv.FormatInt(n, 16))
	default:
		body = strconv.FormatInt(n, 10)
	}
	return body, sign, true
}

func floatBody(x float64, f formatSpec) (body, sign string, numeric bool) {
	sign = signPrefix(math.Signbit(x) && x != 0 || x < 0, f)
	x = math.Abs(x)
	prec := f.precision
	verb := byte('f')
	switch f.typ {
	case 'e', 'E', 'g', 'G':
		verb = f.typ
		if prec < 0 && (f.typ == 'e' || f.typ == 'E') {
			prec = 6
		}
	case '%':
		x *= 100
		verb = 'f'
		if prec < 0 {
			prec = 6
		}
	default: // 'f','F', or numeric default
		verb = 'f'
		if prec < 0 {
			prec = 6
		}
	}
	body = strconv.FormatFloat(x, verb, prec, 64)
	if f.typ == 'E' || f.typ == 'G' {
		body = strings.ToUpper(body)
	}
	if f.typ == '%' {
		body += "%"
	}
	return body, sign, true
}

func signPrefix(negative bool, f formatSpec) string {
	if negative {
		return "-"
	}
	switch f.sign {
	case '+':
		return "+"
	case ' ':
		return " "
	}
	return ""
}

func floatValue(i *interp.Interpreter, v vmtypes.Boxed) float64 {
	switch v.Kind() {
	case vmtypes.KindF32:
		return float64(v.F32())
	case vmtypes.KindF64:
		return v.F64()
	case vmtypes.KindI1:
		return float64(v.I32())
	default:
		return float64(hostabi.LoadI64(i, v))
	}
}

func isNumericKind(v vmtypes.Boxed) bool {
	switch v.Kind() {
	case vmtypes.KindI64, vmtypes.KindF32, vmtypes.KindF64:
		return true
	default:
		return false
	}
}

// padFormat applies width, fill, and alignment to an already-rendered body.
func padFormat(body, sign string, f formatSpec, numeric bool) string {
	full := sign + body
	pad := f.width - len([]rune(full))
	if pad <= 0 {
		return full
	}
	fill := f.fill
	align := f.align
	if align == 0 {
		if numeric {
			align = '>'
		} else {
			align = '<'
		}
	}
	switch align {
	case '<':
		return full + strings.Repeat(string(fill), pad)
	case '^':
		left := pad / 2
		return strings.Repeat(string(fill), left) + full + strings.Repeat(string(fill), pad-left)
	case '=':
		return sign + strings.Repeat(string(fill), pad) + body
	default: // '>'
		return strings.Repeat(string(fill), pad) + full
	}
}

// pyRepr renders repr(v)/ascii(v) for the supported scalar set. Strings are
// quoted and escaped; other scalars fall back to str()-style rendering.
func pyRepr(i *interp.Interpreter, v vmtypes.Boxed, ascii bool) string {
	if v.Kind() == vmtypes.KindRef && v.Ref() != 0 {
		if val, err := i.Load(v.Ref()); err == nil {
			if s, ok := val.(vmtypes.String); ok {
				return pyStrRepr(string(s), ascii)
			}
		}
	}
	return hostabi.FormatScalar(i, v)
}

// pyStrRepr quotes a string the way CPython's repr()/ascii() do: prefer single
// quotes, switch to double quotes only when the value has a single quote and no
// double quote, and escape control characters (plus non-ASCII when ascii).
func pyStrRepr(s string, ascii bool) string {
	quote := byte('\'')
	if strings.Contains(s, "'") && !strings.Contains(s, "\"") {
		quote = '"'
	}
	var b strings.Builder
	b.WriteByte(quote)
	for _, r := range s {
		switch r {
		case rune(quote):
			b.WriteByte('\\')
			b.WriteRune(r)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			switch {
			case r < 0x20 || r == 0x7f:
				fmt.Fprintf(&b, `\x%02x`, r)
			case ascii && r > 0x7f:
				switch {
				case r > 0xffff:
					fmt.Fprintf(&b, `\U%08x`, r)
				case r > 0xff:
					fmt.Fprintf(&b, `\u%04x`, r)
				default:
					fmt.Fprintf(&b, `\x%02x`, r)
				}
			default:
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte(quote)
	return b.String()
}

func normalizeSliceRange(length int, rawStart, rawStop, rawStep int64) (int, int, error) {
	step := rawStep
	if step == omittedSliceBound {
		step = 1
	}
	if step != 1 {
		return 0, 0, fmt.Errorf("extended slice assignment is not supported")
	}
	startOmitted := rawStart == omittedSliceBound
	stopOmitted := rawStop == omittedSliceBound
	start, stop := int(rawStart), int(rawStop)
	if startOmitted {
		start = 0
	} else if start < 0 {
		start += length
	}
	if stopOmitted {
		stop = length
	} else if stop < 0 {
		stop += length
	}
	if start < 0 {
		start = 0
	}
	if start > length {
		start = length
	}
	if stop < 0 {
		stop = 0
	}
	if stop > length {
		stop = length
	}
	if stop < start {
		stop = start
	}
	return start, stop, nil
}

func sliceIndexes(length int, rawStart, rawStop, rawStep int64) ([]int, error) {
	step := rawStep
	if step == omittedSliceBound {
		step = 1
	}
	if step == 0 {
		return nil, fmt.Errorf("slice step cannot be zero")
	}
	startOmitted := rawStart == omittedSliceBound
	stopOmitted := rawStop == omittedSliceBound
	start, stop := int(rawStart), int(rawStop)
	if step > 0 {
		if startOmitted {
			start = 0
		} else if start < 0 {
			start += length
		}
		if stopOmitted {
			stop = length
		} else if stop < 0 {
			stop += length
		}
		if start < 0 {
			start = 0
		}
		if start > length {
			start = length
		}
		if stop < 0 {
			stop = 0
		}
		if stop > length {
			stop = length
		}
		var out []int
		for i := start; i < stop; i += int(step) {
			out = append(out, i)
		}
		return out, nil
	}
	if startOmitted {
		start = length - 1
	} else if start < 0 {
		start += length
	}
	if stopOmitted {
		stop = -1
	} else if stop < 0 {
		stop += length
	}
	if start < -1 {
		start = -1
	}
	if start >= length {
		start = length - 1
	}
	if stop < -1 {
		stop = -1
	}
	if stop >= length {
		stop = length - 1
	}
	var out []int
	for i := start; i > stop; i += int(step) {
		out = append(out, i)
	}
	return out, nil
}

func mapGet(i *interp.Interpreter, ref vmtypes.Boxed, key vmtypes.Boxed) (vmtypes.Boxed, bool) {
	val, err := i.Load(ref.Ref())
	if err != nil {
		return 0, false
	}
	switch m := val.(type) {
	case *vmtypes.TypedMap[bool]:
		return m.Get(key.Bool())
	case *vmtypes.TypedMap[int32]:
		return m.Get(key.I32())
	case *vmtypes.TypedMap[int64]:
		return m.Get(hostabi.LoadI64(i, key))
	case *vmtypes.TypedMap[float32]:
		return m.Get(key.F32())
	case *vmtypes.TypedMap[float64]:
		return m.Get(key.F64())
	case *vmtypes.Map:
		entry, ok := m.Get(mapKey(key))
		return entry.Value, ok
	default:
		return 0, false
	}
}

func mapEntries(i *interp.Interpreter, ref vmtypes.Boxed) ([]vmtypes.Boxed, []vmtypes.Boxed) {
	val, err := i.Load(ref.Ref())
	if err != nil {
		return nil, nil
	}
	var keys, vals []vmtypes.Boxed
	switch m := val.(type) {
	case *vmtypes.TypedMap[bool]:
		m.Range(func(k bool, v vmtypes.Boxed) {
			keys = append(keys, vmtypes.BoxI1(k))
			vals = append(vals, v)
		})
	case *vmtypes.TypedMap[int32]:
		m.Range(func(k int32, v vmtypes.Boxed) {
			keys = append(keys, vmtypes.BoxI32(k))
			vals = append(vals, v)
		})
	case *vmtypes.TypedMap[int64]:
		m.Range(func(k int64, v vmtypes.Boxed) {
			keys = append(keys, vmtypes.BoxI64(k))
			vals = append(vals, v)
		})
	case *vmtypes.TypedMap[float32]:
		m.Range(func(k float32, v vmtypes.Boxed) {
			keys = append(keys, vmtypes.BoxF32(k))
			vals = append(vals, v)
		})
	case *vmtypes.TypedMap[float64]:
		m.Range(func(k float64, v vmtypes.Boxed) {
			keys = append(keys, vmtypes.BoxF64(k))
			vals = append(vals, v)
		})
	case *vmtypes.Map:
		m.Range(func(_ vmtypes.MapKey, entry vmtypes.MapEntry) {
			keys = append(keys, entry.Key)
			vals = append(vals, entry.Value)
		})
	}
	return keys, vals
}

func mapKey(v vmtypes.Boxed) vmtypes.MapKey {
	switch v.Kind() {
	// bool lowers to i1 uniformly (literals and comparison results alike).
	case vmtypes.KindI1:
		return vmtypes.MapKey{Kind: vmtypes.KindI1, Bits: uint64(uint32(v.I32()))}
	case vmtypes.KindI64:
		return vmtypes.MapKey{Kind: vmtypes.KindI64, Bits: uint64(v.I64())}
	case vmtypes.KindF32:
		return vmtypes.MapKey{Kind: vmtypes.KindF32, Bits: uint64(math.Float32bits(v.F32()))}
	case vmtypes.KindF64:
		return vmtypes.MapKey{Kind: vmtypes.KindF64, Bits: math.Float64bits(v.F64())}
	default:
		return vmtypes.MapKey{Kind: vmtypes.KindRef, Bits: uint64(v.Ref())}
	}
}

// callHost emits a call to a value-returning host function.
func (c *lowerer) callHost(function *interp.HostFunction) {
	c.emit(instr.CONST_GET, uint64(c.constOf(function)))
	c.emit(instr.CALL)
}

// callHostVoid emits a call to a void host function, padding a REF_NULL so the
// expression still leaves exactly one value on the stack.
func (c *lowerer) callHostVoid(function *interp.HostFunction) {
	c.emit(instr.CONST_GET, uint64(c.constOf(function)))
	c.emit(instr.CALL)
	c.emit(instr.REF_NULL)
}

// constOf interns a host function once and returns its constant-pool index,
// keyed by pointer identity to avoid the builder's value-based deduplication
// merging two host functions that share a signature.
func (c *lowerer) constOf(function *interp.HostFunction) int {
	if idx, ok := c.consts[function]; ok {
		return idx
	}
	idx := c.prog.Const(function)
	c.consts[function] = idx
	return idx
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
