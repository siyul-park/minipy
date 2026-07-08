// Package builtins provides Python's builtins module as a native module: the
// standard functions (print, len, range, isinstance, …) and the builtin
// exception hierarchy. It is the fallback module for unqualified names. Each
// builtin is a module.Symbol carrying its own type rule, lowering, and runtime
// value. It never depends on the operator module.
package builtins

import (
	"github.com/siyul-park/minipy/ast"
	"github.com/siyul-park/minipy/module"
	"github.com/siyul-park/minipy/token"
	"github.com/siyul-park/minipy/types"

	"github.com/siyul-park/minivm/interp"
	vmtypes "github.com/siyul-park/minivm/types"
)

// Name is the module's registered name.
const Name = "builtins"

// spec is a builtin's arity range and result-type rule.
type spec struct {
	min    int
	max    int
	result resultFunc
}

// New builds the builtins native module.
func New() module.Module {
	return module.NewNative(Name,
		callSymbol("print", spec{1, 1, printable(types.None)}, emitPrint, func(r module.Runtime) vmtypes.Value { return printHost(r.Out()) }),
		callSymbol("str", spec{1, 1, printable(types.Str)}, emitStr, valueHost(strHost)),
		callSymbol("int", spec{1, 1, convert(types.Int)}, emitInt, valueHost(intParseHost)),
		callSymbol("float", spec{1, 1, convert(types.Float)}, emitFloat, valueHost(floatParseHost)),
		callSymbol("bool", spec{1, 1, boolResult}, emitBool, nil),
		callSymbol("abs", spec{1, 1, absResult}, emitAbs, nil),
		callSymbol("len", spec{1, 1, lenResult}, emitLen, nil),
		callSymbol("enumerate", spec{1, 1, enumerateResult}, emitEnumerate, nil),
		callSymbol("zip", spec{2, 2, zipResult}, emitZip, nil),
		callSymbol("range", spec{1, 3, rangeResult}, emitRange, valueHost(rangeIterHost)),
		callSymbol("iter", spec{1, 1, iterResult}, emitIter, nil),
		callSymbol("next", spec{1, 1, nextResult}, emitNext, nil),
		module.NewSymbol(Name, "isinstance", isInstanceCheck, emitIsInstance, nil),
	)
}

func callSymbol(name string, sp spec, emit module.EmitFunc, value module.ValueFunc) module.Symbol {
	check := func(c module.Checker, args []ast.Expr, pos token.Pos) types.Type {
		return checkBuiltin(c, name, sp, args, pos)
	}
	return module.NewSymbol(Name, name, check, emit, value)
}

func checkBuiltin(c module.Checker, name string, sp spec, args []ast.Expr, pos token.Pos) types.Type {
	argTypes := make([]types.Type, len(args))
	for i, a := range args {
		argTypes[i] = c.Check(a)
	}
	if len(args) < sp.min || len(args) > sp.max {
		if sp.min == sp.max {
			c.Error(pos, token.ArityMismatch, "%s() takes exactly %d argument(s) (%d given)", name, sp.min, len(args))
		} else {
			c.Error(pos, token.ArityMismatch, "%s() takes %d to %d arguments (%d given)", name, sp.min, sp.max, len(args))
		}
		return types.Invalid
	}
	result, ok := sp.result(argTypes)
	if !ok {
		c.Error(pos, token.TypeMismatch, "%s() does not accept these arguments", name)
		return types.Invalid
	}
	if name == "range" && len(args) == 3 && isConstIntLiteral(args[2]) && constIntValue(args[2]) == 0 {
		c.Error(args[2].Pos(), token.SyntaxError, "range() step must not be zero")
	}
	return result
}

func valueHost(fn func() *interp.HostFunction) module.ValueFunc {
	return func(module.Runtime) vmtypes.Value { return fn() }
}

// isConstIntLiteral reports whether e is an int literal, optionally with a unary
// +/- sign; this catches range(..., 0) statically when possible.
func isConstIntLiteral(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.IntLit:
		return true
	case *ast.UnaryExpr:
		if x.Op == token.MINUS || x.Op == token.PLUS {
			_, ok := x.X.(*ast.IntLit)
			return ok
		}
	}
	return false
}

// constIntValue evaluates a constant int literal accepted by isConstIntLiteral.
func constIntValue(e ast.Expr) int64 {
	switch x := e.(type) {
	case *ast.IntLit:
		return x.Value
	case *ast.UnaryExpr:
		if x.Op == token.MINUS {
			return -constIntValue(x.X)
		}
		return constIntValue(x.X)
	}
	return 0
}
