package compiler

import (
	"testing"
	"testing/fstest"

	"github.com/siyul-park/minipy/token"
	"github.com/stretchr/testify/require"
)

func TestFinderPrecedence(t *testing.T) {
	// A source operator.py on the path must not shadow the native operator module
	// (CPython BuiltinImporter precedence).
	fsys := fstest.MapFS{"operator.py": {Data: []byte("add = 1\n")}}
	ld := newLoader(defaultRegistry(), []searchEntry{{fsys: fsys, dir: "."}})

	m := ld.loadModule("operator", token.Pos{})
	require.NotNil(t, m)
	require.True(t, m.native)
}
