// Package sysmod provides Python's sys module as a native module: system
// constants (maxsize, platform, version, byteorder) and limited system
// functions (getrecursionlimit, exit). Constants are ConstantSymbol values that
// emit inline. Static types are preferred; dynamic/Any arguments are supported
// with runtime dispatch.
//
// Restriction: sys.exit() is a hard VM halt (UNREACHABLE instruction), not a
// recoverable SystemExit exception as in CPython. The exit code argument is
// evaluated for side effects but discarded; the VM terminates unconditionally
// with no recovery path.
package sysmod

import (
	"math"
	"runtime"

	"github.com/siyul-park/minipy/ast"
	"github.com/siyul-park/minipy/module"
	"github.com/siyul-park/minipy/token"
	"github.com/siyul-park/minipy/types"

	"github.com/siyul-park/minivm/instr"
	vmtypes "github.com/siyul-park/minivm/types"
)

// Name is the module's registered name.
const Name = "sys"

// Version is the minipy version string exposed by sys.version.
const Version = "0.1.0 (minipy)"

// New builds the sys native module.
func New() *module.NativeModule {
	return module.NewNative(Name,
		// Constants
		intConstant("maxsize", math.MaxInt64),
		strConstant("platform", runtime.GOOS),
		strConstant("version", Version),
		strConstant("byteorder", "little"),
		// Functions
		module.NewSymbol("getrecursionlimit", checkGetrecursionlimit, emitGetrecursionlimit, nil),
		module.NewSymbol("exit", checkExit, emitExit, nil),
	)
}

// intConstant builds a ConstantSymbol that emits an I64_CONST instruction.
func intConstant(name string, value int64) *module.NativeConstant {
	return module.NewConstant(name, types.Int, func(e module.Emitter, _ []ast.Expr) {
		e.Emit(instr.I64_CONST, uint64(value))
	})
}

// strConstant builds a ConstantSymbol that emits a CONST_GET instruction for a
// string value from the constant pool.
func strConstant(name string, value string) *module.NativeConstant {
	return module.NewConstant(name, types.Str, func(e module.Emitter, _ []ast.Expr) {
		e.ConstGet(vmtypes.String(value))
	})
}

// --- Check functions ---

// checkGetrecursionlimit type-checks getrecursionlimit(): 0 args, returns int.
func checkGetrecursionlimit(c module.Checker, args []ast.Expr, pos token.Pos) types.Type {
	if len(args) != 0 {
		c.Error(pos, token.ArityMismatch, "getrecursionlimit() takes no arguments (%d given)", len(args))
		return types.Invalid
	}
	return types.Int
}

// checkExit type-checks exit(code): 1 int arg, returns None. Dynamic/Any
// arguments are accepted with a None result.
func checkExit(c module.Checker, args []ast.Expr, pos token.Pos) types.Type {
	if len(args) != 1 {
		c.Error(pos, token.ArityMismatch, "exit() takes exactly 1 argument (%d given)", len(args))
		for _, a := range args {
			c.Check(a)
		}
		return types.Invalid
	}
	t := c.Check(args[0])
	if types.IsDynamic(t) {
		return types.None
	}
	if !types.Equal(t, types.Int) {
		c.Error(args[0].Pos(), token.TypeMismatch, "exit() argument must be int")
		return types.Invalid
	}
	return types.None
}

// --- Emit functions ---

// emitGetrecursionlimit emits I64_CONST 1000 (fixed recursion limit).
func emitGetrecursionlimit(e module.Emitter, _ []ast.Expr) {
	e.Emit(instr.I64_CONST, 1000)
}

// emitExit evaluates the code argument, drops it, then emits UNREACHABLE to
// halt the VM.
func emitExit(e module.Emitter, args []ast.Expr) {
	e.Expr(args[0])
	e.Emit(instr.DROP)
	e.Emit(instr.UNREACHABLE)
}
