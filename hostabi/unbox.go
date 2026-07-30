// Package hostabi shared dynamic-dispatch helpers for native modules.

package hostabi

import (
	"github.com/siyul-park/minipy/types"

	"github.com/siyul-park/minivm/interp"
	vmtypes "github.com/siyul-park/minivm/types"
)

// UnboxFloat extracts a float64 from a boxed value, promoting int if necessary.
// It handles inline values (KindF64, KindI64) and heap-allocated values (KindRef).
func UnboxFloat(i *interp.Interpreter, v vmtypes.Boxed) (float64, error) {
	switch v.Kind() {
	case vmtypes.KindF64:
		return v.F64(), nil
	case vmtypes.KindI64:
		return float64(v.I64()), nil
	case vmtypes.KindRef:
		if v.Ref() == 0 {
			return 0, interp.ErrTypeMismatch
		}
		val, err := i.Load(v.Ref())
		if err != nil {
			return 0, err
		}
		switch n := val.(type) {
		case vmtypes.I64:
			return float64(n), nil
		case vmtypes.F64:
			return float64(n), nil
		default:
			return 0, interp.ErrTypeMismatch
		}
	default:
		return 0, interp.ErrTypeMismatch
	}
}

// UnboxInt extracts an int64 from a boxed value, handling inline and
// heap-allocated integers.
func UnboxInt(i *interp.Interpreter, v vmtypes.Boxed) (int64, error) {
	switch v.Kind() {
	case vmtypes.KindI64:
		return v.I64(), nil
	case vmtypes.KindRef:
		if v.Ref() == 0 {
			return 0, interp.ErrTypeMismatch
		}
		val, err := i.Load(v.Ref())
		if err != nil {
			return 0, err
		}
		switch n := val.(type) {
		case vmtypes.I64:
			return int64(n), nil
		default:
			return 0, interp.ErrTypeMismatch
		}
	default:
		return 0, interp.ErrTypeMismatch
	}
}

// VMParamType returns the VM-level type for a compile-time type, mapping
// dynamic types to TypeRef for runtime dispatch.
func VMParamType(t types.Type) vmtypes.Type {
	if types.IsDynamic(t) {
		return vmtypes.TypeRef
	}
	return t.VM()
}
