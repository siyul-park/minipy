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
	require.Equal(t, "Point", NewClass("Point", []Field{{Name: "x", Type: Int}}).String())
	require.Equal(t, "Iterator[int]", NewIterator(Int).String())
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
	require.Equal(t, vmtypes.TypeRef, NewIterator(Int).VM())
	require.IsType(t, &vmtypes.StructType{}, NewClass("Point", []Field{{Name: "x", Type: Int}}).VM())
	require.Nil(t, Invalid.VM())
}

func TestAssignable(t *testing.T) {
	require.True(t, AssignableTo(Int, Int))
	require.True(t, AssignableTo(NewIterator(Int), NewIterator(Int)))
	require.True(t, AssignableTo(NewClass("Point", nil), NewClass("Point", []Field{{Name: "x", Type: Int}})))
	require.False(t, AssignableTo(Bool, Int))  // bool is not int
	require.False(t, AssignableTo(Int, Float)) // no implicit widening
	require.False(t, AssignableTo(NewIterator(Int), NewIterator(Str)))
	require.False(t, AssignableTo(NewClass("Point", nil), NewClass("Other", nil)))
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
		"int": Int, "float": Float, "bool": Bool, "str": Str, "None": None, "Any": Any,
	} {
		got, ok := Resolve(name)
		require.Truef(t, ok, "name=%s", name)
		require.Equalf(t, want, got, "name=%s", name)
	}
	_, ok := Resolve("list")
	require.False(t, ok)
}

func TestNewUnion(t *testing.T) {
	t.Run("normalizes flatten/dedup/sort/collapse", func(t *testing.T) {
		require.Equal(t, Int, NewUnion(Int))                       // single collapses
		require.Equal(t, Int, NewUnion(Int, Int))                  // dedup to one
		require.Equal(t, "int | str", NewUnion(Str, Int).String()) // sorted canonical
		require.True(t, Equal(NewUnion(Int, Str), NewUnion(Str, Int)))
		require.True(t, Equal(NewUnion(NewUnion(Int, Str), None), NewUnion(Int, Str, None)))
	})
	t.Run("absorbs Any and poisons on Invalid", func(t *testing.T) {
		require.Equal(t, Any, NewUnion(Int, Any))
		require.Equal(t, Invalid, NewUnion(Int, Invalid))
		require.Equal(t, Invalid, NewUnion())
	})
	t.Run("VM is ref", func(t *testing.T) {
		require.Equal(t, vmtypes.TypeRef, NewUnion(Int, Str).VM())
		require.Equal(t, vmtypes.TypeRef, Any.VM())
	})
}

func TestOptional(t *testing.T) {
	opt := NewUnion(Int, None)
	require.True(t, IsOptional(opt))
	require.False(t, IsOptional(NewUnion(Int, Str)))
	require.Equal(t, Int, Without(opt, None)) // unwrap Optional[int] -> int
}

func TestJoin(t *testing.T) {
	require.Equal(t, Int, Join(Int, Int))
	require.Equal(t, Int, Join(Invalid, Int)) // Invalid is bottom
	require.Equal(t, Any, Join(Int, Any))
	require.True(t, Equal(NewUnion(Int, Str), Join(Int, Str)))
	require.True(t, Equal(NewUnion(Int, Str, None), Join(NewUnion(Int, Str), None)))
}

func TestNarrowWithout(t *testing.T) {
	u := NewUnion(Int, Str)
	require.Equal(t, Int, Narrow(u, Int))  // positive narrow to member
	require.Equal(t, Str, Without(u, Int)) // remove member -> remaining collapses
	require.Equal(t, Invalid, Without(Int, Int))
}

func TestAssignable_Union(t *testing.T) {
	u := NewUnion(Int, Str)
	require.True(t, AssignableTo(Int, u))   // widen concrete into union
	require.True(t, AssignableTo(Int, Any)) // widen into Any
	require.True(t, AssignableTo(u, NewUnion(Int, Str, None)))
	require.False(t, AssignableTo(u, Int))   // union not assignable to concrete
	require.False(t, AssignableTo(Float, u)) // not a member
	require.False(t, AssignableTo(Any, Int)) // Any needs a cast
	require.True(t, Printable(u))
	require.True(t, Printable(Any))
}

func TestTypeVar(t *testing.T) {
	a := NewTypeVar(1)
	b := NewTypeVar(1)
	c := NewTypeVar(2)
	require.True(t, a.Equal(b)) // same id
	require.False(t, a.Equal(c))
	require.Nil(t, a.VM()) // must be resolved before codegen
}
