package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRunFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "prog.py")
	require.NoError(t, os.WriteFile(path, []byte("x: int = 6\ny: int = 7\nprint(str(x * y))\n"), 0o644))

	var out bytes.Buffer
	require.NoError(t, runFile(path, &out))
	require.Equal(t, "42\n", out.String())
}

func TestRunFile_CompileError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.py")
	require.NoError(t, os.WriteFile(path, []byte("x = 5\n"), 0o644))

	var out bytes.Buffer
	err := runFile(path, &out)
	require.Error(t, err)
	require.Contains(t, err.Error(), "TypeError")
}

func TestRepl(t *testing.T) {
	t.Run("persists state and echoes bare expressions", func(t *testing.T) {
		in := strings.NewReader("x: int = 6\nprint(str(x * 7))\nx * 7\n")
		var out bytes.Buffer
		require.NoError(t, repl(in, &out))

		// once from the explicit print, once from the auto-echo.
		require.Equal(t, 2, strings.Count(out.String(), "42"))
	})

	t.Run("reports errors without crashing", func(t *testing.T) {
		in := strings.NewReader("y\n")
		var out bytes.Buffer
		require.NoError(t, repl(in, &out))
		require.Contains(t, out.String(), "NameError")
	})

	t.Run("runtime divide by zero maps to ZeroDivisionError", func(t *testing.T) {
		in := strings.NewReader("1 // 0\n")
		var out bytes.Buffer
		require.NoError(t, repl(in, &out))
		require.Contains(t, out.String(), "ZeroDivisionError")
	})
}

func TestRunFile_NotFound(t *testing.T) {
	require.Error(t, runFile(filepath.Join(t.TempDir(), "missing.py"), &bytes.Buffer{}))
}

func TestRootCmd(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "prog.py")
	require.NoError(t, os.WriteFile(path, []byte("print(str(2 + 3))\n"), 0o644))

	t.Run("run subcommand", func(t *testing.T) {
		var out bytes.Buffer
		cmd := newRootCmd()
		cmd.SetOut(&out)
		cmd.SetArgs([]string{"run", path})
		require.NoError(t, cmd.Execute())
		require.Equal(t, "5\n", out.String())
	})

	t.Run("bare file argument", func(t *testing.T) {
		var out bytes.Buffer
		cmd := newRootCmd()
		cmd.SetOut(&out)
		cmd.SetArgs([]string{path})
		require.NoError(t, cmd.Execute())
		require.Equal(t, "5\n", out.String())
	})

	t.Run("no arguments starts the REPL", func(t *testing.T) {
		var out bytes.Buffer
		cmd := newRootCmd()
		cmd.SetIn(strings.NewReader("2 + 3\n"))
		cmd.SetOut(&out)
		cmd.SetArgs([]string{})
		require.NoError(t, cmd.Execute())
		require.Contains(t, out.String(), "5")
	})
}
