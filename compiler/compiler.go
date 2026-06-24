// Package compiler turns minipy source into a runnable minivm program for the
// M0–M2 subset (docs/spec): it parses, type-checks, and lowers a module of
// scalar statements, M1 control flow, and M2 functions. Compile returns a
// *program.Program; run it with minivm's interp.New(prog).Run(ctx).
package compiler

import (
	"fmt"
	"io"
	"math"
	"os"

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
	out io.Writer
}

// loopLabels are the branch targets for the loop currently being lowered: cont
// for `continue` (re-test for while, the increment step for range-for) and brk
// for `break` (past any else block).
type loopLabels struct {
	cont instr.Label
	brk  instr.Label
}

type target struct {
	emit  func(instr.Opcode, ...uint64)
	label func() instr.Label
	bind  func(instr.Label)
	br    func(instr.Label)
	brIf  func(instr.Label)
}

// compiler lowers a typed module to a minivm program. It assumes the checker has
// already validated the module, so it never re-reports errors; it only relies on
// the type table and global symbol table.
type compiler struct {
	prog     *program.Builder
	code     target
	exprType map[ast.Expr]types.Type
	globals  map[string]*global
	funcs    map[string]*fn
	locals   map[string]*local
	host     *hostFuncs
	constIdx map[*interp.HostFunction]int
	loops    []loopLabels // enclosing-loop branch targets, innermost last
}

func newCompiler(b *program.Builder, exprType map[ast.Expr]types.Type, globals map[string]*global, funcs map[string]*fn, host *hostFuncs) *compiler {
	return &compiler{
		prog:     b,
		code:     mainTarget(b),
		exprType: exprType,
		globals:  globals,
		funcs:    funcs,
		host:     host,
		constIdx: map[*interp.HostFunction]int{},
	}
}

func mainTarget(b *program.Builder) target {
	return target{
		emit:  func(op instr.Opcode, operands ...uint64) { b.Emit(op, operands...) },
		label: b.Label,
		bind:  func(l instr.Label) { b.Bind(l) },
		br:    func(l instr.Label) { b.Br(l) },
		brIf:  func(l instr.Label) { b.BrIf(l) },
	}
}

func fnTarget(b *vmtypes.FunctionBuilder) target {
	return target{
		emit:  func(op instr.Opcode, operands ...uint64) { b.Emit(instr.New(op, operands...)) },
		label: b.Label,
		bind:  func(l instr.Label) { b.Bind(l) },
		br:    func(l instr.Label) { b.Br(l) },
		brIf:  func(l instr.Label) { b.BrIf(l) },
	}
}

// WithOutput binds the sink the compiled program's `print` writes to. It
// defaults to os.Stdout; tests and the REPL pass their own writer.
func WithOutput(w io.Writer) Option {
	return func(c *config) { c.out = w }
}

func (c *compiler) emit(op instr.Opcode, operands ...uint64) {
	c.code.emit(op, operands...)
}

func (c *compiler) label() instr.Label {
	return c.code.label()
}

func (c *compiler) bind(l instr.Label) {
	c.code.bind(l)
}

func (c *compiler) br(l instr.Label) {
	c.code.br(l)
}

func (c *compiler) brIf(l instr.Label) {
	c.code.brIf(l)
}

func (c *compiler) constGet(v vmtypes.Value) {
	c.emit(instr.CONST_GET, uint64(c.prog.Const(v)))
}

// Compile reads minipy source from r, type-checks it, and lowers it into a
// minivm program. On any lexical, syntactic, or semantic error it returns a
// token.ErrorList describing every diagnostic found and a nil program.
func Compile(r io.Reader, opts ...Option) (*program.Program, error) {
	cfg := &config{out: os.Stdout}
	for _, opt := range opts {
		opt(cfg)
	}

	mod, parseErr := parser.Parse(r)

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

	host := newHostFuncs(cfg.out)
	b := program.NewBuilder()
	newCompiler(b, chk.exprType, chk.globals, chk.funcs, host).module(mod)

	prog, err := b.Build()
	if err != nil {
		return nil, fmt.Errorf("assemble program: %w", err)
	}

	optimized, err := optimize.NewOptimizer(optimize.O1).Optimize(prog)
	if err != nil {
		return nil, fmt.Errorf("optimize program: %w", err)
	}
	return optimized, nil
}

// module lowers every top-level statement. The entry function terminates by
// running off the end of its code (the VM has no entry-frame RETURN), so a
// trailing NOP gives any control-flow merge label bound at the very end a valid
// landing instruction — branch targets must stay within the code (analysis
// rejects a jump to len(code)).
func (c *compiler) module(mod *ast.Module) {
	c.block(mod.Body)
	c.emit(instr.NOP)
}

// block lowers a statement sequence (a module body or a compound block).
func (c *compiler) block(body []ast.Stmt) {
	for _, s := range body {
		c.stmt(s)
	}
}

func (c *compiler) stmt(s ast.Stmt) {
	switch n := s.(type) {
	case *ast.AnnAssign:
		if n.Value != nil {
			c.expr(n.Value)
			c.set(n.Target.Name)
		}
	case *ast.Assign:
		name := n.Target.(*ast.Name)
		c.expr(n.Value)
		c.set(name.Name)
	case *ast.AugAssign:
		name := n.Target.(*ast.Name)
		t := c.typ(name.Name)
		c.emitBinary(n.Op, t, c.exprType[n.Value],
			func() { c.get(name.Name) },
			func() { c.expr(n.Value) })
		c.set(name.Name)
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
		c.function(n)
	case *ast.Return:
		c.ret(n)
	case *ast.Break:
		c.br(c.loops[len(c.loops)-1].brk)
	case *ast.Continue:
		c.br(c.loops[len(c.loops)-1].cont)
	case *ast.Pass:
		// no-op
	}
}

func (c *compiler) get(name string) {
	if c.locals != nil {
		if l, ok := c.locals[name]; ok {
			c.emit(instr.LOCAL_GET, uint64(l.index))
			return
		}
	}
	c.emit(instr.GLOBAL_GET, uint64(c.globals[name].index))
}

func (c *compiler) set(name string) {
	if c.locals != nil {
		if l, ok := c.locals[name]; ok {
			c.emit(instr.LOCAL_SET, uint64(l.index))
			return
		}
	}
	c.emit(instr.GLOBAL_SET, uint64(c.globals[name].index))
}

func (c *compiler) typ(name string) types.Type {
	if c.locals != nil {
		if l, ok := c.locals[name]; ok {
			return l.typ
		}
	}
	return c.globals[name].typ
}

// emitIf lowers `if`/`elif`/`else`: invert the condition and branch over the
// then-block to the else-block (docs/spec/05-codegen.md).
func (c *compiler) emitIf(n *ast.If) {
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
func (c *compiler) emitWhile(n *ast.While) {
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

// emitFor lowers `for NAME in range(...)` to an integer counter loop driven by
// the target binding: start initializes the counter once, then the stop bound is
// re-tested each iteration and the constant step's sign fixes the test direction
// (docs/spec/05-codegen.md). continue → the increment, break → past the else
// block.
func (c *compiler) emitFor(n *ast.For) {
	args := n.Iter.(*ast.CallExpr).Args
	var startExpr, stopExpr ast.Expr
	step := int64(1)
	switch len(args) {
	case 1:
		stopExpr = args[0]
	case 2:
		startExpr, stopExpr = args[0], args[1]
	default: // 3, validated by the checker
		startExpr, stopExpr, step = args[0], args[1], constIntValue(args[2])
	}

	if startExpr != nil {
		c.expr(startExpr)
	} else {
		c.emit(instr.I64_CONST, 0)
	}
	c.set(n.Target.Name)

	top := c.label()
	cont := c.label()
	elseL := c.label()
	end := c.label()

	c.bind(top)
	c.get(n.Target.Name)
	c.expr(stopExpr)
	if step < 0 {
		c.emit(instr.I64_GT_S)
	} else {
		c.emit(instr.I64_LT_S)
	}
	c.emit(instr.I32_EQZ)
	c.brIf(elseL)

	c.loops = append(c.loops, loopLabels{cont: cont, brk: end})
	c.block(n.Body)
	c.loops = c.loops[:len(c.loops)-1]

	c.bind(cont)
	c.get(n.Target.Name)
	c.emit(instr.I64_CONST, uint64(step))
	c.emit(instr.I64_ADD)
	c.set(n.Target.Name)
	c.br(top)

	c.bind(elseL)
	c.block(n.Orelse)
	c.bind(end)
}

func (c *compiler) function(n *ast.Function) {
	info := c.funcs[n.Name.Name]
	if info == nil {
		return
	}
	fb := vmtypes.NewFunctionBuilder(&vmtypes.FunctionType{
		Params:  vmParamTypes(info),
		Returns: []vmtypes.Type{info.ret.VM()},
	})
	fb.WithLocals(vmLocalTypes(info)...)

	child := *c
	child.code = fnTarget(fb)
	child.locals = info.locals
	child.loops = nil
	child.block(n.Body)
	child.emitNoneReturn()

	fn, err := fb.Build()
	if err != nil {
		panic(err)
	}
	c.constGet(fn)
	c.emit(instr.GLOBAL_SET, uint64(info.slot.index))
}

func (c *compiler) ret(n *ast.Return) {
	if n.Value != nil {
		c.expr(n.Value)
	} else {
		c.emit(instr.REF_NULL)
	}
	c.emit(instr.RETURN)
}

func (c *compiler) emitNoneReturn() {
	c.emit(instr.REF_NULL)
	c.emit(instr.RETURN)
}

func vmParamTypes(info *fn) []vmtypes.Type {
	out := make([]vmtypes.Type, 0, len(info.params))
	for _, p := range info.params {
		out = append(out, p.typ.VM())
	}
	return out
}

func vmLocalTypes(info *fn) []vmtypes.Type {
	out := make([]vmtypes.Type, 0, len(info.order))
	for _, name := range info.order {
		out = append(out, info.locals[name].typ.VM())
	}
	return out
}

// expr lowers an expression, leaving exactly one value on the stack.
func (c *compiler) expr(n ast.Expr) {
	switch x := n.(type) {
	case *ast.IntLit:
		c.emit(instr.I64_CONST, uint64(x.Value))
	case *ast.FloatLit:
		c.emit(instr.F64_CONST, math.Float64bits(x.Value))
	case *ast.BoolLit:
		if x.Value {
			c.emit(instr.I32_CONST, 1)
		} else {
			c.emit(instr.I32_CONST, 0)
		}
	case *ast.NoneLit:
		c.emit(instr.REF_NULL)
	case *ast.StrLit:
		c.constGet(vmtypes.String(x.Value))
	case *ast.Name:
		c.get(x.Name)
	case *ast.UnaryExpr:
		c.unary(x)
	case *ast.BinaryExpr:
		c.emitBinary(x.Op, c.exprType[x.X], c.exprType[x.Y],
			func() { c.expr(x.X) },
			func() { c.expr(x.Y) })
	case *ast.BoolOp:
		c.boolOp(x)
	case *ast.Compare:
		c.compare(x)
	case *ast.CallExpr:
		c.call(x)
	case *ast.IfExp:
		c.ifExp(x)
	}
}

// ifExp lowers the conditional expression `body if cond else orelse`
// (docs/spec/05-codegen.md): branch to the true arm when the condition holds,
// else fall through to the false arm.
func (c *compiler) ifExp(x *ast.IfExp) {
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

func (c *compiler) unary(x *ast.UnaryExpr) {
	switch x.Op {
	case token.NOT:
		c.expr(x.X)
		c.emit(instr.I32_EQZ)
	case token.PLUS:
		c.expr(x.X)
	case token.MINUS:
		if c.exprType[x.X] == types.Float {
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

// emitBinary lowers a binary operation. emitL/emitR push the operands; the
// operator and operand types decide the opcode sequence. The handful of ops that
// need more than one opcode or a host call are special-cased; the rest map to a
// single opcode via simpleBinOp.
func (c *compiler) emitBinary(op token.Type, lt, rt types.Type, emitL, emitR func()) {
	switch op {
	case token.SLASH: // true division always yields float
		emitL()
		if lt == types.Int {
			c.emit(instr.I64_TO_F64_S)
		}
		emitR()
		if lt == types.Int {
			c.emit(instr.I64_TO_F64_S)
		}
		c.emit(instr.F64_DIV)
	case token.DOUBLESLASH:
		emitL()
		emitR()
		if lt == types.Int {
			c.emit(instr.I64_DIV_S)
		} else {
			c.emit(instr.F64_DIV)
			c.emit(instr.F64_FLOOR)
		}
	case token.PERCENT:
		emitL()
		emitR()
		if lt == types.Int {
			c.emit(instr.I64_REM_S)
		} else {
			c.callHost(c.host.floatMod)
		}
	case token.DOUBLESTAR:
		emitL()
		emitR()
		if lt == types.Int {
			c.callHost(c.host.powInt)
		} else {
			c.callHost(c.host.powFloat)
		}
	case token.PLUS:
		emitL()
		emitR()
		if lt == types.Str {
			c.emit(instr.STRING_CONCAT)
		} else {
			c.emit(simpleBinOp(op, lt))
		}
	default:
		emitL()
		emitR()
		c.emit(simpleBinOp(op, lt))
	}
}

// boolOp lowers short-circuiting `and`/`or` (docs/spec/05-codegen.md).
func (c *compiler) boolOp(x *ast.BoolOp) {
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

// compare lowers a (possibly chained) comparison to an i32 result. Operands are
// pure scalars in M0, so a chain `a < b < c` re-evaluates the middle operand
// rather than threading a temporary.
func (c *compiler) compare(x *ast.Compare) {
	c.emitCmp(x.X, x.Ops[0], x.Comparators[0])
	prev := x.Comparators[0]
	for i := 1; i < len(x.Ops); i++ {
		c.emitCmp(prev, x.Ops[i], x.Comparators[i])
		c.emit(instr.I32_AND)
		prev = x.Comparators[i]
	}
}

func (c *compiler) emitCmp(l ast.Expr, op token.Type, r ast.Expr) {
	t := c.exprType[l]
	c.expr(l)
	c.expr(r)
	c.emit(cmpOpcode(op, t))
}

// call lowers a direct builtin or user-function call. Inline builtins emit
// opcodes directly; print/str and the parse helpers go through host functions.
func (c *compiler) call(x *ast.CallExpr) {
	name := x.Fn.(*ast.Name).Name
	if fn, ok := c.funcs[name]; ok {
		for _, arg := range x.Args {
			c.expr(arg)
		}
		c.emit(instr.GLOBAL_GET, uint64(fn.slot.index))
		c.emit(instr.CALL)
		return
	}

	arg := x.Args[0]
	at := c.exprType[arg]

	switch name {
	case "print":
		c.expr(arg)
		c.callHostVoid(c.host.print)
	case "str":
		c.expr(arg)
		if at != types.Str {
			c.callHost(c.host.str)
		}
	case "int":
		c.expr(arg)
		switch at {
		case types.Float:
			c.emit(instr.F64_TO_I64_S)
		case types.Bool:
			c.emit(instr.I32_TO_I64_S)
		case types.Str:
			c.callHost(c.host.intParse)
		}
	case "float":
		c.expr(arg)
		switch at {
		case types.Int:
			c.emit(instr.I64_TO_F64_S)
		case types.Bool:
			c.emit(instr.I32_TO_F64_S)
		case types.Str:
			c.callHost(c.host.floatParse)
		}
	case "bool":
		c.expr(arg)
		switch at {
		case types.Int:
			c.emit(instr.I64_CONST, 0)
			c.emit(instr.I64_NE)
		case types.Float:
			c.emit(instr.F64_CONST, math.Float64bits(0))
			c.emit(instr.F64_NE)
		case types.Str:
			c.emit(instr.STRING_LEN)
			c.emit(instr.I64_CONST, 0)
			c.emit(instr.I64_NE)
		}
	case "abs":
		if at == types.Int {
			c.absInt(arg)
		} else {
			c.expr(arg)
			c.emit(instr.F64_ABS)
		}
	}
}

// absInt lowers abs() on an int inline: branch on the sign and negate when
// negative (the entry frame has no locals for a branchless trick).
func (c *compiler) absInt(arg ast.Expr) {
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

// callHost emits a call to a value-returning host function.
func (c *compiler) callHost(fn *interp.HostFunction) {
	c.emit(instr.CONST_GET, uint64(c.constOf(fn)))
	c.emit(instr.CALL)
}

// callHostVoid emits a call to a void host function, padding a REF_NULL so the
// expression still leaves exactly one value on the stack.
func (c *compiler) callHostVoid(fn *interp.HostFunction) {
	c.emit(instr.CONST_GET, uint64(c.constOf(fn)))
	c.emit(instr.CALL)
	c.emit(instr.REF_NULL)
}

// constOf interns a host function once and returns its constant-pool index,
// keyed by pointer identity to avoid the builder's value-based deduplication
// merging two host functions that share a signature.
func (c *compiler) constOf(fn *interp.HostFunction) int {
	if idx, ok := c.constIdx[fn]; ok {
		return idx
	}
	idx := c.prog.Const(fn)
	c.constIdx[fn] = idx
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
