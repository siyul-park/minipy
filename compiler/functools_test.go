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
