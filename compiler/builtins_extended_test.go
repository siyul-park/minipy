package compiler

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCompileBuiltinsExtended(t *testing.T) {
	t.Run("sorted int list", func(t *testing.T) {
		got := run(t, `
x: list[int] = [3, 1, 2]
print(sorted(x))
`)
		require.Equal(t, "[1, 2, 3]\n", got)
	})

	t.Run("sorted str list", func(t *testing.T) {
		got := run(t, `
x: list[str] = ["banana", "apple", "cherry"]
print(sorted(x))
`)
		require.Equal(t, "['apple', 'banana', 'cherry']\n", got)
	})

	t.Run("sorted reverse keyword", func(t *testing.T) {
		got := run(t, `
x: list[int] = [3, 1, 2]
print(sorted(x, reverse=True))
`)
		require.Equal(t, "[3, 2, 1]\n", got)
	})

	t.Run("sorted key lambda", func(t *testing.T) {
		got := run(t, `
x: list[int] = [3, 1, 2]
print(sorted(x, key=lambda n: -n))
`)
		require.Equal(t, "[3, 2, 1]\n", got)
	})

	t.Run("sorted key evaluated once", func(t *testing.T) {
		got := run(t, `
seen: list[int] = []
def key(n: int) -> int:
    seen.append(n)
    return n
x: list[int] = [3, 1, 2]
sorted(x, key=key)
print(len(seen))
`)
		require.Equal(t, "3\n", got)
	})

	t.Run("sorted key reverse stable", func(t *testing.T) {
		got := run(t, `
x: list[str] = ["bb", "a", "cc", "d"]
print(sorted(x, key=lambda s: len(s), reverse=True))
`)
		require.Equal(t, "['bb', 'cc', 'a', 'd']\n", got)
	})

	t.Run("sorted key named function", func(t *testing.T) {
		got := run(t, `
def size(s: str) -> int:
    return len(s)
x: list[str] = ["bbb", "a", "cc"]
print(sorted(x, key=size))
`)
		require.Equal(t, "['a', 'cc', 'bbb']\n", got)
	})

	t.Run("sorted key none", func(t *testing.T) {
		got := run(t, "x: list[int] = [3, 1, 2]\nprint(sorted(x, key=None))\n")
		require.Equal(t, "[1, 2, 3]\n", got)
	})

	t.Run("sorted does not mutate original", func(t *testing.T) {
		got := run(t, `
x: list[int] = [3, 1, 2]
y: list[int] = sorted(x)
print(x)
print(y)
`)
		require.Equal(t, "[3, 1, 2]\n[1, 2, 3]\n", got)
	})

	t.Run("reversed int list", func(t *testing.T) {
		got := run(t, `
x: list[int] = [1, 2, 3]
print(reversed(x))
`)
		require.Equal(t, "[3, 2, 1]\n", got)
	})

	t.Run("reversed does not mutate original", func(t *testing.T) {
		got := run(t, `
x: list[int] = [1, 2, 3]
y: list[int] = reversed(x)
print(x)
print(y)
`)
		require.Equal(t, "[1, 2, 3]\n[3, 2, 1]\n", got)
	})

	t.Run("min two args", func(t *testing.T) {
		got := run(t, `print(min(3, 1))`)
		require.Equal(t, "1\n", got)
	})

	t.Run("min three args", func(t *testing.T) {
		got := run(t, `print(min(5, 3, 7))`)
		require.Equal(t, "3\n", got)
	})

	t.Run("min list", func(t *testing.T) {
		got := run(t, `
x: list[int] = [4, 2, 5]
print(min(x))
`)
		require.Equal(t, "2\n", got)
	})

	// A list literal passed inline is an unrooted temporary: min() must
	// retain the borrowed element it returns before the array argument is
	// released, or the element is freed out from under the caller.
	t.Run("min of temporary str list literal", func(t *testing.T) {
		got := run(t, `print(min(["banana", "apple", "cherry"]))`)
		require.Equal(t, "apple\n", got)
	})

	t.Run("max two args", func(t *testing.T) {
		got := run(t, `print(max(3, 1))`)
		require.Equal(t, "3\n", got)
	})

	t.Run("max three args", func(t *testing.T) {
		got := run(t, `print(max(5, 3, 7))`)
		require.Equal(t, "7\n", got)
	})

	t.Run("max list", func(t *testing.T) {
		got := run(t, `
x: list[int] = [4, 2, 5]
print(max(x))
`)
		require.Equal(t, "5\n", got)
	})

	// See the "min of temporary str list literal" comment above: max() has
	// the same borrowed-return shape.
	t.Run("max of temporary str list literal", func(t *testing.T) {
		got := run(t, `print(max(["banana", "apple", "cherry"]))`)
		require.Equal(t, "cherry\n", got)
	})

	t.Run("sum int list", func(t *testing.T) {
		got := run(t, `
x: list[int] = [1, 2, 3]
print(sum(x))
`)
		require.Equal(t, "6\n", got)
	})

	t.Run("sum float list", func(t *testing.T) {
		got := run(t, `
x: list[float] = [1.5, 2.5, 3.0]
print(sum(x))
`)
		require.Equal(t, "7.0\n", got)
	})

	t.Run("any true", func(t *testing.T) {
		got := run(t, `
x: list[bool] = [False, True, False]
print(any(x))
`)
		require.Equal(t, "True\n", got)
	})

	t.Run("any false", func(t *testing.T) {
		got := run(t, `
x: list[bool] = [False, False, False]
print(any(x))
`)
		require.Equal(t, "False\n", got)
	})

	t.Run("all true", func(t *testing.T) {
		got := run(t, `
x: list[bool] = [True, True, True]
print(all(x))
`)
		require.Equal(t, "True\n", got)
	})

	t.Run("all false", func(t *testing.T) {
		got := run(t, `
x: list[bool] = [True, False, True]
print(all(x))
`)
		require.Equal(t, "False\n", got)
	})

	t.Run("round no digits", func(t *testing.T) {
		got := run(t, `print(round(3.7))`)
		require.Equal(t, "4\n", got)
	})

	t.Run("round with digits", func(t *testing.T) {
		got := run(t, `print(round(3.14159, 2))`)
		require.Equal(t, "3.14\n", got)
	})

	t.Run("round banker", func(t *testing.T) {
		got := run(t, `print(round(2.5))`)
		require.Equal(t, "2\n", got)
	})

	t.Run("divmod int", func(t *testing.T) {
		got := run(t, `print(divmod(7, 2))`)
		require.Equal(t, "(3, 1)\n", got)
	})

	t.Run("divmod negative", func(t *testing.T) {
		got := run(t, `print(divmod(-7, 2))`)
		require.Equal(t, "(-4, 1)\n", got)
	})

	t.Run("pow int", func(t *testing.T) {
		got := run(t, `print(pow(2, 3))`)
		require.Equal(t, "8\n", got)
	})

	t.Run("pow float", func(t *testing.T) {
		got := run(t, `print(pow(2.0, 3.0))`)
		require.Equal(t, "8.0\n", got)
	})

	t.Run("hex", func(t *testing.T) {
		got := run(t, `print(hex(255))`)
		require.Equal(t, "0xff\n", got)
	})

	t.Run("hex negative", func(t *testing.T) {
		got := run(t, `print(hex(-42))`)
		require.Equal(t, "-0x2a\n", got)
	})

	t.Run("oct", func(t *testing.T) {
		got := run(t, `print(oct(8))`)
		require.Equal(t, "0o10\n", got)
	})

	t.Run("bin", func(t *testing.T) {
		got := run(t, `print(bin(5))`)
		require.Equal(t, "0b101\n", got)
	})

	t.Run("bin negative", func(t *testing.T) {
		got := run(t, `print(bin(-5))`)
		require.Equal(t, "-0b101\n", got)
	})

	t.Run("repr string", func(t *testing.T) {
		got := run(t, `print(repr("hi"))`)
		require.Equal(t, "'hi'\n", got)
	})

	t.Run("repr int", func(t *testing.T) {
		got := run(t, `print(repr(42))`)
		require.Equal(t, "42\n", got)
	})

	t.Run("min str args", func(t *testing.T) {
		got := run(t, `print(min("b", "a", "c"))`)
		require.Equal(t, "a\n", got)
	})

	t.Run("max str args", func(t *testing.T) {
		got := run(t, `print(max("b", "a", "c"))`)
		require.Equal(t, "c\n", got)
	})

	t.Run("pow int float", func(t *testing.T) {
		got := run(t, `print(pow(2, 0.5))`)
		require.Equal(t, "1.4142135623730951\n", got)
	})
}
