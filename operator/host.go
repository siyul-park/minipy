package operator

import (
	"fmt"
	"math"
	"strings"

	"github.com/siyul-park/minipy/hostabi"
	"github.com/siyul-park/minipy/types"

	"github.com/siyul-park/minivm/interp"
	vmtypes "github.com/siyul-park/minivm/types"
)

func powInt() *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{vmtypes.TypeI64, vmtypes.TypeI64}, Returns: []vmtypes.Type{vmtypes.TypeI64}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			base, exp := hostabi.LoadI64(i, params[0]), hostabi.LoadI64(i, params[1])
			if exp < 0 {
				return nil, fmt.Errorf("int ** negative exponent is not an int")
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
			_, elems := hostabi.ArrayElems(i, params[0])
			for _, e := range elems {
				if hostabi.BoxedEqual(i, e, params[1]) {
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
			if strings.Contains(hostabi.LoadStr(i, params[0]), hostabi.LoadStr(i, params[1])) {
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
			_, a := hostabi.ArrayElems(i, params[0])
			_, b := hostabi.ArrayElems(i, params[1])
			elems := make([]vmtypes.Boxed, 0, len(a)+len(b))
			elems = append(elems, a...)
			elems = append(elems, b...)
			return hostabi.AllocArray(i, vmtypes.TypeI8Array, elems)
		},
	)
}

// bytesContentEqual compares two byte arrays by length and content.
func bytesContentEqual(i *interp.Interpreter, a, b vmtypes.Boxed) bool {
	_, ae := hostabi.ArrayElems(i, a)
	_, be := hostabi.ArrayElems(i, b)
	if len(ae) != len(be) {
		return false
	}
	for idx := range ae {
		if ae[idx].I32() != be[idx].I32() {
			return false
		}
	}
	return true
}

func bytesEqual() *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{types.Bytes.VM(), types.Bytes.VM()}, Returns: []vmtypes.Type{vmtypes.TypeI1}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			return []vmtypes.Boxed{vmtypes.BoxI1(bytesContentEqual(i, params[0], params[1]))}, nil
		},
	)
}

// bytesContains reports whether an int needle (0..255) appears among the
// haystack's bytes; needles outside that range are simply absent.
func bytesContains() *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{types.Bytes.VM(), types.Int.VM()}, Returns: []vmtypes.Type{vmtypes.TypeI1}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			_, elems := hostabi.ArrayElems(i, params[0])
			needle := hostabi.LoadI64(i, params[1])
			for _, e := range elems {
				if int64(uint8(e.I32())) == needle {
					return []vmtypes.Boxed{vmtypes.BoxI1(true)}, nil
				}
			}
			return []vmtypes.Boxed{vmtypes.BoxI1(false)}, nil
		},
	)
}
