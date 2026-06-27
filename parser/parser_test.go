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

	t.Run("union annotations", func(t *testing.T) {
		mod, err := parse("x: int | str = 1\n")
		require.NoError(t, err)
		u := mod.Body[0].(*ast.AnnAssign).Ann.(*ast.UnionType)
		require.Len(t, u.Members, 2)
		require.Equal(t, "int", u.Members[0].(*ast.Name).Name)
		require.Equal(t, "str", u.Members[1].(*ast.Name).Name)
	})

	t.Run("multi-member union with None", func(t *testing.T) {
		mod, err := parse("x: int | str | None\n")
		require.NoError(t, err)
		u := mod.Body[0].(*ast.AnnAssign).Ann.(*ast.UnionType)
		require.Len(t, u.Members, 3)
		require.Equal(t, "None", u.Members[2].(*ast.Name).Name)
	})

	t.Run("Optional and Union subscripts parse as subscript", func(t *testing.T) {
		mod, err := parse("x: Optional[int]\ny: Union[int, str]\n")
		require.NoError(t, err)
		opt := mod.Body[0].(*ast.AnnAssign).Ann.(*ast.Subscript)
		require.Equal(t, "Optional", opt.X.(*ast.Name).Name)
		un := mod.Body[1].(*ast.AnnAssign).Ann.(*ast.Subscript)
		require.Equal(t, "Union", un.X.(*ast.Name).Name)
	})

	t.Run("union in function signature and nested generic", func(t *testing.T) {
		mod, err := parse("def f(a: int | None) -> bool | None:\n    return True\n")
		require.NoError(t, err)
		fn := mod.Body[0].(*ast.Function)
		require.IsType(t, &ast.UnionType{}, fn.Params[0].Ann)
		require.IsType(t, &ast.UnionType{}, fn.Returns)

		mod, err = parse("xs: list[int | str]\n")
		require.NoError(t, err)
		sub := mod.Body[0].(*ast.AnnAssign).Ann.(*ast.Subscript)
		require.IsType(t, &ast.UnionType{}, sub.Index)
	})

	t.Run("optional parameter and return annotations", func(t *testing.T) {
		mod, err := parse("def identity(x):\n    return x\n")
		require.NoError(t, err)
		fn := mod.Body[0].(*ast.Function)
		require.Nil(t, fn.Params[0].Ann)
		require.Nil(t, fn.Returns)
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

	t.Run("displays, subscript, method, f-string", func(t *testing.T) {
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

	t.Run("lambda expression", func(t *testing.T) {
		mod, err := parse("f: Callable[[int], int] = lambda x: x + 1\n")
		require.NoError(t, err)
		lambda := mod.Body[0].(*ast.AnnAssign).Value.(*ast.LambdaExpr)
		require.Equal(t, "x", lambda.Params[0].Name.Name)
		require.Equal(t, token.PLUS, lambda.Body.(*ast.BinaryExpr).Op)
	})

	t.Run("global and nonlocal declarations", func(t *testing.T) {
		mod, err := parse(`def f() -> None:
    global x
    nonlocal y
`)
		require.NoError(t, err)
		body := mod.Body[0].(*ast.Function).Body
		require.Equal(t, []string{"x"}, body[0].(*ast.Global).Names)
		require.Equal(t, []string{"y"}, body[1].(*ast.Nonlocal).Names)
	})

	t.Run("class definition with fields methods base and dataclass decorator", func(t *testing.T) {
		mod, err := parse(`@dataclass
class Point(Base):
    x: int
    y: int = 0
    def __init__(self, x: int, y: int) -> None:
        self.x = x
        self.y = y
    def norm2(self) -> int:
        return self.x * self.x + self.y * self.y
`)
		require.NoError(t, err)
		cls := mod.Body[0].(*ast.Class)
		require.Equal(t, "Point", cls.Name.Name)
		require.Equal(t, "Base", cls.BaseClass.Name)
		require.Equal(t, "dataclass", cls.Decorators[0].Name)
		require.Len(t, cls.Body, 4)
		require.Equal(t, "x", cls.Body[0].(*ast.AnnAssign).Target.Name)
		require.Equal(t, "y", cls.Body[1].(*ast.AnnAssign).Target.Name)
		init := cls.Body[2].(*ast.Function)
		require.Equal(t, "__init__", init.Name.Name)
		require.Equal(t, "self", init.Params[0].Name.Name)
		assign := init.Body[0].(*ast.Assign)
		require.Equal(t, "x", assign.Target.(*ast.Attribute).Name)
	})

	t.Run("yield statements and Iterator annotation", func(t *testing.T) {
		mod, err := parse(`def ints() -> Iterator[int]:
    yield 1
    yield
`)
		require.NoError(t, err)
		fn := mod.Body[0].(*ast.Function)
		require.Equal(t, "Iterator", fn.Returns.(*ast.Subscript).X.(*ast.Name).Name)
		require.Equal(t, int64(1), fn.Body[0].(*ast.Yield).Value.(*ast.IntLit).Value)
		require.Nil(t, fn.Body[1].(*ast.Yield).Value)
	})

	t.Run("comprehensions and set display", func(t *testing.T) {
		mod, err := parse("xs: list[int] = [i for i in range(3) if i < 2]\nd: dict[str, int] = {str(i): i for i in range(2)}\ns: set[int] = {i for i in [1, 2]}\nt: set[int] = {1, 2}\n")
		require.NoError(t, err)
		require.IsType(t, &ast.ListComp{}, mod.Body[0].(*ast.AnnAssign).Value)
		require.IsType(t, &ast.DictComp{}, mod.Body[1].(*ast.AnnAssign).Value)
		require.IsType(t, &ast.SetComp{}, mod.Body[2].(*ast.AnnAssign).Value)
		require.IsType(t, &ast.SetLit{}, mod.Body[3].(*ast.AnnAssign).Value)
	})

	t.Run("del with name, subscript, attribute targets", func(t *testing.T) {
		mod, err := parse("del a, b[k], c.x\n")
		require.NoError(t, err)
		d := mod.Body[0].(*ast.Delete)
		require.Len(t, d.Targets, 3)
		require.IsType(t, &ast.Name{}, d.Targets[0])
		require.IsType(t, &ast.Subscript{}, d.Targets[1])
		require.IsType(t, &ast.Attribute{}, d.Targets[2])
	})

	t.Run("assert with and without message", func(t *testing.T) {
		mod, err := parse("assert x\nassert x, \"boom\"\n")
		require.NoError(t, err)
		a0 := mod.Body[0].(*ast.Assert)
		require.Nil(t, a0.Msg)
		a1 := mod.Body[1].(*ast.Assert)
		require.Equal(t, "boom", a1.Msg.(*ast.StrLit).Value)
	})

	t.Run("match as a name is not a match statement", func(t *testing.T) {
		mod, err := parse("match = 1\nprint(match)\nmatch(x)\nmatch.y\n")
		require.NoError(t, err)
		require.IsType(t, &ast.Assign{}, mod.Body[0])
		require.IsType(t, &ast.ExprStmt{}, mod.Body[1])
		require.IsType(t, &ast.ExprStmt{}, mod.Body[2])
		require.IsType(t, &ast.ExprStmt{}, mod.Body[3])
	})

	t.Run("match statement with varied patterns", func(t *testing.T) {
		src := "match p:\n" +
			"    case 200:\n        pass\n" +
			"    case [1, *rest]:\n        pass\n" +
			"    case {\"k\": v, **others}:\n        pass\n" +
			"    case Point(x, y=0):\n        pass\n" +
			"    case 1 | 2 | 3:\n        pass\n" +
			"    case (a, b) as pair if a < b:\n        pass\n" +
			"    case _:\n        pass\n"
		mod, err := parse(src)
		require.NoError(t, err)
		m := mod.Body[0].(*ast.Match)
		require.IsType(t, &ast.Name{}, m.Subject)
		require.Len(t, m.Cases, 7)

		require.IsType(t, &ast.ValuePattern{}, m.Cases[0].Pattern)

		seq := m.Cases[1].Pattern.(*ast.SequencePattern)
		require.Equal(t, 1, seq.Star)
		require.IsType(t, &ast.StarPattern{}, seq.Elems[1])

		mp := m.Cases[2].Pattern.(*ast.MappingPattern)
		require.Equal(t, "others", mp.Rest)
		require.Len(t, mp.Keys, 1)

		cp := m.Cases[3].Pattern.(*ast.ClassPattern)
		require.Len(t, cp.Args, 1)
		require.Equal(t, []string{"y"}, cp.KwNames)

		require.IsType(t, &ast.OrPattern{}, m.Cases[4].Pattern)

		as := m.Cases[5].Pattern.(*ast.AsPattern)
		require.Equal(t, "pair", as.Name)
		require.IsType(t, &ast.SequencePattern{}, as.Pattern)
		require.NotNil(t, m.Cases[5].Guard)

		require.IsType(t, &ast.WildcardPattern{}, m.Cases[6].Pattern)
	})

	t.Run("try except else finally with as name", func(t *testing.T) {
		mod, err := parse(`try:
    x = 1
except ValueError as e:
    x = 2
except:
    x = 3
else:
    x = 4
finally:
    x = 5
`)
		require.NoError(t, err)
		tr := mod.Body[0].(*ast.Try)
		require.Len(t, tr.Body, 1)
		require.Len(t, tr.Handlers, 2)
		require.Equal(t, "ValueError", tr.Handlers[0].Type.(*ast.Name).Name)
		require.Equal(t, "e", tr.Handlers[0].Name)
		require.Nil(t, tr.Handlers[1].Type)
		require.Len(t, tr.Orelse, 1)
		require.Len(t, tr.Finalbody, 1)
	})

	t.Run("raise with expression and bare raise", func(t *testing.T) {
		mod, err := parse("raise ValueError(\"x\")\nraise\n")
		require.NoError(t, err)
		require.IsType(t, &ast.CallExpr{}, mod.Body[0].(*ast.Raise).Exc)
		require.Nil(t, mod.Body[1].(*ast.Raise).Exc)
	})

	t.Run("with one and multiple items", func(t *testing.T) {
		mod, err := parse(`with a as x, b:
    pass
`)
		require.NoError(t, err)
		w := mod.Body[0].(*ast.With)
		require.Len(t, w.Items, 2)
		require.Equal(t, "a", w.Items[0].Context.(*ast.Name).Name)
		require.Equal(t, "x", w.Items[0].OptionalVars.(*ast.Name).Name)
		require.Equal(t, "b", w.Items[1].Context.(*ast.Name).Name)
		require.Nil(t, w.Items[1].OptionalVars)
	})

	t.Run("is and is not comparisons", func(t *testing.T) {
		mod, err := parse("x is None\nx is not y\n")
		require.NoError(t, err)
		first := mod.Body[0].(*ast.ExprStmt).X.(*ast.Compare)
		require.Equal(t, []token.Type{token.IS}, first.Ops)
		second := mod.Body[1].(*ast.ExprStmt).X.(*ast.Compare)
		require.Equal(t, []token.Type{token.ISNOT}, second.Ops)
	})
}

func TestParseErrors(t *testing.T) {
	cases := map[string]token.Code{
		"1 = 2\n":           token.SyntaxError,
		"else:\n    pass\n": token.SyntaxError,
		"@pkg.decorator\ndef f() -> None:\n pass\n":      token.UnsupportedFeature,
		"@other\nclass C:\n    pass\n":                   token.UnsupportedFeature,
		"class C(A, B):\n    pass\n":                     token.UnsupportedFeature,
		"def f() -> Iterator[int]:\n    yield from xs\n": token.UnsupportedFeature,
	}
	for src, code := range cases {
		_, err := parse(src)
		require.Errorf(t, err, "src=%q", src)
		hasCode(t, err, code)
	}
}
