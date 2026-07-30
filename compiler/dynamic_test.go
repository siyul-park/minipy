package compiler

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCompileDynamic(t *testing.T) {
	t.Run("arithmetic on Any int values", func(t *testing.T) {
		src := "x: Any = 42\ny: Any = 8\nprint(str(x + y))\nprint(str(x - y))\nprint(str(x * y))\n"
		require.Equal(t, "50\n34\n336\n", run(t, src))
	})

	t.Run("int division on Any values", func(t *testing.T) {
		src := "x: Any = 10\ny: Any = 3\nprint(str(x // y))\nprint(str(x % y))\n"
		require.Equal(t, "3\n1\n", run(t, src))
	})

	t.Run("true division on Any int values yields float", func(t *testing.T) {
		src := "x: Any = 7\ny: Any = 2\nprint(str(x / y))\n"
		require.Equal(t, "3.5\n", run(t, src))
	})

	t.Run("power on Any int values", func(t *testing.T) {
		src := "x: Any = 2\ny: Any = 10\nprint(str(x ** y))\n"
		require.Equal(t, "1024\n", run(t, src))
	})

	t.Run("arithmetic with mixed int and float Any", func(t *testing.T) {
		src := "x: Any = 3\ny: Any = 1.5\nprint(str(x + y))\n"
		require.Equal(t, "4.5\n", run(t, src))
	})

	t.Run("string concatenation on Any", func(t *testing.T) {
		src := "x: Any = \"hello\"\ny: Any = \" world\"\nprint(x + y)\n"
		require.Equal(t, "hello world\n", run(t, src))
	})

	t.Run("string repetition on Any", func(t *testing.T) {
		src := "x: Any = \"ab\"\nn: Any = 3\nprint(x * n)\n"
		require.Equal(t, "ababab\n", run(t, src))
	})

	t.Run("comparison on Any numeric values", func(t *testing.T) {
		src := "x: Any = 10\ny: Any = 20\n" +
			"print(str(x == y))\nprint(str(x != y))\n" +
			"print(str(x < y))\nprint(str(x > y))\n" +
			"print(str(x <= y))\nprint(str(x >= y))\n"
		require.Equal(t, "False\nTrue\nTrue\nFalse\nTrue\nFalse\n", run(t, src))
	})

	t.Run("comparison on Any string values", func(t *testing.T) {
		src := "x: Any = \"apple\"\ny: Any = \"banana\"\nprint(str(x < y))\nprint(str(x == y))\n"
		require.Equal(t, "True\nFalse\n", run(t, src))
	})

	t.Run("truthiness of Any values", func(t *testing.T) {
		src := "a: Any = 0\nb: Any = 1\nc: Any = \"\"\nd: Any = \"hi\"\n" +
			"print(str(bool(a)))\nprint(str(bool(b)))\n" +
			"print(str(bool(c)))\nprint(str(bool(d)))\n"
		require.Equal(t, "False\nTrue\nFalse\nTrue\n", run(t, src))
	})

	t.Run("truthiness of None", func(t *testing.T) {
		src := "x: Any = None\nprint(str(bool(x)))\n"
		require.Equal(t, "False\n", run(t, src))
	})

	t.Run("str on Any values", func(t *testing.T) {
		src := "a: Any = 42\nb: Any = 3.14\nc: Any = True\nd: Any = None\n" +
			"print(str(a))\nprint(str(b))\nprint(str(c))\nprint(str(d))\n"
		require.Equal(t, "42\n3.14\nTrue\nNone\n", run(t, src))
	})

	t.Run("print on Any values", func(t *testing.T) {
		src := "x: Any = 42\nprint(x)\n"
		require.Equal(t, "42\n", run(t, src))
	})

	t.Run("len on Any string", func(t *testing.T) {
		src := "x: Any = \"hello\"\nprint(str(len(x)))\n"
		require.Equal(t, "5\n", run(t, src))
	})

	t.Run("len on Any list", func(t *testing.T) {
		src := "x: Any = [1, 2, 3]\nprint(str(len(x)))\n"
		require.Equal(t, "3\n", run(t, src))
	})

	t.Run("for loop over Any list", func(t *testing.T) {
		src := "xs: Any = [10, 20, 30]\nfor x in xs:\n    print(str(x))\n"
		require.Equal(t, "10\n20\n30\n", run(t, src))
	})

	t.Run("for loop over Any string", func(t *testing.T) {
		src := "s: Any = \"abc\"\nfor c in s:\n    print(c)\n"
		require.Equal(t, "a\nb\nc\n", run(t, src))
	})

	t.Run("indexing Any list", func(t *testing.T) {
		src := "xs: Any = [10, 20, 30]\nprint(str(xs[0]))\nprint(str(xs[2]))\n"
		require.Equal(t, "10\n30\n", run(t, src))
	})

	t.Run("indexing Any string", func(t *testing.T) {
		src := "s: Any = \"hello\"\nprint(s[1])\n"
		require.Equal(t, "e\n", run(t, src))
	})

	t.Run("negative indexing on Any list", func(t *testing.T) {
		src := "xs: Any = [1, 2, 3]\nprint(str(xs[-1]))\n"
		require.Equal(t, "3\n", run(t, src))
	})

	t.Run("union arithmetic", func(t *testing.T) {
		src := "x: int | float = 5\ny: int | float = 2.0\nprint(str(x + y))\n"
		require.Equal(t, "7.0\n", run(t, src))
	})

	t.Run("isinstance narrowed code still uses static paths", func(t *testing.T) {
		src := "def describe(x: int | str) -> str:\n" +
			"    if isinstance(x, int):\n" +
			"        return \"int:\" + str(x)\n" +
			"    return \"str:\" + x\n" +
			"print(describe(3))\n" +
			"print(describe(\"hi\"))\n"
		require.Equal(t, "int:3\nstr:hi\n", run(t, src))
	})

	t.Run("specialization still produces concrete code", func(t *testing.T) {
		src := "def add(a: int | float, b: int | float) -> int | float:\n" +
			"    if isinstance(a, int):\n" +
			"        if isinstance(b, int):\n" +
			"            return a + b\n" +
			"        return 0\n" +
			"    return 0\n" +
			"print(str(add(3, 4)))\n"
		require.Equal(t, "7\n", run(t, src))
	})

	t.Run("python floor division semantics on negative numbers", func(t *testing.T) {
		src := "x: Any = -7\ny: Any = 2\nprint(str(x // y))\nprint(str(x % y))\n"
		require.Equal(t, "-4\n1\n", run(t, src))
	})

	t.Run("in operator on Any list", func(t *testing.T) {
		src := "xs: Any = [1, 2, 3]\nprint(str(2 in xs))\nprint(str(5 in xs))\n"
		require.Equal(t, "True\nFalse\n", run(t, src))
	})

	t.Run("in operator on Any string", func(t *testing.T) {
		src := "s: Any = \"hello world\"\nprint(str(\"hello\" in s))\nprint(str(\"xyz\" in s))\n"
		require.Equal(t, "True\nFalse\n", run(t, src))
	})
}
