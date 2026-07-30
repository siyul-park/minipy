package builtins

import (
	"github.com/siyul-park/minipy/ast"
	"github.com/siyul-park/minipy/module"
	"github.com/siyul-park/minipy/token"
	"github.com/siyul-park/minipy/types"
)

func minCheck(c module.Checker, args []ast.Expr, pos token.Pos) types.Type {
	return minMaxCheck(c, "min", args, pos)
}

func maxCheck(c module.Checker, args []ast.Expr, pos token.Pos) types.Type {
	return minMaxCheck(c, "max", args, pos)
}

func minMaxCheck(c module.Checker, name string, args []ast.Expr, pos token.Pos) types.Type {
	argTypes := make([]types.Type, len(args))
	for i, a := range args {
		argTypes[i] = c.Check(a)
	}
	if len(args) == 0 {
		c.Error(pos, token.ArityMismatch, "%s() requires at least 1 argument (0 given)", name)
		return types.Invalid
	}
	if len(args) == 1 {
		list, ok := argTypes[0].(*types.List)
		if !ok {
			c.Error(pos, token.TypeMismatch, "%s() does not accept these arguments", name)
			return types.Invalid
		}
		if !comparable(list.Elem) {
			c.Error(pos, token.TypeMismatch, "%s() does not accept these arguments", name)
			return types.Invalid
		}
		return list.Elem
	}
	first := argTypes[0]
	if !comparable(first) {
		c.Error(pos, token.TypeMismatch, "%s() does not accept these arguments", name)
		return types.Invalid
	}
	for _, t := range argTypes[1:] {
		if !types.Equal(t, first) {
			c.Error(pos, token.TypeMismatch, "%s() does not accept these arguments", name)
			return types.Invalid
		}
	}
	return first
}
