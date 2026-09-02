package codegen

import (
	"io"
	"strings"
	"testing"

	"github.com/siyul-park/minipy/compiler"
	"github.com/siyul-park/minivm/optimize"
	"github.com/siyul-park/minivm/program"
	"github.com/stretchr/testify/require"
)

func TestFormat(t *testing.T) {
	t.Run("heads the listing with the program's metrics", func(t *testing.T) {
		listing := Format(compile(t, "x: int = 1\n"))
		require.True(t, strings.HasPrefix(listing, "# instructions"), listing)
		require.Contains(t, listing, "\n.code\n")
	})

	t.Run("keeps a host function on the line its index labels", func(t *testing.T) {
		// A host function's own String() spans two lines and repeats its
		// signature, which is why this renderer exists.
		listing := Format(compile(t, "print(\"x\")\n"))
		require.Contains(t, listing, "<host> func(string)\n")
		require.NotContains(t, listing, "<native>")
	})

	t.Run("disassembles a function constant under its signature", func(t *testing.T) {
		src := "def add(a: int, b: int) -> int:\n    return a + b\nprint(str(add(1, 2)))\n"
		listing := Format(compile(t, src))
		require.Contains(t, listing, "func(i64, i64) i64\n")
		require.Contains(t, listing, "\ti64.add\n")
	})
}

func TestMeasure(t *testing.T) {
	t.Run("counts a function's instructions with the entry code's", func(t *testing.T) {
		bare := Measure(compile(t, "x: int = 1\n"))
		src := "def add(a: int, b: int) -> int:\n    return a + b\nx: int = add(1, 2)\n"
		withFunction := Measure(compile(t, src))

		require.Equal(t, 0, bare.Functions)
		require.Equal(t, 1, withFunction.Functions)
		require.Greater(t, withFunction.Instructions, bare.Instructions)
	})

	t.Run("counts declared global slots", func(t *testing.T) {
		require.Equal(t, 2, Measure(compile(t, "a: int = 1\nb: int = 2\n")).Globals)
	})

	t.Run("counts host functions apart from other constants", func(t *testing.T) {
		metrics := Measure(compile(t, "print(\"x\")\n"))
		require.Equal(t, 1, metrics.HostFunctions)
		require.GreaterOrEqual(t, metrics.Constants, metrics.HostFunctions)
	})
}

func compile(t *testing.T, source string) *program.Program {
	t.Helper()
	prog, err := compiler.Compile(strings.NewReader(source),
		compiler.WithOutput(io.Discard), compiler.WithOptimizationLevel(optimize.O0))
	require.NoError(t, err)
	return prog
}
