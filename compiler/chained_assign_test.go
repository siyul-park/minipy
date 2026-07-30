package compiler

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCompileChainedAssign(t *testing.T) {
	t.Run("two targets int", func(t *testing.T) {
		src := "a = b = 5\nprint(str(a) + \" \" + str(b))\n"
		require.Equal(t, "5 5\n", run(t, src))
	})

	t.Run("two targets string", func(t *testing.T) {
		src := "x = y = \"hello\"\nprint(x + \" \" + y)\n"
		require.Equal(t, "hello hello\n", run(t, src))
	})

	t.Run("three targets", func(t *testing.T) {
		src := "a = b = c = 10\nprint(str(a) + \" \" + str(b) + \" \" + str(c))\n"
		require.Equal(t, "10 10 10\n", run(t, src))
	})

	t.Run("chained with expression", func(t *testing.T) {
		src := "a = b = 2 + 3\nprint(str(a) + \" \" + str(b))\n"
		require.Equal(t, "5 5\n", run(t, src))
	})

	t.Run("chained in function", func(t *testing.T) {
		src := "def f() -> int:\n    x = y = 42\n    return x + y\nprint(str(f()))\n"
		require.Equal(t, "84\n", run(t, src))
	})
}
