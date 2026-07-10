package builtins

import (
	"github.com/siyul-park/minipy/ast"
	"github.com/siyul-park/minipy/module"
	"github.com/siyul-park/minipy/token"
	"github.com/siyul-park/minipy/types"

	"github.com/siyul-park/minivm/instr"
)

const classIDField = "__classid"

func getAttrCheck(c module.Checker, args []ast.Expr, pos token.Pos) types.Type {
	cls, name, ok := checkAttribute(c, "getattr", args, pos)
	if !ok {
		return types.Invalid
	}
	if name == classIDField {
		c.Error(args[1].Pos(), token.UnsupportedFeature, "attribute %q is internal", name)
		return types.Invalid
	}
	_, field, ok := classField(cls, name)
	if !ok {
		c.Error(args[1].Pos(), token.UndefinedName, "field %q is not defined on %s", name, cls.Name)
		return types.Invalid
	}
	return field.Type
}

func hasAttrCheck(c module.Checker, args []ast.Expr, pos token.Pos) types.Type {
	_, name, ok := checkAttribute(c, "hasattr", args, pos)
	if !ok {
		return types.Invalid
	}
	if name == classIDField {
		c.Error(args[1].Pos(), token.UnsupportedFeature, "attribute %q is internal", name)
		return types.Invalid
	}
	return types.Bool
}

func checkAttribute(c module.Checker, name string, args []ast.Expr, pos token.Pos) (*types.Class, string, bool) {
	argTypes := make([]types.Type, len(args))
	for i, arg := range args {
		argTypes[i] = c.Check(arg)
	}
	if len(args) != 2 {
		c.Error(pos, token.ArityMismatch, "%s() takes exactly 2 argument(s) (%d given)", name, len(args))
		return nil, "", false
	}
	cls, ok := argTypes[0].(*types.Class)
	if !ok {
		c.Error(args[0].Pos(), token.UnsupportedFeature, "%s() requires a statically known class instance, got %s", name, argTypes[0])
		return nil, "", false
	}
	literal, ok := args[1].(*ast.StrLit)
	if !ok {
		c.Error(args[1].Pos(), token.UnsupportedFeature, "%s() attribute name must be a string literal", name)
		return nil, "", false
	}
	return cls, literal.Value, true
}

func emitGetAttr(e module.Emitter, args []ast.Expr) {
	cls := e.Type(args[0]).(*types.Class)
	name := args[1].(*ast.StrLit).Value
	index, _, _ := classField(cls, name)
	e.Expr(args[0])
	e.Emit(instr.I32_CONST, uint64(index))
	e.Emit(instr.STRUCT_GET)
}

func emitHasAttr(e module.Emitter, args []ast.Expr) {
	cls := e.Type(args[0]).(*types.Class)
	name := args[1].(*ast.StrLit).Value
	_, _, found := classField(cls, name)
	e.Expr(args[0])
	e.Emit(instr.DROP)
	if found {
		e.Emit(instr.I32_CONST, 1)
	} else {
		e.Emit(instr.I32_CONST, 0)
	}
	e.Emit(instr.I32_CONST, 0)
	e.Emit(instr.I32_NE)
}

func classField(cls *types.Class, name string) (int, types.Field, bool) {
	for i, field := range cls.Fields {
		if field.Name == name {
			return i, field, true
		}
	}
	return 0, types.Field{}, false
}
