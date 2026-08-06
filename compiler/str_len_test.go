package compiler

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestCompileStrLenCodepoints regression-tests len(str) counting UTF-8 bytes
// instead of Unicode codepoints. String iteration was already
// codepoint-correct (strIter, builtins/host.go), so len disagreed with
// iterating the same string; strIndex/strSlice (compiler/runtime.go) were
// already codepoint-correct too. See docs/spec/06-builtins.md, strLenHost.
func TestCompileStrLenCodepoints(t *testing.T) {
	t.Run("ascii string length is unaffected", func(t *testing.T) {
		require.Equal(t, "3\n", run(t, "print(str(len(\"abc\")))\n"))
	})

	t.Run("empty string length is zero", func(t *testing.T) {
		require.Equal(t, "0\n", run(t, "print(str(len(\"\")))\n"))
	})

	t.Run("multi-byte codepoints count once each", func(t *testing.T) {
		require.Equal(t, "5\n", run(t, "print(str(len(\"h\\u00e9llo\")))\n"))
	})

	t.Run("len agrees with the number of iterations", func(t *testing.T) {
		src := "s: str = \"h\\u00e9llo\"\n" +
			"n: int = 0\n" +
			"for c in s:\n" +
			"    n = n + 1\n" +
			"print(str(len(s) == n))\n"
		require.Equal(t, "True\n", run(t, src))
	})

	t.Run("four-byte codepoints (emoji) count once each", func(t *testing.T) {
		require.Equal(t, "2\n", run(t, "print(str(len(\"\\U0001F389x\")))\n"))
	})
}
