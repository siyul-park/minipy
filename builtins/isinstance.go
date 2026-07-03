package builtins

import (
	"github.com/siyul-park/minipy/ast"
	"github.com/siyul-park/minipy/module"
	"github.com/siyul-park/minipy/token"
	"github.com/siyul-park/minipy/types"

	"github.com/siyul-park/minivm/instr"
)

func isInstanceCheck(c module.Checker, args []ast.Expr, pos token.Pos) types.Type {
	if len(args) != 2 {
		c.Error(pos, token.ArityMismatch, "isinstance() takes exactly 2 arguments (%d given)", len(args))
		for _, a := range args {
			c.Check(a)
		}
		return types.Bool
	}
	c.Check(args[0])
	t := c.ResolveType(args[1])
	c.SetType(args[1], t)
	return types.Bool
}

func emitIsInstance(e module.Emitter, args []ast.Expr) {
	e.Expr(args[0])
	e.Emit(instr.REF_TEST, e.TypeIndex(e.Type(args[1])))
	e.Emit(instr.I32_CONST, 0)
	e.Emit(instr.I32_NE)
}
