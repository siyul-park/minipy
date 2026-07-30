package compiler

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCompileAugAssignSubscript(t *testing.T) {
	t.Run("list element increment", func(t *testing.T) {
		src := "xs: list[int] = [1, 2, 3]\nxs[0] += 10\nprint(str(xs[0]))\n"
		require.Equal(t, "11\n", run(t, src))
	})

	t.Run("list element multiply", func(t *testing.T) {
		src := "xs: list[int] = [2, 3, 4]\nxs[1] *= 5\nprint(str(xs[1]))\n"
		require.Equal(t, "15\n", run(t, src))
	})

	t.Run("list element subtract", func(t *testing.T) {
		src := "xs: list[int] = [10, 20, 30]\nxs[2] -= 7\nprint(str(xs[2]))\n"
		require.Equal(t, "23\n", run(t, src))
	})

	t.Run("dict value increment", func(t *testing.T) {
		src := "d: dict[str, int] = {\"a\": 1}\nd[\"a\"] += 5\nprint(str(d[\"a\"]))\n"
		require.Equal(t, "6\n", run(t, src))
	})

	t.Run("dict value multiply", func(t *testing.T) {
		src := "d: dict[str, int] = {\"x\": 3}\nd[\"x\"] *= 4\nprint(str(d[\"x\"]))\n"
		require.Equal(t, "12\n", run(t, src))
	})

	t.Run("dict value subtract", func(t *testing.T) {
		src := "d: dict[str, int] = {\"k\": 100}\nd[\"k\"] -= 25\nprint(str(d[\"k\"]))\n"
		require.Equal(t, "75\n", run(t, src))
	})

	t.Run("list element with variable index", func(t *testing.T) {
		src := "xs: list[int] = [1, 2, 3]\ni: int = 1\nxs[i] += 10\nprint(str(xs[1]))\n"
		require.Equal(t, "12\n", run(t, src))
	})

	t.Run("dict int key increment", func(t *testing.T) {
		src := "d: dict[int, int] = {0: 10, 1: 20}\nd[0] += 5\nprint(str(d[0]))\n"
		require.Equal(t, "15\n", run(t, src))
	})
}
