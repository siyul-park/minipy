package compiler

import (
	"github.com/siyul-park/minipy/ast"
	"github.com/siyul-park/minipy/token"
	"github.com/siyul-park/minipy/types"

	"github.com/siyul-park/minivm/instr"
)

// builtinMethod is one method of a builtin container or string receiver.
//
// One entry owns both halves of the contract. check admits a call and gives it a
// result type; emit lowers the same call. Keeping them in one row is what makes
// the checker and the lowerer structurally unable to disagree about a method's
// arity, argument types, or result — before this table they were two switches
// in two files, dispatching on different axes (receiver type first in the
// checker, method name first in the lowerer) over the same set of names.
type builtinMethod struct {
	name string

	// check reports the call's result type, or types.Invalid after reporting a
	// diagnostic. args holds the already-checked positional argument types.
	check func(c *checker, recv types.Type, call *ast.CallExpr, args []types.Type) types.Type

	// emit lowers the call. The receiver and every argument are already on the
	// stack, and the call's checked result type is c.types[call]. A method whose
	// source result is None still leaves one value, as every expression does.
	emit func(c *lowerer, recv types.Type, call *ast.CallExpr)
}

// builtinMethods returns the methods of a builtin receiver type, or nil when the
// receiver has no method table. A receiver kind is added to this dispatch as its
// methods move out of the checker and lowerer switches.
func builtinMethods(receiver types.Type) map[string]builtinMethod {
	switch receiver.(type) {
	case *types.List:
		return listMethods
	default:
		return nil
	}
}

// lookupBuiltinMethod finds one method of a builtin receiver.
func lookupBuiltinMethod(receiver types.Type, name string) (builtinMethod, bool) {
	method, ok := builtinMethods(receiver)[name]
	return method, ok
}

// listMethods is the `list` method catalogue, in the order the checker used to
// declare its cases.
var listMethods = methodTable(
	builtinMethod{
		name: "append",
		check: func(c *checker, recv types.Type, call *ast.CallExpr, args []types.Type) types.Type {
			list := recv.(*types.List)
			if !c.methodArity(call, "list.append", args, 1, 1) {
				return types.Invalid
			}
			if !types.AssignableTo(args[0], list.Elem) {
				c.errs.Add(call.Args[0].Pos(), token.TypeMismatch, "list.append expects %s, got %s", list.Elem, args[0])
				return types.Invalid
			}
			return types.None
		},
		emit: func(c *lowerer, recv types.Type, call *ast.CallExpr) {
			c.emit(instr.I32_CONST, 1)
			c.emit(instr.ARRAY_APPEND)
			c.emit(instr.DROP)
			c.emit(instr.REF_NULL)
		},
	},
	builtinMethod{
		name: "pop",
		check: func(c *checker, recv types.Type, call *ast.CallExpr, args []types.Type) types.Type {
			list := recv.(*types.List)
			if !c.methodArity(call, "list.pop", args, 0, 1) {
				return types.Invalid
			}
			if len(args) == 1 && !types.Equal(args[0], types.Int) {
				c.errs.Add(call.Args[0].Pos(), token.TypeMismatch, "list.pop index must be int, got %s", args[0])
				return types.Invalid
			}
			return list.Elem
		},
		emit: func(c *lowerer, recv types.Type, call *ast.CallExpr) {
			if len(call.Args) == 0 {
				c.emit(instr.I64_CONST, ^uint64(0))
			}
			c.emitArrayDelete()
		},
	},
	builtinMethod{
		name: "index",
		check: func(c *checker, recv types.Type, call *ast.CallExpr, args []types.Type) types.Type {
			list := recv.(*types.List)
			if !c.methodArity(call, "list.index", args, 1, 1) {
				return types.Invalid
			}
			if !types.AssignableTo(args[0], list.Elem) {
				c.errs.Add(call.Args[0].Pos(), token.TypeMismatch, "list.index expects %s, got %s", list.Elem, args[0])
				return types.Invalid
			}
			return types.Int
		},
		emit: func(c *lowerer, recv types.Type, call *ast.CallExpr) {
			c.callHost(c.listIndex(recv))
		},
	},
	builtinMethod{
		name: "insert",
		check: func(c *checker, recv types.Type, call *ast.CallExpr, args []types.Type) types.Type {
			list := recv.(*types.List)
			if !c.methodArity(call, "list.insert", args, 2, 2) {
				return types.Invalid
			}
			if !types.Equal(args[0], types.Int) {
				c.errs.Add(call.Args[0].Pos(), token.TypeMismatch, "list.insert index must be int, got %s", args[0])
				return types.Invalid
			}
			if !types.AssignableTo(args[1], list.Elem) {
				c.errs.Add(call.Args[1].Pos(), token.TypeMismatch, "list.insert value must be %s, got %s", list.Elem, args[1])
				return types.Invalid
			}
			return types.None
		},
		emit: func(c *lowerer, recv types.Type, call *ast.CallExpr) {
			c.emitListInsert()
			c.emit(instr.REF_NULL)
		},
	},
	builtinMethod{
		name: "extend",
		check: func(c *checker, recv types.Type, call *ast.CallExpr, args []types.Type) types.Type {
			list := recv.(*types.List)
			if !c.methodArity(call, "list.extend", args, 1, 1) {
				return types.Invalid
			}
			if !types.Equal(args[0], recv) {
				c.errs.Add(call.Args[0].Pos(), token.TypeMismatch, "list.extend expects list[%s], got %s", list.Elem, args[0])
				return types.Invalid
			}
			return types.None
		},
		emit: func(c *lowerer, recv types.Type, call *ast.CallExpr) {
			c.emitListExtend()
			c.emit(instr.REF_NULL)
		},
	},
	builtinMethod{
		name: "reverse",
		check: func(c *checker, recv types.Type, call *ast.CallExpr, args []types.Type) types.Type {
			if !c.methodArity(call, "list.reverse", args, 0, 0) {
				return types.Invalid
			}
			return types.None
		},
		emit: func(c *lowerer, recv types.Type, call *ast.CallExpr) {
			c.emitListReverse()
			c.emit(instr.REF_NULL)
		},
	},
	builtinMethod{
		name: "sort",
		check: func(c *checker, recv types.Type, call *ast.CallExpr, args []types.Type) types.Type {
			list := recv.(*types.List)
			if !c.methodArity(call, "list.sort", args, 0, 0) {
				return types.Invalid
			}
			if !types.Equal(list.Elem, types.Int) && !types.Equal(list.Elem, types.Float) &&
				!types.Equal(list.Elem, types.Str) && !types.Equal(list.Elem, types.Bool) {
				c.errs.Add(call.Pos(), token.TypeMismatch,
					"list.sort requires comparable elements (int, float, str, bool), got %s", list.Elem)
				return types.Invalid
			}
			return types.None
		},
		emit: func(c *lowerer, recv types.Type, call *ast.CallExpr) {
			c.callHost(c.listSort(recv))
			c.emit(instr.REF_NULL)
		},
	},
	builtinMethod{
		name: "copy",
		check: func(c *checker, recv types.Type, call *ast.CallExpr, args []types.Type) types.Type {
			if !c.methodArity(call, "list.copy", args, 0, 0) {
				return types.Invalid
			}
			return recv
		},
		emit: func(c *lowerer, recv types.Type, call *ast.CallExpr) {
			c.callHost(c.listCopy(recv))
		},
	},
	builtinMethod{
		name: "count",
		check: func(c *checker, recv types.Type, call *ast.CallExpr, args []types.Type) types.Type {
			list := recv.(*types.List)
			if !c.methodArity(call, "list.count", args, 1, 1) {
				return types.Invalid
			}
			if !types.AssignableTo(args[0], list.Elem) {
				c.errs.Add(call.Args[0].Pos(), token.TypeMismatch, "list.count expects %s, got %s", list.Elem, args[0])
				return types.Invalid
			}
			return types.Int
		},
		emit: func(c *lowerer, recv types.Type, call *ast.CallExpr) {
			c.callHost(c.listCount(recv))
		},
	},
	builtinMethod{
		name: "clear",
		check: func(c *checker, recv types.Type, call *ast.CallExpr, args []types.Type) types.Type {
			if !c.methodArity(call, "list.clear", args, 0, 0) {
				return types.Invalid
			}
			return types.None
		},
		emit: func(c *lowerer, recv types.Type, call *ast.CallExpr) {
			c.callHost(c.listClear(recv))
			c.emit(instr.REF_NULL)
		},
	},
	builtinMethod{
		name: "remove",
		check: func(c *checker, recv types.Type, call *ast.CallExpr, args []types.Type) types.Type {
			list := recv.(*types.List)
			if !c.methodArity(call, "list.remove", args, 1, 1) {
				return types.Invalid
			}
			if !types.AssignableTo(args[0], list.Elem) {
				c.errs.Add(call.Args[0].Pos(), token.TypeMismatch, "list.remove expects %s, got %s", list.Elem, args[0])
				return types.Invalid
			}
			return types.None
		},
		emit: func(c *lowerer, recv types.Type, call *ast.CallExpr) {
			c.callHost(c.listRemove(recv))
			c.emit(instr.REF_NULL)
		},
	},
)

// methodTable indexes a catalogue by method name. A duplicate name is an invalid
// catalogue, not a user error, so it panics at startup rather than letting one
// entry silently shadow another.
func methodTable(methods ...builtinMethod) map[string]builtinMethod {
	table := make(map[string]builtinMethod, len(methods))
	for _, method := range methods {
		if _, duplicate := table[method.name]; duplicate {
			panic("compiler: duplicate builtin method " + method.name)
		}
		table[method.name] = method
	}
	return table
}

// methodArity reports whether a builtin method call has an admissible number of
// positional arguments, adding the diagnostic when it does not. name is the
// method's qualified source name, as it appears in the message.
func (c *checker) methodArity(call *ast.CallExpr, name string, args []types.Type, min, max int) bool {
	if len(args) >= min && len(args) <= max {
		return true
	}
	switch {
	case min == max && min == 0:
		c.errs.Add(call.Pos(), token.ArityMismatch, "%s takes no arguments (%d given)", name, len(args))
	case min == max && min == 1:
		c.errs.Add(call.Pos(), token.ArityMismatch, "%s takes exactly 1 argument (%d given)", name, len(args))
	case min == max:
		c.errs.Add(call.Pos(), token.ArityMismatch, "%s takes exactly %d arguments (%d given)", name, min, len(args))
	case min == 0 && max == 1:
		c.errs.Add(call.Pos(), token.ArityMismatch, "%s takes at most 1 argument (%d given)", name, len(args))
	default:
		c.errs.Add(call.Pos(), token.ArityMismatch, "%s expects %d or %d arguments (%d given)", name, min, max, len(args))
	}
	return false
}
