package hostabi

import (
	"testing"

	"github.com/siyul-park/minivm/interp"
	"github.com/siyul-park/minivm/program"
	vmtypes "github.com/siyul-park/minivm/types"

	"github.com/stretchr/testify/require"
)

func TestPyFloat(t *testing.T) {
	tests := []struct {
		in   float64
		want string
	}{
		{1, "1.0"},
		{1.5, "1.5"},
		{0, "0.0"},
		{-2, "-2.0"},
		{100, "100.0"},
	}
	for _, tt := range tests {
		require.Equalf(t, tt.want, PyFloat(tt.in), "PyFloat(%v)", tt.in)
	}
}

func TestNewIterator(t *testing.T) {
	t.Run("empty is done", func(t *testing.T) {
		it := NewIterator("x", nil)
		require.True(t, it.Done())
	})

	t.Run("reports referenced values", func(t *testing.T) {
		it := NewIterator("x", []vmtypes.Boxed{vmtypes.BoxRef(3), vmtypes.BoxI64(1), vmtypes.BoxRef(7)})
		require.Equal(t, []vmtypes.Ref{1, 3, 7}, it.Refs([]vmtypes.Ref{1}))
	})

	t.Run("walks values then finishes", func(t *testing.T) {
		it := NewIterator("x", []vmtypes.Boxed{vmtypes.BoxI64(1), vmtypes.BoxI64(2)})
		require.False(t, it.Done())
		require.Equal(t, int64(1), it.Current().(vmtypes.Boxed).I64())
		require.True(t, it.Next())
		require.Equal(t, int64(2), it.Current().(vmtypes.Boxed).I64())
		require.False(t, it.Next())
		require.True(t, it.Done())
	})
}

func TestLoadValues(t *testing.T) {
	n, err := LoadI64(nil, vmtypes.BoxI64(7))
	require.NoError(t, err)
	require.Equal(t, int64(7), n)

	_, err = LoadI64(nil, vmtypes.BoxF64(1))
	require.ErrorIs(t, err, interp.ErrTypeMismatch)
	_, err = LoadStr(nil, vmtypes.BoxI64(1))
	require.ErrorIs(t, err, interp.ErrTypeMismatch)
	_, _, err = ArrayElems(nil, vmtypes.BoxI64(1))
	require.ErrorIs(t, err, interp.ErrTypeMismatch)
}

func TestHostValues(t *testing.T) {
	vm := interp.New(program.New(nil))
	defer vm.Close()

	alloc := func(value vmtypes.Value) vmtypes.Boxed {
		addr, err := vm.Alloc(value)
		require.NoError(t, err)
		return vmtypes.BoxRef(addr)
	}

	stringA := alloc(vmtypes.String("a"))
	stringA2 := alloc(vmtypes.String("a"))
	stringB := alloc(vmtypes.String("b"))
	require.Equal(t, "True", FormatScalar(vm, vmtypes.BoxI1(true)))
	require.Equal(t, "False", FormatScalar(vm, vmtypes.BoxI1(false)))
	require.Equal(t, "3", FormatScalar(vm, vmtypes.BoxI64(3)))
	require.Equal(t, "1.5", FormatScalar(vm, vmtypes.BoxF32(1.5)))
	require.Equal(t, "2.5", FormatScalar(vm, vmtypes.BoxF64(2.5)))
	require.Equal(t, "None", FormatScalar(vm, vmtypes.BoxedNull))
	require.Equal(t, "a", FormatScalar(vm, stringA))
	require.Equal(t, "None", FormatScalar(vm, vmtypes.BoxRef(999)))

	text, err := LoadStr(vm, stringA)
	require.NoError(t, err)
	require.Equal(t, "a", text)
	_, err = LoadStr(vm, alloc(vmtypes.I64(1)))
	require.ErrorIs(t, err, interp.ErrTypeMismatch)
	_, err = LoadStr(vm, vmtypes.BoxRef(999))
	require.Error(t, err)

	spilled := alloc(vmtypes.I64(9))
	n, err := LoadI64(vm, spilled)
	require.NoError(t, err)
	require.Equal(t, int64(9), n)
	_, err = LoadI64(vm, stringA)
	require.ErrorIs(t, err, interp.ErrTypeMismatch)
	_, err = LoadI64(vm, vmtypes.BoxedNull)
	require.ErrorIs(t, err, interp.ErrTypeMismatch)

	equal, err := BoxedEqual(vm, vmtypes.BoxI64(1), vmtypes.BoxF64(1))
	require.NoError(t, err)
	require.False(t, equal)
	equal, err = BoxedEqual(vm, vmtypes.BoxI64(1), vmtypes.BoxI64(1))
	require.NoError(t, err)
	require.True(t, equal)
	equal, err = BoxedEqual(vm, stringA, stringA)
	require.NoError(t, err)
	require.True(t, equal)
	equal, err = BoxedEqual(vm, stringA, stringA2)
	require.NoError(t, err)
	require.True(t, equal)
	equal, err = BoxedEqual(vm, stringA, stringB)
	require.NoError(t, err)
	require.False(t, equal)
	_, err = BoxedEqual(vm, stringA, vmtypes.BoxRef(999))
	require.Error(t, err)

	arrays := []vmtypes.Value{
		vmtypes.TypedArray[bool]{true},
		vmtypes.TypedArray[int8]{1},
		vmtypes.TypedArray[int32]{2},
		vmtypes.TypedArray[int64]{3},
		vmtypes.TypedArray[float32]{4},
		vmtypes.TypedArray[float64]{5},
		vmtypes.NewArray(vmtypes.NewArrayType(vmtypes.TypeRef), stringA),
	}
	for _, array := range arrays {
		typ, elems, err := ArrayElems(vm, alloc(array))
		require.NoError(t, err)
		require.NotNil(t, typ)
		require.Len(t, elems, 1)
	}
	_, _, err = ArrayElems(vm, stringA)
	require.ErrorIs(t, err, interp.ErrTypeMismatch)
	_, _, err = ArrayElems(vm, vmtypes.BoxRef(999))
	require.Error(t, err)

	it := NewIterator("items", []vmtypes.Boxed{stringA})
	require.Equal(t, vmtypes.KindRef, it.Kind())
	require.True(t, it.Type().Equals(vmtypes.TypeRef))
	require.Equal(t, "items", it.String())
}
