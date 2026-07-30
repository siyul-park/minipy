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
func New() *module.NativeModule {
	return module.NewNative(Name,
		callSymbol("print", spec{1, 1, printable(types.None)}, emitPrint, nil),
		callSymbol("str", spec{1, 1, printable(types.Str)}, emitStr, nil),
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
		callSymbol("ord", spec{1, 1, ordResult}, emitOrd, valueHost(ordHost)),
		callSymbol("chr", spec{1, 1, chrResult}, emitChr, valueHost(chrHost)),
		callSymbol("sorted", spec{1, 1, sortedResult}, emitSorted, nil),
		callSymbol("reversed", spec{1, 1, reversedResult}, emitReversed, nil),
		module.NewSymbol("min", minCheck, emitMin, nil),
		module.NewSymbol("max", maxCheck, emitMax, nil),
		callSymbol("sum", spec{1, 1, sumResult}, emitSum, nil),
		callSymbol("any", spec{1, 1, anyAllResult}, emitAny, nil),
		callSymbol("all", spec{1, 1, anyAllResult}, emitAll, nil),
		callSymbol("round", spec{1, 2, roundResult}, emitRound, nil),
		callSymbol("divmod", spec{2, 2, divmodResult}, emitDivmod, nil),
		callSymbol("pow", spec{2, 2, powResult}, emitPow, nil),
		callSymbol("hex", spec{1, 1, hexOctBinResult}, emitHex, nil),
		callSymbol("oct", spec{1, 1, hexOctBinResult}, emitOct, nil),
		callSymbol("bin", spec{1, 1, hexOctBinResult}, emitBin, nil),
		callSymbol("repr", spec{1, 1, reprResult}, emitRepr, nil),
		module.NewSymbol("getattr", getAttrCheck, emitGetAttr, nil),
		module.NewSymbol("hasattr", hasAttrCheck, emitHasAttr, nil),
		module.NewSymbol("isinstance", isInstanceCheck, emitIsInstance, nil),
		module.NewSymbol("map", mapCheck, emitMap, nil),
		module.NewSymbol("filter", filterCheck, emitFilter, nil),
	)
}

func callSymbol(name string, sp spec, emit module.EmitFunc, value module.ValueFunc) *module.NativeSymbol {
	check := func(c module.Checker, args []ast.Expr, pos token.Pos) types.Type {
		return checkBuiltin(c, name, sp, args, pos)
	}
	return module.NewSymbol(name, check, emit, value)
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
	if name == "range" && len(args) == 3 {
		if step, ok := constInt(args[2]); ok && step == 0 {
			c.Error(args[2].Pos(), token.SyntaxError, "range() step must not be zero")
		}
	}
	return result
}

func valueHost(fn func() *interp.HostFunction) module.ValueFunc {
	return func(module.Runtime) vmtypes.Value { return fn() }
}

// constInt evaluates an int literal with an optional unary sign.
func constInt(expr ast.Expr) (int64, bool) {
	switch value := expr.(type) {
	case *ast.IntLit:
		return value.Value, true
	case *ast.UnaryExpr:
		literal, ok := value.X.(*ast.IntLit)
		if !ok {
			return 0, false
		}
		switch value.Op {
		case token.MINUS:
			return -literal.Value, true
		case token.PLUS:
			return literal.Value, true
		}
	}
	return 0, false
}
