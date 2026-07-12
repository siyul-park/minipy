// Package operator provides Python's operator module as a native module and is
// the single source of minipy's operator semantics. The compiler routes both
// operator syntax (a + b, ==, in, unary) and operator.* calls through the type
// rules and lowerings defined here, so operator behavior lives in exactly one
// place. It never depends on the builtins module.
package operator

import (
	"math"

	"github.com/siyul-park/minipy/ast"
	"github.com/siyul-park/minipy/module"
	"github.com/siyul-park/minipy/token"
	"github.com/siyul-park/minipy/types"

	"github.com/siyul-park/minivm/instr"
)

// Name is the module's registered name.
const Name = "operator"

type namedOp struct {
	name string
	op   token.Type
}

var binaryOps = []namedOp{
	{"add", token.PLUS},
	{"sub", token.MINUS},
	{"mul", token.STAR},
	{"truediv", token.SLASH},
	{"floordiv", token.DOUBLESLASH},
	{"mod", token.PERCENT},
	{"pow", token.DOUBLESTAR},
	{"and_", token.AMP},
	{"or_", token.PIPE},
	{"xor", token.CARET},
	{"lshift", token.LSHIFT},
	{"rshift", token.RSHIFT},
}

var compareOps = []namedOp{
	{"eq", token.EQ},
	{"ne", token.NE},
	{"lt", token.LT},
	{"le", token.LE},
	{"gt", token.GT},
	{"ge", token.GE},
}

var unaryOps = []namedOp{
	{"neg", token.MINUS},
	{"pos", token.PLUS},
	{"invert", token.TILDE},
}

// New builds the operator native module.
func New() module.Module {
	var symbols []module.Symbol
	for _, op := range binaryOps {
		symbols = append(symbols, binarySymbol(op.name, op.op))
	}
	for _, op := range compareOps {
		symbols = append(symbols, compareSymbol(op.name, op.op))
	}
	for _, op := range unaryOps {
		symbols = append(symbols, unarySymbol(op.name, op.op))
	}
	symbols = append(symbols, containsSymbol(), notSymbol(), absSymbol(), truthSymbol())
	return module.NewNative(Name, symbols...)
}

func arity(c module.Checker, name string, want, got int, pos token.Pos, args []ast.Expr) {
	c.Error(pos, token.ArityMismatch, "%s() takes exactly %d argument(s) (%d given)", name, want, got)
	for _, a := range args {
		c.Check(a)
	}
}

func binarySymbol(name string, op token.Type) module.Symbol {
	return module.NewSymbol(name,
		func(c module.Checker, args []ast.Expr, pos token.Pos) types.Type {
			if len(args) != 2 {
				arity(c, name, 2, len(args), pos, args)
				return types.Invalid
			}
			left := c.Check(args[0])
			right := c.Check(args[1])
			return BinaryType(c, left, op, right, pos)
		},
		func(e module.Emitter, args []ast.Expr) {
			EmitBinary(e, op, e.Type(args[0]), e.Type(args[1]),
				func() { e.Expr(args[0]) },
				func() { e.Expr(args[1]) })
		}, nil)
}

func compareSymbol(name string, op token.Type) module.Symbol {
	return module.NewSymbol(name,
		func(c module.Checker, args []ast.Expr, pos token.Pos) types.Type {
			if len(args) != 2 {
				arity(c, name, 2, len(args), pos, args)
				return types.Invalid
			}
			left := c.Check(args[0])
			right := c.Check(args[1])
			Comparable(c, op, left, right, pos)
			return types.Bool
		},
		func(e module.Emitter, args []ast.Expr) {
			e.Expr(args[0])
			e.Expr(args[1])
			EmitCompareStack(e, op, e.Type(args[0]), e.Type(args[1]))
		}, nil)
}

func unarySymbol(name string, op token.Type) module.Symbol {
	return module.NewSymbol(name,
		func(c module.Checker, args []ast.Expr, pos token.Pos) types.Type {
			if len(args) != 1 {
				arity(c, name, 1, len(args), pos, args)
				return types.Invalid
			}
			return UnaryType(c, op, args[0])
		},
		func(e module.Emitter, args []ast.Expr) {
			EmitUnary(e, op, args[0])
		}, nil)
}

func containsSymbol() module.Symbol {
	return module.NewSymbol("contains",
		func(c module.Checker, args []ast.Expr, pos token.Pos) types.Type {
			if len(args) != 2 {
				arity(c, "contains", 2, len(args), pos, args)
				return types.Invalid
			}
			haystack := c.Check(args[0])
			needle := c.Check(args[1])
			if !ContainsType(needle, haystack) {
				c.Error(pos, token.NotIterable, "'in' requires container RHS, got %s in %s", needle, haystack)
			}
			return types.Bool
		},
		func(e module.Emitter, args []ast.Expr) {
			e.Expr(args[0])
			e.Expr(args[1])
			emitContains(e, token.IN, e.Type(args[1]), e.Type(args[0]))
		}, nil)
}

func notSymbol() module.Symbol {
	return module.NewSymbol("not_",
		func(c module.Checker, args []ast.Expr, pos token.Pos) types.Type {
			if len(args) != 1 {
				arity(c, "not_", 1, len(args), pos, args)
				return types.Invalid
			}
			t := c.Check(args[0])
			if !types.Equal(t, types.Bool) && t != types.Invalid {
				c.Error(args[0].Pos(), token.TypeMismatch, "not_() requires bool, got %s", t)
			}
			return types.Bool
		},
		func(e module.Emitter, args []ast.Expr) {
			e.Expr(args[0])
			e.Emit(instr.I32_EQZ)
		}, nil)
}

func absSymbol() module.Symbol {
	return module.NewSymbol("abs",
		func(c module.Checker, args []ast.Expr, pos token.Pos) types.Type {
			if len(args) != 1 {
				arity(c, "abs", 1, len(args), pos, args)
				return types.Invalid
			}
			t := c.Check(args[0])
			if types.Equal(t, types.Int) || types.Equal(t, types.Float) {
				return t
			}
			if t != types.Invalid {
				c.Error(pos, token.TypeMismatch, "bad operand type for abs(): %s", t)
			}
			return types.Invalid
		},
		func(e module.Emitter, args []ast.Expr) {
			emitAbs(e, args[0])
		}, nil)
}

func truthSymbol() module.Symbol {
	return module.NewSymbol("truth",
		func(c module.Checker, args []ast.Expr, pos token.Pos) types.Type {
			if len(args) != 1 {
				arity(c, "truth", 1, len(args), pos, args)
				return types.Invalid
			}
			t := c.Check(args[0])
			if convertible(t) || isContainer(t) {
				return types.Bool
			}
			if t != types.Invalid {
				c.Error(pos, token.TypeMismatch, "bad operand type for truth(): %s", t)
			}
			return types.Invalid
		},
		func(e module.Emitter, args []ast.Expr) {
			emitTruth(e, args[0])
		}, nil)
}

func emitAbs(e module.Emitter, arg ast.Expr) {
	if e.Type(arg) == types.Int {
		e.Expr(arg)
		e.Emit(instr.DUP)
		e.Emit(instr.I64_CONST, 0)
		e.Emit(instr.I64_LT_S)
		neg := e.Label()
		end := e.Label()
		e.BrIf(neg)
		e.Br(end)
		e.Bind(neg)
		e.Emit(instr.I64_CONST, 0)
		e.Emit(instr.SWAP)
		e.Emit(instr.I64_SUB)
		e.Bind(end)
		return
	}
	e.Expr(arg)
	e.Emit(instr.F64_ABS)
}

func emitTruth(e module.Emitter, arg ast.Expr) {
	e.Expr(arg)
	switch t := e.Type(arg).(type) {
	case *types.List:
		e.Emit(instr.ARRAY_LEN)
		e.Emit(instr.I32_CONST, 0)
		e.Emit(instr.I32_NE)
	case *types.Dict, *types.Set:
		e.Emit(instr.MAP_LEN)
		e.Emit(instr.I32_CONST, 0)
		e.Emit(instr.I32_NE)
	case *types.Tuple:
		e.Emit(instr.DROP)
		if len(t.Elems) == 0 {
			e.Emit(instr.I32_CONST, 0)
		} else {
			e.Emit(instr.I32_CONST, 1)
		}
	case *types.Iterator, *types.Callable, *types.Class:
		e.Emit(instr.REF_IS_NULL)
		e.Emit(instr.I32_EQZ)
	default:
		switch {
		case types.Equal(t, types.Int):
			e.Emit(instr.I64_CONST, 0)
			e.Emit(instr.I64_NE)
		case types.Equal(t, types.Float):
			e.Emit(instr.F64_CONST, math.Float64bits(0))
			e.Emit(instr.F64_NE)
		case types.Equal(t, types.Str):
			e.Emit(instr.STRING_LEN)
			e.Emit(instr.I32_CONST, 0)
			e.Emit(instr.I32_NE)
		}
	}
}

func convertible(t types.Type) bool {
	return types.Equal(t, types.Int) || types.Equal(t, types.Float) ||
		types.Equal(t, types.Bool) || types.Equal(t, types.Str)
}

func isContainer(t types.Type) bool {
	switch t.(type) {
	case *types.List, *types.Dict, *types.Set, *types.Tuple, *types.Iterator:
		return true
	default:
		return false
	}
}
