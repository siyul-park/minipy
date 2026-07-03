package builtins

import (
	"testing"

	"github.com/siyul-park/minipy/types"
)

func TestNew(t *testing.T) {
	m := New()
	if m.Name() != "builtins" {
		t.Fatalf("module name = %q", m.Name())
	}
	want := []string{"print", "str", "int", "float", "bool", "abs", "len",
		"enumerate", "zip", "range", "iter", "next", "isinstance"}
	for _, name := range want {
		if _, ok := m.Symbol(name); !ok {
			t.Errorf("missing symbol %q", name)
		}
	}
}

func TestExceptions(t *testing.T) {
	excs := Exceptions()
	base := make(map[string]string, len(excs))
	order := make(map[string]int, len(excs))
	for i, e := range excs {
		base[e.Name] = e.Base
		order[e.Name] = i
	}

	t.Run("hierarchy", func(t *testing.T) {
		if base["BaseException"] != "" {
			t.Error("BaseException must be the root")
		}
		if base["Exception"] != "BaseException" {
			t.Error("Exception must derive from BaseException")
		}
		for _, name := range []string{"ValueError", "TypeError", "IndexError", "KeyError", "StopIteration"} {
			if base[name] != "Exception" {
				t.Errorf("%s base = %q, want Exception", name, base[name])
			}
		}
	})

	t.Run("base precedes subclass", func(t *testing.T) {
		if order["BaseException"] > order["Exception"] || order["Exception"] > order["ValueError"] {
			t.Error("declaration order must list bases before subclasses")
		}
	})
}

func TestResultFuncs(t *testing.T) {
	list := types.NewList(types.Int)

	t.Run("len", func(t *testing.T) {
		if got, ok := lenResult([]types.Type{list}); !ok || !types.Equal(got, types.Int) {
			t.Fatalf("lenResult(list) = %s, %v", got, ok)
		}
		if _, ok := lenResult([]types.Type{types.Int}); ok {
			t.Fatal("len(int) should be rejected")
		}
	})

	t.Run("abs", func(t *testing.T) {
		if got, ok := absResult([]types.Type{types.Float}); !ok || !types.Equal(got, types.Float) {
			t.Fatalf("absResult(float) = %s, %v", got, ok)
		}
		if _, ok := absResult([]types.Type{types.Str}); ok {
			t.Fatal("abs(str) should be rejected")
		}
	})

	t.Run("range", func(t *testing.T) {
		if got, ok := rangeResult([]types.Type{types.Int, types.Int}); !ok || !types.Equal(got, types.NewIterator(types.Int)) {
			t.Fatalf("rangeResult = %s, %v", got, ok)
		}
		if _, ok := rangeResult([]types.Type{types.Str}); ok {
			t.Fatal("range(str) should be rejected")
		}
	})

	t.Run("enumerate", func(t *testing.T) {
		got, ok := enumerateResult([]types.Type{list})
		want := types.NewList(types.NewTuple(types.Int, types.Int))
		if !ok || !types.Equal(got, want) {
			t.Fatalf("enumerateResult = %s, %v", got, ok)
		}
	})

	t.Run("iter and next", func(t *testing.T) {
		it, ok := iterResult([]types.Type{list})
		if !ok || !types.Equal(it, types.NewIterator(types.Int)) {
			t.Fatalf("iterResult = %s, %v", it, ok)
		}
		if got, ok := nextResult([]types.Type{it}); !ok || !types.Equal(got, types.Int) {
			t.Fatalf("nextResult = %s, %v", got, ok)
		}
	})
}
