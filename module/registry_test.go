package module_test

import (
	"io"
	"testing"

	"github.com/siyul-park/minipy/module"

	"github.com/stretchr/testify/require"
)

type fakeRuntime struct{}

func (fakeRuntime) Out() io.Writer { return io.Discard }

func newTestModule(name string, symbols ...string) module.Module {
	syms := make([]module.Symbol, len(symbols))
	for i, s := range symbols {
		syms[i] = module.NewSymbol(name, s, nil, nil, nil)
	}
	return module.NewNative(name, syms...)
}

func TestRegistry(t *testing.T) {
	a := newTestModule("a", "x", "y")
	b := newTestModule("b", "z")
	reg := module.NewRegistry([]module.Module{a, b}, module.WithFallback("a"))

	t.Run("Module and Has", func(t *testing.T) {
		_, ok := reg.Module("a")
		require.True(t, ok)
		require.True(t, reg.Has("b"))
		require.False(t, reg.Has("c"))
	})

	t.Run("Fallback", func(t *testing.T) {
		require.Equal(t, "a", reg.FallbackName())
		m, ok := reg.Fallback()
		require.True(t, ok)
		require.Equal(t, "a", m.Name())
	})

	t.Run("Symbol and SymbolByKey", func(t *testing.T) {
		_, ok := reg.Symbol("a", "x")
		require.True(t, ok)
		_, ok = reg.SymbolByKey("b.z")
		require.True(t, ok)
		_, ok = reg.SymbolByKey("nodot")
		require.False(t, ok)
	})

	t.Run("FallbackSymbol", func(t *testing.T) {
		_, ok := reg.FallbackSymbol("y")
		require.True(t, ok)
		_, ok = reg.FallbackSymbol("z")
		require.False(t, ok)
	})

	t.Run("Values default to intrinsics", func(t *testing.T) {
		vals := reg.Values(fakeRuntime{})
		v, ok := vals["a"]["x"]
		require.True(t, ok)
		require.IsType(t, module.Intrinsic{}, v)
	})
}

func TestNewNative(t *testing.T) {
	m := newTestModule("m", "a", "b", "a")

	t.Run("Names deduplicated in order", func(t *testing.T) {
		names := m.Names()
		require.Equal(t, []string{"a", "b"}, names)
	})

	t.Run("Symbol lookup", func(t *testing.T) {
		_, ok := m.Symbol("a")
		require.True(t, ok)
		_, ok = m.Symbol("z")
		require.False(t, ok)
	})
}
