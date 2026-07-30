package compiler

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCompileBuiltinsFunctional(t *testing.T) {
	t.Run("map doubles list", func(t *testing.T) {
		got := run(t, `
f: Callable[[int], int] = lambda x: x * 2
xs: list[int] = [1, 2, 3]
print(map(f, xs))
`)
		require.Equal(t, "[2, 4, 6]\n", got)
	})

	t.Run("map with inline lambda", func(t *testing.T) {
		got := run(t, `
xs: list[int] = [1, 2, 3]
result: list[int] = map(lambda x: x + 10, xs)
print(result)
`)
		require.Equal(t, "[11, 12, 13]\n", got)
	})

	t.Run("map str to int", func(t *testing.T) {
		got := run(t, `
xs: list[str] = ["hello", "world", "hi"]
result: list[int] = map(lambda s: len(s), xs)
print(result)
`)
		require.Equal(t, "[5, 5, 2]\n", got)
	})

	t.Run("map empty list", func(t *testing.T) {
		got := run(t, `
xs: list[int] = []
result: list[int] = map(lambda x: x * 2, xs)
print(result)
`)
		require.Equal(t, "[]\n", got)
	})

	t.Run("filter keeps greater than 1", func(t *testing.T) {
		got := run(t, `
f: Callable[[int], bool] = lambda x: x > 1
xs: list[int] = [1, 2, 3]
print(filter(f, xs))
`)
		require.Equal(t, "[2, 3]\n", got)
	})

	t.Run("filter with inline lambda", func(t *testing.T) {
		got := run(t, `
xs: list[int] = [1, 2, 3, 4, 5]
result: list[int] = filter(lambda x: x > 3, xs)
print(result)
`)
		require.Equal(t, "[4, 5]\n", got)
	})

	t.Run("filter keeps even numbers", func(t *testing.T) {
		got := run(t, `
xs: list[int] = [1, 2, 3, 4, 5, 6]
result: list[int] = filter(lambda x: x % 2 == 0, xs)
print(result)
`)
		require.Equal(t, "[2, 4, 6]\n", got)
	})

	t.Run("filter empty list", func(t *testing.T) {
		got := run(t, `
xs: list[int] = []
result: list[int] = filter(lambda x: x > 0, xs)
print(result)
`)
		require.Equal(t, "[]\n", got)
	})

	t.Run("filter removes all", func(t *testing.T) {
		got := run(t, `
xs: list[int] = [1, 2, 3]
result: list[int] = filter(lambda x: x > 10, xs)
print(result)
`)
		require.Equal(t, "[]\n", got)
	})

	t.Run("filter keeps all", func(t *testing.T) {
		got := run(t, `
xs: list[int] = [1, 2, 3]
result: list[int] = filter(lambda x: x > 0, xs)
print(result)
`)
		require.Equal(t, "[1, 2, 3]\n", got)
	})

	t.Run("map then filter", func(t *testing.T) {
		got := run(t, `
xs: list[int] = [1, 2, 3, 4, 5]
doubled: list[int] = map(lambda x: x * 2, xs)
result: list[int] = filter(lambda x: x > 5, doubled)
print(result)
`)
		require.Equal(t, "[6, 8, 10]\n", got)
	})

	t.Run("filter then map", func(t *testing.T) {
		got := run(t, `
xs: list[int] = [1, 2, 3, 4, 5]
filtered: list[int] = filter(lambda x: x > 2, xs)
result: list[int] = map(lambda x: x * 10, filtered)
print(result)
`)
		require.Equal(t, "[30, 40, 50]\n", got)
	})
}
