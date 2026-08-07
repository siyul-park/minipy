package operator

import (
	"github.com/siyul-park/minipy/ast"
	"github.com/siyul-park/minipy/module"
	"github.com/siyul-park/minipy/token"
	"github.com/siyul-park/minipy/types"
)

// PowFloatResult reports whether `base ** exp` yields a float even though both
// operands are ints. CPython returns a float for any negative exponent, which
// is a type rule that depends on a value; a negative integer literal is the
// case a static checker can decide, so it is the case minipy supports. A
// computed negative exponent still raises at runtime.
//
// The checker and the lowerer both consult this, so the two phases agree on
// which `**` takes the float path without either of them owning the rule.
func PowFloatResult(op token.Type, exp ast.Expr) bool {
	if op != token.DOUBLESTAR {
		return false
	}
	switch e := exp.(type) {
	case *ast.IntLit:
		return e.Value < 0
	case *ast.UnaryExpr:
		if e.Op != token.MINUS {
			return false
		}
		lit, ok := e.X.(*ast.IntLit)
		return ok && lit.Value > 0
	default:
		return false
	}
}

// BinaryType applies the arithmetic/bitwise/shift typing rules
// (docs/spec/04-static-semantics.md). Mixed int/float and bool arithmetic are
// rejected; strings, bytes, and homogeneous lists have only their declared
// concatenation and repetition operations.
func BinaryType(c module.Checker, left types.Type, op token.Type, right types.Type, pos token.Pos) types.Type {
	left = types.Erase(left)
	right = types.Erase(right)
	if left == types.Invalid || right == types.Invalid {
		return types.Invalid
	}
	switch op {
	case token.AT:
		c.Error(pos, token.UnsupportedFeature, "matrix multiplication is not supported yet")
		return types.Invalid
	case token.PLUS:
		if types.Equal(left, types.Str) && types.Equal(right, types.Str) {
			return types.Str
		}
		if types.Equal(left, types.Bytes) && types.Equal(right, types.Bytes) {
			return types.Bytes
		}
		if list, ok := left.(*types.List); ok && types.AssignableTo(right, left) {
			return types.NewList(list.Elem)
		}
		if isDynamic(left) || isDynamic(right) {
			return types.Any
		}
		return arith(c, left, op, right, pos)
	case token.STAR:
		if types.Equal(left, types.Str) && types.Equal(right, types.Int) ||
			types.Equal(left, types.Int) && types.Equal(right, types.Str) {
			return types.Str
		}
		if list, ok := left.(*types.List); ok && types.Equal(right, types.Int) {
			return types.NewList(list.Elem)
		}
		if list, ok := right.(*types.List); ok && types.Equal(left, types.Int) {
			return types.NewList(list.Elem)
		}
		if isDynamic(left) || isDynamic(right) {
			return types.Any
		}
		return arith(c, left, op, right, pos)
	case token.MINUS, token.DOUBLESLASH, token.PERCENT, token.DOUBLESTAR:
		return arith(c, left, op, right, pos)
	case token.SLASH:
		if types.Equal(left, types.Int) && types.Equal(right, types.Int) {
			return types.Float
		}
		if types.Equal(left, types.Float) && types.Equal(right, types.Float) {
			return types.Float
		}
		// Mixed int/float division: promote to float.
		if (types.Equal(left, types.Int) && types.Equal(right, types.Float)) ||
			(types.Equal(left, types.Float) && types.Equal(right, types.Int)) {
			return types.Float
		}
		if isDynamic(left) || isDynamic(right) {
			return types.Any
		}
		return mismatch(c, op, left, right, pos)
	case token.AMP, token.PIPE, token.CARET, token.LSHIFT, token.RSHIFT:
		if types.Equal(left, types.Int) && types.Equal(right, types.Int) {
			return types.Int
		}
		if isDynamic(left) || isDynamic(right) {
			return types.Any
		}
		return mismatch(c, op, left, right, pos)
	default:
		return types.Invalid
	}
}

func arith(c module.Checker, left types.Type, op token.Type, right types.Type, pos token.Pos) types.Type {
	if types.Equal(left, types.Int) && types.Equal(right, types.Int) {
		return types.Int
	}
	if types.Equal(left, types.Float) && types.Equal(right, types.Float) {
		return types.Float
	}
	// Mixed int/float: promote to float (standard Python semantics).
	if (types.Equal(left, types.Int) && types.Equal(right, types.Float)) ||
		(types.Equal(left, types.Float) && types.Equal(right, types.Int)) {
		return types.Float
	}
	if isDynamic(left) || isDynamic(right) {
		return types.Any
	}
	return mismatch(c, op, left, right, pos)
}

func mismatch(c module.Checker, op token.Type, left, right types.Type, pos token.Pos) types.Type {
	c.Error(pos, token.TypeMismatch, "unsupported operand type(s) for %s: %s and %s", op, left, right)
	return types.Invalid
}

// isDynamic reports whether a type requires runtime dispatch (Any or Union).
func isDynamic(t types.Type) bool {
	return types.IsDynamic(t)
}

// UnaryType applies the unary operator typing rules for the operand expression.
func UnaryType(c module.Checker, op token.Type, arg ast.Expr) types.Type {
	t := types.Erase(c.Check(arg))
	switch op {
	case token.MINUS, token.PLUS:
		if t.IsNumeric() {
			return t
		}
		if isDynamic(t) {
			return types.Any
		}
		if t != types.Invalid {
			c.Error(arg.Pos(), token.TypeMismatch, "bad operand type for unary %s: %s", op, t)
		}
		return types.Invalid
	case token.TILDE:
		if types.Equal(t, types.Int) {
			return types.Int
		}
		if isDynamic(t) {
			return types.Any
		}
		if t != types.Invalid {
			c.Error(arg.Pos(), token.TypeMismatch, "bad operand type for unary ~: %s", t)
		}
		return types.Invalid
	case token.NOT:
		if isDynamic(t) {
			return types.Bool
		}
		if !types.Equal(t, types.Bool) && t != types.Invalid {
			c.Error(arg.Pos(), token.TypeMismatch, "'not' requires bool, got %s", t)
		}
		return types.Bool
	default:
		return types.Invalid
	}
}

// Comparable checks a single comparison and reports an error for incompatible
// operands. Identity (is/is not) and membership (in/not in) have their own rules.
func Comparable(c module.Checker, op token.Type, left, right types.Type, pos token.Pos) {
	left = types.Erase(left)
	right = types.Erase(right)
	if left == types.Invalid || right == types.Invalid {
		return
	}
	leftEllipsis := types.Equal(left, types.Ellipsis)
	rightEllipsis := types.Equal(right, types.Ellipsis)
	if op == token.IS || op == token.ISNOT {
		if leftEllipsis != rightEllipsis {
			c.Error(pos, token.TypeMismatch, "'%s' requires matching Ellipsis operands, got %s and %s", op, left, right)
			return
		}
		if !identityComparable(left) || !identityComparable(right) {
			c.Error(pos, token.TypeMismatch, "'%s' requires reference operands, got %s and %s", op, left, right)
		}
		return
	}
	if op == token.IN || op == token.NOTIN {
		if isDynamic(right) {
			return
		}
		if !ContainsType(left, right) {
			c.Error(pos, token.NotIterable, "'%s' requires container RHS, got %s in %s", op, left, right)
		}
		return
	}
	if leftEllipsis || rightEllipsis {
		if leftEllipsis != rightEllipsis || op != token.EQ && op != token.NE {
			c.Error(pos, token.NotComparable, "'%s' not supported between instances of %s and %s", op, left, right)
		}
		return
	}
	// Dynamic types accept all comparisons at the checker level; dispatch at runtime.
	if isDynamic(left) || isDynamic(right) {
		return
	}
	if types.Equal(left, types.None) || types.Equal(right, types.None) {
		c.Error(pos, token.UnsupportedFeature, "comparing to None uses 'is'")
		return
	}
	if (types.Equal(left, types.Bytes) || types.Equal(right, types.Bytes)) && op != token.EQ && op != token.NE {
		c.Error(pos, token.NotComparable, "'%s' not supported between instances of %s and %s", op, left, right)
		return
	}
	if !types.Equal(left, right) {
		// Allow mixed int/float comparisons (standard Python semantics).
		if (types.Equal(left, types.Int) && types.Equal(right, types.Float)) ||
			(types.Equal(left, types.Float) && types.Equal(right, types.Int)) {
			return
		}
		c.Error(pos, token.NotComparable, "'%s' not supported between instances of %s and %s", op, left, right)
		return
	}
	// Same-typed operands reach here for every op, including ==/!= on
	// containers (structural equality) and identity-only reference types
	// (Class, Iterator, Callable). Ordering additionally requires an
	// orderable type: dict, set, class, iterator, callable, and containers
	// whose element type CmpOpcode does not natively compare are rejected.
	if isOrderingOp(op) && !orderable(left) {
		c.Error(pos, token.NotComparable, "'%s' not supported between instances of %s and %s", op, left, right)
	}
}

func isOrderingOp(op token.Type) bool {
	switch op {
	case token.LT, token.LE, token.GT, token.GE:
		return true
	default:
		return false
	}
}

// orderable reports whether t supports <, <=, >, and >= lowering. Scalars
// int/float/bool/str are ordered directly; list/tuple are ordered
// lexicographically when every element position is itself one of those
// scalar types. Nested containers, dict, set, class, iterator, and callable
// are not orderable.
func orderable(t types.Type) bool {
	t = types.Erase(t)
	if orderableElem(t) {
		return true
	}
	switch typ := t.(type) {
	case *types.List:
		return orderableElem(typ.Elem)
	case *types.Tuple:
		for _, elem := range typ.Elems {
			if !orderableElem(elem) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

// orderableElem reports whether t is a scalar type CmpOpcode compares
// directly: int, float, bool, or str.
func orderableElem(t types.Type) bool {
	t = types.Erase(t)
	return types.Equal(t, types.Int) || types.Equal(t, types.Float) ||
		types.Equal(t, types.Bool) || types.Equal(t, types.Str)
}

func identityComparable(t types.Type) bool {
	if types.Equal(t, types.None) || types.Equal(t, types.Ellipsis) || types.Equal(t, types.Str) || types.IsAny(t) {
		return true
	}
	switch t.(type) {
	case *types.List, *types.Dict, *types.Set, *types.Tuple, *types.Class, *types.Iterator, *types.Callable, *types.Union:
		return true
	default:
		return false
	}
}

// ContainsType reports whether a needle may be tested for membership in a
// haystack container type.
func ContainsType(needle, haystack types.Type) bool {
	if isDynamic(haystack) {
		return true
	}
	switch t := haystack.(type) {
	case *types.List:
		return types.AssignableTo(needle, t.Elem)
	case *types.Dict:
		return types.AssignableTo(needle, t.Key)
	case *types.Set:
		return types.AssignableTo(needle, t.Elem)
	default:
		if types.Equal(haystack, types.Bytes) {
			return types.Equal(needle, types.Int)
		}
		return types.Equal(haystack, types.Str) && types.Equal(needle, types.Str)
	}
}
