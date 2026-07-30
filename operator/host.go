package operator

import (
	"errors"
	"math"
	"strings"

	"github.com/siyul-park/minipy/hostabi"
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

func listContains(elem, receiver types.Type) *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{receiver.VM(), elem.VM()}, Returns: []vmtypes.Type{vmtypes.TypeI1}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			_, elems, err := hostabi.ArrayElems(i, params[0])
			if err != nil {
				return nil, err
			}
			for _, e := range elems {
				equal, err := hostabi.BoxedEqual(i, e, params[1])
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
