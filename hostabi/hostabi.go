// Package hostabi holds the low-level helpers that bridge minipy's compiled code
// and the minivm runtime: reading and allocating boxed values, and rendering
// scalars the way Python's str()/print() would. Native modules (builtins,
// operator) and the compiler share these helpers, so they live here rather than
// in any single consumer to keep native-module packages independent of the
// compiler.
//
// hostabi owns retain/release ownership for boxed refs crossing the host
// boundary. The rule is: ArrayElems returns borrowed boxes — the returned
// slice is a fresh copy, but the elements still belong to whatever heap value
// they came from, and reading them takes no reference. AllocArray takes
// ownership of the boxes passed to it: it retains each ref-kind element on
// the caller's behalf, so the new array holds an independent, owned
// reference. A caller that hands AllocArray a box it already owns outright
// (for example a value it just allocated itself) must release its own
// reference afterward to avoid leaking the extra retain.
package hostabi

import (
	"math"
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
		s, err := LoadStr(i, v)
		if err != nil {
			return "None"
		}
		return s
	default:
		return "None"
	}
}

// PyFloat mimics CPython's str(float): always shows a fractional part, and
// renders the IEEE special values with CPython's lowercase spellings rather
// than Go's FormatFloat spellings ("+Inf", "-Inf", "NaN").
func PyFloat(f float64) string {
	switch {
	case math.IsNaN(f):
		return "nan"
	case math.IsInf(f, 1):
		return "inf"
	case math.IsInf(f, -1):
		return "-inf"
	}
	s := strconv.FormatFloat(f, 'g', -1, 64)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return s
}

// BoxFloat boxes f as a boxed F64, canonicalizing an IEEE NaN payload's sign
// bit first. minivm's Boxed representation tags non-float kinds with a
// positive-signed NaN shape (exponent all ones, sign clear, non-zero
// mantissa); a genuine NaN with that exact shape — what Go's math package,
// strconv, and ordinary float division all produce — collides with the tag
// and is misread as a different kind the next time a host-call boundary
// inspects it. A NaN's sign carries no meaning str(), comparisons, or
// isnan() ever expose, so flipping it here is invisible to Python-level
// behavior: the value is still NaN, just outside the tagged range.
func BoxFloat(f float64) vmtypes.Boxed {
	if math.IsNaN(f) {
		f = math.Copysign(f, -1)
	}
	return vmtypes.BoxF64(f)
}

// AllocString allocates a heap string and returns it as a single boxed ref.
func AllocString(i *interp.Interpreter, s string) ([]vmtypes.Boxed, error) {
	addr, err := i.Alloc(vmtypes.String(s))
	if err != nil {
		return nil, err
	}
	return []vmtypes.Boxed{vmtypes.BoxRef(addr)}, nil
}

// LoadStr reads a heap string argument.
func LoadStr(i *interp.Interpreter, v vmtypes.Boxed) (string, error) {
	if v.Kind() != vmtypes.KindRef || v.Ref() == 0 {
		return "", interp.ErrTypeMismatch
	}
	val, err := i.Load(v.Ref())
	if err != nil {
		return "", err
	}
	s, ok := val.(vmtypes.String)
	if !ok {
		return "", interp.ErrTypeMismatch
	}
	return string(s), nil
}

// LoadI64 reads an int64 argument whether it arrived inline or spilled to a
// heap cell.
func LoadI64(i *interp.Interpreter, v vmtypes.Boxed) (int64, error) {
	if v.Kind() == vmtypes.KindI64 {
		return v.I64(), nil
	}
	if v.Kind() != vmtypes.KindRef || v.Ref() == 0 {
		return 0, interp.ErrTypeMismatch
	}
	val, err := i.Load(v.Ref())
	if err != nil {
		return 0, err
	}
	n, ok := val.(vmtypes.I64)
	if !ok {
		return 0, interp.ErrTypeMismatch
	}
	return int64(n), nil
}

// AllocArray allocates a heap array whose type and elements are copied from
// the caller and returns it as a single boxed ref. AllocArray takes ownership
// of elems: each ref-kind element is retained so the new array holds its own
// independent reference, decoupled from wherever the element came from (see
// the package doc comment). A caller passing a box it already owns outright
// must release that reference once AllocArray returns successfully.
func AllocArray(i *interp.Interpreter, typ *vmtypes.ArrayType, elems []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
	ownedType := vmtypes.NewArrayType(typ.Elem)
	ownedElems := append([]vmtypes.Boxed(nil), elems...)
	if err := RetainBoxes(i, ownedElems); err != nil {
		return nil, err
	}
	addr, err := i.Alloc(vmtypes.NewArray(ownedType, ownedElems...))
	if err != nil {
		_ = ReleaseBoxes(i, ownedElems)
		return nil, err
	}
	return []vmtypes.Boxed{vmtypes.BoxRef(addr)}, nil
}

// RetainBoxes adds one reference to each ref-kind element of values, leaving
// scalars untouched. Pair with ReleaseBoxes to balance ownership when a boxed
// value already resident elsewhere is stored into a second owner.
func RetainBoxes(i *interp.Interpreter, values []vmtypes.Boxed) error {
	for _, value := range values {
		if value.Kind() == vmtypes.KindRef && value.Ref() != 0 {
			if _, err := i.Retain(value.Ref()); err != nil {
				return err
			}
		}
	}
	return nil
}

// ReleaseBoxes drops one reference from each ref-kind element of values,
// leaving scalars untouched. See RetainBoxes.
func ReleaseBoxes(i *interp.Interpreter, values []vmtypes.Boxed) error {
	for _, value := range values {
		if value.Kind() == vmtypes.KindRef && value.Ref() != 0 {
			if err := i.Release(value.Ref()); err != nil {
				return err
			}
		}
	}
	return nil
}

// ArrayElems reads the element type and boxed elements of a heap array,
// normalizing typed arrays to their boxed representation. The returned
// elements are borrowed: they still belong to the source array (see the
// package doc comment), so a caller that stores one into a second owner must
// retain it first.
func ArrayElems(i *interp.Interpreter, ref vmtypes.Boxed) (*vmtypes.ArrayType, []vmtypes.Boxed, error) {
	if ref.Kind() != vmtypes.KindRef || ref.Ref() == 0 {
		return nil, nil, interp.ErrTypeMismatch
	}
	val, err := i.Load(ref.Ref())
	if err != nil {
		return nil, nil, err
	}
	switch array := val.(type) {
	case *vmtypes.Array:
		return vmtypes.NewArrayType(array.Typ.Elem), append([]vmtypes.Boxed(nil), array.Elems...), nil
	case vmtypes.TypedArray[bool]:
		out := make([]vmtypes.Boxed, len(array))
		for index, elem := range array {
			out[index] = vmtypes.BoxI1(elem)
		}
		return vmtypes.NewArrayType(vmtypes.TypeI1), out, nil
	case vmtypes.TypedArray[int8]:
		out := make([]vmtypes.Boxed, len(array))
		for index, elem := range array {
			out[index] = vmtypes.BoxI32(int32(elem))
		}
		return vmtypes.NewArrayType(vmtypes.TypeI8), out, nil
	case vmtypes.TypedArray[int32]:
		out := make([]vmtypes.Boxed, len(array))
		for index, elem := range array {
			out[index] = vmtypes.BoxI32(elem)
		}
		return vmtypes.NewArrayType(vmtypes.TypeI32), out, nil
	case vmtypes.TypedArray[int64]:
		out := make([]vmtypes.Boxed, len(array))
		for index, elem := range array {
			out[index] = vmtypes.BoxI64(elem)
		}
		return vmtypes.NewArrayType(vmtypes.TypeI64), out, nil
	case vmtypes.TypedArray[float32]:
		out := make([]vmtypes.Boxed, len(array))
		for index, elem := range array {
			out[index] = vmtypes.BoxF32(elem)
		}
		return vmtypes.NewArrayType(vmtypes.TypeF32), out, nil
	case vmtypes.TypedArray[float64]:
		out := make([]vmtypes.Boxed, len(array))
		for index, elem := range array {
			out[index] = vmtypes.BoxF64(elem)
		}
		return vmtypes.NewArrayType(vmtypes.TypeF64), out, nil
	default:
		return nil, nil, interp.ErrTypeMismatch
	}
}

// BoxedEqual reports whether two boxed values are equal, comparing heap strings
// by contents and other live refs by identity. Non-null references are loaded
// before an identity result so stale refs always preserve the interpreter error.
func BoxedEqual(i *interp.Interpreter, left, right vmtypes.Boxed) (bool, error) {
	if left.Kind() != right.Kind() {
		return false, nil
	}
	if left.Kind() != vmtypes.KindRef {
		return left == right, nil
	}
	if left.Ref() == 0 || right.Ref() == 0 {
		return left.Ref() == right.Ref(), nil
	}
	leftValue, err := i.Load(left.Ref())
	if err != nil {
		return false, err
	}
	if left.Ref() == right.Ref() {
		return true, nil
	}
	rightValue, err := i.Load(right.Ref())
	if err != nil {
		return false, err
	}
	leftString, leftIsString := leftValue.(vmtypes.String)
	rightString, rightIsString := rightValue.(vmtypes.String)
	return leftIsString && rightIsString && leftString == rightString, nil
}
