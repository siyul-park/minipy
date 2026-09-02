package builtins

import (
	"github.com/siyul-park/minipy/ast"
	"github.com/siyul-park/minipy/module"
	"github.com/siyul-park/minipy/token"
	"github.com/siyul-park/minipy/types"

	"github.com/siyul-park/minivm/instr"
	vmtypes "github.com/siyul-park/minivm/types"
)

// mapCheck type-checks map(fn, list) -> list[R].
func mapCheck(c module.Checker, args []ast.Expr, pos token.Pos) types.Type {
	if len(args) != 2 {
		c.Error(pos, token.ArityMismatch, "map() takes exactly 2 arguments (%d given)", len(args))
		for _, a := range args {
			c.Check(a)
		}
		return types.Invalid
	}
	// Check the list argument first to determine element type.
	listType := c.Check(args[1])
	list, ok := listType.(*types.List)
	if !ok {
		c.Check(args[0])
		c.Error(args[1].Pos(), token.TypeMismatch, "map() second argument must be a list")
		return types.Invalid
	}
	// Use the list element type to provide a hint for the function argument.
	// The hint is Callable[[elemType], Any] so lambdas can infer parameter types.
	// We use Any as the return type hint since we do not know it yet.
	hint := types.NewCallable([]types.Type{list.Elem}, types.Any)
	fnType := c.CheckWithHint(args[0], hint)
	callable, ok := fnType.(*types.Callable)
	if !ok || len(callable.Params) != 1 {
		c.Error(args[0].Pos(), token.TypeMismatch, "map() first argument must be a callable with 1 parameter")
		return types.Invalid
	}
	if !types.AssignableTo(list.Elem, callable.Params[0]) {
		c.Error(args[1].Pos(), token.TypeMismatch, "map() list element type %s is not assignable to function parameter %s", list.Elem, callable.Params[0])
		return types.Invalid
	}
	return types.NewList(callable.Return)
}

// filterCheck type-checks filter(fn, list) -> list[T].
func filterCheck(c module.Checker, args []ast.Expr, pos token.Pos) types.Type {
	if len(args) != 2 {
		c.Error(pos, token.ArityMismatch, "filter() takes exactly 2 arguments (%d given)", len(args))
		for _, a := range args {
			c.Check(a)
		}
		return types.Invalid
	}
	// Check the list argument first to determine element type.
	listType := c.Check(args[1])
	list, ok := listType.(*types.List)
	if !ok {
		c.Check(args[0])
		c.Error(args[1].Pos(), token.TypeMismatch, "filter() second argument must be a list")
		return types.Invalid
	}
	// Use the list element type to provide a hint for the predicate argument.
	hint := types.NewCallable([]types.Type{list.Elem}, types.Bool)
	fnType := c.CheckWithHint(args[0], hint)
	callable, ok := fnType.(*types.Callable)
	if !ok || len(callable.Params) != 1 {
		c.Error(args[0].Pos(), token.TypeMismatch, "filter() first argument must be a callable with 1 parameter")
		return types.Invalid
	}
	if !types.AssignableTo(list.Elem, callable.Params[0]) {
		c.Error(args[1].Pos(), token.TypeMismatch, "filter() list element type %s is not assignable to function parameter %s", list.Elem, callable.Params[0])
		return types.Invalid
	}
	if !types.Equal(callable.Return, types.Bool) {
		c.Error(args[0].Pos(), token.TypeMismatch, "filter() predicate must return bool, got %s", callable.Return)
		return types.Invalid
	}
	return list
}

// emitMap lowers map(fn, list) to an inline iteration loop.
func emitMap(e module.Emitter, args []ast.Expr) {
	fnSlot := e.Tmp(vmtypes.TypeAny)
	listSlot := e.Tmp(vmtypes.TypeAny)
	outSlot := e.Tmp(vmtypes.TypeAny)
	idxSlot := e.Tmp(vmtypes.TypeI64)
	elemSlot := e.Tmp(vmtypes.TypeAny)

	resultType := types.NewList(e.Type(args[0]).(*types.Callable).Return)

	// Evaluate function and list, store in slots.
	e.Expr(args[0])
	e.Emit(instr.GLOBAL_SET, uint64(fnSlot))
	e.Expr(args[1])
	e.Emit(instr.GLOBAL_SET, uint64(listSlot))

	// Create empty output array.
	e.Emit(instr.I32_CONST, 0)
	e.Emit(instr.ARRAY_NEW_DEFAULT, e.TypeIndex(resultType))
	e.Emit(instr.GLOBAL_SET, uint64(outSlot))

	// i = 0
	e.Emit(instr.I64_CONST, 0)
	e.Emit(instr.GLOBAL_SET, uint64(idxSlot))

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

	// element = list[i]
	e.Emit(instr.GLOBAL_GET, uint64(listSlot))
	e.Emit(instr.GLOBAL_GET, uint64(idxSlot))
	e.Emit(instr.I64_TO_I32)
	e.Emit(instr.ARRAY_GET)
	e.Emit(instr.GLOBAL_SET, uint64(elemSlot))

	// call fn(element) -> result on stack
	e.Emit(instr.GLOBAL_GET, uint64(elemSlot))
	e.Emit(instr.GLOBAL_GET, uint64(fnSlot))
	e.Emit(instr.CALL)

	// append result to output: output, value, 1, ARRAY_APPEND, DROP
	resultSlot := e.Tmp(vmtypes.TypeAny)
	e.Emit(instr.GLOBAL_SET, uint64(resultSlot))
	e.Emit(instr.GLOBAL_GET, uint64(outSlot))
	e.Emit(instr.GLOBAL_GET, uint64(resultSlot))
	e.Emit(instr.I32_CONST, 1)
	e.Emit(instr.ARRAY_APPEND)
	e.Emit(instr.DROP)

	// i++
	e.Emit(instr.GLOBAL_GET, uint64(idxSlot))
	e.Emit(instr.I64_CONST, 1)
	e.Emit(instr.I64_ADD)
	e.Emit(instr.GLOBAL_SET, uint64(idxSlot))
	e.Br(top)

	e.Bind(end)
	e.Emit(instr.GLOBAL_GET, uint64(outSlot))
}

// emitFilter lowers filter(fn, list) to an inline iteration loop.
func emitFilter(e module.Emitter, args []ast.Expr) {
	fnSlot := e.Tmp(vmtypes.TypeAny)
	listSlot := e.Tmp(vmtypes.TypeAny)
	outSlot := e.Tmp(vmtypes.TypeAny)
	idxSlot := e.Tmp(vmtypes.TypeI64)
	elemSlot := e.Tmp(vmtypes.TypeAny)

	resultType := e.Type(args[1])

	// Evaluate function and list, store in slots.
	e.Expr(args[0])
	e.Emit(instr.GLOBAL_SET, uint64(fnSlot))
	e.Expr(args[1])
	e.Emit(instr.GLOBAL_SET, uint64(listSlot))

	// Create empty output array (same type as input list).
	e.Emit(instr.I32_CONST, 0)
	e.Emit(instr.ARRAY_NEW_DEFAULT, e.TypeIndex(resultType))
	e.Emit(instr.GLOBAL_SET, uint64(outSlot))

	// i = 0
	e.Emit(instr.I64_CONST, 0)
	e.Emit(instr.GLOBAL_SET, uint64(idxSlot))

	top := e.Label()
	end := e.Label()
	cont := e.Label()
	e.Bind(top)

	// if i >= len(list): break
	e.Emit(instr.GLOBAL_GET, uint64(idxSlot))
	e.Emit(instr.GLOBAL_GET, uint64(listSlot))
	e.Emit(instr.ARRAY_LEN)
	e.Emit(instr.I32_TO_I64_S)
	e.Emit(instr.I64_LT_S)
	e.Emit(instr.I32_EQZ)
	e.BrIf(end)

	// element = list[i]
	e.Emit(instr.GLOBAL_GET, uint64(listSlot))
	e.Emit(instr.GLOBAL_GET, uint64(idxSlot))
	e.Emit(instr.I64_TO_I32)
	e.Emit(instr.ARRAY_GET)
	e.Emit(instr.GLOBAL_SET, uint64(elemSlot))

	// call fn(element) -> bool on stack
	e.Emit(instr.GLOBAL_GET, uint64(elemSlot))
	e.Emit(instr.GLOBAL_GET, uint64(fnSlot))
	e.Emit(instr.CALL)

	// if result is false, skip append
	e.Emit(instr.I32_EQZ)
	e.BrIf(cont)

	// append element to output
	e.Emit(instr.GLOBAL_GET, uint64(outSlot))
	e.Emit(instr.GLOBAL_GET, uint64(elemSlot))
	e.Emit(instr.I32_CONST, 1)
	e.Emit(instr.ARRAY_APPEND)
	e.Emit(instr.DROP)

	// i++
	e.Bind(cont)
	e.Emit(instr.GLOBAL_GET, uint64(idxSlot))
	e.Emit(instr.I64_CONST, 1)
	e.Emit(instr.I64_ADD)
	e.Emit(instr.GLOBAL_SET, uint64(idxSlot))
	e.Br(top)

	e.Bind(end)
	e.Emit(instr.GLOBAL_GET, uint64(outSlot))
}
