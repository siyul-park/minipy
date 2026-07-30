package random

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	mod := New()
	require.Equal(t, Name, mod.Name())

	expected := []string{
		"random", "randint", "randrange", "uniform",
		"choice", "shuffle", "seed",
	}
	names := mod.Names()
	require.Equal(t, expected, names)

	for _, name := range expected {
		sym, ok := mod.Symbol(name)
		require.True(t, ok, "missing symbol: %s", name)
		require.Equal(t, name, sym.Name())
	}
}
