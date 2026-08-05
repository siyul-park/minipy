package conformance

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o644))
}

func TestLoad(t *testing.T) {
	t.Run("happy path with a plain and a divergent case", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, filepath.Join(root, "lang", "plain.py"), "print(\"hi\")\n")
		writeFile(t, filepath.Join(root, "lang", "plain.expected"), "hi\n")
		writeFile(t, filepath.Join(root, "divergent", "wrap.py"), `# minipy-divergence: int wraps at 64 bits
# minipy-divergence-doc: docs/coding-patterns.md
print("wrap")
`)
		writeFile(t, filepath.Join(root, "divergent", "wrap.expected"), "big\n")
		writeFile(t, filepath.Join(root, "divergent", "wrap.minipy"), "wrapped\n")

		cases, err := Load(root)
		require.NoError(t, err)
		require.Len(t, cases, 2)

		require.Equal(t, "divergent/wrap", cases[0].Name)
		require.Equal(t, "divergent", cases[0].Category)
		require.True(t, cases[0].Divergent)
		require.Equal(t, "int wraps at 64 bits", cases[0].DivergenceReason)
		require.Equal(t, "docs/coding-patterns.md", cases[0].DivergenceDoc)
		require.Equal(t, "big\n", cases[0].Expected)
		require.Equal(t, "wrapped\n", cases[0].MinipyExpected)

		require.Equal(t, "lang/plain", cases[1].Name)
		require.Equal(t, "lang", cases[1].Category)
		require.False(t, cases[1].Divergent)
		require.Empty(t, cases[1].DivergenceReason)
		require.Empty(t, cases[1].DivergenceDoc)
		require.Equal(t, "hi\n", cases[1].Expected)
	})

	t.Run("missing .expected golden", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, filepath.Join(root, "lang", "orphan.py"), "print(1)\n")

		_, err := Load(root)
		require.Error(t, err)
		require.ErrorContains(t, err, "missing sibling .expected golden")
	})

	t.Run("orphan .expected golden with no source", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, filepath.Join(root, "lang", "keep.py"), "print(1)\n")
		writeFile(t, filepath.Join(root, "lang", "keep.expected"), "1\n")
		writeFile(t, filepath.Join(root, "lang", "ghost.expected"), "1\n")

		_, err := Load(root)
		require.Error(t, err)
		require.ErrorContains(t, err, "orphan golden has no sibling .py source")
	})

	t.Run("orphan .minipy golden with no source", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, filepath.Join(root, "lang", "keep.py"), "print(1)\n")
		writeFile(t, filepath.Join(root, "lang", "keep.expected"), "1\n")
		writeFile(t, filepath.Join(root, "lang", "ghost.minipy"), "1\n")

		_, err := Load(root)
		require.Error(t, err)
		require.ErrorContains(t, err, "orphan golden has no sibling .py source")
	})

	t.Run("divergence directive without a .minipy golden", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, filepath.Join(root, "divergent", "wrap.py"), `# minipy-divergence: wraps
# minipy-divergence-doc: docs/coding-patterns.md
print("x")
`)
		writeFile(t, filepath.Join(root, "divergent", "wrap.expected"), "x\n")

		_, err := Load(root)
		require.Error(t, err)
		require.ErrorContains(t, err, "has a minipy-divergence directive but no sibling .minipy golden")
	})

	t.Run(".minipy golden without a divergence directive", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, filepath.Join(root, "divergent", "wrap.py"), "print(\"x\")\n")
		writeFile(t, filepath.Join(root, "divergent", "wrap.expected"), "x\n")
		writeFile(t, filepath.Join(root, "divergent", "wrap.minipy"), "x\n")

		_, err := Load(root)
		require.Error(t, err)
		require.ErrorContains(t, err, "has a .minipy golden but no minipy-divergence directive")
	})

	t.Run("divergence directive missing its doc pointer", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, filepath.Join(root, "divergent", "wrap.py"), `# minipy-divergence: wraps
print("x")
`)
		writeFile(t, filepath.Join(root, "divergent", "wrap.expected"), "x\n")
		writeFile(t, filepath.Join(root, "divergent", "wrap.minipy"), "x\n")

		_, err := Load(root)
		require.Error(t, err)
		require.ErrorContains(t, err, "has a minipy-divergence directive but no minipy-divergence-doc")
	})

	t.Run("doc pointer resolves to a file that does not exist", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, filepath.Join(root, "divergent", "wrap.py"), `# minipy-divergence: wraps
# minipy-divergence-doc: docs/does-not-exist.md
print("x")
`)
		writeFile(t, filepath.Join(root, "divergent", "wrap.expected"), "x\n")
		writeFile(t, filepath.Join(root, "divergent", "wrap.minipy"), "x\n")

		_, err := Load(root)
		require.Error(t, err)
		require.ErrorContains(t, err, "does not exist")
	})

	t.Run("doc pointer with an anchor fragment resolves without it", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, filepath.Join(root, "divergent", "wrap.py"), `# minipy-divergence: wraps
# minipy-divergence-doc: docs/coding-patterns.md#amendment-process
print("x")
`)
		writeFile(t, filepath.Join(root, "divergent", "wrap.expected"), "x\n")
		writeFile(t, filepath.Join(root, "divergent", "wrap.minipy"), "x\n")

		cases, err := Load(root)
		require.NoError(t, err)
		require.Len(t, cases, 1)
		require.Equal(t, "docs/coding-patterns.md#amendment-process", cases[0].DivergenceDoc)
	})
}
