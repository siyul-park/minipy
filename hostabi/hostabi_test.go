package hostabi

import (
	"math"
	"testing"

	"github.com/siyul-park/minivm/interp"
	"github.com/siyul-park/minivm/program"
	vmtypes "github.com/siyul-park/minivm/types"

	"github.com/stretchr/testify/require"
)

func TestPyFloat(t *testing.T) {
	sum := 0.1 + valueOf(0.2) // computed at runtime: keeps Go's constant folder
	// from evaluating 0.1+0.2 exactly, which would hide the double-rounding
	// error str(0.1+0.2) must show.
	tests := []struct {
		in   float64
		want string
	}{
		{1, "1.0"},
		{1.5, "1.5"},
		{0, "0.0"},
		{-2, "-2.0"},
		{100, "100.0"},
		{sum, "0.30000000000000004"},
		{1e16, "1e+16"},
		{1e-5, "1e-05"},
		{1e22, "1e+22"},
		{math.Copysign(0, -1), "-0.0"},
		{math.Inf(1), "inf"},
		{math.Inf(-1), "-inf"},
		{math.NaN(), "nan"},
	}
	for _, tt := range tests {
		require.Equalf(t, tt.want, PyFloat(tt.in), "PyFloat(%v)", tt.in)
	}
}

// valueOf defeats Go's untyped-constant arithmetic folding so a test input
// like 0.1+valueOf(0.2) is computed with float64 rounding at each step, the
// way runtime float addition works, rather than as one exact-then-rounded
// constant expression.
func valueOf(f float64) float64 { return f }

func TestBoxFloat(t *testing.T) {
	t.Run("passes ordinary floats through", func(t *testing.T) {
		require.Equal(t, vmtypes.KindF64, BoxFloat(1.5).Kind())
		require.Equal(t, 1.5, BoxFloat(1.5).F64())
		require.Equal(t, vmtypes.KindF64, BoxFloat(math.Inf(1)).Kind())
		require.Equal(t, vmtypes.KindF64, BoxFloat(math.Inf(-1)).Kind())
	})

	t.Run("canonicalizes a positive-signed NaN so it still reports KindF64", func(t *testing.T) {
		// math.NaN() (and every NaN strconv/the standard library produce) is
		// positive-signed, the exact shape minivm's Boxed representation
		// reserves to tag non-float kinds; boxing it unchanged would make
		// Kind() misreport it as some other kind.
		boxed := BoxFloat(math.NaN())
		require.Equal(t, vmtypes.KindF64, boxed.Kind())
		require.True(t, math.IsNaN(boxed.F64()))
	})

	t.Run("leaves an already negative-signed NaN as F64", func(t *testing.T) {
		boxed := BoxFloat(math.Copysign(math.NaN(), -1))
		require.Equal(t, vmtypes.KindF64, boxed.Kind())
		require.True(t, math.IsNaN(boxed.F64()))
	})
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
		vmtypes.NewArray(vmtypes.NewArrayType(vmtypes.TypeAny), stringA),
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
	require.True(t, it.Type().Equals(vmtypes.TypeAny))
	require.Equal(t, "items", it.String())
}

// TestAllocArrayRetainsElements guards the ownership contract described in
// the package doc comment: AllocArray must retain the (possibly borrowed)
// elements it is given, so the new array stays valid even after whatever
// released a source array that only ever borrowed those elements out — the
// exact sequence a host function sees when a list literal is passed inline
// as a call argument (an unrooted temporary the VM releases once the host
// call returns).
func TestAllocArrayRetainsElements(t *testing.T) {
	vm := interp.New(program.New(nil))
	defer vm.Close()

	strAddr, err := vm.Alloc(vmtypes.String("a"))
	require.NoError(t, err)
	elem := vmtypes.BoxRef(strAddr)

	srcType := vmtypes.NewArrayType(vmtypes.TypeAny)
	srcAddr, err := vm.Alloc(vmtypes.NewArray(srcType, elem))
	require.NoError(t, err)

	_, elems, err := ArrayElems(vm, vmtypes.BoxRef(srcAddr))
	require.NoError(t, err)
	require.Len(t, elems, 1)

	out, err := AllocArray(vm, srcType, elems)
	require.NoError(t, err)
	require.Len(t, out, 1)

	// Release the source array the way the minivm calling convention
	// releases a temporary argument once a host call returns without
	// reusing its box in the result.
	require.NoError(t, vm.Release(srcAddr))

	val, err := vm.Load(out[0].Ref())
	require.NoError(t, err)
	arr, ok := val.(*vmtypes.Array)
	require.True(t, ok)
	require.Len(t, arr.Elems, 1)

	s, err := LoadStr(vm, arr.Elems[0])
	require.NoError(t, err)
	require.Equal(t, "a", s)

	_, err = AllocArray(vm, srcType, []vmtypes.Boxed{vmtypes.BoxRef(999)})
	require.Error(t, err)
}

// TestRetainReleaseBoxes exercises RetainBoxes/ReleaseBoxes directly: scalar
// elements are left untouched, and a retained ref-kind element survives a
// release that would otherwise have reclaimed it.
func TestRetainReleaseBoxes(t *testing.T) {
	vm := interp.New(program.New(nil))
	defer vm.Close()

	addr, err := vm.Alloc(vmtypes.String("a"))
	require.NoError(t, err)
	ref := vmtypes.BoxRef(addr)
	values := []vmtypes.Boxed{vmtypes.BoxI64(1), ref}

	require.NoError(t, RetainBoxes(vm, values))
	// One release drops the extra retain; the string is still live.
	require.NoError(t, vm.Release(addr))
	_, err = vm.Load(addr)
	require.NoError(t, err)

	require.NoError(t, ReleaseBoxes(vm, values))
	_, err = vm.Load(addr)
	require.Error(t, err)
}
