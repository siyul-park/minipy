package builtins

import "github.com/siyul-park/minipy/types"

// resultFunc validates a builtin's argument types and returns its result type.
type resultFunc func([]types.Type) (types.Type, bool)

func printable(result types.Type) resultFunc {
	return func(args []types.Type) (types.Type, bool) {
		if len(args) == 1 && types.Printable(args[0]) {
			return result, true
		}
		return types.Invalid, false
	}
}

func convert(result types.Type) resultFunc {
	return func(args []types.Type) (types.Type, bool) {
		if len(args) == 1 && convertible(args[0]) {
			return result, true
		}
		return types.Invalid, false
	}
}

func boolResult(args []types.Type) (types.Type, bool) {
	if len(args) == 1 && (convertible(args[0]) || isContainer(args[0])) {
		return types.Bool, true
	}
	return types.Invalid, false
}

func absResult(args []types.Type) (types.Type, bool) {
	if len(args) == 1 && (types.Equal(args[0], types.Int) || types.Equal(args[0], types.Float)) {
		return args[0], true
	}
	return types.Invalid, false
}

func lenResult(args []types.Type) (types.Type, bool) {
	if len(args) != 1 {
		return types.Invalid, false
	}
	switch args[0].(type) {
	case *types.List, *types.Dict, *types.Set, *types.Tuple:
		return types.Int, true
	default:
		if types.Equal(args[0], types.Str) {
			return types.Int, true
		}
	}
	return types.Invalid, false
}

func enumerateResult(args []types.Type) (types.Type, bool) {
	if len(args) == 1 {
		if list, ok := args[0].(*types.List); ok {
			return types.NewList(types.NewTuple(types.Int, list.Elem)), true
		}
	}
	return types.Invalid, false
}

func zipResult(args []types.Type) (types.Type, bool) {
	if len(args) == 2 {
		a, aok := args[0].(*types.List)
		b, bok := args[1].(*types.List)
		if aok && bok {
			return types.NewList(types.NewTuple(a.Elem, b.Elem)), true
		}
	}
	return types.Invalid, false
}

func rangeResult(args []types.Type) (types.Type, bool) {
	for _, arg := range args {
		if !types.Equal(arg, types.Int) {
			return types.Invalid, false
		}
	}
	return types.NewIterator(types.Int), true
}

func iterResult(args []types.Type) (types.Type, bool) {
	if len(args) == 1 {
		if elem := iterableElem(args[0]); elem != types.Invalid {
			return types.NewIterator(elem), true
		}
	}
	return types.Invalid, false
}

func nextResult(args []types.Type) (types.Type, bool) {
	if len(args) == 1 {
		if it, ok := args[0].(*types.Iterator); ok {
			return it.Elem, true
		}
	}
	return types.Invalid, false
}

func ordResult(args []types.Type) (types.Type, bool) {
	if len(args) == 1 && types.Equal(args[0], types.Str) {
		return types.Int, true
	}
	return types.Invalid, false
}

func chrResult(args []types.Type) (types.Type, bool) {
	if len(args) == 1 && types.Equal(args[0], types.Int) {
		return types.Str, true
	}
	return types.Invalid, false
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

func iterableElem(t types.Type) types.Type {
	switch x := t.(type) {
	case *types.List:
		return x.Elem
	case *types.Dict:
		return x.Key
	case *types.Set:
		return x.Elem
	case *types.Iterator:
		return x.Elem
	default:
		if types.Equal(t, types.Str) {
			return types.Str
		}
		return types.Invalid
	}
}
