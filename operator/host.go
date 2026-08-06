package operator

import (
	"errors"
	"math"
	"strings"

	"github.com/siyul-park/minipy/hostabi"
	"github.com/siyul-park/minipy/token"
	"github.com/siyul-park/minipy/types"

	"github.com/siyul-park/minivm/interp"
	vmtypes "github.com/siyul-park/minivm/types"
)

// Exceptions raised by operator host functions have stable identities so the
// compiler runtime boundary can classify them without matching messages.
var (
	ErrNegativeExponent = errors.New("int ** negative exponent is not an int")
	ErrRepeatOverflow   = errors.New("repeated sequence is too long")
)

func powInt() *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{vmtypes.TypeI64, vmtypes.TypeI64}, Returns: []vmtypes.Type{vmtypes.TypeI64}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			base, err := hostabi.LoadI64(i, params[0])
			if err != nil {
				return nil, err
			}
			exp, err := hostabi.LoadI64(i, params[1])
			if err != nil {
				return nil, err
			}
			if exp < 0 {
				return nil, ErrNegativeExponent
			}
			result := int64(1)
			for exp > 0 {
				if exp&1 == 1 {
					result *= base
				}
				exp >>= 1
				if exp > 0 {
					base *= base
				}
			}
			return []vmtypes.Boxed{vmtypes.BoxI64(result)}, nil
		},
	)
}

func powFloat() *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{vmtypes.TypeF64, vmtypes.TypeF64}, Returns: []vmtypes.Type{vmtypes.TypeF64}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			return []vmtypes.Boxed{vmtypes.BoxF64(math.Pow(params[0].F64(), params[1].F64()))}, nil
		},
	)
}

// listContains implements `in`/`not in` over list[T]: CPython compares each
// element to the needle with `==`, which is structural for containers (list,
// tuple, dict, set), not identity. It reuses structuralEqual — the same
// element comparison containerEqual uses for `list == list` — rather than a
// second identity-only notion of equality via hostabi.BoxedEqual.
func listContains(elem, receiver types.Type) *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{receiver.VM(), elem.VM()}, Returns: []vmtypes.Type{vmtypes.TypeI1}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			_, elems, err := hostabi.ArrayElems(i, params[0])
			if err != nil {
				return nil, err
			}
			for _, e := range elems {
				equal, err := structuralEqual(i, elem, e, params[1])
				if err != nil {
					return nil, err
				}
				if equal {
					return []vmtypes.Boxed{vmtypes.BoxI1(true)}, nil
				}
			}
			return []vmtypes.Boxed{vmtypes.BoxI1(false)}, nil
		},
	)
}

func strContains() *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{vmtypes.TypeString, vmtypes.TypeString}, Returns: []vmtypes.Type{vmtypes.TypeI1}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			haystack, err := hostabi.LoadStr(i, params[0])
			if err != nil {
				return nil, err
			}
			needle, err := hostabi.LoadStr(i, params[1])
			if err != nil {
				return nil, err
			}
			if strings.Contains(haystack, needle) {
				return []vmtypes.Boxed{vmtypes.BoxI1(true)}, nil
			}
			return []vmtypes.Boxed{vmtypes.BoxI1(false)}, nil
		},
	)
}

// bytesConcat allocates a new byte array holding the left operand's bytes
// followed by the right operand's, leaving both operands untouched (bytes is
// immutable — docs/spec/02-types.md).
func bytesConcat() *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{types.Bytes.VM(), types.Bytes.VM()}, Returns: []vmtypes.Type{types.Bytes.VM()}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			_, a, err := hostabi.ArrayElems(i, params[0])
			if err != nil {
				return nil, err
			}
			_, b, err := hostabi.ArrayElems(i, params[1])
			if err != nil {
				return nil, err
			}
			elems := make([]vmtypes.Boxed, 0, len(a)+len(b))
			elems = append(elems, a...)
			elems = append(elems, b...)
			return hostabi.AllocArray(i, vmtypes.TypeI8Array, elems)
		},
	)
}

func bytesEqual() *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{types.Bytes.VM(), types.Bytes.VM()}, Returns: []vmtypes.Type{vmtypes.TypeI1}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			_, left, err := hostabi.ArrayElems(i, params[0])
			if err != nil {
				return nil, err
			}
			_, right, err := hostabi.ArrayElems(i, params[1])
			if err != nil {
				return nil, err
			}
			if len(left) != len(right) {
				return []vmtypes.Boxed{vmtypes.BoxI1(false)}, nil
			}
			for index := range left {
				if left[index].I32() != right[index].I32() {
					return []vmtypes.Boxed{vmtypes.BoxI1(false)}, nil
				}
			}
			return []vmtypes.Boxed{vmtypes.BoxI1(true)}, nil
		},
	)
}

// bytesContains reports whether an int needle (0..255) appears among the
// haystack's bytes; needles outside that range are simply absent.
func bytesContains() *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{types.Bytes.VM(), types.Int.VM()}, Returns: []vmtypes.Type{vmtypes.TypeI1}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			_, elems, err := hostabi.ArrayElems(i, params[0])
			if err != nil {
				return nil, err
			}
			needle, err := hostabi.LoadI64(i, params[1])
			if err != nil {
				return nil, err
			}
			for _, e := range elems {
				if int64(uint8(e.I32())) == needle {
					return []vmtypes.Boxed{vmtypes.BoxI1(true)}, nil
				}
			}
			return []vmtypes.Boxed{vmtypes.BoxI1(false)}, nil
		},
	)
}

func stringRepeat() *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{vmtypes.TypeI64, vmtypes.TypeString}, Returns: []vmtypes.Type{vmtypes.TypeString}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			count, err := hostabi.LoadI64(i, params[0])
			if err != nil {
				return nil, err
			}
			value, err := hostabi.LoadStr(i, params[1])
			if err != nil {
				return nil, err
			}
			if count <= 0 {
				return hostabi.AllocString(i, "")
			}
			if len(value) > 0 && count > int64(^uint(0)>>1)/int64(len(value)) {
				return nil, ErrRepeatOverflow
			}
			return hostabi.AllocString(i, strings.Repeat(value, int(count)))
		},
	)
}

func listRepeat(list types.Type) *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{list.VM(), vmtypes.TypeI64}, Returns: []vmtypes.Type{list.VM()}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			typ, elements, err := hostabi.ArrayElems(i, params[0])
			if err != nil {
				return nil, err
			}
			count, err := hostabi.LoadI64(i, params[1])
			if err != nil {
				return nil, err
			}
			if count <= 0 || len(elements) == 0 {
				return hostabi.AllocArray(i, typ, nil)
			}
			if count > int64(^uint(0)>>1)/int64(len(elements)) {
				return nil, ErrRepeatOverflow
			}
			values := make([]vmtypes.Boxed, 0, int(count)*len(elements))
			for range count {
				values = append(values, elements...)
			}
			return hostabi.AllocArray(i, typ, values)
		},
	)
}

// containerEqual returns a host function implementing CPython's structural
// `==` for list, tuple, dict, and set (docs/spec/04-static-semantics.md):
// same length/size, elements (or values) compared recursively through the
// container's statically known element type. `!=` reuses this and inverts
// the result (see EmitCompareStack), matching the bytesEqual precedent.
func containerEqual(t types.Type) *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{t.VM(), t.VM()}, Returns: []vmtypes.Type{vmtypes.TypeI1}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			eq, err := structuralEqual(i, t, params[0], params[1])
			if err != nil {
				return nil, err
			}
			return []vmtypes.Boxed{vmtypes.BoxI1(eq)}, nil
		},
	)
}

// containerCompare returns a host function implementing CPython's
// lexicographic ordering for list/tuple (docs/spec/04-static-semantics.md):
// elements compared pairwise, the first difference decides, and a prefix
// relationship falls back to length. The checker (Comparable/orderable)
// admits this only when every element position is int, float, bool, or str,
// so scalarCompare never sees another kind. The result is an i64 in
// {-1, 0, 1}; EmitCompareStack turns it into the requested `<`/`<=`/`>`/`>=`
// by comparing against 0, reusing one host function for all four operators.
func containerCompare(t types.Type) *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{t.VM(), t.VM()}, Returns: []vmtypes.Type{vmtypes.TypeI64}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			cmp, err := lexicographicCompare(i, t, params[0], params[1])
			if err != nil {
				return nil, err
			}
			return []vmtypes.Boxed{vmtypes.BoxI64(int64(cmp))}, nil
		},
	)
}

// structuralEqual dispatches container equality by the static element type,
// recursing into nested lists/tuples/dicts/sets. Dynamic element types
// (Any/union, e.g. list[Any]) fall back to the dynamic comparison used for
// top-level Any values (dynamic.go), preserving that path's identity-for-refs
// behavior rather than inventing new deep-equality semantics for it. Every
// other type (scalars, str, None, Ellipsis, and reference-identity types
// such as class/iterator/callable) delegates to hostabi.BoxedEqual, which
// already compares strings by content and other refs by identity.
func structuralEqual(i *interp.Interpreter, t types.Type, left, right vmtypes.Boxed) (bool, error) {
	switch typ := types.Erase(t).(type) {
	case *types.List:
		return arrayEqual(i, typ.Elem, left, right)
	case *types.Tuple:
		return tupleEqual(i, typ, left, right)
	case *types.Dict:
		return dictEqual(i, typ.Value, left, right)
	case *types.Set:
		return setEqual(i, left, right)
	default:
		if types.IsDynamic(types.Erase(t)) {
			return dynCmp(i, left, right, token.EQ)
		}
		return hostabi.BoxedEqual(i, left, right)
	}
}

// arrayEqual compares two list values: equal length, then every element
// pair equal under elem's structural equality.
func arrayEqual(i *interp.Interpreter, elem types.Type, left, right vmtypes.Boxed) (bool, error) {
	_, leftElems, err := hostabi.ArrayElems(i, left)
	if err != nil {
		return false, err
	}
	_, rightElems, err := hostabi.ArrayElems(i, right)
	if err != nil {
		return false, err
	}
	if len(leftElems) != len(rightElems) {
		return false, nil
	}
	for index := range leftElems {
		eq, err := structuralEqual(i, elem, leftElems[index], rightElems[index])
		if err != nil {
			return false, err
		}
		if !eq {
			return false, nil
		}
	}
	return true, nil
}

// tupleEqual compares two tuple values field by field, each field under its
// own declared type (tuples are heterogeneous fixed-arity structs). Both
// operands share arity because the checker only admits `==` between equal
// tuple types.
func tupleEqual(i *interp.Interpreter, t *types.Tuple, left, right vmtypes.Boxed) (bool, error) {
	leftStruct, err := loadStruct(i, left)
	if err != nil {
		return false, err
	}
	rightStruct, err := loadStruct(i, right)
	if err != nil {
		return false, err
	}
	for index, elem := range t.Elems {
		eq, err := structuralEqual(i, elem, leftStruct.Field(index), rightStruct.Field(index))
		if err != nil {
			return false, err
		}
		if !eq {
			return false, nil
		}
	}
	return true, nil
}

// dictEqual compares two dict values: equal size, then every left key found
// in right with structurally equal values. Dict keys are limited to
// int/float/bool/str (docs/spec/04-static-semantics.md), so key lookup
// through mapGet's boxed comparison is exact without recursion.
func dictEqual(i *interp.Interpreter, value types.Type, left, right vmtypes.Boxed) (bool, error) {
	leftMap, err := loadMapValue(i, left)
	if err != nil {
		return false, err
	}
	rightMap, err := loadMapValue(i, right)
	if err != nil {
		return false, err
	}
	if mapLen(leftMap) != mapLen(rightMap) {
		return false, nil
	}
	for _, key := range mapKeys(leftMap) {
		leftValue, _ := mapGet(leftMap, key)
		rightValue, found := mapGet(rightMap, key)
		if !found {
			return false, nil
		}
		eq, err := structuralEqual(i, value, leftValue, rightValue)
		if err != nil {
			return false, err
		}
		if !eq {
			return false, nil
		}
	}
	return true, nil
}

// setEqual compares two set values: equal size and every left member present
// in right. Set elements are limited to int/float/bool/str, so membership
// through mapGet's boxed comparison is exact.
func setEqual(i *interp.Interpreter, left, right vmtypes.Boxed) (bool, error) {
	leftMap, err := loadMapValue(i, left)
	if err != nil {
		return false, err
	}
	rightMap, err := loadMapValue(i, right)
	if err != nil {
		return false, err
	}
	if mapLen(leftMap) != mapLen(rightMap) {
		return false, nil
	}
	for _, key := range mapKeys(leftMap) {
		if _, found := mapGet(rightMap, key); !found {
			return false, nil
		}
	}
	return true, nil
}

// lexicographicCompare returns -1/0/1 comparing left and right of type t
// under CPython's element-wise ordering. The checker's `orderable` rule
// guarantees t is a list/tuple whose element positions are scalar
// (int/float/bool/str), so scalarCompare covers every leaf reached here.
func lexicographicCompare(i *interp.Interpreter, t types.Type, left, right vmtypes.Boxed) (int, error) {
	switch typ := types.Erase(t).(type) {
	case *types.List:
		return sequenceCompare(i, typ.Elem, left, right)
	case *types.Tuple:
		return tupleCompare(i, typ, left, right)
	default:
		return scalarCompare(i, t, left, right)
	}
}

// sequenceCompare compares two lists element by element up to the shorter
// length; the first differing pair decides, and an unbroken common prefix
// falls back to comparing lengths (the shorter list sorts first).
func sequenceCompare(i *interp.Interpreter, elem types.Type, left, right vmtypes.Boxed) (int, error) {
	_, leftElems, err := hostabi.ArrayElems(i, left)
	if err != nil {
		return 0, err
	}
	_, rightElems, err := hostabi.ArrayElems(i, right)
	if err != nil {
		return 0, err
	}
	limit := min(len(leftElems), len(rightElems))
	for index := 0; index < limit; index++ {
		cmp, err := scalarCompare(i, elem, leftElems[index], rightElems[index])
		if err != nil {
			return 0, err
		}
		if cmp != 0 {
			return cmp, nil
		}
	}
	return compareInt64(int64(len(leftElems)), int64(len(rightElems))), nil
}

// tupleCompare compares two tuples field by field; both operands share
// arity because the checker only admits ordering between equal tuple types.
func tupleCompare(i *interp.Interpreter, t *types.Tuple, left, right vmtypes.Boxed) (int, error) {
	leftStruct, err := loadStruct(i, left)
	if err != nil {
		return 0, err
	}
	rightStruct, err := loadStruct(i, right)
	if err != nil {
		return 0, err
	}
	for index, elem := range t.Elems {
		cmp, err := scalarCompare(i, elem, leftStruct.Field(index), rightStruct.Field(index))
		if err != nil {
			return 0, err
		}
		if cmp != 0 {
			return cmp, nil
		}
	}
	return 0, nil
}

// scalarCompare orders one leaf element pair. t is always int, float, bool,
// or str here: orderable admits only those as list/tuple element positions.
func scalarCompare(i *interp.Interpreter, t types.Type, left, right vmtypes.Boxed) (int, error) {
	switch types.Erase(t) {
	case types.Float:
		return compareFloat(left.F64(), right.F64()), nil
	case types.Str:
		leftStr, err := hostabi.LoadStr(i, left)
		if err != nil {
			return 0, err
		}
		rightStr, err := hostabi.LoadStr(i, right)
		if err != nil {
			return 0, err
		}
		return compareStr(leftStr, rightStr), nil
	case types.Bool:
		return compareBool(left.Bool(), right.Bool()), nil
	default:
		leftInt, err := hostabi.LoadI64(i, left)
		if err != nil {
			return 0, err
		}
		rightInt, err := hostabi.LoadI64(i, right)
		if err != nil {
			return 0, err
		}
		return compareInt64(leftInt, rightInt), nil
	}
}

func compareInt64(left, right int64) int {
	switch {
	case left < right:
		return -1
	case left > right:
		return 1
	default:
		return 0
	}
}

func compareFloat(left, right float64) int {
	switch {
	case left < right:
		return -1
	case left > right:
		return 1
	default:
		return 0
	}
}

func compareStr(left, right string) int {
	switch {
	case left < right:
		return -1
	case left > right:
		return 1
	default:
		return 0
	}
}

func compareBool(left, right bool) int {
	switch {
	case !left && right:
		return -1
	case left && !right:
		return 1
	default:
		return 0
	}
}

// loadStruct reads a heap tuple argument (tuples lower to minivm structs;
// docs/spec/05-codegen.md).
func loadStruct(i *interp.Interpreter, v vmtypes.Boxed) (*vmtypes.Struct, error) {
	if v.Kind() != vmtypes.KindRef || v.Ref() == 0 {
		return nil, interp.ErrTypeMismatch
	}
	val, err := i.Load(v.Ref())
	if err != nil {
		return nil, err
	}
	s, ok := val.(*vmtypes.Struct)
	if !ok {
		return nil, interp.ErrTypeMismatch
	}
	return s, nil
}

// loadMapValue reads a heap dict/set argument (both lower to minivm maps;
// docs/spec/05-codegen.md).
func loadMapValue(i *interp.Interpreter, v vmtypes.Boxed) (vmtypes.Value, error) {
	if v.Kind() != vmtypes.KindRef || v.Ref() == 0 {
		return nil, interp.ErrTypeMismatch
	}
	return i.Load(v.Ref())
}
