package types

import (
	"testing"

	vmtypes "github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestType_String(t *testing.T) {
	require.Equal(t, "int", Int.String())
	require.Equal(t, "float", Float.String())
	require.Equal(t, "bool", Bool.String())
	require.Equal(t, "str", Str.String())
	require.Equal(t, "None", None.String())
	require.Equal(t, "Point", ClassOf("Point", []Field{{Name: "x", Type: Int}}).String())
	require.Equal(t, "Iterator[int]", IteratorOf(Int).String())
	require.Equal(t, "<invalid>", Invalid.String())
}

func TestType_IsNumeric(t *testing.T) {
	require.True(t, Int.IsNumeric())
	require.True(t, Float.IsNumeric())
	require.False(t, Bool.IsNumeric())
	require.False(t, Str.IsNumeric())
}

func TestType_VM(t *testing.T) {
	require.Equal(t, vmtypes.TypeI64, Int.VM())
	require.Equal(t, vmtypes.TypeF64, Float.VM())
	require.Equal(t, vmtypes.TypeI1, Bool.VM())
	require.Equal(t, vmtypes.TypeString, Str.VM())
	require.Equal(t, vmtypes.TypeRef, None.VM())
	require.Equal(t, vmtypes.TypeRef, IteratorOf(Int).VM())
	require.IsType(t, &vmtypes.StructType{}, ClassOf("Point", []Field{{Name: "x", Type: Int}}).VM())
	require.Nil(t, Invalid.VM())
}

func TestAssignable(t *testing.T) {
	require.True(t, AssignableTo(Int, Int))
	require.True(t, AssignableTo(IteratorOf(Int), IteratorOf(Int)))
	require.True(t, AssignableTo(ClassOf("Point", nil), ClassOf("Point", []Field{{Name: "x", Type: Int}})))
	require.False(t, AssignableTo(Bool, Int))  // bool is not int
	require.False(t, AssignableTo(Int, Float)) // no implicit widening
	require.False(t, AssignableTo(IteratorOf(Int), IteratorOf(Str)))
	require.False(t, AssignableTo(ClassOf("Point", nil), ClassOf("Other", nil)))
	require.False(t, AssignableTo(Invalid, Invalid))
}

func TestPrintable(t *testing.T) {
	for _, ty := range []Type{Int, Float, Bool, Str, None} {
		require.Truef(t, Printable(ty), "%s should be printable", ty)
	}
	require.False(t, Printable(Invalid))
}

func TestResolve(t *testing.T) {
	for name, want := range map[string]Type{
		"int": Int, "float": Float, "bool": Bool, "str": Str, "None": None,
	} {
		got, ok := Resolve(name)
		require.Truef(t, ok, "name=%s", name)
		require.Equalf(t, want, got, "name=%s", name)
	}
	_, ok := Resolve("list")
	require.False(t, ok)
}
