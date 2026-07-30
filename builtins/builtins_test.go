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
		"enumerate", "zip", "range", "iter", "next", "ord", "chr",
		"sorted", "reversed", "min", "max", "sum", "any", "all",
		"round", "divmod", "pow", "hex", "oct", "bin", "repr",
		"getattr", "hasattr", "isinstance"}
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
		require.Equal(t, "Exception", base["ArithmeticError"])
		require.Equal(t, "ArithmeticError", base["ZeroDivisionError"])
		require.Equal(t, "ArithmeticError", base["OverflowError"])
		require.Equal(t, "Exception", base["LookupError"])
		require.Equal(t, "LookupError", base["IndexError"])
		require.Equal(t, "LookupError", base["KeyError"])
		require.Equal(t, "Exception", base["NameError"])
		require.Equal(t, "NameError", base["UnboundLocalError"])
		for _, name := range []string{"ValueError", "TypeError", "StopIteration", "AssertionError", "RuntimeError"} {
			require.Equalf(t, "Exception", base[name], "%s base", name)
		}
	})

	t.Run("base precedes subclass", func(t *testing.T) {
		require.Less(t, order["BaseException"], order["Exception"])
		require.Less(t, order["Exception"], order["ArithmeticError"])
		require.Less(t, order["ArithmeticError"], order["ZeroDivisionError"])
		require.Less(t, order["Exception"], order["LookupError"])
		require.Less(t, order["LookupError"], order["IndexError"])
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

	t.Run("sorted", func(t *testing.T) {
		list := types.NewList(types.Int)
		got, ok := sortedResult([]types.Type{list})
		require.True(t, ok)
		require.Truef(t, types.Equal(got, list), "sortedResult = %s", got)

		_, ok = sortedResult([]types.Type{types.Int})
		require.False(t, ok)
	})

	t.Run("reversed", func(t *testing.T) {
		list := types.NewList(types.Str)
		got, ok := reversedResult([]types.Type{list})
		require.True(t, ok)
		require.Truef(t, types.Equal(got, list), "reversedResult = %s", got)

		_, ok = reversedResult([]types.Type{types.Int})
		require.False(t, ok)
	})

	t.Run("sum", func(t *testing.T) {
		got, ok := sumResult([]types.Type{types.NewList(types.Int)})
		require.True(t, ok)
		require.Truef(t, types.Equal(got, types.Int), "sumResult = %s", got)

		got, ok = sumResult([]types.Type{types.NewList(types.Float)})
		require.True(t, ok)
		require.Truef(t, types.Equal(got, types.Float), "sumResult = %s", got)

		_, ok = sumResult([]types.Type{types.NewList(types.Str)})
		require.False(t, ok)
	})

	t.Run("any all", func(t *testing.T) {
		got, ok := anyAllResult([]types.Type{types.NewList(types.Bool)})
		require.True(t, ok)
		require.Truef(t, types.Equal(got, types.Bool), "anyAllResult = %s", got)

		_, ok = anyAllResult([]types.Type{types.NewList(types.Int)})
		require.False(t, ok)
	})

	t.Run("round", func(t *testing.T) {
		got, ok := roundResult([]types.Type{types.Float})
		require.True(t, ok)
		require.Truef(t, types.Equal(got, types.Int), "roundResult(1 arg) = %s", got)

		got, ok = roundResult([]types.Type{types.Float, types.Int})
		require.True(t, ok)
		require.Truef(t, types.Equal(got, types.Float), "roundResult(2 args) = %s", got)

		_, ok = roundResult([]types.Type{types.Int})
		require.False(t, ok)
	})

	t.Run("divmod", func(t *testing.T) {
		got, ok := divmodResult([]types.Type{types.Int, types.Int})
		require.True(t, ok)
		require.Truef(t, types.Equal(got, types.NewTuple(types.Int, types.Int)), "divmodResult = %s", got)

		got, ok = divmodResult([]types.Type{types.Float, types.Float})
		require.True(t, ok)
		require.Truef(t, types.Equal(got, types.NewTuple(types.Float, types.Float)), "divmodResult = %s", got)

		_, ok = divmodResult([]types.Type{types.Int, types.Float})
		require.False(t, ok)
	})

	t.Run("pow", func(t *testing.T) {
		got, ok := powResult([]types.Type{types.Int, types.Int})
		require.True(t, ok)
		require.Truef(t, types.Equal(got, types.Int), "powResult(int,int) = %s", got)

		got, ok = powResult([]types.Type{types.Float, types.Float})
		require.True(t, ok)
		require.Truef(t, types.Equal(got, types.Float), "powResult(float,float) = %s", got)

		got, ok = powResult([]types.Type{types.Int, types.Float})
		require.True(t, ok)
		require.Truef(t, types.Equal(got, types.Float), "powResult(int,float) = %s", got)

		_, ok = powResult([]types.Type{types.Str, types.Int})
		require.False(t, ok)
	})

	t.Run("hex oct bin", func(t *testing.T) {
		got, ok := hexOctBinResult([]types.Type{types.Int})
		require.True(t, ok)
		require.Truef(t, types.Equal(got, types.Str), "hexOctBinResult = %s", got)

		_, ok = hexOctBinResult([]types.Type{types.Float})
		require.False(t, ok)
	})

	t.Run("repr", func(t *testing.T) {
		got, ok := reprResult([]types.Type{types.Str})
		require.True(t, ok)
		require.Truef(t, types.Equal(got, types.Str), "reprResult = %s", got)

		got, ok = reprResult([]types.Type{types.Int})
		require.True(t, ok)
		require.Truef(t, types.Equal(got, types.Str), "reprResult = %s", got)
	})
}
