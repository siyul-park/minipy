package hostabi

import (
	"math"

	"testing"

	"github.com/siyul-park/minivm/interp"
	"github.com/siyul-park/minivm/program"
	vmtypes "github.com/siyul-park/minivm/types"

	"github.com/stretchr/testify/require"
)

// newMap builds the representation minivm itself picks for a map with these key
// and value types, so each test exercises the concrete form real bytecode would
// produce rather than one chosen by the test.
func newMap(key, value vmtypes.Type) vmtypes.Value {
	return vmtypes.NewMapForType(vmtypes.NewMapType(key, value), 0)
}

func TestLoadMap(t *testing.T) {
	vm := interp.New(program.New(nil))
	defer vm.Close()

	t.Run("reads the map a reference points at", func(t *testing.T) {
		addr, err := vm.Alloc(newMap(vmtypes.TypeI64, vmtypes.TypeI64))
		require.NoError(t, err)

		loaded, err := LoadMap(vm, vmtypes.BoxRef(addr))
		require.NoError(t, err)
		require.IsType(t, &vmtypes.TypedMap[int64]{}, loaded)
	})

	t.Run("rejects a null reference", func(t *testing.T) {
		_, err := LoadMap(vm, vmtypes.BoxedNull)
		require.ErrorIs(t, err, interp.ErrTypeMismatch)
	})

	t.Run("rejects a scalar", func(t *testing.T) {
		_, err := LoadMap(vm, vmtypes.BoxI64(1))
		require.ErrorIs(t, err, interp.ErrTypeMismatch)
	})
}

func TestMapSet(t *testing.T) {
	vm := interp.New(program.New(nil))
	defer vm.Close()

	t.Run("stores under an int key", func(t *testing.T) {
		m := newMap(vmtypes.TypeI64, vmtypes.TypeI64)
		_, replaced, err := MapSet(vm, m, vmtypes.BoxI64(7), vmtypes.BoxI64(70))
		require.NoError(t, err)
		require.False(t, replaced)

		value, found, err := MapGet(vm, m, vmtypes.BoxI64(7))
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, vmtypes.BoxI64(70), value)
	})

	t.Run("reports the value a second store displaces", func(t *testing.T) {
		m := newMap(vmtypes.TypeI64, vmtypes.TypeI64)
		_, _, err := MapSet(vm, m, vmtypes.BoxI64(1), vmtypes.BoxI64(10))
		require.NoError(t, err)

		old, replaced, err := MapSet(vm, m, vmtypes.BoxI64(1), vmtypes.BoxI64(20))
		require.NoError(t, err)
		require.True(t, replaced)
		require.Equal(t, vmtypes.BoxI64(10), old)
	})

	t.Run("indexes a string key by content, not by reference", func(t *testing.T) {
		m := newMap(vmtypes.TypeString, vmtypes.TypeI64)
		stored, err := AllocString(vm, "k")
		require.NoError(t, err)
		_, _, err = MapSet(vm, m, stored[0], vmtypes.BoxI64(1))
		require.NoError(t, err)

		// A distinct heap string with equal content must reach the same entry.
		probe, err := AllocString(vm, "k")
		require.NoError(t, err)
		require.NotEqual(t, stored[0], probe[0])

		value, found, err := MapGet(vm, m, probe[0])
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, vmtypes.BoxI64(1), value)

		length, err := MapLen(m)
		require.NoError(t, err)
		require.Equal(t, 1, length)
	})

	t.Run("rejects a value that is not a map", func(t *testing.T) {
		_, _, err := MapSet(vm, vmtypes.String("x"), vmtypes.BoxI64(1), vmtypes.BoxI64(1))
		require.ErrorIs(t, err, interp.ErrTypeMismatch)
	})
}

func TestMapGet(t *testing.T) {
	vm := interp.New(program.New(nil))
	defer vm.Close()

	t.Run("reports a missing key", func(t *testing.T) {
		m := newMap(vmtypes.TypeI64, vmtypes.TypeI64)
		_, found, err := MapGet(vm, m, vmtypes.BoxI64(1))
		require.NoError(t, err)
		require.False(t, found)
	})
}

func TestMapDelete(t *testing.T) {
	vm := interp.New(program.New(nil))
	defer vm.Close()

	t.Run("removes a string key by content", func(t *testing.T) {
		m := newMap(vmtypes.TypeString, vmtypes.TypeI64)
		stored, err := AllocString(vm, "k")
		require.NoError(t, err)
		_, _, err = MapSet(vm, m, stored[0], vmtypes.BoxI64(1))
		require.NoError(t, err)

		probe, err := AllocString(vm, "k")
		require.NoError(t, err)
		value, found, err := MapDelete(vm, m, probe[0])
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, vmtypes.BoxI64(1), value)

		length, err := MapLen(m)
		require.NoError(t, err)
		require.Equal(t, 0, length)
	})

	t.Run("reports a missing key", func(t *testing.T) {
		m := newMap(vmtypes.TypeI64, vmtypes.TypeI64)
		_, found, err := MapDelete(vm, m, vmtypes.BoxI64(1))
		require.NoError(t, err)
		require.False(t, found)
	})
}

func TestMapClear(t *testing.T) {
	vm := interp.New(program.New(nil))
	defer vm.Close()

	t.Run("drops every entry", func(t *testing.T) {
		m := newMap(vmtypes.TypeI64, vmtypes.TypeI64)
		_, _, err := MapSet(vm, m, vmtypes.BoxI64(1), vmtypes.BoxI64(1))
		require.NoError(t, err)

		require.NoError(t, MapClear(m))
		length, err := MapLen(m)
		require.NoError(t, err)
		require.Equal(t, 0, length)
	})

	t.Run("rejects a value that is not a map", func(t *testing.T) {
		require.ErrorIs(t, MapClear(vmtypes.String("x")), interp.ErrTypeMismatch)
	})
}

func TestMapEntries(t *testing.T) {
	vm := interp.New(program.New(nil))
	defer vm.Close()

	t.Run("pairs keys with their values", func(t *testing.T) {
		m := newMap(vmtypes.TypeI64, vmtypes.TypeI64)
		_, _, err := MapSet(vm, m, vmtypes.BoxI64(2), vmtypes.BoxI64(20))
		require.NoError(t, err)

		keys, values, err := MapEntries(vm, m)
		require.NoError(t, err)
		require.Equal(t, []vmtypes.Boxed{vmtypes.BoxI64(2)}, keys)
		require.Equal(t, []vmtypes.Boxed{vmtypes.BoxI64(20)}, values)
		require.NoError(t, ReleaseBoxes(vm, keys))
	})

	t.Run("materializes a content-keyed map's key as a live string", func(t *testing.T) {
		m := newMap(vmtypes.TypeString, vmtypes.TypeI64)
		stored, err := AllocString(vm, "k")
		require.NoError(t, err)
		_, _, err = MapSet(vm, m, stored[0], vmtypes.BoxI64(1))
		require.NoError(t, err)

		keys, _, err := MapEntries(vm, m)
		require.NoError(t, err)
		require.Len(t, keys, 1)
		text, err := LoadStr(vm, keys[0])
		require.NoError(t, err)
		require.Equal(t, "k", text)

		// The key is owned by this call: releasing it is what balances the
		// reference MapEntries handed over.
		require.NoError(t, ReleaseBoxes(vm, keys))
	})

	t.Run("rejects a value that is not a map", func(t *testing.T) {
		_, _, err := MapEntries(vm, vmtypes.String("x"))
		require.ErrorIs(t, err, interp.ErrTypeMismatch)
	})
}

// TestMapKeyIdentity pins how a generic map indexes its keys, through the public
// map operations rather than the key builder behind them. The rules mirror the
// interpreter's own, so a key published by MAP_SET and a key published by a host
// function reach the same entry.
func TestMapKeyIdentity(t *testing.T) {
	vm := interp.New(program.New(nil))
	defer vm.Close()

	// A key type that is a reference but not a string is what makes minivm pick
	// the generic representation, the one that indexes by MapKey.
	generic := func(t *testing.T) vmtypes.Value {
		t.Helper()
		m := newMap(vmtypes.TypeAny, vmtypes.TypeI64)
		require.IsType(t, &vmtypes.Map{}, m, "this key type must select the generic representation")
		return m
	}

	sameEntry := func(t *testing.T, first, second vmtypes.Boxed) {
		t.Helper()
		m := generic(t)
		_, _, err := MapSet(vm, m, first, vmtypes.BoxI64(1))
		require.NoError(t, err)
		_, replaced, err := MapSet(vm, m, second, vmtypes.BoxI64(2))
		require.NoError(t, err)
		require.True(t, replaced, "the second key must reach the entry the first made")

		length, err := MapLen(m)
		require.NoError(t, err)
		require.Equal(t, 1, length)
	}

	t.Run("i1 indexes through its i32 form", func(t *testing.T) {
		sameEntry(t, vmtypes.BoxI1(true), vmtypes.BoxI32(1))
	})

	t.Run("negative zero indexes as zero", func(t *testing.T) {
		sameEntry(t, vmtypes.BoxF64(negZero()), vmtypes.BoxF64(0))
	})

	t.Run("a string indexes by content", func(t *testing.T) {
		first, err := AllocString(vm, "k")
		require.NoError(t, err)
		second, err := AllocString(vm, "k")
		require.NoError(t, err)
		require.NotEqual(t, first[0], second[0], "the two strings must be distinct heap values")

		sameEntry(t, first[0], second[0])
	})

	t.Run("a heap-spilled int indexes by its value", func(t *testing.T) {
		first, err := BoxInt(vm, 1<<60)
		require.NoError(t, err)
		second, err := BoxInt(vm, 1<<60)
		require.NoError(t, err)
		require.Equal(t, vmtypes.KindRef, first.Kind())
		require.NotEqual(t, first, second, "the two ints must be distinct heap values")

		sameEntry(t, first, second)
	})

	t.Run("any other reference indexes by heap address", func(t *testing.T) {
		m := generic(t)
		first, err := vm.Alloc(newMap(vmtypes.TypeI64, vmtypes.TypeI64))
		require.NoError(t, err)
		second, err := vm.Alloc(newMap(vmtypes.TypeI64, vmtypes.TypeI64))
		require.NoError(t, err)

		_, _, err = MapSet(vm, m, vmtypes.BoxRef(first), vmtypes.BoxI64(1))
		require.NoError(t, err)
		_, replaced, err := MapSet(vm, m, vmtypes.BoxRef(second), vmtypes.BoxI64(2))
		require.NoError(t, err)
		require.False(t, replaced, "two distinct references are two entries")

		length, err := MapLen(m)
		require.NoError(t, err)
		require.Equal(t, 2, length)
	})
}

// negZero returns -0.0 without letting Go's constant folder collapse it to 0.
func negZero() float64 { return math.Copysign(0, -1) }
