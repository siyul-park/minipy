package operator

import (
	"fmt"

	"github.com/siyul-park/minipy/ast"
	"github.com/siyul-park/minipy/module"
	"github.com/siyul-park/minipy/token"
	"github.com/siyul-park/minipy/types"
	"github.com/siyul-park/minivm/interp"

	"github.com/siyul-park/minivm/instr"
	vmtypes "github.com/siyul-park/minivm/types"
)

// EmitPowFloat lowers `base ** exp` on the float path, promoting integer
// operands, for the case PowFloatResult admits: both operands are ints but the
// exponent is a negative literal, so the result is a float.
func EmitPowFloat(e module.Emitter, left, right types.Type, pushLeft, pushRight func()) {
	pushLeft()
	emitPowFloatTail(e, left, right, pushRight)
}

// emitPowFloatTail completes the float power path with the base already pushed.
func emitPowFloatTail(e module.Emitter, left, right types.Type, pushRight func()) {
	if types.Equal(types.Erase(left), types.Int) {
		e.Emit(instr.I64_TO_F64_S)
	}
	pushRight()
	if types.Equal(types.Erase(right), types.Int) {
		e.Emit(instr.I64_TO_F64_S)
	}
	e.CallHost(e.Once(module.HostKey(Name, "pow", "float"), powFloat))
}

// EmitBinary lowers a checked binary operation. pushLeft and pushRight evaluate
// the operands exactly once; this package owns the complete opcode/host lowering
// selected by the checker rules in BinaryType.
func EmitBinary(e module.Emitter, op token.Type, left, right types.Type, pushLeft, pushRight func()) {
	if isDynamicEmit(left) || isDynamicEmit(right) {
		pushLeft()
		pushRight()
		e.CallHost(e.Once(module.HostKey(Name, "binary", "dynamic", op), func() *interp.HostFunction { return dynBinaryOp(op) }))
		return
	}
	switch op {
	case token.SLASH:
		pushLeft()
		if types.Equal(types.Erase(left), types.Int) {
			e.Emit(instr.I64_TO_F64_S)
		}
		pushRight()
		if types.Equal(types.Erase(right), types.Int) {
			e.Emit(instr.I64_TO_F64_S)
		}
		e.Emit(instr.F64_DIV)
	case token.DOUBLESLASH:
		if types.Equal(types.Erase(left), types.Int) && types.Equal(types.Erase(right), types.Int) {
			emitIntDivMod(e, pushLeft, pushRight, true)
			return
		}
		// Mixed or float: promote int operand and use float floor division.
		pushLeft()
		if types.Equal(types.Erase(left), types.Int) {
			e.Emit(instr.I64_TO_F64_S)
		}
		pushRight()
		if types.Equal(types.Erase(right), types.Int) {
			e.Emit(instr.I64_TO_F64_S)
		}
		e.Emit(instr.F64_DIV)
		e.Emit(instr.F64_FLOOR)
	case token.PERCENT:
		if types.Equal(types.Erase(left), types.Int) && types.Equal(types.Erase(right), types.Int) {
			emitIntDivMod(e, pushLeft, pushRight, false)
			return
		}
		// Mixed or float: promote int operand and use float modulo.
		pushLeft()
		if types.Equal(types.Erase(left), types.Int) {
			e.Emit(instr.I64_TO_F64_S)
		}
		pushRight()
		if types.Equal(types.Erase(right), types.Int) {
			e.Emit(instr.I64_TO_F64_S)
		}
		e.Emit(instr.F64_MOD)
	case token.DOUBLESTAR:
		pushLeft()
		if types.Equal(types.Erase(left), types.Int) && types.Equal(types.Erase(right), types.Int) {
			pushRight()
			e.CallHost(e.Once(module.HostKey(Name, "pow", "int"), powInt))
		} else {
			emitPowFloatTail(e, left, right, pushRight)
		}
	case token.PLUS:
		pushLeft()
		pushRight()
		el := types.Erase(left)
		switch el.(type) {
		case *types.List:
			emitListConcat(e, el)
		default:
			switch {
			case types.Equal(el, types.Str):
				e.Emit(instr.STRING_CONCAT)
			case types.Equal(el, types.Bytes):
				e.CallHost(e.Once(module.HostKey(Name, "concat", "bytes"), bytesConcat))
			default:
				// For mixed int/float, promote the int operand.
				if isMixedNumeric(left, right) {
					emitMixedArith(e, op, left, right)
				} else {
					e.Emit(simpleBinOp(op, el))
				}
			}
		}
	case token.STAR:
		pushLeft()
		pushRight()
		el, er := types.Erase(left), types.Erase(right)
		if _, ok := el.(*types.List); ok {
			e.CallHost(e.Once(module.HostKey(Name, "repeat", el), func() *interp.HostFunction { return listRepeat(el) }))
		} else if _, ok := er.(*types.List); ok {
			e.Emit(instr.SWAP)
			e.CallHost(e.Once(module.HostKey(Name, "repeat", er), func() *interp.HostFunction { return listRepeat(er) }))
		} else if types.Equal(el, types.Str) {
			e.Emit(instr.SWAP)
			e.CallHost(e.Once(module.HostKey(Name, "repeat", "str"), stringRepeat))
		} else if types.Equal(er, types.Str) {
			e.CallHost(e.Once(module.HostKey(Name, "repeat", "str"), stringRepeat))
		} else if isMixedNumeric(left, right) {
			emitMixedArith(e, op, left, right)
		} else {
			e.Emit(simpleBinOp(op, el))
		}
	case token.MINUS:
		pushLeft()
		pushRight()
		if _, ok := types.Erase(left).(*types.Set); ok {
			e.CallHost(e.Once(module.HostKey(Name, "set", token.MINUS, left), func() *interp.HostFunction { return setBinary(token.MINUS, left) }))
		} else if isMixedNumeric(left, right) {
			emitMixedArith(e, op, left, right)
		} else {
			e.Emit(simpleBinOp(op, types.Erase(left)))
		}
	case token.AMP, token.PIPE, token.CARET:
		pushLeft()
		pushRight()
		if _, ok := types.Erase(left).(*types.Set); ok {
			e.CallHost(e.Once(module.HostKey(Name, "set", op, left), func() *interp.HostFunction { return setBinary(op, left) }))
		} else {
			e.Emit(simpleBinOp(op, types.Erase(left)))
		}
	default:
		pushLeft()
		pushRight()
		e.Emit(simpleBinOp(op, types.Erase(left)))
	}
}

// EmitCompareStack lowers a checked comparison whose operands are already on
// the stack, left then right.
func EmitCompareStack(e module.Emitter, op token.Type, left, right types.Type) {
	if op == token.IN || op == token.NOTIN {
		if isDynamicEmit(right) {
			e.CallHost(e.Once(module.HostKey(Name, "contains", "dynamic", op), func() *interp.HostFunction { return dynContains(op) }))
			return
		}
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
	if isDynamicEmit(left) || isDynamicEmit(right) {
		e.CallHost(e.Once(module.HostKey(Name, "compare", "dynamic", op), func() *interp.HostFunction { return dynCompare(op) }))
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
		e.CallHost(e.Once(module.HostKey(Name, "equal", "bytes"), bytesEqual))
		if op == token.NE {
			e.Emit(instr.I32_EQZ)
		}
		return
	}
	el := types.Erase(left)
	if _, ok := el.(*types.Set); ok && isOrderingOp(op) {
		e.CallHost(e.Once(module.HostKey(Name, "relation", op, left), func() *interp.HostFunction { return setRelation(op, left) }))
		return
	}
	if isContainerType(el) {
		emitContainerCompare(e, op, el)
		return
	}
	if isIdentityOnlyType(el) {
		// Comparable admits ==/!= between two operands of the same class,
		// Iterator, or Callable type but neither special-methods nor operator
		// overloading exist for them, so CPython's identity-based default
		// __eq__ is the only meaningful behavior. This mirrors the REF_EQ
		// lowering `is`/`is not` already use for these reference types
		// (identityComparable in types.go); ordering is rejected at check
		// time (Comparable/orderable), so LT/LE/GT/GE never reach here.
		e.Emit(instr.REF_EQ)
		if op == token.NE {
			e.Emit(instr.I32_EQZ)
		}
		return
	}
	// Mixed int/float comparisons: promote the int operand to f64.
	if isMixedNumeric(left, right) {
		if types.Equal(types.Erase(left), types.Int) {
			// Stack: [int, float] -- promote int (second from top).
			e.Emit(instr.SWAP)
			e.Emit(instr.I64_TO_F64_S)
			e.Emit(instr.SWAP)
		} else {
			// Stack: [float, int] -- promote int (top of stack).
			e.Emit(instr.I64_TO_F64_S)
		}
		e.Emit(CmpOpcode(op, types.Float))
		return
	}
	e.Emit(CmpOpcode(op, types.Erase(left)))
}

// emitContainerCompare lowers a checked list/tuple/dict/set comparison whose
// operands are already on the stack. == and != use structural equality
// (containerEqual, host.go); ordering uses lexicographic comparison
// (containerCompare, host.go) reduced to `<`/`<=`/`>`/`>=` against zero. The
// checker's orderable rule (types.go) admits ordering only for list/tuple
// with scalar element positions, so LT/LE/GT/GE never reach here for dict,
// set, or a container whose elements CmpOpcode cannot compare.
func emitContainerCompare(e module.Emitter, op token.Type, t types.Type) {
	if op == token.EQ || op == token.NE {
		e.CallHost(e.Once(module.HostKey(Name, "equal", t), func() *interp.HostFunction { return containerEqual(t) }))
		if op == token.NE {
			e.Emit(instr.I32_EQZ)
		}
		return
	}
	e.CallHost(e.Once(module.HostKey(Name, "compare", t), func() *interp.HostFunction { return containerCompare(t) }))
	e.Emit(instr.I64_CONST, 0)
	e.Emit(orderOpcode(op))
}

// orderOpcode maps an ordering operator to the opcode that compares
// containerCompare's -1/0/1 result against zero.
func orderOpcode(op token.Type) instr.Opcode {
	switch op {
	case token.LT:
		return instr.I64_LT_S
	case token.LE:
		return instr.I64_LE_S
	case token.GT:
		return instr.I64_GT_S
	case token.GE:
		return instr.I64_GE_S
	default:
		panic(fmt.Sprintf("operator: no ordering opcode for %s", op))
	}
}

// isContainerType reports whether t is a list, dict, set, or tuple — the
// structurally comparable container types (docs/spec/04-static-semantics.md).
func isContainerType(t types.Type) bool {
	switch t.(type) {
	case *types.List, *types.Dict, *types.Set, *types.Tuple:
		return true
	default:
		return false
	}
}

// isIdentityOnlyType reports whether t is a reference type with no
// structural equality, so == falls back to the same identity check as `is`
// (identityComparable in types.go).
func isIdentityOnlyType(t types.Type) bool {
	switch t.(type) {
	case *types.Class, *types.Iterator, *types.Callable:
		return true
	default:
		return false
	}
}

// EmitUnary lowers a checked unary operation on arg.
func EmitUnary(e module.Emitter, op token.Type, arg ast.Expr) {
	argType := types.Erase(e.Type(arg))
	if isDynamicEmit(argType) {
		switch op {
		case token.NOT:
			e.Expr(arg)
			e.CallHost(e.Once(module.HostKey(Name, "bool", "dynamic"), DynBool))
			e.Emit(instr.I32_EQZ)
		case token.MINUS:
			e.Expr(arg)
			e.CallHost(e.Once(module.HostKey(Name, "neg", "dynamic"), dynUnaryNeg))
		case token.PLUS:
			e.Expr(arg)
			e.CallHost(e.Once(module.HostKey(Name, "pos", "dynamic"), dynUnaryPos))
		case token.TILDE:
			e.Expr(arg)
			e.CallHost(e.Once(module.HostKey(Name, "invert", "dynamic"), dynUnaryInvert))
		}
		return
	}
	switch op {
	case token.NOT:
		e.Expr(arg)
		e.Emit(instr.I32_EQZ)
	case token.PLUS:
		e.Expr(arg)
	case token.MINUS:
		if types.Equal(types.Erase(e.Type(arg)), types.Float) {
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
		e.CallHost(e.Once(module.HostKey(Name, "contains", needle, haystack), func() *interp.HostFunction { return listContains(needle, haystack) }))
	default:
		if types.Equal(haystack, types.Str) {
			e.CallHost(e.Once(module.HostKey(Name, "contains", "str"), strContains))
		} else if types.Equal(haystack, types.Bytes) {
			e.CallHost(e.Once(module.HostKey(Name, "contains", "bytes"), bytesContains))
		}
	}
	if op == token.NOTIN {
		e.Emit(instr.I32_EQZ)
	}
}

func emitListConcat(e module.Emitter, list types.Type) {
	rightSlot := e.Tmp(vmtypes.TypeAny)
	leftSlot := e.Tmp(vmtypes.TypeAny)
	resultSlot := e.Tmp(vmtypes.TypeAny)

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
	indexSlot := e.Tmp(vmtypes.TypeI32)
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

// isMixedNumeric reports whether one operand is int and the other is float.
// Literal wrappers are erased so that a Literal[1,2]-typed operand is still
// recognized as int for promotion purposes.
func isMixedNumeric(left, right types.Type) bool {
	l, r := types.Erase(left), types.Erase(right)
	return (types.Equal(l, types.Int) && types.Equal(r, types.Float)) ||
		(types.Equal(l, types.Float) && types.Equal(r, types.Int))
}

// emitMixedArith handles PLUS, MINUS, STAR for mixed int/float operands.
// The operands are already on the stack (left then right). The int operand is
// promoted in-place, then the float operation is emitted.
func emitMixedArith(e module.Emitter, op token.Type, left, right types.Type) {
	if types.Equal(types.Erase(left), types.Int) {
		// Stack: [int, float] -- need to promote the int (second from top).
		e.Emit(instr.SWAP)
		e.Emit(instr.I64_TO_F64_S)
		e.Emit(instr.SWAP)
	} else {
		// Stack: [float, int] -- promote the int (top of stack).
		e.Emit(instr.I64_TO_F64_S)
	}
	e.Emit(simpleBinOp(op, types.Float))
}

// emitIntDivMod implements Python's floor quotient and divisor-signed remainder
// from minivm's truncating signed remainder. Operands are evaluated once.
func emitIntDivMod(e module.Emitter, pushLeft, pushRight func(), quotient bool) {
	if !quotient {
		emitIntMod(e, pushLeft, pushRight)
		return
	}
	leftSlot := e.Tmp(vmtypes.TypeI64)
	rightSlot := e.Tmp(vmtypes.TypeI64)
	remainderSlot := e.Tmp(vmtypes.TypeI64)

	pushLeft()
	e.Emit(instr.GLOBAL_SET, uint64(leftSlot))
	pushRight()
	e.Emit(instr.GLOBAL_SET, uint64(rightSlot))

	var quotientSlot int
	if quotient {
		quotientSlot = e.Tmp(vmtypes.TypeI64)
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

// emitIntMod implements Python's divisor-signed remainder as
// `((left rem divisor) + divisor) rem divisor`, which is exact for every
// combination of signs: a truncating remainder differs from Python's only when
// the operands' signs differ, and adding the divisor once then re-reducing
// lands on the same value in that case and leaves it alone otherwise.
//
// The sign-correction form it replaces needed three scratch slots and about
// twenty instructions to reach the same answer. This trades roughly a dozen
// interpreter dispatches for one more hardware division, which is the right way
// round for a threaded interpreter.
func emitIntMod(e module.Emitter, pushLeft, pushRight func()) {
	divisorSlot := e.Tmp(vmtypes.TypeI64)
	pushRight()
	e.Emit(instr.GLOBAL_SET, uint64(divisorSlot))

	pushLeft()
	e.Emit(instr.GLOBAL_GET, uint64(divisorSlot))
	e.Emit(instr.I64_REM_S)
	e.Emit(instr.GLOBAL_GET, uint64(divisorSlot))
	e.Emit(instr.I64_ADD)
	e.Emit(instr.GLOBAL_GET, uint64(divisorSlot))
	e.Emit(instr.I64_REM_S)
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

// isDynamicEmit reports whether a type requires dynamic dispatch in emission.
func isDynamicEmit(t types.Type) bool {
	return types.IsDynamic(types.Erase(t))
}
