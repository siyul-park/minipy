// Package math provides Python's math module as a native module: mathematical
// constants (pi, e, tau, inf, nan) and standard functions (ceil, floor, sqrt,
// etc.). Each symbol is either a ConstantSymbol (inline value) or a callable
// Symbol backed by a host function. Static types are preferred; dynamic/Any
// arguments are supported with runtime dispatch.
//
// Restriction: math.ceil and math.floor return float, not int. CPython's
// math.ceil(2.3) returns 3 (int), but minipy returns 3.0 (float) because the
// minivm type system maps these operations to f64 uniformly.
package math

import (
	"errors"
	"fmt"
	gomath "math"

	"github.com/siyul-park/minipy/ast"
	"github.com/siyul-park/minipy/module"
	"github.com/siyul-park/minipy/token"
	"github.com/siyul-park/minipy/types"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/interp"
	vmtypes "github.com/siyul-park/minivm/types"
)

// Name is the module's registered name.
const Name = "math"

// Runtime errors exposed by math host functions.
var (
	ErrFactorial = errors.New("factorial() not defined for negative values")
)

// New builds the math native module.
func New() *module.NativeModule {
	return module.NewNative(Name,
		// Constants
		constant("pi", gomath.Pi),
		constant("e", gomath.E),
		constant("tau", 2*gomath.Pi),
		constant("inf", gomath.Inf(1)),
		constant("nan", gomath.NaN()),
		// Unary float functions
		unaryFloat("ceil", gomath.Ceil),
		unaryFloat("floor", gomath.Floor),
		unaryFloat("sqrt", gomath.Sqrt),
		unaryFloat("log", gomath.Log),
		unaryFloat("log2", gomath.Log2),
		unaryFloat("log10", gomath.Log10),
		unaryFloat("exp", gomath.Exp),
		unaryFloat("sin", gomath.Sin),
		unaryFloat("cos", gomath.Cos),
		unaryFloat("tan", gomath.Tan),
		unaryFloat("asin", gomath.Asin),
		unaryFloat("acos", gomath.Acos),
		unaryFloat("atan", gomath.Atan),
		unaryFloat("fabs", gomath.Abs),
		unaryFloat("trunc", gomath.Trunc),
		unaryFloat("degrees", degrees),
		unaryFloat("radians", radians),
		// Binary float functions
		binaryFloat("atan2", gomath.Atan2),
		binaryFloat("fmod", gomath.Mod),
		binaryFloat("copysign", gomath.Copysign),
		binaryFloat("pow", gomath.Pow),
		// Predicates
		predicate("isnan", gomath.IsNaN),
		predicate("isinf", isInfWrap),
		predicate("isfinite", isFinite),
		// Integer functions
		module.NewSymbol("gcd", checkGCD, emitGCD(gcdHost), nil),
		module.NewSymbol("factorial", checkFactorial, emitFactorial(factorialHost), nil),
	)
}

// constant builds a ConstantSymbol that emits an F64_CONST instruction.
func constant(name string, value float64) *module.NativeConstant {
	bits := gomath.Float64bits(value)
	return module.NewConstant(name, types.Float, func(e module.Emitter, _ []ast.Expr) {
		e.Emit(instr.F64_CONST, uint64(bits))
	})
}

// unaryFloat builds a callable symbol: (float) -> float, accepting int with
// promotion.
func unaryFloat(name string, fn func(float64) float64) *module.NativeSymbol {
	host := unaryFloatHost(fn)
	return module.NewSymbol(name, checkUnaryFloat(name), emitUnaryFloat(host, fn), nil)
}

// binaryFloat builds a callable symbol: (float, float) -> float, accepting int
// with promotion.
func binaryFloat(name string, fn func(float64, float64) float64) *module.NativeSymbol {
	host := binaryFloatHost(fn)
	return module.NewSymbol(name, checkBinaryFloat(name), emitBinaryFloat(host, fn), nil)
}

// predicate builds a callable symbol: (float) -> bool, accepting int with
// promotion.
func predicate(name string, fn func(float64) bool) *module.NativeSymbol {
	host := predicateHost(fn)
	return module.NewSymbol(name, checkPredicate(name), emitPredicate(host, fn), nil)
}

// --- Check functions ---

func checkUnaryFloat(name string) module.CheckFunc {
	return func(c module.Checker, args []ast.Expr, pos token.Pos) types.Type {
		if len(args) != 1 {
			c.Error(pos, token.ArityMismatch, "%s() takes exactly 1 argument (%d given)", name, len(args))
			return types.Invalid
		}
		t := c.Check(args[0])
		if types.IsDynamic(t) {
			return types.Any
		}
		if !isNumeric(t) {
			c.Error(pos, token.TypeMismatch, "%s() argument must be int or float", name)
			return types.Invalid
		}
		return types.Float
	}
}

func checkBinaryFloat(name string) module.CheckFunc {
	return func(c module.Checker, args []ast.Expr, pos token.Pos) types.Type {
		if len(args) != 2 {
			c.Error(pos, token.ArityMismatch, "%s() takes exactly 2 arguments (%d given)", name, len(args))
			return types.Invalid
		}
		t0 := c.Check(args[0])
		t1 := c.Check(args[1])
		if types.IsDynamic(t0) || types.IsDynamic(t1) {
			return types.Any
		}
		if !isNumeric(t0) || !isNumeric(t1) {
			c.Error(pos, token.TypeMismatch, "%s() arguments must be int or float", name)
			return types.Invalid
		}
		return types.Float
	}
}

func checkPredicate(name string) module.CheckFunc {
	return func(c module.Checker, args []ast.Expr, pos token.Pos) types.Type {
		if len(args) != 1 {
			c.Error(pos, token.ArityMismatch, "%s() takes exactly 1 argument (%d given)", name, len(args))
			return types.Invalid
		}
		t := c.Check(args[0])
		if types.IsDynamic(t) {
			return types.Any
		}
		if !isNumeric(t) {
			c.Error(pos, token.TypeMismatch, "%s() argument must be int or float", name)
			return types.Invalid
		}
		return types.Bool
	}
}

var checkGCD module.CheckFunc = func(c module.Checker, args []ast.Expr, pos token.Pos) types.Type {
	if len(args) != 2 {
		c.Error(pos, token.ArityMismatch, "gcd() takes exactly 2 arguments (%d given)", len(args))
		return types.Invalid
	}
	t0 := c.Check(args[0])
	t1 := c.Check(args[1])
	if types.IsDynamic(t0) || types.IsDynamic(t1) {
		return types.Any
	}
	if !types.Equal(t0, types.Int) || !types.Equal(t1, types.Int) {
		c.Error(pos, token.TypeMismatch, "gcd() arguments must be int")
		return types.Invalid
	}
	return types.Int
}

var checkFactorial module.CheckFunc = func(c module.Checker, args []ast.Expr, pos token.Pos) types.Type {
	if len(args) != 1 {
		c.Error(pos, token.ArityMismatch, "factorial() takes exactly 1 argument (%d given)", len(args))
		return types.Invalid
	}
	t := c.Check(args[0])
	if types.IsDynamic(t) {
		return types.Any
	}
	if !types.Equal(t, types.Int) {
		c.Error(pos, token.TypeMismatch, "factorial() argument must be int")
		return types.Invalid
	}
	return types.Int
}

// --- Emit functions ---

func emitUnaryFloat(host *interp.HostFunction, fn func(float64) float64) module.EmitFunc {
	return func(e module.Emitter, args []ast.Expr) {
		e.Expr(args[0])
		argType := e.Type(args[0])
		if types.IsDynamic(argType) {
			e.CallHost(dynUnaryFloatHost(fn))
			return
		}
		if types.Equal(argType, types.Int) {
			e.Emit(instr.I64_TO_F64_S)
		}
		e.CallHost(host)
	}
}

func emitBinaryFloat(host *interp.HostFunction, fn func(float64, float64) float64) module.EmitFunc {
	return func(e module.Emitter, args []ast.Expr) {
		t0 := e.Type(args[0])
		t1 := e.Type(args[1])
		if types.IsDynamic(t0) || types.IsDynamic(t1) {
			e.Expr(args[0])
			e.Expr(args[1])
			e.CallHost(dynBinaryFloatHost(fn, t0, t1))
			return
		}
		e.Expr(args[0])
		if types.Equal(t0, types.Int) {
			e.Emit(instr.I64_TO_F64_S)
		}
		e.Expr(args[1])
		if types.Equal(t1, types.Int) {
			e.Emit(instr.I64_TO_F64_S)
		}
		e.CallHost(host)
	}
}

func emitPredicate(host *interp.HostFunction, fn func(float64) bool) module.EmitFunc {
	return func(e module.Emitter, args []ast.Expr) {
		e.Expr(args[0])
		argType := e.Type(args[0])
		if types.IsDynamic(argType) {
			e.CallHost(dynPredicateHost(fn))
			return
		}
		if types.Equal(argType, types.Int) {
			e.Emit(instr.I64_TO_F64_S)
		}
		e.CallHost(host)
	}
}

func emitGCD(hostFn func() *interp.HostFunction) module.EmitFunc {
	return func(e module.Emitter, args []ast.Expr) {
		t0 := e.Type(args[0])
		t1 := e.Type(args[1])
		if types.IsDynamic(t0) || types.IsDynamic(t1) {
			e.Expr(args[0])
			e.Expr(args[1])
			e.CallHost(dynGCDHost(t0, t1))
			return
		}
		e.Expr(args[0])
		e.Expr(args[1])
		e.CallHost(hostFn())
	}
}

func emitFactorial(hostFn func() *interp.HostFunction) module.EmitFunc {
	return func(e module.Emitter, args []ast.Expr) {
		t0 := e.Type(args[0])
		if types.IsDynamic(t0) {
			e.Expr(args[0])
			e.CallHost(dynFactorialHost())
			return
		}
		e.Expr(args[0])
		e.CallHost(hostFn())
	}
}

// --- Host functions ---

func unaryFloatHost(fn func(float64) float64) *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{vmtypes.TypeF64}, Returns: []vmtypes.Type{vmtypes.TypeF64}},
		func(_ *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			return []vmtypes.Boxed{vmtypes.BoxF64(fn(params[0].F64()))}, nil
		},
	)
}

func binaryFloatHost(fn func(float64, float64) float64) *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{vmtypes.TypeF64, vmtypes.TypeF64}, Returns: []vmtypes.Type{vmtypes.TypeF64}},
		func(_ *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			return []vmtypes.Boxed{vmtypes.BoxF64(fn(params[0].F64(), params[1].F64()))}, nil
		},
	)
}

func predicateHost(fn func(float64) bool) *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{vmtypes.TypeF64}, Returns: []vmtypes.Type{vmtypes.TypeI1}},
		func(_ *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			return []vmtypes.Boxed{vmtypes.BoxI1(fn(params[0].F64()))}, nil
		},
	)
}

func gcdHost() *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{vmtypes.TypeI64, vmtypes.TypeI64}, Returns: []vmtypes.Type{vmtypes.TypeI64}},
		func(_ *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			a := params[0].I64()
			b := params[1].I64()
			if a < 0 {
				a = -a
			}
			if b < 0 {
				b = -b
			}
			for b != 0 {
				a, b = b, a%b
			}
			return []vmtypes.Boxed{vmtypes.BoxI64(a)}, nil
		},
	)
}

func factorialHost() *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{vmtypes.TypeI64}, Returns: []vmtypes.Type{vmtypes.TypeI64}},
		func(_ *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			n := params[0].I64()
			if n < 0 {
				return nil, fmt.Errorf("%w: %d", ErrFactorial, n)
			}
			result := int64(1)
			for i := int64(2); i <= n; i++ {
				result *= i
			}
			return []vmtypes.Boxed{vmtypes.BoxI64(result)}, nil
		},
	)
}

// --- Dynamic dispatch host functions ---
// These accept vmtypes.TypeRef parameters for dynamic-typed arguments, unbox
// the value at runtime, promote to float64 or int64 as needed, and perform
// the math operation directly.

// dynUnaryFloatHost creates a dynamic dispatch host function for unary float
// operations. It accepts a TypeRef argument, unboxes it to float64, and applies fn.
func dynUnaryFloatHost(fn func(float64) float64) *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{vmtypes.TypeRef}, Returns: []vmtypes.Type{vmtypes.TypeF64}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			f, err := unboxFloat(i, params[0])
			if err != nil {
				return nil, err
			}
			return []vmtypes.Boxed{vmtypes.BoxF64(fn(f))}, nil
		},
	)
}

// dynBinaryFloatHost creates a dynamic dispatch host function for binary float
// operations. Each argument may be either a direct numeric type (when one arg
// is static) or a TypeRef (when dynamic). The host unboxes as needed.
func dynBinaryFloatHost(fn func(float64, float64) float64, t0, t1 types.Type) *interp.HostFunction {
	p0 := vmParamType(t0)
	p1 := vmParamType(t1)
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{p0, p1}, Returns: []vmtypes.Type{vmtypes.TypeF64}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			f0, err := unboxFloat(i, params[0])
			if err != nil {
				return nil, err
			}
			f1, err := unboxFloat(i, params[1])
			if err != nil {
				return nil, err
			}
			return []vmtypes.Boxed{vmtypes.BoxF64(fn(f0, f1))}, nil
		},
	)
}

// dynPredicateHost creates a dynamic dispatch host function for predicates.
func dynPredicateHost(fn func(float64) bool) *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{vmtypes.TypeRef}, Returns: []vmtypes.Type{vmtypes.TypeI1}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			f, err := unboxFloat(i, params[0])
			if err != nil {
				return nil, err
			}
			return []vmtypes.Boxed{vmtypes.BoxI1(fn(f))}, nil
		},
	)
}

// dynGCDHost creates a dynamic dispatch host function for gcd.
func dynGCDHost(t0, t1 types.Type) *interp.HostFunction {
	p0 := vmParamType(t0)
	p1 := vmParamType(t1)
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{p0, p1}, Returns: []vmtypes.Type{vmtypes.TypeI64}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			a, err := unboxInt(i, params[0])
			if err != nil {
				return nil, err
			}
			b, err := unboxInt(i, params[1])
			if err != nil {
				return nil, err
			}
			if a < 0 {
				a = -a
			}
			if b < 0 {
				b = -b
			}
			for b != 0 {
				a, b = b, a%b
			}
			return []vmtypes.Boxed{vmtypes.BoxI64(a)}, nil
		},
	)
}

// dynFactorialHost creates a dynamic dispatch host function for factorial.
func dynFactorialHost() *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{vmtypes.TypeRef}, Returns: []vmtypes.Type{vmtypes.TypeI64}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			n, err := unboxInt(i, params[0])
			if err != nil {
				return nil, err
			}
			if n < 0 {
				return nil, fmt.Errorf("%w: %d", ErrFactorial, n)
			}
			result := int64(1)
			for j := int64(2); j <= n; j++ {
				result *= j
			}
			return []vmtypes.Boxed{vmtypes.BoxI64(result)}, nil
		},
	)
}

// unboxFloat extracts a float64 from a boxed value, promoting int if necessary.
func unboxFloat(i *interp.Interpreter, v vmtypes.Boxed) (float64, error) {
	switch v.Kind() {
	case vmtypes.KindF64:
		return v.F64(), nil
	case vmtypes.KindI64:
		return float64(v.I64()), nil
	case vmtypes.KindRef:
		if v.Ref() == 0 {
			return 0, interp.ErrTypeMismatch
		}
		val, err := i.Load(v.Ref())
		if err != nil {
			return 0, err
		}
		switch n := val.(type) {
		case vmtypes.I64:
			return float64(n), nil
		case vmtypes.F64:
			return float64(n), nil
		default:
			return 0, interp.ErrTypeMismatch
		}
	default:
		return 0, interp.ErrTypeMismatch
	}
}

// unboxInt extracts an int64 from a boxed value.
func unboxInt(i *interp.Interpreter, v vmtypes.Boxed) (int64, error) {
	switch v.Kind() {
	case vmtypes.KindI64:
		return v.I64(), nil
	case vmtypes.KindRef:
		if v.Ref() == 0 {
			return 0, interp.ErrTypeMismatch
		}
		val, err := i.Load(v.Ref())
		if err != nil {
			return 0, err
		}
		switch n := val.(type) {
		case vmtypes.I64:
			return int64(n), nil
		default:
			return 0, interp.ErrTypeMismatch
		}
	default:
		return 0, interp.ErrTypeMismatch
	}
}

// vmParamType returns the VM-level type for a compile-time type, mapping
// dynamic types to TypeRef.
func vmParamType(t types.Type) vmtypes.Type {
	if types.IsDynamic(t) {
		return vmtypes.TypeRef
	}
	return t.VM()
}

// --- Helpers ---

func isNumeric(t types.Type) bool {
	return types.Equal(t, types.Int) || types.Equal(t, types.Float)
}

func degrees(rad float64) float64 { return rad * (180.0 / gomath.Pi) }
func radians(deg float64) float64 { return deg * (gomath.Pi / 180.0) }

func isFinite(f float64) bool { return !gomath.IsInf(f, 0) && !gomath.IsNaN(f) }

// isInfWrap adapts math.IsInf to the unary predicate signature.
func isInfWrap(f float64) bool { return gomath.IsInf(f, 0) }
