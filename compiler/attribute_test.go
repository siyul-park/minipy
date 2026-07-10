package compiler

import (
	"testing"

	"github.com/siyul-park/minipy/token"

	"github.com/stretchr/testify/require"
)

func TestCompileAttributeBuiltins(t *testing.T) {
	t.Run("field lookup and presence run", func(t *testing.T) {
		src := "@dataclass\n" +
			"class Point:\n" +
			"    x: int\n" +
			"p: Point = Point(7)\n" +
			"print(str(getattr(p, \"x\")))\n" +
			"print(str(hasattr(p, \"x\")))\n" +
			"print(str(hasattr(p, \"missing\")))\n"
		require.Equal(t, "7\nTrue\nFalse\n", run(t, src))
	})

	t.Run("inherited field runs", func(t *testing.T) {
		src := "@dataclass\n" +
			"class Base:\n" +
			"    x: int\n" +
			"@dataclass\n" +
			"class Child(Base):\n" +
			"    y: int\n" +
			"c: Child = Child(3, 4)\n" +
			"print(str(getattr(c, \"x\")))\n"
		require.Equal(t, "3\n", run(t, src))
	})

	t.Run("source field with internal spelling runs", func(t *testing.T) {
		src := "@dataclass\n" +
			"class C:\n" +
			"    __classid: int\n" +
			"c: C = C(5)\n" +
			"print(str(getattr(c, \"__classid\")))\n" +
			"print(str(hasattr(c, \"__classid\")))\n"
		require.Equal(t, "5\nTrue\n", run(t, src))
	})

	t.Run("receiver is evaluated once", func(t *testing.T) {
		src := "calls: int = 0\n" +
			"@dataclass\n" +
			"class Point:\n" +
			"    x: int\n" +
			"def make() -> Point:\n" +
			"    global calls\n" +
			"    calls = calls + 1\n" +
			"    return Point(9)\n" +
			"print(str(getattr(make(), \"x\")))\n" +
			"print(str(hasattr(make(), \"x\")))\n" +
			"print(str(calls))\n"
		require.Equal(t, "9\nTrue\n2\n", run(t, src))
	})
}

func TestCheckAttributeBuiltins(t *testing.T) {
	cases := map[string]token.Code{
		"@dataclass\nclass C:\n    x: int\nc: C = C(1)\ngetattr(c)\n":                                  token.ArityMismatch,
		"@dataclass\nclass C:\n    x: int\nc: C = C(1)\nname: str = \"x\"\ngetattr(c, name)\n": token.UnsupportedFeature,
		"getattr(1, \"x\")\n":                                                                     token.UnsupportedFeature,
		"@dataclass\nclass C:\n    x: int\nc: C = C(1)\ngetattr(c, \"missing\")\n":              token.UndefinedName,
		"class C:\n    def f(self) -> int:\n        return 1\nc: C = C()\ngetattr(c, \"f\")\n":      token.UndefinedName,
		"hasattr(ValueError(\"x\"), \"__classid\")\n":                                          token.UnsupportedFeature,
	}
	for src, want := range cases {
		errs := checkOnly(t, src)
		require.NotEmptyf(t, errs, "src=%q", src)
		require.Equalf(t, want, errs[0].Code, "src=%q", src)
	}
}
