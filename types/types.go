// Package types is the minipy source-level type system. It stays separate from
// minivm's runtime types because minipy distinguishes `bool` from `int` and
// forbids implicit `int`/`float` mixing (docs/spec/02-types.md) — distinctions
// the VM, where both are `i32`/`i64`, cannot express. Each source type maps to a
// minivm runtime type through VM, reusing minivm's types for the lowering rather
// than re-modelling them.
package types

import vmtypes "github.com/siyul-park/minivm/types"

// Type is a minipy source-level type.
type Type int

const (
	Invalid Type = iota
	Int
	Float
	Bool
	Str
	None
)

// String returns the minipy spelling of the type.
func (t Type) String() string {
	switch t {
	case Int:
		return "int"
	case Float:
		return "float"
	case Bool:
		return "bool"
	case Str:
		return "str"
	case None:
		return "None"
	default:
		return "<invalid>"
	}
}

// IsNumeric reports whether t is int or float.
func (t Type) IsNumeric() bool {
	return t == Int || t == Float
}

// VM returns the minivm runtime type that backs t.
func (t Type) VM() vmtypes.Type {
	switch t {
	case Int:
		return vmtypes.TypeI64
	case Float:
		return vmtypes.TypeF64
	case Bool:
		return vmtypes.TypeI32
	case Str:
		return vmtypes.TypeString
	case None:
		return vmtypes.TypeRef
	default:
		return nil
	}
}

// AssignableTo reports whether a value of type src may be stored where dst is
// expected. M0 has no implicit coercion, so types must match exactly: `bool` is
// not assignable to `int`, and there is no `int`->`float` widening.
func AssignableTo(src, dst Type) bool {
	return src != Invalid && src == dst
}

// Printable reports whether str()/print() accept t.
func Printable(t Type) bool {
	return t == Int || t == Float || t == Bool || t == Str || t == None
}

// Resolve maps an annotation name to a source type. ok is false for names
// outside the M0 scalar set.
func Resolve(name string) (Type, bool) {
	switch name {
	case "int":
		return Int, true
	case "float":
		return Float, true
	case "bool":
		return Bool, true
	case "str":
		return Str, true
	case "None":
		return None, true
	default:
		return Invalid, false
	}
}
