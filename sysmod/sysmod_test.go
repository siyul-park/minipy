package sysmod

import (
	"testing"

	"github.com/siyul-park/minipy/module"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	mod := New()
	require.Equal(t, Name, mod.Name())

	expected := []string{
		"maxsize", "platform", "version", "byteorder",
		"getrecursionlimit", "exit",
	}
	names := mod.Names()
	require.Equal(t, expected, names)

	for _, name := range expected {
		sym, ok := mod.Symbol(name)
		require.True(t, ok, "missing symbol: %s", name)
		require.Equal(t, name, sym.Name())
	}
}

func TestConstants(t *testing.T) {
	mod := New()
	constants := []string{"maxsize", "platform", "version", "byteorder"}
	for _, name := range constants {
		sym, ok := mod.Symbol(name)
		require.True(t, ok)
		_, ok = sym.(module.ConstantSymbol)
		require.True(t, ok, "%s should satisfy ConstantSymbol", name)
	}
}

func TestCallables(t *testing.T) {
	mod := New()
	callables := []string{"getrecursionlimit", "exit"}
	for _, name := range callables {
		sym, ok := mod.Symbol(name)
		require.True(t, ok)
		_, ok = sym.(module.ConstantSymbol)
		require.False(t, ok, "%s should not be a ConstantSymbol", name)
	}
}
