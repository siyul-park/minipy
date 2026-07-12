package module_test

import (
	"io"
	"testing"

	"github.com/siyul-park/minipy/ast"
	"github.com/siyul-park/minipy/module"
	"github.com/siyul-park/minipy/token"
	"github.com/siyul-park/minipy/types"

	vmtypes "github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

type fakeRuntime struct{}

func (fakeRuntime) Out() io.Writer { return io.Discard }

func newTestModule(name string, symbols ...string) module.Module {
	syms := make([]module.Symbol, len(symbols))
	for i, symbol := range symbols {
		syms[i] = module.NewSymbol(symbol,
			func(module.Checker, []ast.Expr, token.Pos) types.Type { return types.None },
			func(module.Emitter, []ast.Expr) {},
			nil,
		)
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

	t.Run("Values omit inline-only symbols", func(t *testing.T) {
		vals := reg.Values(fakeRuntime{})
		_, ok := vals["a"]["x"]
		require.False(t, ok)
	})

	t.Run("Values include runtime symbols", func(t *testing.T) {
		runtime := module.NewNative("runtime", module.NewSymbol("value",
			func(module.Checker, []ast.Expr, token.Pos) types.Type { return types.Str },
			func(module.Emitter, []ast.Expr) {},
			func(module.Runtime) vmtypes.Value { return vmtypes.String("ok") },
		))
		values := module.NewRegistry([]module.Module{runtime}).Values(fakeRuntime{})
		require.Equal(t, vmtypes.String("ok"), values["runtime"]["value"])
	})

	t.Run("invalid registry panics", func(t *testing.T) {
		require.Panics(t, func() { module.NewRegistry([]module.Module{a, a}) })
		require.Panics(t, func() {
			module.NewRegistry([]module.Module{a}, module.WithFallback("missing"))
		})
	})
}

func TestNewNative(t *testing.T) {
	m := newTestModule("m", "a", "b")

	t.Run("Names preserve registration order", func(t *testing.T) {
		require.Equal(t, []string{"a", "b"}, m.Names())
	})

	t.Run("Symbol lookup", func(t *testing.T) {
		_, ok := m.Symbol("a")
		require.True(t, ok)
		_, ok = m.Symbol("z")
		require.False(t, ok)
	})

	t.Run("duplicate symbol panics", func(t *testing.T) {
		require.Panics(t, func() { newTestModule("m", "a", "a") })
	})

	t.Run("invalid definitions panic", func(t *testing.T) {
		require.Panics(t, func() { module.NewNative("") })
		require.Panics(t, func() { module.NewSymbol("", nil, nil, nil) })
		require.Panics(t, func() { module.NewSymbol("x", nil, func(module.Emitter, []ast.Expr) {}, nil) })
		require.Panics(t, func() {
			module.NewSymbol("x", func(module.Checker, []ast.Expr, token.Pos) types.Type { return types.None }, nil, nil)
		})
	})
}
