// Package hostabi holds the low-level helpers that bridge minipy's compiled code
// and the minivm runtime: reading and allocating boxed values, and rendering
// scalars the way Python's str()/print() would. Native modules (builtins,
// operator) and the compiler share these helpers, so they live here rather than
// in any single consumer to keep native-module packages independent of the
// compiler.
package hostabi

import (
	"strconv"
	"strings"

	"github.com/siyul-park/minivm/interp"
	vmtypes "github.com/siyul-park/minivm/types"
)

// FormatScalar renders a boxed scalar the way Python's str()/print() would.
func FormatScalar(i *interp.Interpreter, v vmtypes.Boxed) string {
	switch v.Kind() {
	// bool lowers to i1 uniformly (literals and comparison results alike).
	case vmtypes.KindI1:
		if v.I32() != 0 {
			return "True"
		}
		return "False"
	case vmtypes.KindI64:
		return strconv.FormatInt(v.I64(), 10)
	case vmtypes.KindF32:
		return PyFloat(float64(v.F32()))
	case vmtypes.KindF64:
		return PyFloat(v.F64())
	case vmtypes.KindRef:
		if v.Ref() == 0 {
			return "None"
		}
		return LoadStr(i, v)
	default:
		return "None"
	}
}

// PyFloat mimics CPython's str(float): always shows a fractional part.
func PyFloat(f float64) string {
	s := strconv.FormatFloat(f, 'g', -1, 64)
	if !strings.ContainsAny(s, ".eEnitf") {
		s += ".0"
	}
	return s
}

// AllocString allocates a heap string and returns it as a single boxed ref.
func AllocString(i *interp.Interpreter, s string) ([]vmtypes.Boxed, error) {
	addr, err := i.Alloc(vmtypes.String(s))
	if err != nil {
		return nil, err
	}
	return []vmtypes.Boxed{vmtypes.BoxRef(addr)}, nil
}

// LoadStr reads a heap string argument, returning "" for null or non-strings.
func LoadStr(i *interp.Interpreter, v vmtypes.Boxed) string {
	if v.Kind() != vmtypes.KindRef || v.Ref() == 0 {
		return ""
	}
	val, err := i.Load(v.Ref())
	if err != nil {
		return ""
	}
	if s, ok := val.(vmtypes.String); ok {
		return string(s)
	}
	return ""
}

// LoadI64 reads an int64 argument whether it arrived inline or spilled to a
// heap cell.
func LoadI64(i *interp.Interpreter, v vmtypes.Boxed) int64 {
	if v.Kind() == vmtypes.KindRef {
		val, err := i.Load(v.Ref())
		if err != nil {
			return 0
		}
		if n, ok := val.(vmtypes.I64); ok {
			return int64(n)
		}
		return 0
	}
	return v.I64()
}

// AllocArray allocates a heap array of the given type and returns it as a single
// boxed ref.
func AllocArray(i *interp.Interpreter, typ *vmtypes.ArrayType, elems []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
	addr, err := i.Alloc(vmtypes.NewArray(typ, elems...))
	if err != nil {
		return nil, err
	}
	return []vmtypes.Boxed{vmtypes.BoxRef(addr)}, nil
}

// ArrayElems reads the element type and boxed elements of a heap array,
// normalizing typed arrays to their boxed representation.
func ArrayElems(i *interp.Interpreter, ref vmtypes.Boxed) (*vmtypes.ArrayType, []vmtypes.Boxed) {
	val, err := i.Load(ref.Ref())
	if err != nil {
		return vmtypes.NewArrayType(vmtypes.TypeRef), nil
	}
	switch a := val.(type) {
	case *vmtypes.Array:
		return a.Typ, append([]vmtypes.Boxed(nil), a.Elems...)
	case vmtypes.TypedArray[int8]:
		out := make([]vmtypes.Boxed, len(a))
		for idx, elem := range a {
			out[idx] = vmtypes.BoxI32(int32(elem))
		}
		return vmtypes.TypeI8Array, out
	case vmtypes.TypedArray[int32]:
		out := make([]vmtypes.Boxed, len(a))
		for idx, elem := range a {
			out[idx] = vmtypes.BoxI32(elem)
		}
		return vmtypes.TypeI32Array, out
	case vmtypes.TypedArray[int64]:
		out := make([]vmtypes.Boxed, len(a))
		for idx, elem := range a {
			out[idx] = vmtypes.BoxI64(elem)
		}
		return vmtypes.TypeI64Array, out
	case vmtypes.TypedArray[float32]:
		out := make([]vmtypes.Boxed, len(a))
		for idx, elem := range a {
			out[idx] = vmtypes.BoxF32(elem)
		}
		return vmtypes.TypeF32Array, out
	case vmtypes.TypedArray[float64]:
		out := make([]vmtypes.Boxed, len(a))
		for idx, elem := range a {
			out[idx] = vmtypes.BoxF64(elem)
		}
		return vmtypes.TypeF64Array, out
	default:
		return vmtypes.NewArrayType(vmtypes.TypeRef), nil
	}
}

// BoxedEqual reports whether two boxed values are equal, comparing heap strings
// by contents and other refs by identity.
func BoxedEqual(i *interp.Interpreter, a, b vmtypes.Boxed) bool {
	if a.Kind() != b.Kind() {
		return false
	}
	if a.Kind() != vmtypes.KindRef {
		return a == b
	}
	av, _ := i.Load(a.Ref())
	bv, _ := i.Load(b.Ref())
	as, aok := av.(vmtypes.String)
	bs, bok := bv.(vmtypes.String)
	if aok && bok {
		return as == bs
	}
	return a.Ref() == b.Ref()
}
