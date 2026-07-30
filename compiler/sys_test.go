package compiler

import (
	"strings"
	"testing"

	"github.com/siyul-park/minipy/token"
	"github.com/stretchr/testify/require"
)

func TestSysModule(t *testing.T) {
	t.Run("maxsize constant", func(t *testing.T) {
		src := "import sys\nprint(str(sys.maxsize))\n"
		require.Equal(t, "9223372036854775807\n", run(t, src))
	})

	t.Run("platform constant", func(t *testing.T) {
		src := "import sys\nprint(sys.platform)\n"
		out := run(t, src)
		out = strings.TrimSpace(out)
		require.NotEmpty(t, out)
	})

	t.Run("version constant", func(t *testing.T) {
		src := "import sys\nprint(sys.version)\n"
		require.Equal(t, "0.1.0 (minipy)\n", run(t, src))
	})

	t.Run("byteorder constant", func(t *testing.T) {
		src := "import sys\nprint(sys.byteorder)\n"
		require.Equal(t, "little\n", run(t, src))
	})

	t.Run("getrecursionlimit returns 1000", func(t *testing.T) {
		src := "import sys\nprint(str(sys.getrecursionlimit()))\n"
		require.Equal(t, "1000\n", run(t, src))
	})

	t.Run("from import maxsize", func(t *testing.T) {
		src := "from sys import maxsize\nprint(str(maxsize))\n"
		require.Equal(t, "9223372036854775807\n", run(t, src))
	})

	t.Run("from import getrecursionlimit", func(t *testing.T) {
		src := "from sys import getrecursionlimit\nprint(str(getrecursionlimit()))\n"
		require.Equal(t, "1000\n", run(t, src))
	})

	t.Run("from import byteorder", func(t *testing.T) {
		src := "from sys import byteorder\nprint(byteorder)\n"
		require.Equal(t, "little\n", run(t, src))
	})
}

func TestSysModuleErrors(t *testing.T) {
	t.Run("getrecursionlimit with arguments", func(t *testing.T) {
		src := "import sys\nsys.getrecursionlimit(42)\n"
		_, err := Compile(strings.NewReader(src))
		require.Error(t, err)
		code(t, err, token.ArityMismatch)
	})

	t.Run("exit with wrong type", func(t *testing.T) {
		src := "import sys\nsys.exit(\"x\")\n"
		_, err := Compile(strings.NewReader(src))
		require.Error(t, err)
		code(t, err, token.TypeMismatch)
	})

	t.Run("exit with no arguments", func(t *testing.T) {
		src := "import sys\nsys.exit()\n"
		_, err := Compile(strings.NewReader(src))
		require.Error(t, err)
		code(t, err, token.ArityMismatch)
	})
}
