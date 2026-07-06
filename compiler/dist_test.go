package compiler

import (
	"testing"
	"testing/fstest"

	"github.com/siyul-park/minipy/token"

	"github.com/stretchr/testify/require"
)

func TestDistIndex(t *testing.T) {
	// A site-packages-style root: the "Pillow" distribution provides the "PIL"
	// import package, so distribution name and import name differ.
	fsys := fstest.MapFS{
		"PIL/__init__.py": {Data: []byte("x = 1\n")},
		"Pillow-9.0.0.dist-info/METADATA": {Data: []byte(
			"Metadata-Version: 2.1\nName: Pillow\nVersion: 9.0.0\n\nUpstream body\n")},
		"Pillow-9.0.0.dist-info/RECORD":        {Data: []byte("PIL/__init__.py,,\n")},
		"Pillow-9.0.0.dist-info/top_level.txt": {Data: []byte("PIL\n")},
	}
	ld := newLoader(defaultRegistry(), []searchEntry{{fsys: fsys, dir: "."}})

	t.Run("maps import name to distribution", func(t *testing.T) {
		d, ok := ld.distribution("PIL")
		require.True(t, ok)
		require.Equal(t, "Pillow", d.name)
		require.Equal(t, "9.0.0", d.version)
	})

	t.Run("import resolves regardless of distribution name", func(t *testing.T) {
		m := ld.loadModule("PIL", token.Pos{})
		require.NotNil(t, m)
		require.True(t, m.isPackage)
	})

	t.Run("unknown import has no distribution", func(t *testing.T) {
		_, ok := ld.distribution("nope")
		require.False(t, ok)
	})

	t.Run("dist-info without RECORD is ignored", func(t *testing.T) {
		bad := fstest.MapFS{
			"m.py":                     {Data: []byte("x = 1\n")},
			"m-1.0.dist-info/METADATA": {Data: []byte("Name: m\nVersion: 1.0\n\n")},
		}
		ldBad := newLoader(defaultRegistry(), []searchEntry{{fsys: bad, dir: "."}})
		_, ok := ldBad.distribution("m")
		require.False(t, ok)
	})
}

func TestFinderPrecedence(t *testing.T) {
	// A source operator.py on the path must not shadow the native operator module
	// (CPython BuiltinImporter precedence).
	fsys := fstest.MapFS{"operator.py": {Data: []byte("add = 1\n")}}
	ld := newLoader(defaultRegistry(), []searchEntry{{fsys: fsys, dir: "."}})

	m := ld.loadModule("operator", token.Pos{})
	require.NotNil(t, m)
	require.True(t, m.native)
}
