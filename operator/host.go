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
		&vmtypes.FunctionType{Params: []vmtypes.Type{receiver.VM(), elem.VM()}, Returns: []vmtypes.Type{vmtypes.TypeI32}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			_, elems := hostabi.ArrayElems(i, params[0])
			for _, e := range elems {
				if hostabi.BoxedEqual(i, e, params[1]) {
					return []vmtypes.Boxed{vmtypes.BoxI32(1)}, nil
				}
			}
			return []vmtypes.Boxed{vmtypes.BoxI32(0)}, nil
		},
	)
}

func strContains() *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{vmtypes.TypeString, vmtypes.TypeString}, Returns: []vmtypes.Type{vmtypes.TypeI32}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			if strings.Contains(hostabi.LoadStr(i, params[0]), hostabi.LoadStr(i, params[1])) {
				return []vmtypes.Boxed{vmtypes.BoxI32(1)}, nil
			}
			return []vmtypes.Boxed{vmtypes.BoxI32(0)}, nil
		},
	)
}
