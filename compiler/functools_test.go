package compiler

import (
	"strings"
	"testing"

	"github.com/siyul-park/minipy/token"
	"github.com/stretchr/testify/require"
)

func TestFunctoolsModule(t *testing.T) {
	t.Run("reduce sum ints", func(t *testing.T) {
		src := "from functools import reduce\nresult: int = reduce(lambda a, b: a + b, [1, 2, 3, 4])\nprint(str(result))\n"
		require.Equal(t, "10\n", run(t, src))
	})

	t.Run("reduce sum ints with initial", func(t *testing.T) {
		src := "from functools import reduce\nresult: int = reduce(lambda a, b: a + b, [1, 2, 3, 4], 10)\nprint(str(result))\n"
		require.Equal(t, "20\n", run(t, src))
	})

	t.Run("reduce concat strings", func(t *testing.T) {
		src := "import functools\nresult: str = functools.reduce(lambda a, b: a + b, [\"hello\", \" \", \"world\"])\nprint(result)\n"
		require.Equal(t, "hello world\n", run(t, src))
	})

	t.Run("reduce single element no initial", func(t *testing.T) {
		src := "from functools import reduce\nresult: int = reduce(lambda a, b: a + b, [42])\nprint(str(result))\n"
		require.Equal(t, "42\n", run(t, src))
	})

	t.Run("reduce with initial and empty list", func(t *testing.T) {
		src := "from functools import reduce\nxs: list[int] = []\nresult: int = reduce(lambda a, b: a + b, xs, 99)\nprint(str(result))\n"
		require.Equal(t, "99\n", run(t, src))
	})
}

func TestFunctoolsModuleErrors(t *testing.T) {
	// The accumulator is one slot carrying the element type across the whole
	// fold, so a callback returning something else would change what that slot
	// holds mid-loop. Before this was checked, the fold silently dropped every
	// step but the last: reduce over [1, 2, 3] with an int -> float adder
	// printed 3.0 instead of 6.0.
	t.Run("reduce with a function returning a different type", func(t *testing.T) {
		src := "from functools import reduce\n" +
			"def f(a: int, b: int) -> float:\n" +
			"    return float(a) + float(b)\n" +
			"xs: list[int] = [1, 2, 3]\n" +
			"print(reduce(f, xs))\n"
		_, err := Compile(strings.NewReader(src))
		require.Error(t, err)
		code(t, err, token.TypeMismatch)
	})

	t.Run("reduce with non-list second argument", func(t *testing.T) {
		src := "from functools import reduce\nreduce(lambda a, b: a + b, 5)\n"
		_, err := Compile(strings.NewReader(src))
		require.Error(t, err)
		code(t, err, token.TypeMismatch)
	})

	t.Run("reduce with wrong arity function", func(t *testing.T) {
		src := "from functools import reduce\nreduce(lambda a: a, [1, 2, 3])\n"
		_, err := Compile(strings.NewReader(src))
		require.Error(t, err)
		code(t, err, token.TypeMismatch)
	})

	t.Run("reduce with too many arguments", func(t *testing.T) {
		src := "from functools import reduce\nreduce(lambda a, b: a + b, [1], 0, 1)\n"
		_, err := Compile(strings.NewReader(src))
		require.Error(t, err)
		code(t, err, token.ArityMismatch)
	})

	t.Run("reduce with too few arguments", func(t *testing.T) {
		src := "from functools import reduce\nreduce(lambda a, b: a + b)\n"
		_, err := Compile(strings.NewReader(src))
		require.Error(t, err)
		code(t, err, token.ArityMismatch)
	})
}

func TestFunctoolsModuleRuntimeErrors(t *testing.T) {
	t.Run("reduce with empty list and no initial value", func(t *testing.T) {
		src := "from functools import reduce\nxs: list[int] = []\nresult: int = reduce(lambda a, b: a + b, xs)\nprint(str(result))\n"
		err := runError(t, src)
		require.Error(t, err)
		require.Contains(t, err.Error(), "reduce")
	})
}
