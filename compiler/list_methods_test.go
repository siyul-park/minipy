package compiler

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCompileListMethods(t *testing.T) {
	t.Run("sort int list", func(t *testing.T) {
		src := "xs: list[int] = [3, 1, 2]\n" +
			"xs.sort()\n" +
			"print(str(xs[0]) + \" \" + str(xs[1]) + \" \" + str(xs[2]))\n"
		require.Equal(t, "1 2 3\n", run(t, src))
	})

	t.Run("sort float list", func(t *testing.T) {
		src := "xs: list[float] = [3.2, 1.1, 2.5]\n" +
			"xs.sort()\n" +
			"print(str(xs[0]) + \" \" + str(xs[1]) + \" \" + str(xs[2]))\n"
		require.Equal(t, "1.1 2.5 3.2\n", run(t, src))
	})

	t.Run("sort str list", func(t *testing.T) {
		src := "xs: list[str] = [\"banana\", \"apple\", \"cherry\"]\n" +
			"xs.sort()\n" +
			"print(xs[0] + \" \" + xs[1] + \" \" + xs[2])\n"
		require.Equal(t, "apple banana cherry\n", run(t, src))
	})

	t.Run("sort bool list", func(t *testing.T) {
		src := "xs: list[bool] = [True, False, True, False]\n" +
			"xs.sort()\n" +
			"print(str(xs[0]) + \" \" + str(xs[1]) + \" \" + str(xs[2]) + \" \" + str(xs[3]))\n"
		require.Equal(t, "False False True True\n", run(t, src))
	})

	t.Run("copy returns new list", func(t *testing.T) {
		src := "xs: list[int] = [1, 2, 3]\n" +
			"ys: list[int] = xs.copy()\n" +
			"ys.append(4)\n" +
			"print(str(len(xs)) + \" \" + str(len(ys)))\n"
		require.Equal(t, "3 4\n", run(t, src))
	})

	t.Run("count occurrences", func(t *testing.T) {
		src := "xs: list[int] = [1, 2, 1, 3, 1]\n" +
			"print(str(xs.count(1)) + \" \" + str(xs.count(2)) + \" \" + str(xs.count(4)))\n"
		require.Equal(t, "3 1 0\n", run(t, src))
	})

	t.Run("clear empties the list", func(t *testing.T) {
		src := "xs: list[int] = [1, 2, 3]\n" +
			"xs.clear()\n" +
			"print(str(len(xs)))\n"
		require.Equal(t, "0\n", run(t, src))
	})

	t.Run("remove first occurrence", func(t *testing.T) {
		src := "xs: list[int] = [1, 2, 3, 2]\n" +
			"xs.remove(2)\n" +
			"print(str(len(xs)) + \" \" + str(xs[0]) + \" \" + str(xs[1]) + \" \" + str(xs[2]))\n"
		require.Equal(t, "3 1 3 2\n", run(t, src))
	})

	t.Run("remove raises ValueError when not found", func(t *testing.T) {
		src := "xs: list[int] = [1, 2, 3]\n" +
			"try:\n" +
			"    xs.remove(5)\n" +
			"except ValueError:\n" +
			"    print(\"not found\")\n"
		require.Equal(t, "not found\n", run(t, src))
	})

	t.Run("sort empty list", func(t *testing.T) {
		src := "xs: list[int] = []\n" +
			"xs.sort()\n" +
			"print(str(len(xs)))\n"
		require.Equal(t, "0\n", run(t, src))
	})

	t.Run("copy empty list", func(t *testing.T) {
		src := "xs: list[int] = []\n" +
			"ys: list[int] = xs.copy()\n" +
			"print(str(len(ys)))\n"
		require.Equal(t, "0\n", run(t, src))
	})

	t.Run("count on empty list", func(t *testing.T) {
		src := "xs: list[int] = []\n" +
			"print(str(xs.count(1)))\n"
		require.Equal(t, "0\n", run(t, src))
	})
}
