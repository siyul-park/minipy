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
	require.Equal(t, "EllipsisType", Ellipsis.String())
	require.Equal(t, "None", None.String())
	require.Equal(t, "list[int]", NewList(Int).String())
	require.Equal(t, "dict[str, int]", NewDict(Str, Int).String())
	require.Equal(t, "set[int]", NewSet(Int).String())
	require.Equal(t, "tuple[int, str]", NewTuple(Int, Str).String())
	require.Equal(t, "tuple[int,]", NewTuple(Int).String())
	require.Equal(t, "Point", NewClass("Point", []Field{{Name: "x", Type: Int}}).String())
	require.Equal(t, "Iterator[int]", NewIterator(Int).String())
	require.Equal(t, "Callable[[int, str], bool]", NewCallable([]Type{Int, Str}, Bool).String())
	require.Equal(t, "Literal[1]", NewLiteral(IntLiteral(1)).String())
	require.Equal(t, "Literal[\"a\", \"b\"]", NewLiteral(StrLiteral("b"), StrLiteral("a")).String())
	require.Equal(t, "<class>", (*Class)(nil).String())
	require.Equal(t, "list[<invalid>]", (*List)(nil).String())
	require.Equal(t, "dict[<invalid>, <invalid>]", (*Dict)(nil).String())
	require.Equal(t, "set[<invalid>]", (*Set)(nil).String())
	require.Equal(t, "tuple[<invalid>]", (*Tuple)(nil).String())
	require.Equal(t, "Iterator[<invalid>]", (*Iterator)(nil).String())
	require.Equal(t, "Callable[[<invalid>], <invalid>]", (*Callable)(nil).String())
	require.Equal(t, "<invalid>", (&Union{}).String())
	require.Equal(t, "<invalid> | int", (&Union{Members: []Type{nil, Int}}).String())
	require.Equal(t, "<invalid>", Invalid.String())
}

func TestType_IsNumeric(t *testing.T) {
	require.True(t, Int.IsNumeric())
	require.True(t, Float.IsNumeric())
	require.False(t, Bool.IsNumeric())
	require.False(t, Str.IsNumeric())
	require.False(t, Ellipsis.IsNumeric())
	require.False(t, NewList(Int).IsNumeric())
	require.False(t, NewDict(Str, Int).IsNumeric())
	require.False(t, NewSet(Int).IsNumeric())
	require.False(t, NewTuple(Int).IsNumeric())
	require.False(t, NewClass("Point", nil).IsNumeric())
	require.False(t, NewIterator(Int).IsNumeric())
	require.False(t, NewCallable(nil, None).IsNumeric())
	require.False(t, NewUnion(Int, Str).IsNumeric())
	require.False(t, NewLiteral(IntLiteral(1)).IsNumeric())
}

func TestType_VM(t *testing.T) {
	require.Equal(t, vmtypes.TypeI64, Int.VM())
	require.Equal(t, vmtypes.TypeF64, Float.VM())
	require.Equal(t, vmtypes.TypeI1, Bool.VM())
	require.Equal(t, vmtypes.TypeString, Str.VM())
	require.Equal(t, vmtypes.NewStructType(), Ellipsis.VM())
	require.Equal(t, vmtypes.TypeRef, None.VM())
	require.IsType(t, &vmtypes.ArrayType{}, NewList(Int).VM())
	require.IsType(t, &vmtypes.MapType{}, NewDict(Str, Int).VM())
	require.IsType(t, &vmtypes.MapType{}, NewSet(Int).VM())
	require.IsType(t, &vmtypes.StructType{}, NewTuple(Int, Str).VM())
	require.Equal(t, vmtypes.TypeRef, NewIterator(Int).VM())
	require.IsType(t, &vmtypes.FunctionType{}, NewCallable([]Type{Int}, Str).VM())
	require.IsType(t, &vmtypes.StructType{}, NewClass("Point", []Field{{Name: "x", Type: Int}}).VM())
	require.Equal(t, vmtypes.TypeI64, NewLiteral(IntLiteral(1)).VM())
	require.Nil(t, Invalid.VM())
	require.Nil(t, (*List)(nil).VM())
	require.Nil(t, (&List{}).VM())
	require.Nil(t, (*Dict)(nil).VM())
	require.Nil(t, (&Dict{}).VM())
	require.Nil(t, (*Set)(nil).VM())
	require.Nil(t, (&Set{}).VM())
	require.Nil(t, (*Tuple)(nil).VM())
	require.Nil(t, (*Class)(nil).VM())
	require.Nil(t, (*Callable)(nil).VM())
}

func TestAssignable(t *testing.T) {
	require.True(t, AssignableTo(Int, Int))
	require.True(t, AssignableTo(NewList(Int), NewList(Int)))
	require.True(t, AssignableTo(NewDict(Str, Int), NewDict(Str, Int)))
	require.True(t, AssignableTo(NewSet(Int), NewSet(Int)))
	require.True(t, AssignableTo(NewTuple(Int, Str), NewTuple(Int, Str)))
	require.True(t, AssignableTo(NewCallable([]Type{Int}, Str), NewCallable([]Type{Int}, Str)))
	require.True(t, AssignableTo(NewIterator(Int), NewIterator(Int)))
	require.True(t, AssignableTo(NewClass("Point", nil), NewClass("Point", []Field{{Name: "x", Type: Int}})))
	require.False(t, AssignableTo(Bool, Int))  // bool is not int
	require.False(t, AssignableTo(Int, Float)) // no implicit widening
	require.True(t, AssignableTo(Ellipsis, Ellipsis))
	require.False(t, AssignableTo(Ellipsis, None))
	require.False(t, AssignableTo(NewList(Int), NewList(Str)))
	require.False(t, AssignableTo(NewDict(Str, Int), NewDict(Str, Str)))
	require.False(t, AssignableTo(NewSet(Int), NewSet(Str)))
	require.False(t, AssignableTo(NewTuple(Int), NewTuple(Int, Str)))
	require.False(t, AssignableTo(NewCallable([]Type{Int}, Str), NewCallable([]Type{Str}, Str)))
	require.False(t, AssignableTo(NewIterator(Int), NewIterator(Str)))
	require.False(t, AssignableTo(NewClass("Point", nil), NewClass("Other", nil)))
	require.True(t, AssignableTo(NewLiteral(IntLiteral(1)), Int))
	require.True(t, AssignableTo(NewLiteral(IntLiteral(1)), NewLiteral(IntLiteral(1), IntLiteral(2))))
	require.True(t, AssignableTo(NewLiteral(IntLiteral(1)), NewUnion(NewLiteral(IntLiteral(1)), Str)))
	require.False(t, AssignableTo(Int, NewLiteral(IntLiteral(1))))
	require.False(t, AssignableTo(NewLiteral(IntLiteral(2)), NewLiteral(IntLiteral(1))))
	require.False(t, AssignableTo(nil, Int))
	require.False(t, AssignableTo(Int, nil))
	require.False(t, AssignableTo(Invalid, Invalid))
}

func TestPrintable(t *testing.T) {
	for _, ty := range []Type{Int, Float, Bool, Str, None, Any, NewList(Int), NewDict(Str, Int), NewSet(Int), NewTuple(Int, Str), NewUnion(Int, Str), NewLiteral(StrLiteral("x"))} {
		require.Truef(t, Printable(ty), "%s should be printable", ty)
	}
	require.False(t, Printable(nil))
	require.False(t, Printable(Invalid))
	require.False(t, Printable(NewClass("Point", nil)))
	require.False(t, Printable(NewUnion(Int, NewClass("Point", nil))))
	require.False(t, Printable(NewList(NewClass("Point", nil))))
	require.False(t, Printable(NewDict(Str, NewClass("Point", nil))))
	require.False(t, Printable(NewTuple(Int, NewClass("Point", nil))))
}

func TestBytes(t *testing.T) {
	t.Run("name", func(t *testing.T) {
		require.Equal(t, "bytes", Bytes.String())
	})
	t.Run("non-numeric", func(t *testing.T) {
		require.False(t, Bytes.IsNumeric())
	})
	t.Run("VM is an i8 array", func(t *testing.T) {
		require.Equal(t, vmtypes.NewArrayType(vmtypes.TypeI8), Bytes.VM())
	})
	t.Run("equality", func(t *testing.T) {
		require.True(t, Equal(Bytes, Bytes))
		require.False(t, Equal(Bytes, Str))
		require.False(t, Equal(Bytes, NewList(Int)))
	})
	t.Run("not printable", func(t *testing.T) {
		require.False(t, Printable(Bytes))
	})
}

func TestResolve(t *testing.T) {
	for name, want := range map[string]Type{
		"int": Int, "float": Float, "bool": Bool, "str": Str, "bytes": Bytes, "EllipsisType": Ellipsis, "None": None, "Any": Any,
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
	t.Run("detects union", func(t *testing.T) {
		u, ok := isUnion(NewUnion(Int, Str))
		require.True(t, ok)
		require.Len(t, u.Members, 2)
		_, ok = isUnion(Int)
		require.False(t, ok)
	})
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
