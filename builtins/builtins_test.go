package builtins

import (
	"testing"

	"github.com/siyul-park/minipy/types"

	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	m := New()
	require.Equal(t, "builtins", m.Name())
	want := []string{"print", "str", "int", "float", "bool", "abs", "len",
		"enumerate", "zip", "range", "iter", "next", "ord", "chr", "getattr", "hasattr", "isinstance"}
	for _, name := range want {
		_, ok := m.Symbol(name)
		require.Truef(t, ok, "missing symbol %q", name)
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
		require.Empty(t, base["BaseException"])
		require.Equal(t, "BaseException", base["Exception"])
		for _, name := range []string{"ValueError", "TypeError", "IndexError", "KeyError", "StopIteration"} {
			require.Equalf(t, "Exception", base[name], "%s base", name)
		}
	})

	t.Run("base precedes subclass", func(t *testing.T) {
		require.Less(t, order["BaseException"], order["Exception"])
		require.Less(t, order["Exception"], order["ValueError"])
	})
}

func TestResultFuncs(t *testing.T) {
	list := types.NewList(types.Int)

	t.Run("len", func(t *testing.T) {
		got, ok := lenResult([]types.Type{list})
		require.True(t, ok)
		require.Truef(t, types.Equal(got, types.Int), "lenResult(list) = %s", got)

		_, ok = lenResult([]types.Type{types.Int})
		require.False(t, ok)
	})

	t.Run("len bytes", func(t *testing.T) {
		got, ok := lenResult([]types.Type{types.Bytes})
		require.True(t, ok)
		require.Truef(t, types.Equal(got, types.Int), "lenResult(bytes) = %s", got)
	})

	t.Run("abs", func(t *testing.T) {
		got, ok := absResult([]types.Type{types.Float})
		require.True(t, ok)
		require.Truef(t, types.Equal(got, types.Float), "absResult(float) = %s", got)

		_, ok = absResult([]types.Type{types.Str})
		require.False(t, ok)
	})

	t.Run("range", func(t *testing.T) {
		got, ok := rangeResult([]types.Type{types.Int, types.Int})
		require.True(t, ok)
		require.Truef(t, types.Equal(got, types.NewIterator(types.Int)), "rangeResult = %s", got)

		_, ok = rangeResult([]types.Type{types.Str})
		require.False(t, ok)
	})

	t.Run("enumerate", func(t *testing.T) {
		got, ok := enumerateResult([]types.Type{list})
		want := types.NewList(types.NewTuple(types.Int, types.Int))
		require.True(t, ok)
		require.Truef(t, types.Equal(got, want), "enumerateResult = %s", got)
	})

	t.Run("iter and next", func(t *testing.T) {
		it, ok := iterResult([]types.Type{list})
		require.True(t, ok)
		require.Truef(t, types.Equal(it, types.NewIterator(types.Int)), "iterResult = %s", it)

		got, ok := nextResult([]types.Type{it})
		require.True(t, ok)
		require.Truef(t, types.Equal(got, types.Int), "nextResult = %s", got)
	})

	t.Run("iter bytes yields int", func(t *testing.T) {
		it, ok := iterResult([]types.Type{types.Bytes})
		require.True(t, ok)
		require.Truef(t, types.Equal(it, types.NewIterator(types.Int)), "iterResult(bytes) = %s", it)
	})

	t.Run("ord", func(t *testing.T) {
		got, ok := ordResult([]types.Type{types.Str})
		require.True(t, ok)
		require.Truef(t, types.Equal(got, types.Int), "ordResult = %s", got)

		_, ok = ordResult([]types.Type{types.Int})
		require.False(t, ok)
	})

	t.Run("chr", func(t *testing.T) {
		got, ok := chrResult([]types.Type{types.Int})
		require.True(t, ok)
		require.Truef(t, types.Equal(got, types.Str), "chrResult = %s", got)

		_, ok = chrResult([]types.Type{types.Str})
		require.False(t, ok)
	})
}
