package ast

import (
	"testing"

	"github.com/siyul-park/minipy/token"
	"github.com/stretchr/testify/require"
)

func TestNodePos(t *testing.T) {
	pos := token.Pos{Line: 4, Column: 9}

	exprs := []Expr{
		&Name{Base: Base{Position: pos}, Name: "x"},
		&IntLit{Base: Base{Position: pos}},
		&FloatLit{Base: Base{Position: pos}},
		&StrLit{Base: Base{Position: pos}},
		&BoolLit{Base: Base{Position: pos}},
		&NoneLit{Base: Base{Position: pos}},
		&UnaryExpr{Base: Base{Position: pos}},
		&BinaryExpr{Base: Base{Position: pos}},
		&BoolOp{Base: Base{Position: pos}},
		&Compare{Base: Base{Position: pos}},
		&CallExpr{Base: Base{Position: pos}},
		&IfExp{Base: Base{Position: pos}},
	}
	for _, e := range exprs {
		require.Equal(t, pos, e.Pos())
	}

	stmts := []Stmt{
		&AnnAssign{Base: Base{Position: pos}},
		&Assign{Base: Base{Position: pos}},
		&AugAssign{Base: Base{Position: pos}},
		&ExprStmt{Base: Base{Position: pos}},
		&If{Base: Base{Position: pos}},
		&While{Base: Base{Position: pos}},
		&For{Base: Base{Position: pos}},
		&Function{Base: Base{Position: pos}},
		&Return{Base: Base{Position: pos}},
		&Break{Base: Base{Position: pos}},
		&Continue{Base: Base{Position: pos}},
		&Pass{Base: Base{Position: pos}},
	}
	for _, s := range stmts {
		require.Equal(t, pos, s.Pos())
	}

	mod := &Module{Base: Base{Position: pos}}
	require.Equal(t, pos, mod.Pos())
}
