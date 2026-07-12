package compiler

import (
	"github.com/siyul-park/minipy/ast"
	"github.com/siyul-park/minipy/module"
	"github.com/siyul-park/minipy/types"
)

// checkedProgram is the semantic information produced by checking and consumed
// by lowering. The checker owns all mutation while building it; the lowerer
// treats the result as read-only.
type checkedProgram struct {
	entry      *moduleInfo
	reg        *module.Registry
	modules    map[string]*moduleInfo
	types      map[ast.Expr]types.Type
	globals    map[string]*global
	functions  map[string]*function
	classes    map[string]*class
	aliasDecls map[*ast.AnnAssign]bool
	lambdas    map[*ast.LambdaExpr]*function
	genExprs   map[*ast.GeneratorExp]*function
	callSpec   map[*ast.CallExpr]*specialization
	callArgs   map[*ast.CallExpr][]ast.Expr
	attrSym    map[*ast.Attribute]string
	attrMod    map[*ast.Attribute]string
	attrNative map[*ast.Attribute]module.Symbol
	lenDunder  map[*ast.CallExpr]bool
}

func (c *checker) result(entry *moduleInfo) *checkedProgram {
	return &checkedProgram{
		entry:      entry,
		reg:        c.reg,
		modules:    c.modules,
		types:      c.types,
		globals:    c.globals,
		functions:  c.functions,
		classes:    c.classes,
		aliasDecls: c.aliasDecls,
		lambdas:    c.lambdas,
		genExprs:   c.genExprs,
		callSpec:   c.callSpec,
		callArgs:   c.callArgs,
		attrSym:    c.attrSym,
		attrMod:    c.attrMod,
		attrNative: c.attrNative,
		lenDunder:  c.lenDunder,
	}
}
