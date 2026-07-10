package operator

import (
	"github.com/siyul-park/minipy/ast"
	"github.com/siyul-park/minipy/module"
	"github.com/siyul-park/minipy/token"
	"github.com/siyul-park/minipy/types"
)

// BinaryType applies the arithmetic/bitwise/shift typing rules
// (docs/spec/04-static-semantics.md). Mixed int/float and bool arithmetic are
// rejected; `str + str` and list `+`/`*` are the only non-numeric cases.
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
		return arith(c, left, op, right, pos)
	case token.STAR:
		if types.Equal(left, types.Str) && types.Equal(right, types.Int) {
			return types.Str
		}
		if list, ok := left.(*types.List); ok && types.Equal(right, types.Int) {
			return types.NewList(list.Elem)
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
		return mismatch(c, op, left, right, pos)
	case token.AMP, token.PIPE, token.CARET, token.LSHIFT, token.RSHIFT:
		if types.Equal(left, types.Int) && types.Equal(right, types.Int) {
			return types.Int
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
	return mismatch(c, op, left, right, pos)
}

func mismatch(c module.Checker, op token.Type, left, right types.Type, pos token.Pos) types.Type {
	c.Error(pos, token.TypeMismatch, "unsupported operand type(s) for %s: %s and %s", op, left, right)
	return types.Invalid
}

// UnaryType applies the unary operator typing rules for the operand expression.
func UnaryType(c module.Checker, op token.Type, arg ast.Expr) types.Type {
	t := types.Erase(c.Check(arg))
	switch op {
	case token.MINUS, token.PLUS:
		if t.IsNumeric() {
			return t
		}
		if t != types.Invalid {
			c.Error(arg.Pos(), token.TypeMismatch, "bad operand type for unary %s: %s", op, t)
		}
		return types.Invalid
	case token.TILDE:
		if types.Equal(t, types.Int) {
			return types.Int
		}
		if t != types.Invalid {
			c.Error(arg.Pos(), token.TypeMismatch, "bad operand type for unary ~: %s", t)
		}
		return types.Invalid
	case token.NOT:
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
	if op == token.IS || op == token.ISNOT {
		if !identityComparable(left) || !identityComparable(right) {
			c.Error(pos, token.TypeMismatch, "'%s' requires reference operands, got %s and %s", op, left, right)
		}
		return
	}
	if op == token.IN || op == token.NOTIN {
		if !ContainsType(left, right) {
			c.Error(pos, token.NotIterable, "'%s' requires container RHS, got %s in %s", op, left, right)
		}
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
		c.Error(pos, token.NotComparable, "'%s' not supported between instances of %s and %s", op, left, right)
	}
}

func identityComparable(t types.Type) bool {
	if types.Equal(t, types.None) || types.Equal(t, types.Str) || types.IsAny(t) {
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
