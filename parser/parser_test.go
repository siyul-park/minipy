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

	t.Run("if elif else folds into nested If", func(t *testing.T) {
		mod, err := parse(`if a == 1:
    x = 1
elif a == 2:
    x = 2
else:
    x = 3
`)
		require.NoError(t, err)

		top := mod.Body[0].(*ast.If)
		require.IsType(t, &ast.Compare{}, top.Cond)
		require.Len(t, top.Body, 1)

		elif := top.Orelse[0].(*ast.If)
		require.Len(t, elif.Body, 1)
		require.IsType(t, &ast.Assign{}, elif.Orelse[0])
	})

	t.Run("inline block", func(t *testing.T) {
		mod, err := parse("if a: b = 1\n")
		require.NoError(t, err)
		ifs := mod.Body[0].(*ast.If)
		require.Len(t, ifs.Body, 1)
		require.IsType(t, &ast.Assign{}, ifs.Body[0])
	})

	t.Run("while with else", func(t *testing.T) {
		mod, err := parse(`while a < 3:
    a = a + 1
else:
    pass
`)
		require.NoError(t, err)
		w := mod.Body[0].(*ast.While)
		require.Len(t, w.Body, 1)
		require.IsType(t, &ast.Pass{}, w.Orelse[0])
	})

	t.Run("for over range", func(t *testing.T) {
		mod, err := parse(`for i in range(5):
    pass
`)
		require.NoError(t, err)
		f := mod.Body[0].(*ast.For)
		require.Equal(t, "i", f.Target.(*ast.Name).Name)
		call := f.Iter.(*ast.CallExpr)
		require.Equal(t, "range", call.Fn.(*ast.Name).Name)
		require.IsType(t, &ast.Pass{}, f.Body[0])
	})

	t.Run("nested loop with break and continue", func(t *testing.T) {
		mod, err := parse(`for i in range(3):
    if i == 0:
        continue
    break
`)
		require.NoError(t, err)
		f := mod.Body[0].(*ast.For)
		require.Len(t, f.Body, 2)
		inner := f.Body[0].(*ast.If)
		require.IsType(t, &ast.Continue{}, inner.Body[0])
		require.IsType(t, &ast.Break{}, f.Body[1])
	})

	t.Run("conditional expression", func(t *testing.T) {
		mod, err := parse("x = 1 if c else 2\n")
		require.NoError(t, err)
		ifExp := mod.Body[0].(*ast.Assign).Value.(*ast.IfExp)
		require.Equal(t, int64(1), ifExp.Body.(*ast.IntLit).Value)
		require.Equal(t, "c", ifExp.Cond.(*ast.Name).Name)
		require.Equal(t, int64(2), ifExp.Orelse.(*ast.IntLit).Value)
	})

	t.Run("function definition with return", func(t *testing.T) {
		mod, err := parse(`def add(x: int, y: int) -> int:
    return x + y
`)
		require.NoError(t, err)
		fn := mod.Body[0].(*ast.Function)
		require.Equal(t, "add", fn.Name.Name)
		require.Len(t, fn.Params, 2)
		require.Equal(t, "x", fn.Params[0].Name.Name)
		require.Equal(t, "int", fn.Params[0].Ann.(*ast.Name).Name)
		require.Equal(t, "int", fn.Returns.(*ast.Name).Name)
		ret := fn.Body[0].(*ast.Return)
		require.Equal(t, token.PLUS, ret.Value.(*ast.BinaryExpr).Op)
	})

	t.Run("decorated function", func(t *testing.T) {
		mod, err := parse("@staticmethod\ndef f() -> None:\n    return\n")
		require.NoError(t, err)
		fn := mod.Body[0].(*ast.Function)
		require.Equal(t, "staticmethod", fn.Decorators[0].Name)
		require.Nil(t, fn.Body[0].(*ast.Return).Value)
	})

	t.Run("M3 displays, subscript, method, f-string", func(t *testing.T) {
		mod, err := parse("xs: list[int] = [1, 2]\nd: dict[str, int] = {\"a\": 1}\na, b = (1, 2)\nprint(f\"a={a}\")\nxs.append(d[\"a\"])\n")
		require.NoError(t, err)
		require.IsType(t, &ast.ListLit{}, mod.Body[0].(*ast.AnnAssign).Value)
		require.IsType(t, &ast.DictLit{}, mod.Body[1].(*ast.AnnAssign).Value)
		require.IsType(t, &ast.TupleLit{}, mod.Body[2].(*ast.Assign).Target)
		require.IsType(t, &ast.FString{}, mod.Body[3].(*ast.ExprStmt).X.(*ast.CallExpr).Args[0])
		call := mod.Body[4].(*ast.ExprStmt).X.(*ast.CallExpr)
		require.Equal(t, "append", call.Fn.(*ast.Attribute).Name)
		require.IsType(t, &ast.Subscript{}, call.Args[0])
	})
}

func TestParseErrors(t *testing.T) {
	cases := map[string]token.Code{
		"x = lambda: 1\n":        token.UnsupportedFeature,
		"1 = 2\n":                token.SyntaxError,
		"else:\n    pass\n":      token.SyntaxError,
		"def f(x) -> int:\n p\n": token.MissingAnnotation,
		"@pkg.decorator\ndef f() -> None:\n pass\n": token.UnsupportedFeature,
	}
	for src, code := range cases {
		_, err := parse(src)
		require.Errorf(t, err, "src=%q", src)
		hasCode(t, err, code)
	}
}
