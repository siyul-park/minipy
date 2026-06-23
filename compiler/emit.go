package compiler

import (
	"math"

	"github.com/siyul-park/minipy/ast"
	"github.com/siyul-park/minipy/token"
	"github.com/siyul-park/minipy/types"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/interp"
	"github.com/siyul-park/minivm/program"
	vmtypes "github.com/siyul-park/minivm/types"
)

// emitter lowers a typed module to a minivm program. It assumes the checker has
// already validated the module, so it never re-reports errors; it only relies on
// the type table and global symbol table.
type emitter struct {
	b        *program.Builder
	exprType map[ast.Expr]types.Type
	globals  map[string]*global
	host     *hostFuncs
	constIdx map[*interp.HostFunction]int
}

func newEmitter(b *program.Builder, exprType map[ast.Expr]types.Type, globals map[string]*global, host *hostFuncs) *emitter {
	return &emitter{
		b:        b,
		exprType: exprType,
		globals:  globals,
		host:     host,
		constIdx: map[*interp.HostFunction]int{},
	}
}

// module lowers every top-level statement.
func (e *emitter) module(mod *ast.Module) {
	for _, s := range mod.Body {
		e.stmt(s)
	}
}

func (e *emitter) stmt(s ast.Stmt) {
	switch n := s.(type) {
	case *ast.AnnAssign:
		if n.Value != nil {
			e.expr(n.Value)
			e.b.Emit(instr.GLOBAL_SET, uint64(e.globals[n.Target.Name].index))
		}
	case *ast.Assign:
		name := n.Target.(*ast.Name)
		e.expr(n.Value)
		e.b.Emit(instr.GLOBAL_SET, uint64(e.globals[name.Name].index))
	case *ast.AugAssign:
		name := n.Target.(*ast.Name)
		g := e.globals[name.Name]
		e.emitBinary(n.Op, g.typ, e.exprType[n.Value],
			func() { e.b.Emit(instr.GLOBAL_GET, uint64(g.index)) },
			func() { e.expr(n.Value) })
		e.b.Emit(instr.GLOBAL_SET, uint64(g.index))
	case *ast.ExprStmt:
		e.expr(n.X)
		e.b.Emit(instr.DROP)
	}
}

// expr lowers an expression, leaving exactly one value on the stack.
func (e *emitter) expr(n ast.Expr) {
	switch x := n.(type) {
	case *ast.IntLit:
		e.b.Emit(instr.I64_CONST, uint64(x.Value))
	case *ast.FloatLit:
		e.b.Emit(instr.F64_CONST, math.Float64bits(x.Value))
	case *ast.BoolLit:
		if x.Value {
			e.b.Emit(instr.I32_CONST, 1)
		} else {
			e.b.Emit(instr.I32_CONST, 0)
		}
	case *ast.NoneLit:
		e.b.Emit(instr.REF_NULL)
	case *ast.StrLit:
		e.b.ConstGet(vmtypes.String(x.Value))
	case *ast.Name:
		e.b.Emit(instr.GLOBAL_GET, uint64(e.globals[x.Name].index))
	case *ast.UnaryExpr:
		e.unary(x)
	case *ast.BinaryExpr:
		e.emitBinary(x.Op, e.exprType[x.X], e.exprType[x.Y],
			func() { e.expr(x.X) },
			func() { e.expr(x.Y) })
	case *ast.BoolOp:
		e.boolOp(x)
	case *ast.Compare:
		e.compare(x)
	case *ast.CallExpr:
		e.call(x)
	}
}

func (e *emitter) unary(x *ast.UnaryExpr) {
	switch x.Op {
	case token.NOT:
		e.expr(x.X)
		e.b.Emit(instr.I32_EQZ)
	case token.PLUS:
		e.expr(x.X)
	case token.MINUS:
		if e.exprType[x.X] == types.Float {
			e.expr(x.X)
			e.b.Emit(instr.F64_NEG)
		} else {
			e.b.Emit(instr.I64_CONST, 0)
			e.expr(x.X)
			e.b.Emit(instr.I64_SUB)
		}
	case token.TILDE:
		e.expr(x.X)
		e.b.Emit(instr.I64_CONST, ^uint64(0))
		e.b.Emit(instr.I64_XOR)
	}
}

// emitBinary lowers a binary operation. emitL/emitR push the operands; the
// operator and operand types decide the opcode sequence. The handful of ops that
// need more than one opcode or a host call are special-cased; the rest map to a
// single opcode via simpleBinOp.
func (e *emitter) emitBinary(op token.Type, lt, rt types.Type, emitL, emitR func()) {
	switch op {
	case token.SLASH: // true division always yields float
		emitL()
		if lt == types.Int {
			e.b.Emit(instr.I64_TO_F64_S)
		}
		emitR()
		if lt == types.Int {
			e.b.Emit(instr.I64_TO_F64_S)
		}
		e.b.Emit(instr.F64_DIV)
	case token.DOUBLESLASH:
		emitL()
		emitR()
		if lt == types.Int {
			e.b.Emit(instr.I64_DIV_S)
		} else {
			e.b.Emit(instr.F64_DIV)
			e.b.Emit(instr.F64_FLOOR)
		}
	case token.PERCENT:
		emitL()
		emitR()
		if lt == types.Int {
			e.b.Emit(instr.I64_REM_S)
		} else {
			e.callHost(e.host.floatMod)
		}
	case token.DOUBLESTAR:
		emitL()
		emitR()
		if lt == types.Int {
			e.callHost(e.host.powInt)
		} else {
			e.callHost(e.host.powFloat)
		}
	case token.PLUS:
		emitL()
		emitR()
		if lt == types.Str {
			e.b.Emit(instr.STRING_CONCAT)
		} else {
			e.b.Emit(simpleBinOp(op, lt))
		}
	default:
		emitL()
		emitR()
		e.b.Emit(simpleBinOp(op, lt))
	}
}

// boolOp lowers short-circuiting `and`/`or` (docs/spec/05-codegen.md).
func (e *emitter) boolOp(x *ast.BoolOp) {
	e.expr(x.X)
	e.b.Emit(instr.DUP)
	if x.Op == token.AND {
		eval := e.b.Label()
		end := e.b.Label()
		e.b.BrIf(eval)
		e.b.Br(end)
		e.b.Bind(eval)
		e.b.Emit(instr.DROP)
		e.expr(x.Y)
		e.b.Bind(end)
		return
	}
	end := e.b.Label()
	e.b.BrIf(end)
	e.b.Emit(instr.DROP)
	e.expr(x.Y)
	e.b.Bind(end)
}

// compare lowers a (possibly chained) comparison to an i32 result. Operands are
// pure scalars in M0, so a chain `a < b < c` re-evaluates the middle operand
// rather than threading a temporary.
func (e *emitter) compare(x *ast.Compare) {
	e.emitCmp(x.X, x.Ops[0], x.Comparators[0])
	prev := x.Comparators[0]
	for i := 1; i < len(x.Ops); i++ {
		e.emitCmp(prev, x.Ops[i], x.Comparators[i])
		e.b.Emit(instr.I32_AND)
		prev = x.Comparators[i]
	}
}

func (e *emitter) emitCmp(l ast.Expr, op token.Type, r ast.Expr) {
	t := e.exprType[l]
	e.expr(l)
	e.expr(r)
	e.b.Emit(cmpOpcode(op, t))
}

// call lowers a builtin call. Inline builtins emit opcodes directly; print/str
// and the parse helpers go through host functions.
func (e *emitter) call(x *ast.CallExpr) {
	name := x.Fn.(*ast.Name).Name
	arg := x.Args[0]
	at := e.exprType[arg]

	switch name {
	case "print":
		e.expr(arg)
		e.callHostVoid(e.host.print)
	case "str":
		e.expr(arg)
		if at != types.Str {
			e.callHost(e.host.str)
		}
	case "int":
		e.expr(arg)
		switch at {
		case types.Float:
			e.b.Emit(instr.F64_TO_I64_S)
		case types.Bool:
			e.b.Emit(instr.I32_TO_I64_S)
		case types.Str:
			e.callHost(e.host.intParse)
		}
	case "float":
		e.expr(arg)
		switch at {
		case types.Int:
			e.b.Emit(instr.I64_TO_F64_S)
		case types.Bool:
			e.b.Emit(instr.I32_TO_F64_S)
		case types.Str:
			e.callHost(e.host.floatParse)
		}
	case "bool":
		e.expr(arg)
		switch at {
		case types.Int:
			e.b.Emit(instr.I64_CONST, 0)
			e.b.Emit(instr.I64_NE)
		case types.Float:
			e.b.Emit(instr.F64_CONST, math.Float64bits(0))
			e.b.Emit(instr.F64_NE)
		case types.Str:
			e.b.Emit(instr.STRING_LEN)
			e.b.Emit(instr.I64_CONST, 0)
			e.b.Emit(instr.I64_NE)
		}
	case "abs":
		if at == types.Int {
			e.absInt(arg)
		} else {
			e.expr(arg)
			e.b.Emit(instr.F64_ABS)
		}
	}
}

// absInt lowers abs() on an int inline: branch on the sign and negate when
// negative (the entry frame has no locals for a branchless trick).
func (e *emitter) absInt(arg ast.Expr) {
	e.expr(arg)
	e.b.Emit(instr.DUP)
	e.b.Emit(instr.I64_CONST, 0)
	e.b.Emit(instr.I64_LT_S)
	neg := e.b.Label()
	end := e.b.Label()
	e.b.BrIf(neg)
	e.b.Br(end)
	e.b.Bind(neg)
	e.b.Emit(instr.I64_CONST, 0)
	e.b.Emit(instr.SWAP)
	e.b.Emit(instr.I64_SUB)
	e.b.Bind(end)
}

// callHost emits a call to a value-returning host function.
func (e *emitter) callHost(fn *interp.HostFunction) {
	e.b.Emit(instr.CONST_GET, uint64(e.constOf(fn)))
	e.b.Emit(instr.CALL)
}

// callHostVoid emits a call to a void host function, padding a REF_NULL so the
// expression still leaves exactly one value on the stack.
func (e *emitter) callHostVoid(fn *interp.HostFunction) {
	e.b.Emit(instr.CONST_GET, uint64(e.constOf(fn)))
	e.b.Emit(instr.CALL)
	e.b.Emit(instr.REF_NULL)
}

// constOf interns a host function once and returns its constant-pool index,
// keyed by pointer identity to avoid the builder's value-based deduplication
// merging two host functions that share a signature.
func (e *emitter) constOf(fn *interp.HostFunction) int {
	if idx, ok := e.constIdx[fn]; ok {
		return idx
	}
	idx := e.b.Const(fn)
	e.constIdx[fn] = idx
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
