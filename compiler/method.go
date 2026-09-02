package compiler

import (
	"github.com/siyul-park/minipy/ast"
	"github.com/siyul-park/minipy/token"
	"github.com/siyul-park/minipy/types"

	"github.com/siyul-park/minivm/instr"
	vmtypes "github.com/siyul-park/minivm/types"
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
	case *types.Dict:
		return dictMethods
	case *types.Set:
		return setMethods
	}
	if types.Equal(receiver, types.Str) {
		return strMethods
	}
	return nil
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
				// The omitted index is the last element; -1 is a known
				// constant, so it normalizes without a runtime test.
				c.emit(instr.I64_CONST, ^uint64(0))
				c.emitArrayDelete(-1, true)
				return
			}
			c.emitArrayDelete(constIndex(call.Args[0]))
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

// dictMethods is the `dict` method catalogue, in the order the checker used to
// declare its cases.
var dictMethods = methodTable(
	builtinMethod{
		name: "get",
		check: func(c *checker, recv types.Type, call *ast.CallExpr, args []types.Type) types.Type {
			dict := recv.(*types.Dict)
			if len(args) < 1 || len(args) > 2 || !types.AssignableTo(args[0], dict.Key) ||
				(len(args) == 2 && !types.AssignableTo(args[1], dict.Value)) {
				c.errs.Add(call.Pos(), token.TypeMismatch, "dict.get expects key and optional default")
				return types.Invalid
			}
			if len(args) == 1 {
				// No default: a missing key returns None, matching CPython.
				// The two-argument form still returns V, since its default is
				// always assignable to V and there is no None case.
				return types.NewUnion(dict.Value, types.None)
			}
			return dict.Value
		},
		emit: func(c *lowerer, recv types.Type, call *ast.CallExpr) {
			if len(call.Args) == 1 {
				c.emitZeroValue(c.types[call])
			}
			c.callHost(c.dictGet(recv, c.types[call]))
		},
	},
	builtinMethod{
		name: "keys",
		check: func(c *checker, recv types.Type, call *ast.CallExpr, args []types.Type) types.Type {
			if len(args) != 0 {
				return c.unsupportedMethod(call, recv, "keys")
			}
			return types.NewList(recv.(*types.Dict).Key)
		},
		emit: func(c *lowerer, recv types.Type, call *ast.CallExpr) {
			c.emit(instr.MAP_KEYS)
		},
	},
	builtinMethod{
		name: "values",
		check: func(c *checker, recv types.Type, call *ast.CallExpr, args []types.Type) types.Type {
			if len(args) != 0 {
				return c.unsupportedMethod(call, recv, "values")
			}
			return types.NewList(recv.(*types.Dict).Value)
		},
		emit: func(c *lowerer, recv types.Type, call *ast.CallExpr) {
			c.callHost(c.dictValues(recv, c.types[call]))
		},
	},
	builtinMethod{
		name: "items",
		check: func(c *checker, recv types.Type, call *ast.CallExpr, args []types.Type) types.Type {
			if len(args) != 0 {
				return c.unsupportedMethod(call, recv, "items")
			}
			dict := recv.(*types.Dict)
			return types.NewList(types.NewTuple(dict.Key, dict.Value))
		},
		emit: func(c *lowerer, recv types.Type, call *ast.CallExpr) {
			c.callHost(c.dictItems(recv, c.types[call]))
		},
	},
	builtinMethod{
		name: "pop",
		check: func(c *checker, recv types.Type, call *ast.CallExpr, args []types.Type) types.Type {
			dict := recv.(*types.Dict)
			if !c.methodArity(call, "dict.pop", args, 1, 2) {
				return types.Invalid
			}
			if !types.AssignableTo(args[0], dict.Key) {
				c.errs.Add(call.Pos(), token.TypeMismatch, "dict.pop key argument must be assignable to %s", dict.Key)
				return types.Invalid
			}
			if len(args) == 2 && !types.AssignableTo(args[1], dict.Value) {
				c.errs.Add(call.Pos(), token.TypeMismatch, "dict.pop default argument must be assignable to %s", dict.Value)
				return types.Invalid
			}
			return dict.Value
		},
		emit: func(c *lowerer, recv types.Type, call *ast.CallExpr) {
			if len(call.Args) == 2 {
				c.callHost(c.dictPopDefault(recv, c.types[call]))
				return
			}
			c.callHost(c.dictPop(recv, c.types[call]))
		},
	},
	builtinMethod{
		name: "update",
		check: func(c *checker, recv types.Type, call *ast.CallExpr, args []types.Type) types.Type {
			if len(args) != 1 || !types.Equal(args[0], recv) {
				c.errs.Add(call.Pos(), token.TypeMismatch, "dict.update expects a dict of the same type")
				return types.Invalid
			}
			return types.None
		},
		emit: func(c *lowerer, recv types.Type, call *ast.CallExpr) {
			c.callHost(c.dictUpdate(recv))
			c.emit(instr.REF_NULL)
		},
	},
	builtinMethod{
		name: "setdefault",
		check: func(c *checker, recv types.Type, call *ast.CallExpr, args []types.Type) types.Type {
			dict := recv.(*types.Dict)
			if len(args) != 2 || !types.AssignableTo(args[0], dict.Key) || !types.AssignableTo(args[1], dict.Value) {
				c.errs.Add(call.Pos(), token.TypeMismatch, "dict.setdefault expects (key, default)")
				return types.Invalid
			}
			return dict.Value
		},
		emit: func(c *lowerer, recv types.Type, call *ast.CallExpr) {
			c.callHost(c.dictSetDefault(recv, c.types[call]))
		},
	},
	builtinMethod{
		name: "clear",
		check: func(c *checker, recv types.Type, call *ast.CallExpr, args []types.Type) types.Type {
			if !c.methodArity(call, "dict.clear", args, 0, 0) {
				return types.Invalid
			}
			return types.None
		},
		emit: func(c *lowerer, recv types.Type, call *ast.CallExpr) {
			c.callHost(c.dictClear(recv))
			c.emit(instr.REF_NULL)
		},
	},
	builtinMethod{
		name: "copy",
		check: func(c *checker, recv types.Type, call *ast.CallExpr, args []types.Type) types.Type {
			if !c.methodArity(call, "dict.copy", args, 0, 0) {
				return types.Invalid
			}
			return recv
		},
		emit: func(c *lowerer, recv types.Type, call *ast.CallExpr) {
			c.callHost(c.dictCopy(recv))
		},
	},
)

// setMethods is the `set` method catalogue, in the order the checker used to
// declare its cases. Every entry but pop, clear and copy takes one argument, so
// their checks are built by elementMethod and setOperation.
var setMethods = methodTable(
	elementMethod("add", func(types.Type) types.Type { return types.None },
		func(c *lowerer, recv types.Type, call *ast.CallExpr) {
			c.emit(instr.I32_CONST, 1)
			c.emit(instr.MAP_SET)
			c.emit(instr.REF_NULL)
		}),
	elementMethod("remove", func(types.Type) types.Type { return types.None },
		func(c *lowerer, recv types.Type, call *ast.CallExpr) {
			c.callHost(c.setRemove(recv))
			c.emit(instr.REF_NULL)
		}),
	elementMethod("discard", func(types.Type) types.Type { return types.None },
		func(c *lowerer, recv types.Type, call *ast.CallExpr) {
			c.callHost(c.setDiscard(recv))
			c.emit(instr.REF_NULL)
		}),
	builtinMethod{
		name: "pop",
		check: func(c *checker, recv types.Type, call *ast.CallExpr, args []types.Type) types.Type {
			if !c.methodArity(call, "set.pop", args, 0, 0) {
				return types.Invalid
			}
			return recv.(*types.Set).Elem
		},
		emit: func(c *lowerer, recv types.Type, call *ast.CallExpr) {
			c.callHost(c.setPop(recv, c.types[call]))
		},
	},
	builtinMethod{
		name: "clear",
		check: func(c *checker, recv types.Type, call *ast.CallExpr, args []types.Type) types.Type {
			if !c.methodArity(call, "set.clear", args, 0, 0) {
				return types.Invalid
			}
			return types.None
		},
		emit: func(c *lowerer, recv types.Type, call *ast.CallExpr) {
			// A set is a map from element to bool, so it clears through the
			// same host function a dict does.
			c.callHost(c.dictClear(recv))
			c.emit(instr.REF_NULL)
		},
	},
	setOperation("union", func(recv types.Type) types.Type { return recv },
		func(c *lowerer, recv types.Type, call *ast.CallExpr) { c.callHost(c.setUnion(recv)) }),
	setOperation("intersection", func(recv types.Type) types.Type { return recv },
		func(c *lowerer, recv types.Type, call *ast.CallExpr) { c.callHost(c.setIntersection(recv)) }),
	setOperation("difference", func(recv types.Type) types.Type { return recv },
		func(c *lowerer, recv types.Type, call *ast.CallExpr) { c.callHost(c.setDifference(recv)) }),
	setOperation("issubset", func(types.Type) types.Type { return types.Bool },
		func(c *lowerer, recv types.Type, call *ast.CallExpr) { c.callHost(c.setIsSubset(recv)) }),
	setOperation("issuperset", func(types.Type) types.Type { return types.Bool },
		func(c *lowerer, recv types.Type, call *ast.CallExpr) { c.callHost(c.setIsSuperset(recv)) }),
	builtinMethod{
		name: "copy",
		check: func(c *checker, recv types.Type, call *ast.CallExpr, args []types.Type) types.Type {
			if !c.methodArity(call, "set.copy", args, 0, 0) {
				return types.Invalid
			}
			return recv
		},
		emit: func(c *lowerer, recv types.Type, call *ast.CallExpr) {
			c.callHost(c.setCopy(recv))
		},
	},
)

// elementMethod builds a set method taking exactly one element of the receiver.
func elementMethod(name string, result func(recv types.Type) types.Type, emit func(*lowerer, types.Type, *ast.CallExpr)) builtinMethod {
	return builtinMethod{
		name: name,
		check: func(c *checker, recv types.Type, call *ast.CallExpr, args []types.Type) types.Type {
			elem := recv.(*types.Set).Elem
			if !c.methodArity(call, "set."+name, args, 1, 1) {
				return types.Invalid
			}
			if !types.AssignableTo(args[0], elem) {
				c.errs.Add(call.Args[0].Pos(), token.TypeMismatch, "set.%s expects %s, got %s", name, elem, args[0])
				return types.Invalid
			}
			return result(recv)
		},
		emit: emit,
	}
}

// setOperation builds a set method taking exactly one set of the receiver's own
// type, which is what makes the element types line up without a second check.
func setOperation(name string, result func(recv types.Type) types.Type, emit func(*lowerer, types.Type, *ast.CallExpr)) builtinMethod {
	return builtinMethod{
		name: name,
		check: func(c *checker, recv types.Type, call *ast.CallExpr, args []types.Type) types.Type {
			if !c.methodArity(call, "set."+name, args, 1, 1) {
				return types.Invalid
			}
			if !types.Equal(args[0], recv) {
				c.errs.Add(call.Args[0].Pos(), token.TypeMismatch, "set.%s expects %s, got %s", name, recv, args[0])
				return types.Invalid
			}
			return result(recv)
		},
		emit: emit,
	}
}

// strMethods is the `str` method catalogue, in the order the checker used to
// declare its cases.
//
// Every entry reports its result for an admitted argument list and nothing for
// anything else, which lands on the shared unsupported-method diagnostic. That
// is the only diagnostic str methods have ever produced: unlike the container
// methods, they never reported a specific arity or argument-type error.
var strMethods = methodTable(
	strMethod("upper", noArgs(types.Str),
		func(c *lowerer, recv types.Type, call *ast.CallExpr) { c.callHost(c.strUpper()) }),
	strMethod("lower", noArgs(types.Str),
		func(c *lowerer, recv types.Type, call *ast.CallExpr) { c.callHost(c.strLower()) }),
	strMethod("split", func(args []types.Type) (types.Type, bool) {
		if len(args) <= 1 && (len(args) == 0 || types.Equal(args[0], types.Str)) {
			return types.NewList(types.Str), true
		}
		return nil, false
	}, func(c *lowerer, recv types.Type, call *ast.CallExpr) {
		if len(call.Args) == 0 {
			c.callHost(c.strSplitWhitespace())
			return
		}
		c.callHost(c.strSplit())
	}),
	strMethod("join", func(args []types.Type) (types.Type, bool) {
		if len(args) == 1 {
			if list, ok := args[0].(*types.List); ok && types.Equal(list.Elem, types.Str) {
				return types.Str, true
			}
		}
		return nil, false
	}, func(c *lowerer, recv types.Type, call *ast.CallExpr) { c.callHost(c.strJoin()) }),
	strMethod("find", oneStrArg(types.Int),
		func(c *lowerer, recv types.Type, call *ast.CallExpr) { c.callHost(c.strFind()) }),
	strMethod("strip", optionalStrArg(types.Str),
		func(c *lowerer, recv types.Type, call *ast.CallExpr) {
			if len(call.Args) == 0 {
				c.callHost(c.strStripNoArg())
				return
			}
			c.callHost(c.strStripChars())
		}),
	strMethod("lstrip", optionalStrArg(types.Str),
		func(c *lowerer, recv types.Type, call *ast.CallExpr) {
			if len(call.Args) == 0 {
				c.callHost(c.strLStripNoArg())
				return
			}
			c.callHost(c.strLStripChars())
		}),
	strMethod("rstrip", optionalStrArg(types.Str),
		func(c *lowerer, recv types.Type, call *ast.CallExpr) {
			if len(call.Args) == 0 {
				c.callHost(c.strRStripNoArg())
				return
			}
			c.callHost(c.strRStripChars())
		}),
	strMethod("startswith", oneStrArg(types.Bool),
		func(c *lowerer, recv types.Type, call *ast.CallExpr) { c.callHost(c.strStartsWith()) }),
	strMethod("endswith", oneStrArg(types.Bool),
		func(c *lowerer, recv types.Type, call *ast.CallExpr) { c.callHost(c.strEndsWith()) }),
	strMethod("replace", func(args []types.Type) (types.Type, bool) {
		if (len(args) == 2 || len(args) == 3) &&
			types.Equal(args[0], types.Str) && types.Equal(args[1], types.Str) &&
			(len(args) == 2 || types.Equal(args[2], types.Int)) {
			return types.Str, true
		}
		return nil, false
	}, func(c *lowerer, recv types.Type, call *ast.CallExpr) {
		if len(call.Args) == 2 {
			// The omitted count means "every occurrence"; -1 is how the host
			// function spells that.
			c.emit(instr.I64_CONST, ^uint64(0))
		}
		c.callHost(c.strReplace())
	}),
	strMethod("count", oneStrArg(types.Int),
		func(c *lowerer, recv types.Type, call *ast.CallExpr) { c.callHost(c.strCount()) }),
	strMethod("isdigit", noArgs(types.Bool),
		func(c *lowerer, recv types.Type, call *ast.CallExpr) { c.callHost(c.strIsDigit()) }),
	strMethod("isalpha", noArgs(types.Bool),
		func(c *lowerer, recv types.Type, call *ast.CallExpr) { c.callHost(c.strIsAlpha()) }),
	strMethod("isalnum", noArgs(types.Bool),
		func(c *lowerer, recv types.Type, call *ast.CallExpr) { c.callHost(c.strIsAlnum()) }),
	strMethod("isspace", noArgs(types.Bool),
		func(c *lowerer, recv types.Type, call *ast.CallExpr) { c.callHost(c.strIsSpace()) }),
	strMethod("capitalize", noArgs(types.Str),
		func(c *lowerer, recv types.Type, call *ast.CallExpr) { c.callHost(c.strCapitalize()) }),
	strMethod("title", noArgs(types.Str),
		func(c *lowerer, recv types.Type, call *ast.CallExpr) { c.callHost(c.strTitle()) }),
	strMethod("swapcase", noArgs(types.Str),
		func(c *lowerer, recv types.Type, call *ast.CallExpr) { c.callHost(c.strSwapCase()) }),
	strMethod("center", widthAndFill,
		func(c *lowerer, recv types.Type, call *ast.CallExpr) {
			emitDefaultFill(c, call)
			c.callHost(c.strCenter())
		}),
	strMethod("ljust", widthAndFill,
		func(c *lowerer, recv types.Type, call *ast.CallExpr) {
			emitDefaultFill(c, call)
			c.callHost(c.strLJust())
		}),
	strMethod("rjust", widthAndFill,
		func(c *lowerer, recv types.Type, call *ast.CallExpr) {
			emitDefaultFill(c, call)
			c.callHost(c.strRJust())
		}),
	strMethod("zfill", func(args []types.Type) (types.Type, bool) {
		if len(args) == 1 && types.Equal(args[0], types.Int) {
			return types.Str, true
		}
		return nil, false
	}, func(c *lowerer, recv types.Type, call *ast.CallExpr) { c.callHost(c.strZFill()) }),
	strMethod("encode", noArgs(types.Bytes),
		func(c *lowerer, recv types.Type, call *ast.CallExpr) { c.callHost(c.strEncode()) }),
	builtinMethod{
		name: "format",
		check: func(c *checker, recv types.Type, call *ast.CallExpr, args []types.Type) types.Type {
			for _, arg := range args {
				if arg != types.Invalid && !types.Printable(arg) {
					c.errs.Add(call.Pos(), token.TypeMismatch, "str.format() argument must be printable")
					return types.Invalid
				}
			}
			return types.Str
		},
		emit: func(c *lowerer, recv types.Type, call *ast.CallExpr) {
			c.emitStrFormat(call)
		},
	},
)

// strMethod builds a str method whose admissible argument lists are decided by
// shape. shape reports the result type for an admitted list, and false for
// anything else.
func strMethod(name string, shape func(args []types.Type) (types.Type, bool), emit func(*lowerer, types.Type, *ast.CallExpr)) builtinMethod {
	return builtinMethod{
		name: name,
		check: func(c *checker, recv types.Type, call *ast.CallExpr, args []types.Type) types.Type {
			result, ok := shape(args)
			if !ok {
				return c.unsupportedMethod(call, recv, name)
			}
			return result
		},
		emit: emit,
	}
}

// noArgs admits an empty argument list.
func noArgs(result types.Type) func([]types.Type) (types.Type, bool) {
	return func(args []types.Type) (types.Type, bool) { return result, len(args) == 0 }
}

// oneStrArg admits exactly one str argument.
func oneStrArg(result types.Type) func([]types.Type) (types.Type, bool) {
	return func(args []types.Type) (types.Type, bool) {
		return result, len(args) == 1 && types.Equal(args[0], types.Str)
	}
}

// optionalStrArg admits no argument or one str argument.
func optionalStrArg(result types.Type) func([]types.Type) (types.Type, bool) {
	return func(args []types.Type) (types.Type, bool) {
		return result, len(args) == 0 || (len(args) == 1 && types.Equal(args[0], types.Str))
	}
}

// widthAndFill admits a width with an optional single-character fill, the shape
// center, ljust, and rjust share.
func widthAndFill(args []types.Type) (types.Type, bool) {
	if len(args) == 1 && types.Equal(args[0], types.Int) {
		return types.Str, true
	}
	if len(args) == 2 && types.Equal(args[0], types.Int) && types.Equal(args[1], types.Str) {
		return types.Str, true
	}
	return nil, false
}

// emitDefaultFill pushes the space the padding methods use when the fill
// character is omitted, so the host function always receives both arguments.
func emitDefaultFill(c *lowerer, call *ast.CallExpr) {
	if len(call.Args) == 1 {
		c.constGet(vmtypes.String(" "))
	}
}

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

// unsupportedMethod reports a method call the receiver's catalogue admits by
// name but not in this shape, with the same diagnostic an unknown name gets.
func (c *checker) unsupportedMethod(call *ast.CallExpr, receiver types.Type, name string) types.Type {
	c.errs.Add(call.Pos(), token.UnsupportedFeature, "method %s on %s is not supported", name, receiver)
	return types.Invalid
}
