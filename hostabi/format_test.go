package hostabi

import (
	"bytes"
	"testing"

	pytypes "github.com/siyul-park/minipy/types"

	"github.com/siyul-park/minivm/interp"
	"github.com/siyul-park/minivm/program"
	vmtypes "github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestFormatValue(t *testing.T) {
	vm := interp.New(program.New(nil))
	defer vm.Close()

	alloc := func(value vmtypes.Value) vmtypes.Boxed {
		addr, err := vm.Alloc(value)
		require.NoError(t, err)
		return vmtypes.BoxRef(addr)
	}
	text := alloc(vmtypes.String("x"))

	tests := []struct {
		name  string
		value vmtypes.Boxed
		typ   pytypes.Type
		want  string
	}{
		{"int", vmtypes.BoxI64(7), pytypes.Int, "7"},
		{"float", vmtypes.BoxF64(1.5), pytypes.Float, "1.5"},
		{"bool", vmtypes.BoxI1(true), pytypes.Bool, "True"},
		{"string", text, pytypes.Str, "x"},
		{"none", vmtypes.BoxedNull, pytypes.None, "None"},
		{"any int", vmtypes.BoxI64(8), pytypes.Any, "8"},
		{"any string", text, pytypes.Any, "x"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := FormatValue(vm, tt.value, tt.typ)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}

	listType := pytypes.NewList(pytypes.Bool)
	list := alloc(vmtypes.TypedArray[bool]{true, false})
	got, err := FormatValue(vm, list, listType)
	require.NoError(t, err)
	require.Equal(t, "[True, False]", got)

	tupleType := pytypes.NewTuple(pytypes.Int, pytypes.Str)
	tuple := alloc(vmtypes.NewStruct(tupleType.VM().(*vmtypes.StructType), vmtypes.BoxI64(1), text))
	got, err = FormatValue(vm, tuple, tupleType)
	require.NoError(t, err)
	require.Equal(t, "(1, 'x')", got)

	dictType := pytypes.NewDict(pytypes.Int, pytypes.Str)
	dict := vmtypes.NewMapForType(dictType.VM().(*vmtypes.MapType), 2)
	dict.(*vmtypes.TypedMap[int64]).Set(2, alloc(vmtypes.String("b")))
	dict.(*vmtypes.TypedMap[int64]).Set(1, alloc(vmtypes.String("a")))
	got, err = FormatValue(vm, alloc(dict), dictType)
	require.NoError(t, err)
	require.Equal(t, "{1: 'a', 2: 'b'}", got)

	boolDictType := pytypes.NewDict(pytypes.Bool, pytypes.Int)
	boolDict := vmtypes.NewMapForType(boolDictType.VM().(*vmtypes.MapType), 2)
	boolDict.(*vmtypes.TypedMap[bool]).Set(true, vmtypes.BoxI64(1))
	boolDict.(*vmtypes.TypedMap[bool]).Set(false, vmtypes.BoxI64(0))
	got, err = FormatValue(vm, alloc(boolDict), boolDictType)
	require.NoError(t, err)
	require.Equal(t, "{False: 0, True: 1}", got)

	floatDictType := pytypes.NewDict(pytypes.Float, pytypes.Int)
	floatDict := vmtypes.NewMapForType(floatDictType.VM().(*vmtypes.MapType), 1)
	floatDict.(*vmtypes.TypedMap[float64]).Set(1.5, vmtypes.BoxI64(2))
	got, err = FormatValue(vm, alloc(floatDict), floatDictType)
	require.NoError(t, err)
	require.Equal(t, "{1.5: 2}", got)

	stringDictType := pytypes.NewDict(pytypes.Str, pytypes.Int)
	stringDict := vmtypes.NewMapForType(stringDictType.VM().(*vmtypes.MapType), 1).(*vmtypes.Map)
	stringKey := alloc(vmtypes.String("k"))
	stringDict.Set(
		vmtypes.MapKey{Kind: vmtypes.KindRef, Bits: uint64(stringKey.Ref())},
		vmtypes.MapEntry{Key: stringKey, Value: vmtypes.BoxI64(3)},
	)
	got, err = FormatValue(vm, alloc(stringDict), stringDictType)
	require.NoError(t, err)
	require.Equal(t, "{'k': 3}", got)

	setType := pytypes.NewSet(pytypes.Int)
	set := vmtypes.NewMapForType(setType.VM().(*vmtypes.MapType), 2)
	set.(*vmtypes.TypedMap[int64]).Set(2, vmtypes.BoxI1(true))
	set.(*vmtypes.TypedMap[int64]).Set(1, vmtypes.BoxI1(true))
	got, err = FormatValue(vm, alloc(set), setType)
	require.NoError(t, err)
	require.Equal(t, "{1, 2}", got)

	heapScalars := []struct {
		value vmtypes.Value
		typ   pytypes.Type
		want  string
	}{
		{vmtypes.I64(9), pytypes.Int, "9"},
		{vmtypes.F32(1.25), pytypes.Float, "1.25"},
		{vmtypes.F64(2.5), pytypes.Float, "2.5"},
		{vmtypes.I1(true), pytypes.Bool, "True"},
	}
	for _, tt := range heapScalars {
		got, err = FormatValue(vm, alloc(tt.value), tt.typ)
		require.NoError(t, err)
		require.Equal(t, tt.want, got)
	}

	intList := alloc(vmtypes.TypedArray[int64]{1, 2})
	got, err = FormatValue(vm, intList, pytypes.NewList(pytypes.Int))
	require.NoError(t, err)
	require.Equal(t, "[1, 2]", got)
	floatList := alloc(vmtypes.TypedArray[float64]{1.5, 2.5})
	got, err = FormatValue(vm, floatList, pytypes.NewList(pytypes.Float))
	require.NoError(t, err)
	require.Equal(t, "[1.5, 2.5]", got)
	stringList := alloc(vmtypes.NewArray(vmtypes.NewArrayType(vmtypes.TypeString), text))
	got, err = FormatValue(vm, stringList, pytypes.NewList(pytypes.Str))
	require.NoError(t, err)
	require.Equal(t, "['x']", got)

	oneTupleType := pytypes.NewTuple(pytypes.Int)
	oneTuple := alloc(vmtypes.NewStruct(oneTupleType.VM().(*vmtypes.StructType), vmtypes.BoxI64(1)))
	got, err = FormatValue(vm, oneTuple, oneTupleType)
	require.NoError(t, err)
	require.Equal(t, "(1,)", got)

	emptySetType := pytypes.NewSet(pytypes.Int)
	emptySet := vmtypes.NewMapForType(emptySetType.VM().(*vmtypes.MapType), 0)
	got, err = FormatValue(vm, alloc(emptySet), emptySetType)
	require.NoError(t, err)
	require.Equal(t, "set()", got)

	union := pytypes.NewUnion(pytypes.Str, pytypes.None)
	got, err = FormatValue(vm, text, union)
	require.NoError(t, err)
	require.Equal(t, "x", got)
	got, err = FormatValue(vm, vmtypes.BoxedNull, union)
	require.NoError(t, err)
	require.Equal(t, "None", got)

	_, err = FormatValue(vm, vmtypes.BoxRef(999), pytypes.Str)
	require.Error(t, err)

	invalid := []struct {
		value vmtypes.Boxed
		typ   pytypes.Type
	}{
		{text, pytypes.Int},
		{text, pytypes.Float},
		{text, pytypes.Bool},
		{vmtypes.BoxI64(1), pytypes.Str},
		{text, pytypes.NewList(pytypes.Int)},
		{text, pytypes.NewTuple(pytypes.Int)},
		{text, pytypes.NewDict(pytypes.Int, pytypes.Int)},
		{text, pytypes.NewSet(pytypes.Int)},
	}
	for _, tt := range invalid {
		_, err = FormatValue(vm, tt.value, tt.typ)
		require.Error(t, err)
	}
}

func TestFormatFunctions(t *testing.T) {
	vm := interp.New(program.New(nil))
	defer vm.Close()

	var out bytes.Buffer
	_, err := PrintFunction(&out, pytypes.Int).Fn(vm, []vmtypes.Boxed{vmtypes.BoxI64(3)})
	require.NoError(t, err)
	require.Equal(t, "3\n", out.String())

	values, err := StringFunction(pytypes.Bool).Fn(vm, []vmtypes.Boxed{vmtypes.BoxI1(false)})
	require.NoError(t, err)
	require.Len(t, values, 1)
	text, err := LoadStr(vm, values[0])
	require.NoError(t, err)
	require.Equal(t, "False", text)

	_, err = PrintFunction(&out, pytypes.Str).Fn(vm, []vmtypes.Boxed{vmtypes.BoxI64(1)})
	require.Error(t, err)
	_, err = StringFunction(pytypes.Str).Fn(vm, []vmtypes.Boxed{vmtypes.BoxI64(1)})
	require.Error(t, err)
}

func TestReprString(t *testing.T) {
	require.Equal(t, `'x'`, ReprString("x", false))
	require.Equal(t, `"'"`, ReprString("'", false))
	require.Equal(t, `'\\\n\t'`, ReprString("\\\n\t", false))
	require.Equal(t, `'\x01'`, ReprString("\x01", false))
	require.Equal(t, `'\uac00'`, ReprString("가", true))
	require.Equal(t, `'\U0001f600'`, ReprString("😀", true))
	require.Equal(t, `'가'`, ReprString("가", false))
}
