package parser

import (
	"strings"
	"testing"

	"github.com/siyul-park/minipy/ast"
	"github.com/siyul-park/minipy/token"
	"github.com/stretchr/testify/require"
)

func parse(src string) (*ast.Module, error) {
	return Parse(strings.NewReader(src))
}

func hasCode(t *testing.T, err error, code token.Code) {
	t.Helper()
	el, ok := err.(token.ErrorList)
	require.Truef(t, ok, "expected token.ErrorList, got %T", err)
	for _, e := range el {
		if e.Code == code {
			return
		}
	}
	t.Fatalf("expected diagnostic %s, got %v", code, err)
}

func TestParse(t *testing.T) {
	t.Run("annotated declaration", func(t *testing.T) {
		mod, err := parse("x: int = 6\n")
		require.NoError(t, err)
		require.Len(t, mod.Body, 1)

		ann, ok := mod.Body[0].(*ast.AnnAssign)
		require.True(t, ok)
		require.Equal(t, "x", ann.Target.Name)
		require.Equal(t, "int", ann.Ann.(*ast.Name).Name)
		require.Equal(t, int64(6), ann.Value.(*ast.IntLit).Value)
	})

	t.Run("bare annotation without value", func(t *testing.T) {
		mod, err := parse("y: float\n")
		require.NoError(t, err)
		ann := mod.Body[0].(*ast.AnnAssign)
		require.Nil(t, ann.Value)
		require.Equal(t, "float", ann.Ann.(*ast.Name).Name)
	})

	t.Run("plain and augmented assignment", func(t *testing.T) {
		mod, err := parse("x = 1\nx += 2\n")
		require.NoError(t, err)
		require.Len(t, mod.Body, 2)

		_, ok := mod.Body[0].(*ast.Assign)
		require.True(t, ok)
		aug := mod.Body[1].(*ast.AugAssign)
		require.Equal(t, token.PLUS, aug.Op)
	})

	t.Run("call expression statement", func(t *testing.T) {
		mod, err := parse("print(str(x * y))\n")
		require.NoError(t, err)

		es := mod.Body[0].(*ast.ExprStmt)
		outer := es.X.(*ast.CallExpr)
		require.Equal(t, "print", outer.Fn.(*ast.Name).Name)
		require.Len(t, outer.Args, 1)

		inner := outer.Args[0].(*ast.CallExpr)
		require.Equal(t, "str", inner.Fn.(*ast.Name).Name)
		mul := inner.Args[0].(*ast.BinaryExpr)
		require.Equal(t, token.STAR, mul.Op)
		require.Equal(t, "x", mul.X.(*ast.Name).Name)
		require.Equal(t, "y", mul.Y.(*ast.Name).Name)
	})

	t.Run("arithmetic precedence", func(t *testing.T) {
		mod, err := parse("1 + 2 * 3\n")
		require.NoError(t, err)

		add := mod.Body[0].(*ast.ExprStmt).X.(*ast.BinaryExpr)
		require.Equal(t, token.PLUS, add.Op)
		require.Equal(t, int64(1), add.X.(*ast.IntLit).Value)

		mul := add.Y.(*ast.BinaryExpr)
		require.Equal(t, token.STAR, mul.Op)
	})

	t.Run("power is right associative", func(t *testing.T) {
		mod, err := parse("2 ** 3 ** 2\n")
		require.NoError(t, err)

		outer := mod.Body[0].(*ast.ExprStmt).X.(*ast.BinaryExpr)
		require.Equal(t, token.DOUBLESTAR, outer.Op)
		require.Equal(t, int64(2), outer.X.(*ast.IntLit).Value)
		inner := outer.Y.(*ast.BinaryExpr)
		require.Equal(t, token.DOUBLESTAR, inner.Op)
	})

	t.Run("unary, boolean, and comparison", func(t *testing.T) {
		mod, err := parse("not -a == b and c\n")
		require.NoError(t, err)

		and := mod.Body[0].(*ast.ExprStmt).X.(*ast.BoolOp)
		require.Equal(t, token.AND, and.Op)
		notExpr := and.X.(*ast.UnaryExpr)
		require.Equal(t, token.NOT, notExpr.Op)
		cmp := notExpr.X.(*ast.Compare)
		require.Equal(t, []token.Type{token.EQ}, cmp.Ops)
		require.Equal(t, token.MINUS, cmp.X.(*ast.UnaryExpr).Op)
	})

	t.Run("grouping overrides precedence", func(t *testing.T) {
		mod, err := parse("(1 + 2) * 3\n")
		require.NoError(t, err)

		mul := mod.Body[0].(*ast.ExprStmt).X.(*ast.BinaryExpr)
		require.Equal(t, token.STAR, mul.Op)
		require.Equal(t, token.PLUS, mul.X.(*ast.BinaryExpr).Op)
	})

	t.Run("adjacent string concatenation", func(t *testing.T) {
		mod, err := parse(`"ab" "cd"` + "\n")
		require.NoError(t, err)
		require.Equal(t, "abcd", mod.Body[0].(*ast.ExprStmt).X.(*ast.StrLit).Value)
	})

	t.Run("semicolon separated statements", func(t *testing.T) {
		mod, err := parse("a = 1; b = 2\n")
		require.NoError(t, err)
		require.Len(t, mod.Body, 2)
	})
}

func TestParseErrors(t *testing.T) {
	cases := map[string]token.Code{
		"if x:\n    pass\n": token.UnsupportedFeature,
		"def f():\n    x\n": token.UnsupportedFeature,
		"pass\n":            token.UnsupportedFeature,
		"return 1\n":        token.UnsupportedFeature,
		"x = lambda: 1\n":   token.UnsupportedFeature,
		"xs = [1, 2]\n":     token.UnsupportedFeature,
		"d = {}\n":          token.UnsupportedFeature,
		"t = (1, 2)\n":      token.UnsupportedFeature,
		"v: list[int]\n":    token.UnsupportedType,
		"1 = 2\n":           token.SyntaxError,
	}
	for src, code := range cases {
		_, err := parse(src)
		require.Errorf(t, err, "src=%q", src)
		hasCode(t, err, code)
	}
}
