package compiler

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestP0VariadicAndCallUnpackingRegression(t *testing.T) {
	t.Run("static tuple star call and keyword-only argument", func(t *testing.T) {
		src := "def total(a: int, b: int, *, c: int = 3) -> int:\n" +
			"    return a + b + c\n" +
			"args: tuple[int, int] = (1, 2)\n" +
			"print(str(total(*args)))\n" +
			"print(str(total(*args, c=4)))\n"
		require.Equal(t, "6\n7\n", run(t, src))
	})

	t.Run("duplicate keyword from positional binding is rejected", func(t *testing.T) {
		errs := checkOnly(t, "def f(a: int) -> int:\n"+
			"    return a\n"+
			"print(str(f(1, a=2)))\n")
		require.NotEmpty(t, errs)
	})
}

func TestP0MatchStarAndRestPatternsRegression(t *testing.T) {
	t.Run("list starred rest capture", func(t *testing.T) {
		src := "xs: list[int] = [1, 2, 3]\n" +
			"match xs:\n" +
			"    case [head, *tail]:\n" +
			"        print(str(head))\n" +
			"        print(str(tail[0]))\n"
		require.Equal(t, "1\n2\n", run(t, src))
	})

	t.Run("mapping rest capture copies unconsumed keys", func(t *testing.T) {
		src := "data: dict[str, int] = {\"x\": 1, \"y\": 2}\n" +
			"match data:\n" +
			"    case {\"x\": x, **rest}:\n" +
			"        print(str(x))\n" +
			"        print(str(rest[\"y\"]))\n"
		require.Equal(t, "1\n2\n", run(t, src))
	})
}
