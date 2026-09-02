package codegen

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoad(t *testing.T) {
	write := func(t *testing.T, root, name, content string) {
		t.Helper()
		path := filepath.Join(root, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}

	t.Run("pairs a source with its golden", func(t *testing.T) {
		root := t.TempDir()
		write(t, root, "loops/while_sum.py", "x: int = 1\n")
		write(t, root, "loops/while_sum.masm", ".code\n")

		cases, err := Load(root)
		require.NoError(t, err)
		require.Len(t, cases, 1)
		require.Equal(t, "loops/while_sum", cases[0].Name)
		require.Equal(t, "loops", cases[0].Category)
		require.Equal(t, ".code\n", cases[0].Expected)
	})

	t.Run("accepts a new source with no golden yet", func(t *testing.T) {
		root := t.TempDir()
		write(t, root, "loops/while_sum.py", "x: int = 1\n")

		cases, err := Load(root)
		require.NoError(t, err)
		require.Len(t, cases, 1)
		require.Empty(t, cases[0].Expected)
	})

	t.Run("rejects a golden whose source is gone", func(t *testing.T) {
		root := t.TempDir()
		write(t, root, "loops/renamed.masm", ".code\n")

		_, err := Load(root)
		require.ErrorContains(t, err, "golden has no sibling .py source")
	})

	t.Run("sorts cases by name", func(t *testing.T) {
		root := t.TempDir()
		write(t, root, "loops/b.py", "x: int = 1\n")
		write(t, root, "calls/a.py", "x: int = 1\n")

		cases, err := Load(root)
		require.NoError(t, err)
		require.Equal(t, []string{"calls/a", "loops/b"}, []string{cases[0].Name, cases[1].Name})
	})

	t.Run("reports a missing root", func(t *testing.T) {
		_, err := Load(filepath.Join(t.TempDir(), "absent"))
		require.Error(t, err)
	})
}
