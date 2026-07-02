// Package compiler turns minipy source into a runnable minivm program for the
// supported subset (docs/spec): it parses, type-checks, and lowers a module of
// scalar statements, control flow, and functions. Compile returns a
// *program.Program; run it with minivm's interp.New(prog).Run(ctx).
package compiler

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"os"
	"strings"

	"github.com/siyul-park/minipy/ast"
	"github.com/siyul-park/minipy/parser"
	"github.com/siyul-park/minipy/token"
	"github.com/siyul-park/minipy/types"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/interp"
	"github.com/siyul-park/minivm/optimize"
	"github.com/siyul-park/minivm/program"
	vmtypes "github.com/siyul-park/minivm/types"
)

// Option configures a Compile call.
type Option func(*config)

// config holds compile-time options.
type config struct {
	out   io.Writer
	level optimize.Level
}

// Compiler turns minipy source into a runnable minivm program. It mirrors the
// package-level Compile convenience function while keeping options reusable for
// one source stream.
type Compiler struct {
	src       []byte
	err       error
	config    config
	prog      *program.Builder
	code      target
	types     map[ast.Expr]types.Type
	globals   map[string]*global
	functions map[string]*function
	classes   map[string]*class
	lambdas   map[*ast.LambdaExpr]*function
	callSpec  map[*ast.CallExpr]*specialization
	callArgs  map[*ast.CallExpr][]ast.Expr
	locals    map[string]*local
	current   *function
	temps     map[string]int
	host      *host
	consts    map[*interp.HostFunction]int
	loops     []loopLabels // enclosing-loop branch targets, innermost last
	finally   []finallyFrame
	excepts   []int
	next      int
	boxed     map[*local]bool
}

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

// WithOutput binds the sink the compiled program's `print` writes to. It
// defaults to os.Stdout; tests and the REPL pass their own writer.
func WithOutput(w io.Writer) Option {
	return func(c *config) { c.out = w }
}

// WithOptimizationLevel selects the minivm optimizer pipeline used after
// lowering. It defaults to optimize.O0.
func WithOptimizationLevel(level optimize.Level) Option {
	return func(c *config) { c.level = level }
}

// New returns a Compiler over source read from r.
func New(r io.Reader, opts ...Option) *Compiler {
	config := defaultConfig()
	for _, opt := range opts {
		opt(&config)
	}
	src, err := io.ReadAll(r)
	if err != nil {
		// Keep constructor parser-like and error-free; Compile reports the read
		// failure as a regular error.
		src = []byte{}
	}
	return &Compiler{src: src, err: err, config: config}
}

func defaultConfig() config {
	return config{out: os.Stdout, level: optimize.O0}
}

// init resets per-compile lowering state. Compiler is reusable; each
// Compile call gets a fresh builder, symbol tables, loop stack, and host cache.
func (c *Compiler) init(b *program.Builder, check *checker, host *host) {
	c.prog = b
	c.code = mainTarget(b)
	c.types = check.types
	c.globals = check.globals
	c.functions = check.functions
	c.classes = check.classes
	c.lambdas = check.lambdas
	c.callSpec = check.callSpec
	c.callArgs = check.callArgs
	c.locals = nil
	c.current = nil
	c.temps = map[string]int{}
	c.host = host
	c.consts = map[*interp.HostFunction]int{}
	c.loops = nil
	c.finally = nil
	c.excepts = nil
	c.next = len(check.globals)
	c.boxed = map[*local]bool{}
}

func (c *Compiler) emit(op instr.Opcode, operands ...uint64) {
	c.code.emit(op, operands...)
}

func (c *Compiler) label() instr.Label {
	return c.code.label()
}

func (c *Compiler) bind(l instr.Label) {
	c.code.bind(l)
}

func (c *Compiler) br(l instr.Label) {
	c.code.br(l)
}

func (c *Compiler) brIf(l instr.Label) {
	c.code.brIf(l)
}

func (c *Compiler) tryRegion(start, end, catch instr.Label, depth int) {
	c.code.try(start, end, catch, depth)
}

func (c *Compiler) constGet(v vmtypes.Value) {
	c.emit(instr.CONST_GET, uint64(c.prog.Const(v)))
}

func (c *Compiler) typeIndex(t types.Type) uint64 {
	return uint64(c.prog.Type(t.VM()))
}

func (c *Compiler) tmp() int {
	idx := c.next
	c.next++
	return idx
}

// Compile reads minipy source from r, type-checks it, and lowers it into a
// minivm program. On any lexical, syntactic, or semantic error it returns a
// token.ErrorList describing every diagnostic found and a nil program.
func Compile(r io.Reader, opts ...Option) (*program.Program, error) {
	return New(r, opts...).Compile()
}

// Compile parses, type-checks, lowers, optimizes, and verifies c's source.
func (c *Compiler) Compile() (*program.Program, error) {
	if c.err != nil {
		return nil, fmt.Errorf("read source: %w", c.err)
	}
	mod, parseErr := parser.Parse(bytes.NewReader(c.src))

	chk := newChecker()
	chk.check(mod)

	var errs token.ErrorList
	if pl, ok := parseErr.(token.ErrorList); ok {
		errs = append(errs, pl...)
	}
	errs = append(errs, chk.errs...)
	if err := errs.Err(); err != nil {
		return nil, err
	}

	host := newHost(c.config.out, chk.classes)
	b := program.NewBuilder()
	c.init(b, chk, host)
	c.module(mod)

	prog, err := b.Build()
	if err != nil {
		return nil, fmt.Errorf("assemble program: %w", err)
	}

	typesPool := append([]vmtypes.Type(nil), prog.Types...)
	handlers := append([]instr.Handler(nil), prog.Handlers...)
	optimized, err := optimize.NewOptimizer(c.config.level).Optimize(prog)
	if err != nil {
		return nil, fmt.Errorf("optimize program: %w", err)
	}
	optimized.Types = typesPool
	optimized.Handlers = handlers
	if err := program.Verify(optimized); err != nil {
		return nil, fmt.Errorf("verify program: %w", err)
	}
	return optimized, nil
}

// module lowers every top-level statement. The entry function terminates by
// running off the end of its code (the VM has no entry-frame RETURN), so a
// trailing NOP gives any control-flow merge label bound at the very end a valid
// landing instruction — branch targets must stay within the code (analysis
// rejects a jump to len(code)).
func (c *Compiler) module(mod *ast.Module) {
	c.buildCallSpecs(c.callSpec)
	c.block(mod.Body)
	c.emit(instr.NOP)
}

// block lowers a statement sequence (a module body or a compound block).
func (c *Compiler) block(body []ast.Stmt) {
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
func (c *Compiler) truth(cond ast.Expr) (known bool, truth bool) {
	return fold(cond, c.typ, func(e ast.Expr) types.Type { return c.types[e] })
}

func (c *Compiler) stmt(s ast.Stmt) {
	switch n := s.(type) {
	case *ast.AnnAssign:
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
		c.classStmt(n)
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

// deleteStmt lowers `del`. A deleted name is overwritten with minivm's
// uninitialized slot value (REF_NULL for ref kinds, the typed zero for scalars);
// dict items use MAP_DELETE, list items use the native ARRAY_DELETE (remove +
// shift) via emitArrayDelete, and attributes are zeroed in place.
func (c *Compiler) deleteStmt(n *ast.Delete) {
	for _, target := range n.Targets {
		switch t := target.(type) {
		case *ast.Name:
			c.emitZeroValue(c.typ(t.Name))
			c.set(t.Name)
		case *ast.Subscript:
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
func (c *Compiler) assertStmt(n *ast.Assert) {
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
func (c *Compiler) emitMatch(n *ast.Match) {
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

func (c *Compiler) emitTry(n *ast.Try) {
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
		c.tryRegion(start, end, catch, 0)
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
	c.tryRegion(start, end, catch, 0)
}

func (c *Compiler) emitTryFinally(body func(), finalizer func()) {
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
	c.tryRegion(start, end, catch, 0)
}

func (c *Compiler) finalizer(body []ast.Stmt) func() {
	if len(body) == 0 {
		return nil
	}
	return func() { c.block(body) }
}

func (c *Compiler) inlineFinalizers() {
	for i := len(c.finally) - 1; i >= 0; i-- {
		c.finally[i].emit()
	}
}

func (c *Compiler) emitCaughtInstance(errSlot, instSlot int) {
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

// emitTrapInstance lowers a caught VM trap (a null-payload types.Error) into
// an exception instance. It reads the trap's numeric code natively via
// ERROR_CODE and matches it against trapClasses entirely in bytecode,
// skipping excInstance's host round trip for the traps minipy classifies most
// often; an unrecognized code still defers to the host for its message text.
func (c *Compiler) emitTrapInstance(errSlot int) {
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
		c.emitTrapClassInstance(tc.class, tc.message)
		c.br(done)
	}
	c.bind(fallback)
	c.emit(instr.GLOBAL_GET, uint64(errSlot))
	c.callHost(c.host.excInstance)
	c.bind(done)
}

// emitTrapClassInstance builds an exception instance for a natively
// classified trap. It shares excInstance's convention of allocating with
// BaseException's struct type regardless of the target subclass: every
// exception class inherits the same {classID, message} field layout, and
// runtime dispatch (emitExceptionClassID) only ever inspects the classID
// value, not the struct's nominal type.
func (c *Compiler) emitTrapClassInstance(class, message string) {
	info := c.classes[class]
	c.emit(instr.STRUCT_NEW_DEFAULT, c.typeIndex(c.classes["BaseException"].typ))
	c.emit(instr.DUP)
	c.emit(instr.I32_CONST, 0)
	c.emit(instr.I64_CONST, uint64(info.classID))
	c.emit(instr.STRUCT_SET)
	c.emit(instr.DUP)
	c.emit(instr.I32_CONST, 1)
	c.constGet(vmtypes.String(message))
	c.emit(instr.STRUCT_SET)
}

func (c *Compiler) emitExceptionTest(instSlot int, typ ast.Expr, next instr.Label) {
	name := typ.(*ast.Name).Name
	info := c.classes[name]
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

func (c *Compiler) emitExceptionClassID(instSlot int) {
	c.emit(instr.GLOBAL_GET, uint64(instSlot))
	c.emit(instr.I32_CONST, 0)
	c.emit(instr.STRUCT_GET)
}

func (c *Compiler) emitRaise(n *ast.Raise) {
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
			if cls := c.classes[name.Name]; cls != nil && isException(cls) {
				c.emitExceptionInstance(cls, call.Args)
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

func (c *Compiler) emitExceptionInstance(cls *class, args []ast.Expr) {
	c.emit(instr.STRUCT_NEW_DEFAULT, c.typeIndex(cls.typ))
	c.emit(instr.DUP)
	c.emit(instr.I32_CONST, 0)
	c.emit(instr.I64_CONST, uint64(cls.classID))
	c.emit(instr.STRUCT_SET)
	c.emit(instr.DUP)
	c.emit(instr.I32_CONST, 1)
	if len(args) > 0 {
		c.expr(args[0])
	} else {
		c.constGet(vmtypes.String(""))
	}
	c.emit(instr.STRUCT_SET)
}

func (c *Compiler) emitWith(n *ast.With) {
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

func (c *Compiler) methodOwner(name, method string) (*class, *function) {
	for info := c.classes[name]; info != nil; info = info.base {
		if found := info.methods[method]; found != nil {
			return info, found
		}
	}
	return nil, nil
}

// emitPatternTest tests the value in global slot `slot` (static type typ) against
// p, binding captures as it goes, and branches to next on mismatch.
func (c *Compiler) emitPatternTest(p ast.Pattern, slot int, typ types.Type, next instr.Label) {
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
		c.emit(cmpOpcode(token.EQ, typ))
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
func (c *Compiler) bindSlot(name string, slot int) {
	if name == "" || name == "_" {
		return
	}
	c.emit(instr.GLOBAL_GET, uint64(slot))
	c.set(name)
}

// childSlot extracts a sub-value of the slot value at the given index (a
// list/tuple/struct element) into a fresh temp slot and returns it.
func (c *Compiler) childSlot(parent int, index int, op instr.Opcode) int {
	child := c.tmp()
	c.emit(instr.GLOBAL_GET, uint64(parent))
	c.emit(instr.I32_CONST, uint64(index))
	c.emit(op)
	c.emit(instr.GLOBAL_SET, uint64(child))
	return child
}

func (c *Compiler) emitSequenceTest(pat *ast.SequencePattern, slot int, typ types.Type, next instr.Label) {
	switch s := typ.(type) {
	case *types.Tuple:
		for i, e := range pat.Elems {
			child := c.childSlot(slot, i, instr.STRUCT_GET)
			c.emitPatternTest(e, child, s.Elems[i], next)
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

func (c *Compiler) emitMappingTest(pat *ast.MappingPattern, slot int, typ types.Type, next instr.Label) {
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
		c.callHost(c.host.dictRest(d))
		c.set(pat.Rest)
	}
}

func (c *Compiler) emitClassTest(pat *ast.ClassPattern, slot int, typ types.Type, next instr.Label) {
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

func (c *Compiler) assignTarget(target ast.Expr, value ast.Expr) {
	switch t := target.(type) {
	case *ast.Subscript:
		c.expr(t.X)
		c.expr(t.Index)
		c.expr(value)
		switch c.types[t.X].(type) {
		case *types.List:
			c.emit(instr.SWAP)
			c.emit(instr.I64_TO_I32)
			c.emit(instr.SWAP)
			c.emit(instr.ARRAY_SET)
		case *types.Dict:
			c.emit(instr.MAP_SET)
		default:
			panic("unsupported subscript assignment")
		}
	case *ast.TupleLit:
		c.unpackAssign(t, value)
	case *ast.Attribute:
		c.expr(t.X)
		c.emit(instr.I32_CONST, uint64(c.fieldIndex(t)))
		c.expr(value)
		c.emit(instr.STRUCT_SET)
	default:
		panic("unsupported assignment target")
	}
}

func (c *Compiler) augAssignAttribute(n *ast.AugAssign) {
	attr := n.Target.(*ast.Attribute)
	c.emitBinary(n.Op, c.types[attr], c.types[n.Value],
		func() { c.attribute(attr) },
		func() { c.expr(n.Value) })
	c.expr(attr.X)
	c.emit(instr.SWAP)
	c.emit(instr.I32_CONST, uint64(c.fieldIndex(attr)))
	c.emit(instr.SWAP)
	c.emit(instr.STRUCT_SET)
}

func (c *Compiler) classStmt(*ast.Class) {
	// Classes are compile-time metadata; instances are structs.
}

func (c *Compiler) unpackAssign(target *ast.TupleLit, value ast.Expr) {
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

func (c *Compiler) unpackAssignStar(target *ast.TupleLit, value ast.Expr, valueSlot int, star int) {
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
				c.callHost(c.host.listSlice(c.types[value]))
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

func (c *Compiler) emitUnpackIndex(value ast.Expr, valueSlot int, idx int) {
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

func (c *Compiler) emitListUnpackIndex(valueSlot int, idx int) {
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

func (c *Compiler) emitTupleRestList(valueSlot int, tuple *types.Tuple, star int, suffix int) {
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

func (c *Compiler) get(name string) {
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
	c.emit(instr.GLOBAL_GET, uint64(c.globals[name].index))
}

func (c *Compiler) set(name string) {
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
	c.emit(instr.GLOBAL_SET, uint64(c.globals[name].index))
}

// narrowCast unboxes a ref-backed binding (union/Any) to the concrete type the
// checker narrowed this use to. Flow-proven narrowing (isinstance / is-None)
// recorded a concrete type on the use node while the slot stays a ref, so a
// checked REF_CAST recovers the unboxed value. No cast is emitted when the use
// itself is still dynamic or None.
func (c *Compiler) narrowCast(x *ast.Name) {
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

func (c *Compiler) typ(name string) types.Type {
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
	return c.globals[name].typ
}

// emitIf lowers `if`/`elif`/`else`: invert the condition and branch over the
// then-block to the else-block (docs/spec/05-codegen.md).
func (c *Compiler) emitIf(n *ast.If) {
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
func (c *Compiler) emitWhile(n *ast.While) {
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
func (c *Compiler) emitFor(n *ast.For) {
	if c.iterates(c.types[n.Iter]) {
		c.emitIteratorFor(n, func() {
			c.iterate(n.Iter, c.types[n.Iter])
		})
		return
	}
	c.emitIterableFor(n)
}

func (c *Compiler) emitIteratorFor(n *ast.For, emitIter func()) {
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

func (c *Compiler) iterates(t types.Type) bool {
	switch t.(type) {
	case *types.Iterator, *types.Dict, *types.Set:
		return true
	default:
		return types.Equal(t, types.Str)
	}
}

func (c *Compiler) iterate(expr ast.Expr, typ types.Type) {
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
			c.callHost(c.host.strIter())
		}
	}
}

func (c *Compiler) emitIterableFor(n *ast.For) {
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

func (c *Compiler) setLoopTarget(target ast.Expr) {
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

func (c *Compiler) functionStmt(n *ast.Function) {
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
	c.funcValue(info, n.Body)
	if c.current != nil {
		c.set(n.Name.Name)
		return
	}
	c.emit(instr.GLOBAL_SET, uint64(info.slot.index))
}

// buildSpec compiles one specialization to a function constant, recording its
// index on the instance. Specializations are top-level and capture nothing.
func (c *Compiler) buildSpec(spec *specialization) {
	if spec == nil || spec.emitted || spec.emitting {
		return
	}
	spec.emitting = true
	c.buildCallSpecs(spec.calls)

	info := spec.info
	fb := vmtypes.NewFunctionBuilder(&vmtypes.FunctionType{
		Params:  vmParams(info),
		Returns: vmReturns(info.result),
	})
	fb.WithLocals(vmLocals(info)...)

	child := *c
	child.code = fnTarget(fb)
	child.current = info
	child.locals = info.locals
	child.types = spec.types
	child.callSpec = spec.calls
	child.callArgs = spec.args
	child.loops = nil
	child.finally = nil
	child.excepts = nil
	child.temps = map[string]int{}
	child.boxed = map[*local]bool{}
	child.block(info.body)
	child.emitNoneReturn()
	if child.next > c.next {
		c.next = child.next
	}

	f, err := fb.Build()
	if err != nil {
		panic(err)
	}
	info.constIdx = c.prog.Const(f)
	spec.emitted = true
	spec.emitting = false
}

func (c *Compiler) buildCallSpecs(calls map[*ast.CallExpr]*specialization) {
	for _, spec := range calls {
		c.buildSpec(spec)
	}
}

func (c *Compiler) function(n *ast.Function) *function {
	if c.current != nil {
		return c.current.children[n.Name.Name]
	}
	return c.functions[n.Name.Name]
}

func (c *Compiler) funcValue(info *function, body []ast.Stmt) {
	fb := vmtypes.NewFunctionBuilder(&vmtypes.FunctionType{
		Params:  vmParams(info),
		Returns: vmReturns(info.result),
	})
	fb.WithLocals(vmLocals(info)...)
	fb.WithCaptures(vmCaps(info)...)

	child := *c
	child.code = fnTarget(fb)
	child.current = info
	child.locals = info.locals
	child.loops = nil
	child.finally = nil
	child.excepts = nil
	child.temps = map[string]int{}
	child.boxed = map[*local]bool{}
	child.block(body)
	child.emitNoneReturn()
	if child.next > c.next {
		c.next = child.next
	}

	function, err := fb.Build()
	if err != nil {
		panic(err)
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

func (c *Compiler) emitCapture(cap *capture) {
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

func (c *Compiler) returnStmt(n *ast.Return) {
	if n.Value != nil {
		c.expr(n.Value)
	} else {
		c.emit(instr.REF_NULL)
	}
	c.inlineFinalizers()
	c.emit(instr.RETURN)
}

func (c *Compiler) yield(n *ast.Yield) {
	if n.Value != nil {
		c.expr(n.Value)
	} else {
		c.emit(instr.REF_NULL)
	}
	c.emit(instr.YIELD)
	c.emit(instr.DROP)
}

func (c *Compiler) emitNoneReturn() {
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
func (c *Compiler) expr(n ast.Expr) {
	switch x := n.(type) {
	case *ast.IntLit:
		c.emit(instr.I64_CONST, uint64(x.Value))
	case *ast.FloatLit:
		c.emit(instr.F64_CONST, math.Float64bits(x.Value))
	case *ast.BoolLit:
		// bool lowers to i1. There is no i1 const opcode, so push the value as
		// i32 and normalize to i1 via `!= 0` so the result carries KindI1 — this
		// keeps bool literals interchangeable with comparison results (e.g. as
		// map keys, where the verifier matches key kinds exactly).
		if x.Value {
			c.emit(instr.I32_CONST, 1)
		} else {
			c.emit(instr.I32_CONST, 0)
		}
		c.emit(instr.I32_CONST, 0)
		c.emit(instr.I32_NE)
	case *ast.NoneLit:
		c.emit(instr.REF_NULL)
	case *ast.StrLit:
		c.constGet(vmtypes.String(x.Value))
	case *ast.Name:
		c.get(x.Name)
		c.narrowCast(x)
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
	case *ast.GeneratorExp:
		c.generatorExp(x)
	case *ast.TupleLit:
		c.tupleLit(x)
	case *ast.Subscript:
		c.subscript(x)
	case *ast.Attribute:
		c.attribute(x)
	case *ast.FString:
		c.fstring(x)
	}
}

func (c *Compiler) listLit(x *ast.ListLit) {
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
				c.callHost(c.host.listExtend(c.types[x]))
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

func (c *Compiler) dictLit(x *ast.DictLit) {
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
				c.callHost(c.host.dictMerge(c.types[x]))
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

func (c *Compiler) appendListSlot(slot int, emitElem func()) {
	c.emit(instr.GLOBAL_GET, uint64(slot))
	emitElem()
	c.emit(instr.I32_CONST, 1)
	c.emit(instr.ARRAY_APPEND)
	c.emit(instr.DROP)
}

func (c *Compiler) appendTupleToListSlot(slot int, tupleExpr ast.Expr, tuple *types.Tuple) {
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

func (c *Compiler) setLit(x *ast.SetLit) {
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
				c.callHost(c.host.dictMerge(c.types[x]))
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

func (c *Compiler) lambda(x *ast.LambdaExpr) {
	info := c.lambdas[x]
	if info == nil {
		return
	}
	c.funcValue(info, []ast.Stmt{&ast.Return{Base: ast.Base{Position: x.Pos()}, Value: x.Body}})
}

func (c *Compiler) listComp(x *ast.ListComp) {
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

func (c *Compiler) dictComp(x *ast.DictComp) {
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

func (c *Compiler) setComp(x *ast.SetComp) {
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

func (c *Compiler) generatorExp(x *ast.GeneratorExp) {
	iter := c.types[x].(*types.Iterator)
	listType := types.NewList(iter.Elem)
	slot := c.tmp()
	c.emit(instr.I32_CONST, 0)
	c.emit(instr.ARRAY_NEW_DEFAULT, c.typeIndex(listType))
	c.emit(instr.GLOBAL_SET, uint64(slot))
	c.comp(x.Clauses, func() {
		c.appendListSlot(slot, func() { c.expr(x.Elem) })
	})
	c.emit(instr.GLOBAL_GET, uint64(slot))
	c.callHost(c.host.listIter(listType))
}

func (c *Compiler) comp(clauses []*ast.Comprehension, body func()) {
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
func (c *Compiler) iterComp(clause *ast.Comprehension, targetSlot int, body func()) {
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

func (c *Compiler) iteratorComp(clause *ast.Comprehension, targetSlot int, emitIter func(), body func()) {
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

func (c *Compiler) compFilters(filters []ast.Expr, cont instr.Label) {
	for _, filter := range filters {
		c.expr(filter)
		c.emit(instr.I32_EQZ)
		c.brIf(cont)
	}
}

func (c *Compiler) tupleLit(x *ast.TupleLit) {
	t := c.types[x].(*types.Tuple)
	c.emit(instr.STRUCT_NEW_DEFAULT, c.typeIndex(t))
	for i, elem := range x.Elems {
		c.emit(instr.DUP)
		c.emit(instr.I32_CONST, uint64(i))
		c.expr(elem)
		c.emit(instr.STRUCT_SET)
	}
}

func (c *Compiler) subscript(x *ast.Subscript) {
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
	default:
		if types.Equal(c.types[x.X], types.Str) {
			c.callStringIndex(x)
		}
	}
}

func (c *Compiler) namedExpr(x *ast.NamedExpr) {
	c.expr(x.Value)
	c.set(x.Target.Name)
	c.get(x.Target.Name)
}

func (c *Compiler) slice(x *ast.Subscript, s *ast.Slice) {
	c.expr(x.X)
	c.sliceBound(s.Lower)
	c.sliceBound(s.Upper)
	c.sliceBound(s.Step)
	switch c.types[x.X].(type) {
	case *types.List:
		c.callHost(c.host.listSlice(c.types[x.X]))
	default:
		c.callHost(c.host.strSlice)
	}
}

func (c *Compiler) sliceBound(x ast.Expr) {
	if x == nil {
		c.emit(instr.I64_CONST, uint64(1)<<63)
		return
	}
	c.expr(x)
}

func (c *Compiler) callStringIndex(x *ast.Subscript) {
	// Stack already has string and index; helper returns a one-codepoint string.
	c.callHost(c.host.strIndex)
}

func (c *Compiler) attribute(x *ast.Attribute) {
	c.expr(x.X)
	c.emit(instr.I32_CONST, uint64(c.fieldIndex(x)))
	c.emit(instr.STRUCT_GET)
}

func (c *Compiler) fieldIndex(x *ast.Attribute) int {
	cls := c.types[x.X].(*types.Class)
	return c.classes[cls.Name].fieldIndex[x.Name]
}

func (c *Compiler) fstring(x *ast.FString) {
	c.constGet(vmtypes.String(""))
	for _, part := range x.Parts {
		c.fstringPart(part)
		c.emit(instr.STRING_CONCAT)
	}
}

func (c *Compiler) fstringPart(part ast.FStringPart) {
	switch p := part.(type) {
	case *ast.FStringText:
		c.constGet(vmtypes.String(p.Value))
	case *ast.FStringExpr:
		if p.Debug != "" {
			c.constGet(vmtypes.String(p.Debug))
			c.expr(p.Expr)
			c.callHost(c.host.str)
			c.emit(instr.STRING_CONCAT)
			return
		}
		c.expr(p.Expr)
		if format, ok := staticFStringFormat(p.Format); ok && format != "" {
			c.constGet(vmtypes.String(format))
			c.callHost(c.host.format(c.types[p.Expr]))
			return
		}
		if !types.Equal(c.types[p.Expr], types.Str) || p.Conversion != 0 || len(p.Format) > 0 {
			c.callHost(c.host.str)
		}
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
func (c *Compiler) ifExp(x *ast.IfExp) {
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

func (c *Compiler) unary(x *ast.UnaryExpr) {
	switch x.Op {
	case token.NOT:
		c.expr(x.X)
		c.emit(instr.I32_EQZ)
	case token.PLUS:
		c.expr(x.X)
	case token.MINUS:
		if c.types[x.X] == types.Float {
			c.expr(x.X)
			c.emit(instr.F64_NEG)
		} else {
			c.emit(instr.I64_CONST, 0)
			c.expr(x.X)
			c.emit(instr.I64_SUB)
		}
	case token.TILDE:
		c.expr(x.X)
		c.emit(instr.I64_CONST, ^uint64(0))
		c.emit(instr.I64_XOR)
	}
}

// emitBinary lowers a binary operation. pushLeft/pushRight push the operands; the
// operator and operand types decide the opcode sequence. The handful of ops that
// need more than one opcode or a host call are special-cased; the rest map to a
// single opcode via simpleBinOp.
func (c *Compiler) emitBinary(op token.Type, left, right types.Type, pushLeft, pushRight func()) {
	switch op {
	case token.SLASH: // true division always yields float
		pushLeft()
		if left == types.Int {
			c.emit(instr.I64_TO_F64_S)
		}
		pushRight()
		if left == types.Int {
			c.emit(instr.I64_TO_F64_S)
		}
		c.emit(instr.F64_DIV)
	case token.DOUBLESLASH:
		pushLeft()
		pushRight()
		if left == types.Int {
			c.emit(instr.I64_DIV_S)
		} else {
			c.emit(instr.F64_DIV)
			c.emit(instr.F64_FLOOR)
		}
	case token.PERCENT:
		pushLeft()
		pushRight()
		if left == types.Int {
			c.emit(instr.I64_REM_S)
		} else {
			c.emit(instr.F64_MOD)
		}
	case token.DOUBLESTAR:
		pushLeft()
		pushRight()
		if left == types.Int {
			c.callHost(c.host.powInt)
		} else {
			c.callHost(c.host.powFloat)
		}
	case token.PLUS:
		pushLeft()
		pushRight()
		if left == types.Str {
			c.emit(instr.STRING_CONCAT)
		} else {
			c.emit(simpleBinOp(op, left))
		}
	default:
		pushLeft()
		pushRight()
		c.emit(simpleBinOp(op, left))
	}
}

// boolOp lowers short-circuiting `and`/`or` (docs/spec/05-codegen.md).
func (c *Compiler) boolOp(x *ast.BoolOp) {
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
func (c *Compiler) compare(x *ast.Compare) {
	if len(x.Ops) == 1 {
		c.emitCmp(x.X, x.Ops[0], x.Comparators[0])
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

func (c *Compiler) emitCmp(left ast.Expr, op token.Type, right ast.Expr) {
	c.expr(left)
	c.expr(right)
	c.emitCmpStack(op, c.types[left], c.types[right])
}

func (c *Compiler) emitCmpStack(op token.Type, left types.Type, right types.Type) {
	if op == token.IN || op == token.NOTIN {
		c.emit(instr.SWAP)
		c.containsType(op, left, right)
		return
	}
	if op == token.IS || op == token.ISNOT {
		c.emit(instr.REF_EQ)
		if op == token.ISNOT {
			c.emit(instr.I32_EQZ)
		}
		return
	}
	c.emit(cmpOpcode(op, left))
}

func (c *Compiler) contains(op token.Type, left ast.Expr, right ast.Expr) {
	c.containsType(op, c.types[left], c.types[right])
}

func (c *Compiler) containsType(op token.Type, left types.Type, right types.Type) {
	switch right.(type) {
	case *types.Dict:
		c.emit(instr.MAP_LOOKUP)
		c.emit(instr.SWAP)
		c.emit(instr.DROP)
	case *types.List:
		c.callHost(c.host.listContains(left, right))
	default:
		if types.Equal(right, types.Str) {
			c.callHost(c.host.strContains)
		}
	}
	if op == token.NOTIN {
		c.emit(instr.I32_EQZ)
	}
}

func (c *Compiler) emitResumeIterator(slot int) {
	c.emit(instr.GLOBAL_GET, uint64(slot))
	c.emit(instr.REF_NULL)
	c.emit(instr.RESUME)
	c.emit(instr.DROP)
}

// call lowers a direct builtin or user-function call. Inline builtins emit
// opcodes directly; print/str and the parse helpers go through host functions.
func (c *Compiler) call(x *ast.CallExpr) {
	if attr, ok := x.Fn.(*ast.Attribute); ok {
		c.methodCall(x, attr)
		return
	}
	name, isName := x.Fn.(*ast.Name)
	if isName && name.Name == "isinstance" && len(x.Args) == 2 {
		// isinstance(value, T) → REF_TEST recovers the runtime tag as i32; the
		// trailing I32_NE normalizes it to a minipy bool (i1).
		c.expr(x.Args[0])
		c.emit(instr.REF_TEST, c.typeIndex(c.types[x.Args[1]]))
		c.emit(instr.I32_CONST, 0)
		c.emit(instr.I32_NE)
		return
	}
	if isName {
		if cls, ok := c.classes[name.Name]; ok {
			c.construct(x, cls)
			return
		}
		if info, ok := c.functions[name.Name]; ok {
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
		if !isBuiltin(name.Name) {
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

	var arg ast.Expr
	var typ types.Type
	if len(x.Args) > 0 {
		arg = x.Args[0]
		typ = c.types[arg]
	}

	switch name.Name {
	case "print":
		c.expr(arg)
		c.callHostVoid(c.host.print)
	case "str":
		c.expr(arg)
		if typ != types.Str {
			c.callHost(c.host.str)
		}
	case "int":
		c.expr(arg)
		switch typ {
		case types.Float:
			c.emit(instr.F64_TO_I64_S)
		case types.Bool:
			c.emit(instr.I32_TO_I64_S)
		case types.Str:
			c.callHost(c.host.intParse)
		}
	case "float":
		c.expr(arg)
		switch typ {
		case types.Int:
			c.emit(instr.I64_TO_F64_S)
		case types.Bool:
			c.emit(instr.I32_TO_F64_S)
		case types.Str:
			c.callHost(c.host.floatParse)
		}
	case "bool":
		c.expr(arg)
		switch typ {
		case types.Int:
			c.emit(instr.I64_CONST, 0)
			c.emit(instr.I64_NE)
		case types.Float:
			c.emit(instr.F64_CONST, math.Float64bits(0))
			c.emit(instr.F64_NE)
		case types.Str:
			c.emit(instr.STRING_LEN)
			c.emit(instr.I32_CONST, 0)
			c.emit(instr.I32_NE)
		default:
			switch t := typ.(type) {
			case *types.List:
				c.emit(instr.ARRAY_LEN)
				c.emit(instr.I32_CONST, 0)
				c.emit(instr.I32_NE)
			case *types.Dict, *types.Set:
				c.emit(instr.MAP_LEN)
				c.emit(instr.I32_CONST, 0)
				c.emit(instr.I32_NE)
			case *types.Tuple:
				c.emit(instr.DROP)
				if len(t.Elems) == 0 {
					c.emit(instr.I32_CONST, 0)
				} else {
					c.emit(instr.I32_CONST, 1)
				}
			case *types.Iterator, *types.Callable, *types.Class:
				c.emit(instr.REF_IS_NULL)
				c.emit(instr.I32_EQZ)
			}
		}
	case "abs":
		if typ == types.Int {
			c.absInt(arg)
		} else {
			c.expr(arg)
			c.emit(instr.F64_ABS)
		}
	case "len":
		c.expr(arg)
		switch typ.(type) {
		case *types.List:
			c.emit(instr.ARRAY_LEN)
		case *types.Dict, *types.Set:
			c.emit(instr.MAP_LEN)
		case *types.Tuple:
			c.emit(instr.I32_CONST, uint64(len(typ.(*types.Tuple).Elems)))
		default:
			c.emit(instr.STRING_LEN)
		}
		c.emit(instr.I32_TO_I64_S)
	case "enumerate":
		c.expr(arg)
		c.callHost(c.host.enumerate(c.types[x]))
	case "zip":
		c.expr(x.Args[0])
		c.expr(x.Args[1])
		c.callHost(c.host.zip(c.types[x]))
	case "range":
		c.rangeCall(x)
	case "iter":
		c.iterCall(arg, typ)
	case "next":
		c.nextCall(arg)
	}
}

func (c *Compiler) functionCallArgs(x *ast.CallExpr, info *function) []ast.Expr {
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

func (c *Compiler) construct(x *ast.CallExpr, cls *class) {
	if isException(cls) {
		c.emitExceptionInstance(cls, x.Args)
		return
	}
	c.emit(instr.STRUCT_NEW_DEFAULT, c.typeIndex(cls.typ))
	c.applyFieldDefaults(cls)
	args := c.callArgs[x]
	if args == nil {
		args = x.Args
	}
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

func (c *Compiler) applyFieldDefaults(cls *class) {
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

func (c *Compiler) rangeCall(x *ast.CallExpr) {
	switch len(x.Args) {
	case 1:
		c.emit(instr.I64_CONST, 0)
		c.expr(x.Args[0])
		c.emit(instr.I64_CONST, 1)
	case 2:
		c.expr(x.Args[0])
		c.expr(x.Args[1])
		c.emit(instr.I64_CONST, 1)
	default:
		c.expr(x.Args[0])
		c.expr(x.Args[1])
		c.expr(x.Args[2])
	}
	c.callHost(c.host.rangeIter)
}

func (c *Compiler) iterCall(arg ast.Expr, typ types.Type) {
	if _, ok := typ.(*types.Iterator); ok {
		c.expr(arg)
		return
	}
	c.expr(arg)
	switch typ.(type) {
	case *types.Dict, *types.Set:
		c.emit(instr.MAP_ITER)
	case *types.List:
		c.callHost(c.host.listIter(typ))
	default:
		if types.Equal(typ, types.Str) {
			c.callHost(c.host.strIter())
		}
	}
}

func (c *Compiler) nextCall(arg ast.Expr) {
	valSlot := c.tmp()
	done := c.label()
	end := c.label()
	c.expr(arg)
	c.emit(instr.DUP)
	c.emit(instr.CORO_DONE)
	c.brIf(done)
	c.emit(instr.DUP)
	c.emit(instr.CORO_VALUE)
	c.emit(instr.GLOBAL_SET, uint64(valSlot))
	c.emit(instr.REF_NULL)
	c.emit(instr.RESUME)
	c.emit(instr.DROP)
	c.emit(instr.GLOBAL_GET, uint64(valSlot))
	c.br(end)
	c.bind(done)
	c.emit(instr.REF_NULL)
	c.emit(instr.RESUME)
	c.emit(instr.DROP)
	c.emit(instr.UNREACHABLE)
	c.bind(end)
}

func (c *Compiler) methodCall(x *ast.CallExpr, attr *ast.Attribute) {
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
		c.callHost(c.host.dictGet(recvType, c.types[x]))
	case "keys":
		c.emit(instr.MAP_KEYS)
	case "values":
		c.callHost(c.host.dictValues(recvType, c.types[x]))
	case "items":
		c.callHost(c.host.dictItems(recvType, c.types[x]))
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
	case "upper":
		c.callHost(c.host.strUpper)
	case "lower":
		c.callHost(c.host.strLower)
	case "split":
		if len(x.Args) == 0 {
			c.constGet(vmtypes.String(" "))
		}
		c.callHost(c.host.strSplit)
	case "join":
		c.callHost(c.host.strJoin)
	case "find":
		c.callHost(c.host.strFind)
	default:
		panic("unsupported method")
	}
}

func (c *Compiler) emitZeroValue(t types.Type) {
	switch {
	case types.Equal(t, types.Int):
		c.emit(instr.I64_CONST, 0)
	case types.Equal(t, types.Float):
		c.emit(instr.F64_CONST, 0)
	case types.Equal(t, types.Bool):
		c.emit(instr.I32_CONST, 0)
	case types.Equal(t, types.Str):
		c.constGet(vmtypes.String(""))
	default:
		c.emit(instr.REF_NULL)
	}
}

// absInt lowers abs() on an int inline: branch on the sign and negate when
// negative (the entry frame has no locals for a branchless trick).
func (c *Compiler) absInt(arg ast.Expr) {
	c.expr(arg)
	c.emit(instr.DUP)
	c.emit(instr.I64_CONST, 0)
	c.emit(instr.I64_LT_S)
	neg := c.label()
	end := c.label()
	c.brIf(neg)
	c.br(end)
	c.bind(neg)
	c.emit(instr.I64_CONST, 0)
	c.emit(instr.SWAP)
	c.emit(instr.I64_SUB)
	c.bind(end)
}

// emitArrayDelete lowers `list, index(i64) -> removed`: it normalizes a
// Python-style negative index (relative to the end) before the native
// ARRAY_DELETE, which traps on an already non-negative out-of-range index.
// Backs `del list[i]` and `list.pop(i)`.
func (c *Compiler) emitArrayDelete() {
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

// callHost emits a call to a value-returning host function.
func (c *Compiler) callHost(function *interp.HostFunction) {
	c.emit(instr.CONST_GET, uint64(c.constOf(function)))
	c.emit(instr.CALL)
}

// callHostVoid emits a call to a void host function, padding a REF_NULL so the
// expression still leaves exactly one value on the stack.
func (c *Compiler) callHostVoid(function *interp.HostFunction) {
	c.emit(instr.CONST_GET, uint64(c.constOf(function)))
	c.emit(instr.CALL)
	c.emit(instr.REF_NULL)
}

// constOf interns a host function once and returns its constant-pool index,
// keyed by pointer identity to avoid the builder's value-based deduplication
// merging two host functions that share a signature.
func (c *Compiler) constOf(function *interp.HostFunction) int {
	if idx, ok := c.consts[function]; ok {
		return idx
	}
	idx := c.prog.Const(function)
	c.consts[function] = idx
	return idx
}

// simpleBinOp returns the single opcode for an operator that maps directly to
// one (`+ - *` for int/float; `& | ^ << >>` for int).
func simpleBinOp(op token.Type, t types.Type) instr.Opcode {
	if t == types.Float {
		switch op {
		case token.PLUS:
			return instr.F64_ADD
		case token.MINUS:
			return instr.F64_SUB
		case token.STAR:
			return instr.F64_MUL
		}
		return instr.NOP
	}
	switch op {
	case token.PLUS:
		return instr.I64_ADD
	case token.MINUS:
		return instr.I64_SUB
	case token.STAR:
		return instr.I64_MUL
	case token.AMP:
		return instr.I64_AND
	case token.PIPE:
		return instr.I64_OR
	case token.CARET:
		return instr.I64_XOR
	case token.LSHIFT:
		return instr.I64_SHL
	case token.RSHIFT:
		return instr.I64_SHR_S
	}
	return instr.NOP
}

func cmpOpcode(op token.Type, t types.Type) instr.Opcode {
	switch t {
	case types.Float:
		switch op {
		case token.EQ:
			return instr.F64_EQ
		case token.NE:
			return instr.F64_NE
		case token.LT:
			return instr.F64_LT
		case token.LE:
			return instr.F64_LE
		case token.GT:
			return instr.F64_GT
		case token.GE:
			return instr.F64_GE
		}
	case types.Str:
		switch op {
		case token.EQ:
			return instr.STRING_EQ
		case token.NE:
			return instr.STRING_NE
		case token.LT:
			return instr.STRING_LT
		case token.LE:
			return instr.STRING_LE
		case token.GT:
			return instr.STRING_GT
		case token.GE:
			return instr.STRING_GE
		}
	case types.Bool:
		switch op {
		case token.EQ:
			return instr.I32_EQ
		case token.NE:
			return instr.I32_NE
		case token.LT:
			return instr.I32_LT_S
		case token.LE:
			return instr.I32_LE_S
		case token.GT:
			return instr.I32_GT_S
		case token.GE:
			return instr.I32_GE_S
		}
	default: // Int
		switch op {
		case token.EQ:
			return instr.I64_EQ
		case token.NE:
			return instr.I64_NE
		case token.LT:
			return instr.I64_LT_S
		case token.LE:
			return instr.I64_LE_S
		case token.GT:
			return instr.I64_GT_S
		case token.GE:
			return instr.I64_GE_S
		}
	}
	return instr.NOP
}
