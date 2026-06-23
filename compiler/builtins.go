package compiler

import (
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"

	"github.com/siyul-park/minipy/types"

	"github.com/siyul-park/minivm/interp"
	vmtypes "github.com/siyul-park/minivm/types"
)

// hostFuncs holds the minivm host functions backing the M0 builtins. print
// writes to the configured sink; the rest are helpers for conversions and the
// operators with no single opcode.
//
// pow and float modulo stay host functions for now: lowering them inline needs
// a loop with temporaries, but the module-entry frame has no local slots
// (bp == sp). They are candidates for a JIT-able extension op later
// (docs/spec/05-codegen.md); see emit.go.
type hostFuncs struct {
	print      *interp.HostFunction
	str        *interp.HostFunction
	intParse   *interp.HostFunction
	floatParse *interp.HostFunction
	powInt     *interp.HostFunction
	powFloat   *interp.HostFunction
	floatMod   *interp.HostFunction
}

// isBuiltin reports whether name is an M0 builtin.
func isBuiltin(name string) bool {
	switch name {
	case "print", "str", "int", "float", "bool", "abs":
		return true
	default:
		return false
	}
}

// builtinReturn returns the result type of a unary builtin call given its
// argument type, or ok=false when the argument type is unsupported.
func builtinReturn(name string, arg types.Type) (types.Type, bool) {
	switch name {
	case "print":
		if types.Printable(arg) {
			return types.None, true
		}
	case "str":
		if types.Printable(arg) {
			return types.Str, true
		}
	case "int":
		if convertible(arg) {
			return types.Int, true
		}
	case "float":
		if convertible(arg) {
			return types.Float, true
		}
	case "bool":
		if convertible(arg) {
			return types.Bool, true
		}
	case "abs":
		if arg == types.Int || arg == types.Float {
			return arg, true
		}
	}
	return types.Invalid, false
}

func convertible(t types.Type) bool {
	return t == types.Int || t == types.Float || t == types.Bool || t == types.Str
}

// newHostFuncs builds the host-function set, binding print's output to out.
func newHostFuncs(out io.Writer) *hostFuncs {
	return &hostFuncs{
		print: interp.NewHostFunction(
			&vmtypes.FunctionType{Params: []vmtypes.Type{vmtypes.TypeRef}, Returns: nil},
			func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
				fmt.Fprintln(out, formatScalar(i, params[0]))
				return nil, nil
			},
		),
		str: interp.NewHostFunction(
			&vmtypes.FunctionType{Params: []vmtypes.Type{vmtypes.TypeRef}, Returns: []vmtypes.Type{vmtypes.TypeString}},
			func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
				return allocString(i, formatScalar(i, params[0]))
			},
		),
		intParse: interp.NewHostFunction(
			&vmtypes.FunctionType{Params: []vmtypes.Type{vmtypes.TypeString}, Returns: []vmtypes.Type{vmtypes.TypeI64}},
			func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
				s := loadStr(i, params[0])
				n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
				if err != nil {
					return nil, fmt.Errorf("invalid literal for int() with base 10: %q", s)
				}
				return []vmtypes.Boxed{vmtypes.BoxI64(n)}, nil
			},
		),
		floatParse: interp.NewHostFunction(
			&vmtypes.FunctionType{Params: []vmtypes.Type{vmtypes.TypeString}, Returns: []vmtypes.Type{vmtypes.TypeF64}},
			func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
				s := loadStr(i, params[0])
				f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
				if err != nil {
					return nil, fmt.Errorf("could not convert string to float: %q", s)
				}
				return []vmtypes.Boxed{vmtypes.BoxF64(f)}, nil
			},
		),
		powInt: interp.NewHostFunction(
			&vmtypes.FunctionType{Params: []vmtypes.Type{vmtypes.TypeI64, vmtypes.TypeI64}, Returns: []vmtypes.Type{vmtypes.TypeI64}},
			func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
				base, exp := loadI64(i, params[0]), loadI64(i, params[1])
				if exp < 0 {
					return nil, fmt.Errorf("int ** negative exponent is not an int")
				}
				result := int64(1)
				for ; exp > 0; exp-- {
					result *= base
				}
				return []vmtypes.Boxed{vmtypes.BoxI64(result)}, nil
			},
		),
		powFloat: interp.NewHostFunction(
			&vmtypes.FunctionType{Params: []vmtypes.Type{vmtypes.TypeF64, vmtypes.TypeF64}, Returns: []vmtypes.Type{vmtypes.TypeF64}},
			func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
				return []vmtypes.Boxed{vmtypes.BoxF64(math.Pow(params[0].F64(), params[1].F64()))}, nil
			},
		),
		floatMod: interp.NewHostFunction(
			&vmtypes.FunctionType{Params: []vmtypes.Type{vmtypes.TypeF64, vmtypes.TypeF64}, Returns: []vmtypes.Type{vmtypes.TypeF64}},
			func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
				a, b := params[0].F64(), params[1].F64()
				r := math.Mod(a, b)
				if r != 0 && (r < 0) != (b < 0) {
					r += b
				}
				return []vmtypes.Boxed{vmtypes.BoxF64(r)}, nil
			},
		),
	}
}

// formatScalar renders a boxed scalar the way Python's str()/print() would.
func formatScalar(i *interp.Interpreter, v vmtypes.Boxed) string {
	switch v.Kind() {
	case vmtypes.KindI32:
		if v.I32() != 0 {
			return "True"
		}
		return "False"
	case vmtypes.KindI64:
		return strconv.FormatInt(v.I64(), 10)
	case vmtypes.KindF32:
		return pyFloat(float64(v.F32()))
	case vmtypes.KindF64:
		return pyFloat(v.F64())
	case vmtypes.KindRef:
		if v.Ref() == 0 {
			return "None"
		}
		return loadStr(i, v)
	default:
		return "None"
	}
}

// pyFloat mimics CPython's str(float): always shows a fractional part.
func pyFloat(f float64) string {
	s := strconv.FormatFloat(f, 'g', -1, 64)
	if !strings.ContainsAny(s, ".eEnitf") {
		s += ".0"
	}
	return s
}

func allocString(i *interp.Interpreter, s string) ([]vmtypes.Boxed, error) {
	addr, err := i.Alloc(vmtypes.String(s))
	if err != nil {
		return nil, err
	}
	return []vmtypes.Boxed{vmtypes.BoxRef(addr)}, nil
}

func loadStr(i *interp.Interpreter, v vmtypes.Boxed) string {
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

// loadI64 reads an int64 argument whether it arrived inline or spilled to a
// heap cell.
func loadI64(i *interp.Interpreter, v vmtypes.Boxed) int64 {
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
