package operator

import (
	"github.com/siyul-park/minipy/ast"
	"github.com/siyul-park/minipy/module"
	"github.com/siyul-park/minipy/token"
	"github.com/siyul-park/minipy/types"

	"github.com/siyul-park/minivm/instr"
)

// EmitBinary lowers a binary operation. pushLeft/pushRight push the operands; the
// operator and operand types decide the opcode sequence. The handful of ops that
// need more than one opcode or a host call are special-cased; the rest map to a
// single opcode via simpleBinOp.
func EmitBinary(e module.Emitter, op token.Type, left, right types.Type, pushLeft, pushRight func()) {
	switch op {
	case token.SLASH: // true division always yields float
		pushLeft()
		if left == types.Int {
			e.Emit(instr.I64_TO_F64_S)
		}
		pushRight()
		if left == types.Int {
			e.Emit(instr.I64_TO_F64_S)
		}
		e.Emit(instr.F64_DIV)
	case token.DOUBLESLASH:
		pushLeft()
		pushRight()
		if left == types.Int {
			e.Emit(instr.I64_DIV_S)
		} else {
			e.Emit(instr.F64_DIV)
			e.Emit(instr.F64_FLOOR)
		}
	case token.PERCENT:
		pushLeft()
		pushRight()
		if left == types.Int {
			e.Emit(instr.I64_REM_S)
		} else {
			e.Emit(instr.F64_MOD)
		}
	case token.DOUBLESTAR:
		pushLeft()
		pushRight()
		if left == types.Int {
			e.CallHost(powInt())
		} else {
			e.CallHost(powFloat())
		}
	case token.PLUS:
		pushLeft()
		pushRight()
		if left == types.Str {
			e.Emit(instr.STRING_CONCAT)
		} else {
			e.Emit(simpleBinOp(op, left))
		}
	default:
		pushLeft()
		pushRight()
		e.Emit(simpleBinOp(op, left))
	}
}

// EmitCompareStack lowers a single comparison whose operands are already on the
// stack (left then right). Membership and identity have dedicated lowerings.
func EmitCompareStack(e module.Emitter, op token.Type, left, right types.Type) {
	if op == token.IN || op == token.NOTIN {
		e.Emit(instr.SWAP)
		emitContains(e, op, left, right)
		return
	}
	if op == token.IS || op == token.ISNOT {
		e.Emit(instr.REF_EQ)
		if op == token.ISNOT {
			e.Emit(instr.I32_EQZ)
		}
		return
	}
	e.Emit(cmpOpcode(op, left))
}

// EmitContainsCall lowers operator.contains(haystack, needle) with the haystack
// and needle already pushed in that order.
func EmitContainsCall(e module.Emitter, needle, haystack types.Type) {
	emitContains(e, token.IN, needle, haystack)
}

func emitContains(e module.Emitter, op token.Type, needle, haystack types.Type) {
	switch haystack.(type) {
	case *types.Dict:
		e.Emit(instr.MAP_LOOKUP)
		e.Emit(instr.SWAP)
		e.Emit(instr.DROP)
	case *types.List:
		e.CallHost(listContains(needle, haystack))
	default:
		if types.Equal(haystack, types.Str) {
			e.CallHost(strContains())
		}
	}
	if op == token.NOTIN {
		e.Emit(instr.I32_EQZ)
	}
}

// EmitUnary lowers a unary operation on the given operand expression.
func EmitUnary(e module.Emitter, op token.Type, arg ast.Expr) {
	switch op {
	case token.NOT:
		e.Expr(arg)
		e.Emit(instr.I32_EQZ)
	case token.PLUS:
		e.Expr(arg)
	case token.MINUS:
		if e.Type(arg) == types.Float {
			e.Expr(arg)
			e.Emit(instr.F64_NEG)
		} else {
			e.Emit(instr.I64_CONST, 0)
			e.Expr(arg)
			e.Emit(instr.I64_SUB)
		}
	case token.TILDE:
		e.Expr(arg)
		e.Emit(instr.I64_CONST, ^uint64(0))
		e.Emit(instr.I64_XOR)
	}
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

// CmpOpcode exposes the comparison opcode table for pattern-match codegen.
func CmpOpcode(op token.Type, t types.Type) instr.Opcode { return cmpOpcode(op, t) }
