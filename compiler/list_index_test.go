package compiler

import (
	"errors"
	"testing"

	"github.com/siyul-park/minivm/interp"
	"github.com/stretchr/testify/require"
)

func TestCompileListNegativeIndex(t *testing.T) {
	t.Run("read last element", func(t *testing.T) {
		src := "xs: list[int] = [10, 20, 30]\nprint(str(xs[-1]))\n"
		require.Equal(t, "30\n", run(t, src))
	})

	t.Run("read with index beyond first element", func(t *testing.T) {
		src := "xs: list[int] = [10, 20, 30]\nprint(str(xs[-3]))\n"
		require.Equal(t, "10\n", run(t, src))
	})

	t.Run("plain assignment writes the last element", func(t *testing.T) {
		src := "xs: list[int] = [10, 20, 30]\nxs[-1] = 99\nprint(str(xs[0]) + \" \" + str(xs[1]) + \" \" + str(xs[2]))\n"
		require.Equal(t, "10 20 99\n", run(t, src))
	})

	t.Run("chained assignment writes through a negative index", func(t *testing.T) {
		src := "xs: list[int] = [1, 2, 3]\nys: list[int] = [0, 0, 0]\nxs[-1] = ys[-1] = 7\n" +
			"print(str(xs[2]) + \" \" + str(ys[2]))\n"
		require.Equal(t, "7 7\n", run(t, src))
	})

	t.Run("augmented assignment reads and writes the same negative slot", func(t *testing.T) {
		src := "xs: list[int] = [1, 2, 3]\nxs[-1] += 10\nprint(str(xs[2]))\n"
		require.Equal(t, "13\n", run(t, src))
	})

	t.Run("negative index on str list", func(t *testing.T) {
		src := "xs: list[str] = [\"a\", \"b\", \"c\"]\nprint(xs[-2])\n"
		require.Equal(t, "b\n", run(t, src))
	})

	t.Run("read out of range after normalization traps", func(t *testing.T) {
		src := "xs: list[int] = [1, 2, 3]\nz = xs[-4]\n"
		err := runError(t, src)
		require.True(t, errors.Is(err, interp.ErrIndexOutOfRange))
	})

	t.Run("write out of range after normalization traps", func(t *testing.T) {
		src := "xs: list[int] = [1, 2, 3]\nxs[-4] = 9\n"
		err := runError(t, src)
		require.True(t, errors.Is(err, interp.ErrIndexOutOfRange))
	})
}
