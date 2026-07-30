package operator

import (
	"fmt"

	"github.com/siyul-park/minipy/ast"
	"github.com/siyul-park/minipy/module"
	"github.com/siyul-park/minipy/token"
	"github.com/siyul-park/minipy/types"

	"github.com/siyul-park/minivm/instr"
)

// EmitBinary lowers a checked binary operation. pushLeft and pushRight evaluate
// the operands exactly once; this package owns the complete opcode/host lowering
// selected by the checker rules in BinaryType.
func EmitBinary(e module.Emitter, op token.Type, left, right types.Type, pushLeft, pushRight func()) {
	switch op {
	case token.SLASH:
		pushLeft()
		if types.Equal(left, types.Int) {
			e.Emit(instr.I64_TO_F64_S)
		}
		pushRight()
		if types.Equal(right, types.Int) {
			e.Emit(instr.I64_TO_F64_S)
		}
		e.Emit(instr.F64_DIV)
	case token.DOUBLESLASH:
		if types.Equal(left, types.Int) {
			emitIntDivMod(e, pushLeft, pushRight, true)
			return
		}
		pushLeft()
		pushRight()
		e.Emit(instr.F64_DIV)
		e.Emit(instr.F64_FLOOR)
	case token.PERCENT:
		if types.Equal(left, types.Int) {
			emitIntDivMod(e, pushLeft, pushRight, false)
			return
		}
		pushLeft()
		pushRight()
		e.Emit(instr.F64_MOD)
	case token.DOUBLESTAR:
		pushLeft()
		pushRight()
		if types.Equal(left, types.Int) {
			e.CallHost(powInt())
		} else {
			e.CallHost(powFloat())
		}
	case token.PLUS:
		pushLeft()
		pushRight()
		switch left.(type) {
		case *types.List:
			emitListConcat(e, left)
		default:
			switch {
			case types.Equal(left, types.Str):
				e.Emit(instr.STRING_CONCAT)
			case types.Equal(left, types.Bytes):
				e.CallHost(bytesConcat())
			default:
				e.Emit(simpleBinOp(op, left))
			}
		}
	case token.STAR:
		pushLeft()
		pushRight()
		if _, ok := left.(*types.List); ok {
			e.CallHost(listRepeat(left))
		} else if _, ok := right.(*types.List); ok {
			e.Emit(instr.SWAP)
			e.CallHost(listRepeat(right))
		} else if types.Equal(left, types.Str) {
			e.Emit(instr.SWAP)
			e.CallHost(stringRepeat())
		} else if types.Equal(right, types.Str) {
			e.CallHost(stringRepeat())
		} else {
			e.Emit(simpleBinOp(op, left))
		}
	default:
		pushLeft()
		pushRight()
		e.Emit(simpleBinOp(op, left))
	}
}

// EmitCompareStack lowers a checked comparison whose operands are already on
// the stack, left then right.
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
	if types.Equal(left, types.Ellipsis) && types.Equal(right, types.Ellipsis) {
		e.Emit(instr.REF_EQ)
		if op == token.NE {
			e.Emit(instr.I32_EQZ)
		}
		return
	}
	if types.Equal(left, types.Bytes) || types.Equal(right, types.Bytes) {
		// The checker admits only bytes equality here. Keep != as an inversion
		// because minivm currently interns host functions by signature rather
		// than semantic identity.
		e.CallHost(bytesEqual())
		if op == token.NE {
			e.Emit(instr.I32_EQZ)
		}
		return
	}
	e.Emit(CmpOpcode(op, left))
}

// EmitUnary lowers a checked unary operation on arg.
func EmitUnary(e module.Emitter, op token.Type, arg ast.Expr) {
	switch op {
	case token.NOT:
		e.Expr(arg)
		e.Emit(instr.I32_EQZ)
	case token.PLUS:
		e.Expr(arg)
	case token.MINUS:
		if types.Equal(e.Type(arg), types.Float) {
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

// CmpOpcode returns the direct comparison opcode used by ordinary comparisons
// and pattern matching. Unsupported inputs indicate a checker/lowerer invariant
// violation and panic rather than silently emitting NOP.
func CmpOpcode(op token.Type, typ types.Type) instr.Opcode {
	switch typ {
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
	case types.None:
		if op == token.EQ {
			return instr.REF_EQ
		}
	default:
		if types.Equal(typ, types.Int) {
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
	}
	panic(fmt.Sprintf("operator: no comparison opcode for %s and %v", op, typ))
}

func emitContains(e module.Emitter, op token.Type, needle, haystack types.Type) {
	switch haystack.(type) {
	case *types.Dict, *types.Set:
		e.Emit(instr.MAP_LOOKUP)
		e.Emit(instr.SWAP)
		e.Emit(instr.DROP)
		e.Emit(instr.I32_CONST, 0)
		e.Emit(instr.I32_NE)
	case *types.List:
		e.CallHost(listContains(needle, haystack))
	default:
		if types.Equal(haystack, types.Str) {
			e.CallHost(strContains())
		} else if types.Equal(haystack, types.Bytes) {
			e.CallHost(bytesContains())
		}
	}
	if op == token.NOTIN {
		e.Emit(instr.I32_EQZ)
	}
}

func emitListConcat(e module.Emitter, list types.Type) {
	rightSlot := e.Tmp()
	leftSlot := e.Tmp()
	resultSlot := e.Tmp()

	e.Emit(instr.GLOBAL_SET, uint64(rightSlot))
	e.Emit(instr.GLOBAL_SET, uint64(leftSlot))
	e.Emit(instr.I32_CONST, 0)
	e.Emit(instr.ARRAY_NEW_DEFAULT, e.TypeIndex(list))
	e.Emit(instr.GLOBAL_SET, uint64(resultSlot))

	emitListAppend(e, resultSlot, leftSlot)
	emitListAppend(e, resultSlot, rightSlot)
	e.Emit(instr.GLOBAL_GET, uint64(resultSlot))
}

func emitListAppend(e module.Emitter, resultSlot, sourceSlot int) {
	indexSlot := e.Tmp()
	top := e.Label()
	done := e.Label()

	e.Emit(instr.I32_CONST, 0)
	e.Emit(instr.GLOBAL_SET, uint64(indexSlot))
	e.Bind(top)
	e.Emit(instr.GLOBAL_GET, uint64(indexSlot))
	e.Emit(instr.GLOBAL_GET, uint64(sourceSlot))
	e.Emit(instr.ARRAY_LEN)
	e.Emit(instr.I32_LT_S)
	e.Emit(instr.I32_EQZ)
	e.BrIf(done)

	e.Emit(instr.GLOBAL_GET, uint64(resultSlot))
	e.Emit(instr.GLOBAL_GET, uint64(sourceSlot))
	e.Emit(instr.GLOBAL_GET, uint64(indexSlot))
	e.Emit(instr.ARRAY_GET)
	e.Emit(instr.I32_CONST, 1)
	e.Emit(instr.ARRAY_APPEND)
	e.Emit(instr.DROP)

	e.Emit(instr.GLOBAL_GET, uint64(indexSlot))
	e.Emit(instr.I32_CONST, 1)
	e.Emit(instr.I32_ADD)
	e.Emit(instr.GLOBAL_SET, uint64(indexSlot))
	e.Br(top)
	e.Bind(done)
}

// emitIntDivMod implements Python's floor quotient and divisor-signed remainder
// from minivm's truncating signed remainder. Operands are evaluated once.
func emitIntDivMod(e module.Emitter, pushLeft, pushRight func(), quotient bool) {
	leftSlot := e.Tmp()
	rightSlot := e.Tmp()
	remainderSlot := e.Tmp()

	pushLeft()
	e.Emit(instr.GLOBAL_SET, uint64(leftSlot))
	pushRight()
	e.Emit(instr.GLOBAL_SET, uint64(rightSlot))

	var quotientSlot int
	if quotient {
		quotientSlot = e.Tmp()
		e.Emit(instr.GLOBAL_GET, uint64(leftSlot))
		e.Emit(instr.GLOBAL_GET, uint64(rightSlot))
		e.Emit(instr.I64_DIV_S)
		e.Emit(instr.GLOBAL_SET, uint64(quotientSlot))
	}

	e.Emit(instr.GLOBAL_GET, uint64(leftSlot))
	e.Emit(instr.GLOBAL_GET, uint64(rightSlot))
	e.Emit(instr.I64_REM_S)
	e.Emit(instr.GLOBAL_SET, uint64(remainderSlot))

	// adjustment = remainder != 0 && operands have opposite signs.
	e.Emit(instr.GLOBAL_GET, uint64(remainderSlot))
	e.Emit(instr.I64_CONST, 0)
	e.Emit(instr.I64_NE)
	e.Emit(instr.GLOBAL_GET, uint64(leftSlot))
	e.Emit(instr.GLOBAL_GET, uint64(rightSlot))
	e.Emit(instr.I64_XOR)
	e.Emit(instr.I64_CONST, 0)
	e.Emit(instr.I64_LT_S)
	e.Emit(instr.I32_AND)
	e.Emit(instr.I32_TO_I64_S)

	if quotient {
		e.Emit(instr.GLOBAL_GET, uint64(quotientSlot))
		e.Emit(instr.SWAP)
		e.Emit(instr.I64_SUB)
		return
	}

	// Python remainder = truncating remainder + divisor * adjustment.
	e.Emit(instr.GLOBAL_GET, uint64(rightSlot))
	e.Emit(instr.I64_MUL)
	e.Emit(instr.GLOBAL_GET, uint64(remainderSlot))
	e.Emit(instr.I64_ADD)
}

func simpleBinOp(op token.Type, typ types.Type) instr.Opcode {
	// Augmented attribute assignment currently omits the checked receiver type
	// at its compiler call site. Preserve its established integer lowering until
	// that out-of-scope caller can pass the checker-owned type explicitly.
	if typ == nil {
		typ = types.Int
	}
	if types.Equal(typ, types.Float) {
		switch op {
		case token.PLUS:
			return instr.F64_ADD
		case token.MINUS:
			return instr.F64_SUB
		case token.STAR:
			return instr.F64_MUL
		}
	} else if types.Equal(typ, types.Int) {
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
	}
	panic(fmt.Sprintf("operator: no binary opcode for %s and %v", op, typ))
}
