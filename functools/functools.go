// Package functools provides Python's functools module as a native module:
// higher-order functions that act on or return other functions. Currently
// exposes reduce(fn, xs[, initial]) which applies fn cumulatively to the items
// of xs. Static types are preferred; dynamic/Any arguments are supported with
// runtime dispatch following the same inline-emit pattern as builtins map/filter.
package functools

import (
	"errors"

	"github.com/siyul-park/minipy/ast"
	"github.com/siyul-park/minipy/hostabi"
	"github.com/siyul-park/minipy/module"
	"github.com/siyul-park/minipy/token"
	"github.com/siyul-park/minipy/types"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/interp"
	vmtypes "github.com/siyul-park/minivm/types"
)

// Name is the module's registered name.
const Name = "functools"

// ErrReduceEmpty is raised when reduce() is called with an empty sequence and
// no initial value.
var ErrReduceEmpty = errors.New("reduce() of empty iterable with no initial value")

// New builds the functools native module.
func New() *module.NativeModule {
	return module.NewNative(Name,
		module.NewSymbol("reduce", reduceCheck, emitReduce, nil),
	)
}

// reduceCheck type-checks reduce(fn, xs) or reduce(fn, xs, initial).
func reduceCheck(c module.Checker, args []ast.Expr, pos token.Pos) types.Type {
	if len(args) < 2 || len(args) > 3 {
		c.Error(pos, token.ArityMismatch, "reduce() takes 2 or 3 arguments (%d given)", len(args))
		for _, a := range args {
			c.Check(a)
		}
		return types.Invalid
	}

	// Check the list argument first to determine element type.
	listType := c.Check(args[1])
	list, ok := listType.(*types.List)
	if !ok {
		if types.IsDynamic(listType) {
			c.Check(args[0])
			if len(args) == 3 {
				c.Check(args[2])
			}
			return types.Any
		}
		c.Check(args[0])
		if len(args) == 3 {
			c.Check(args[2])
		}
		c.Error(args[1].Pos(), token.TypeMismatch, "reduce() second argument must be a list")
		return types.Invalid
	}

	elemType := list.Elem
	if types.IsDynamic(elemType) {
		c.Check(args[0])
		if len(args) == 3 {
			c.Check(args[2])
		}
		return types.Any
	}

	// Use the element type to provide a hint for the function argument.
	// The hint is Callable[[T, T], T] so lambdas can infer parameter types.
	hint := types.NewCallable([]types.Type{elemType, elemType}, elemType)
	fnType := c.CheckWithHint(args[0], hint)
	callable, ok := fnType.(*types.Callable)
	if !ok || len(callable.Params) != 2 {
		if types.IsDynamic(fnType) {
			if len(args) == 3 {
				c.Check(args[2])
			}
			return types.Any
		}
		c.Error(args[0].Pos(), token.TypeMismatch, "reduce() first argument must be a callable with 2 parameters")
		if len(args) == 3 {
			c.Check(args[2])
		}
		return types.Invalid
	}

	if !types.AssignableTo(elemType, callable.Params[0]) {
		c.Error(args[0].Pos(), token.TypeMismatch, "reduce() list element type %s is not assignable to function parameter %s", elemType, callable.Params[0])
		if len(args) == 3 {
			c.Check(args[2])
		}
		return types.Invalid
	}
	if !types.AssignableTo(elemType, callable.Params[1]) {
		c.Error(args[0].Pos(), token.TypeMismatch, "reduce() list element type %s is not assignable to function parameter %s", elemType, callable.Params[1])
		if len(args) == 3 {
			c.Check(args[2])
		}
		return types.Invalid
	}

	// Validate optional initial value matches the element type.
	if len(args) == 3 {
		initType := c.Check(args[2])
		if types.IsDynamic(initType) {
			return types.Any
		}
		if !types.AssignableTo(initType, elemType) {
			c.Error(args[2].Pos(), token.TypeMismatch, "reduce() initial value type %s is not assignable to element type %s", initType, elemType)
			return types.Invalid
		}
	}

	return callable.Return
}

// emitReduce lowers reduce(fn, xs[, initial]) to an inline iteration loop.
func emitReduce(e module.Emitter, args []ast.Expr) {
	// The accumulator carries the sequence's element type, which is also
	// reduce()'s result type, so it must be declared with that kind rather than
	// as a reference: the caller may store the result straight into a typed slot.
	elem := types.IterableElem(types.Erase(e.Type(args[1])))
	fnSlot := e.Tmp(vmtypes.TypeRef)
	listSlot := e.Tmp(vmtypes.TypeRef)
	accSlot := e.Tmp(hostabi.VMParamType(elem))
	idxSlot := e.Tmp(vmtypes.TypeI64)

	// Evaluate function and list, store in slots.
	e.Expr(args[0])
	e.Emit(instr.GLOBAL_SET, uint64(fnSlot))
	e.Expr(args[1])
	e.Emit(instr.GLOBAL_SET, uint64(listSlot))

	if len(args) == 3 {
		// 3-arg form: accumulator = initial, start index = 0.
		e.Expr(args[2])
		e.Emit(instr.GLOBAL_SET, uint64(accSlot))
		e.Emit(instr.I64_CONST, 0)
		e.Emit(instr.GLOBAL_SET, uint64(idxSlot))
	} else {
		// 2-arg form: check if list is empty, then accumulator = list[0], start index = 1.
		ok := e.Label()

		e.Emit(instr.GLOBAL_GET, uint64(listSlot))
		e.Emit(instr.ARRAY_LEN)
		e.Emit(instr.I32_CONST, 0)
		e.Emit(instr.I32_GT_S)
		e.BrIf(ok)

		// Empty list: call host function that raises an error.
		e.CallHost(reduceEmptyHost())

		// The host errors unconditionally, so the DROP/assignment below is
		// unreachable but needed for stack balance at compile time.
		e.Emit(instr.DROP)

		e.Bind(ok)

		// accumulator = list[0]
		e.Emit(instr.GLOBAL_GET, uint64(listSlot))
		e.Emit(instr.I32_CONST, 0)
		e.Emit(instr.ARRAY_GET)
		e.Emit(instr.GLOBAL_SET, uint64(accSlot))

		// i = 1
		e.Emit(instr.I64_CONST, 1)
		e.Emit(instr.GLOBAL_SET, uint64(idxSlot))
	}

	top := e.Label()
	end := e.Label()
	e.Bind(top)

	// if i >= len(list): break
	e.Emit(instr.GLOBAL_GET, uint64(idxSlot))
	e.Emit(instr.GLOBAL_GET, uint64(listSlot))
	e.Emit(instr.ARRAY_LEN)
	e.Emit(instr.I32_TO_I64_S)
	e.Emit(instr.I64_LT_S)
	e.Emit(instr.I32_EQZ)
	e.BrIf(end)

	// call fn(accumulator, list[i])
	e.Emit(instr.GLOBAL_GET, uint64(accSlot))
	e.Emit(instr.GLOBAL_GET, uint64(listSlot))
	e.Emit(instr.GLOBAL_GET, uint64(idxSlot))
	e.Emit(instr.I64_TO_I32)
	e.Emit(instr.ARRAY_GET)
	e.Emit(instr.GLOBAL_GET, uint64(fnSlot))
	e.Emit(instr.CALL)

	// accumulator = result
	e.Emit(instr.GLOBAL_SET, uint64(accSlot))

	// i++
	e.Emit(instr.GLOBAL_GET, uint64(idxSlot))
	e.Emit(instr.I64_CONST, 1)
	e.Emit(instr.I64_ADD)
	e.Emit(instr.GLOBAL_SET, uint64(idxSlot))
	e.Br(top)

	e.Bind(end)
	// Leave accumulator on stack as the result.
	e.Emit(instr.GLOBAL_GET, uint64(accSlot))
}

// reduceEmptyHost returns a host function that always errors with
// ErrReduceEmpty. It is called at runtime when reduce is given an empty
// iterable with no initial value.
func reduceEmptyHost() *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{}, Returns: []vmtypes.Type{vmtypes.TypeRef}},
		func(_ *interp.Interpreter, _ []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			return nil, ErrReduceEmpty
		},
	)
}
