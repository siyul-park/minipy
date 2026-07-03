package module_test

import (
	"io"
	"testing"

	"github.com/siyul-park/minipy/module"
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
		if _, ok := reg.Module("a"); !ok {
			t.Fatal("module a missing")
		}
		if !reg.Has("b") || reg.Has("c") {
			t.Fatal("Has mismatch")
		}
	})

	t.Run("Fallback", func(t *testing.T) {
		if reg.FallbackName() != "a" {
			t.Fatalf("fallback name = %q", reg.FallbackName())
		}
		m, ok := reg.Fallback()
		if !ok || m.Name() != "a" {
			t.Fatal("fallback module mismatch")
		}
	})

	t.Run("Symbol and SymbolByKey", func(t *testing.T) {
		if _, ok := reg.Symbol("a", "x"); !ok {
			t.Fatal("a.x missing")
		}
		if _, ok := reg.SymbolByKey("b.z"); !ok {
			t.Fatal("b.z missing")
		}
		if _, ok := reg.SymbolByKey("nodot"); ok {
			t.Fatal("unqualified key should not resolve")
		}
	})

	t.Run("FallbackSymbol", func(t *testing.T) {
		if _, ok := reg.FallbackSymbol("y"); !ok {
			t.Fatal("fallback symbol y missing")
		}
		if _, ok := reg.FallbackSymbol("z"); ok {
			t.Fatal("z is not in the fallback module")
		}
	})

	t.Run("Values default to intrinsics", func(t *testing.T) {
		vals := reg.Values(fakeRuntime{})
		v, ok := vals["a"]["x"]
		if !ok {
			t.Fatal("a.x has no value")
		}
		if _, ok := v.(module.Intrinsic); !ok {
			t.Fatalf("a.x value = %T, want module.Intrinsic", v)
		}
	})
}

func TestNewNative(t *testing.T) {
	m := newTestModule("m", "a", "b", "a")

	t.Run("Names deduplicated in order", func(t *testing.T) {
		names := m.Names()
		if len(names) != 2 || names[0] != "a" || names[1] != "b" {
			t.Fatalf("names = %v, want [a b]", names)
		}
	})

	t.Run("Symbol lookup", func(t *testing.T) {
		if _, ok := m.Symbol("a"); !ok {
			t.Fatal("a missing")
		}
		if _, ok := m.Symbol("z"); ok {
			t.Fatal("z should be absent")
		}
	})
}
