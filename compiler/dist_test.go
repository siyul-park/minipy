package compiler

import (
	"testing"
	"testing/fstest"

	"github.com/siyul-park/minipy/token"
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
		if !ok {
			t.Fatal("expected PIL to resolve to a distribution")
		}
		if d.name != "Pillow" || d.version != "9.0.0" {
			t.Fatalf("got %q %q, want Pillow 9.0.0", d.name, d.version)
		}
	})

	t.Run("import resolves regardless of distribution name", func(t *testing.T) {
		m := ld.loadModule("PIL", token.Pos{})
		if m == nil {
			t.Fatal("import PIL failed")
		}
		if !m.isPackage {
			t.Fatal("PIL should be a package")
		}
	})

	t.Run("unknown import has no distribution", func(t *testing.T) {
		if _, ok := ld.distribution("nope"); ok {
			t.Fatal("unexpected distribution for unknown import")
		}
	})

	t.Run("dist-info without RECORD is ignored", func(t *testing.T) {
		bad := fstest.MapFS{
			"m.py":                     {Data: []byte("x = 1\n")},
			"m-1.0.dist-info/METADATA": {Data: []byte("Name: m\nVersion: 1.0\n\n")},
		}
		ldBad := newLoader(defaultRegistry(), []searchEntry{{fsys: bad, dir: "."}})
		if _, ok := ldBad.distribution("m"); ok {
			t.Fatal("dist-info lacking RECORD must be ignored")
		}
	})
}

func TestFinderPrecedence(t *testing.T) {
	// A source operator.py on the path must not shadow the native operator module
	// (CPython BuiltinImporter precedence).
	fsys := fstest.MapFS{"operator.py": {Data: []byte("add = 1\n")}}
	ld := newLoader(defaultRegistry(), []searchEntry{{fsys: fsys, dir: "."}})

	m := ld.loadModule("operator", token.Pos{})
	if m == nil {
		t.Fatal("import operator failed")
	}
	if !m.native {
		t.Fatal("native operator module should win over operator.py on the path")
	}
}
