package compiler

import (
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"

	"github.com/siyul-park/minipy/types"

	"github.com/siyul-park/minivm/interp"
	vmtypes "github.com/siyul-park/minivm/types"
)

// host holds the minivm host functions backing builtins. print
// writes to the configured sink; the rest are helpers for conversions and the
// operators with no single opcode.
//
// pow stays a host function: it needs a loop with temporaries, and minivm has
// no native exponentiation opcode. Float modulo lowers to the native F64_MOD
// opcode instead (see compiler.go's PERCENT case).
type host struct {
	print       *interp.HostFunction
	str         *interp.HostFunction
	rangeIter   *interp.HostFunction
	intParse    *interp.HostFunction
	floatParse  *interp.HostFunction
	powInt      *interp.HostFunction
	powFloat    *interp.HostFunction
	strIndex    *interp.HostFunction
	strUpper    *interp.HostFunction
	strLower    *interp.HostFunction
	strSplit    *interp.HostFunction
	strJoin     *interp.HostFunction
	strFind     *interp.HostFunction
	strContains *interp.HostFunction
	strSlice    *interp.HostFunction
	excInstance *interp.HostFunction
}

type rangeIterator struct {
	stop, step int64
	current    vmtypes.Boxed
	done       bool
}

func newRangeIterator(start, stop, step int64) *rangeIterator {
	it := &rangeIterator{stop: stop, step: step, done: true}
	if step > 0 {
		it.done = start >= stop
	} else {
		it.done = start <= stop
	}
	if !it.done {
		it.current = vmtypes.BoxI64(start)
	}
	return it
}

func (it *rangeIterator) Kind() vmtypes.Kind { return vmtypes.KindRef }
func (it *rangeIterator) Type() vmtypes.Type { return vmtypes.TypeRef }
func (it *rangeIterator) String() string     { return "range.iterator" }
func (it *rangeIterator) Current() vmtypes.Value {
	if it.done {
		return vmtypes.BoxedNull
	}
	return it.current
}
func (it *rangeIterator) Done() bool { return it.done }
func (it *rangeIterator) Next() bool {
	if it.done {
		return false
	}
	next := it.current.I64() + it.step
	if (it.step > 0 && next >= it.stop) || (it.step < 0 && next <= it.stop) {
		it.current = vmtypes.BoxedNull
		it.done = true
		return false
	}
	it.current = vmtypes.BoxI64(next)
	return true
}

type boxedIterator struct {
	name    string
	values  []vmtypes.Boxed
	idx     int
	current vmtypes.Boxed
	done    bool
}

func newBoxedIterator(name string, values []vmtypes.Boxed) *boxedIterator {
	it := &boxedIterator{name: name, values: append([]vmtypes.Boxed(nil), values...), done: true}
	if len(values) > 0 {
		it.current = values[0]
		it.idx = 1
		it.done = false
	}
	return it
}

func (it *boxedIterator) Kind() vmtypes.Kind { return vmtypes.KindRef }
func (it *boxedIterator) Type() vmtypes.Type { return vmtypes.TypeRef }
func (it *boxedIterator) String() string     { return it.name }
func (it *boxedIterator) Current() vmtypes.Value {
	if it.done {
		return vmtypes.BoxedNull
	}
	return it.current
}
func (it *boxedIterator) Done() bool { return it.done }
func (it *boxedIterator) Next() bool {
	if it.idx >= len(it.values) {
		it.current = vmtypes.BoxedNull
		it.done = true
		return false
	}
	it.current = it.values[it.idx]
	it.idx++
	it.done = false
	return true
}
func (it *boxedIterator) Refs() []vmtypes.Ref {
	var refs []vmtypes.Ref
	for _, v := range it.values {
		if v.Kind() == vmtypes.KindRef && v.Ref() != 0 {
			refs = append(refs, vmtypes.Ref(v.Ref()))
		}
	}
	return refs
}

func (h *host) dictGet(receiver, result types.Type) *interp.HostFunction {
	dict := receiver.(*types.Dict)
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{receiver.VM(), dict.Key.VM(), result.VM()}, Returns: []vmtypes.Type{result.VM()}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			if val, ok := mapGet(i, params[0], params[1]); ok {
				return []vmtypes.Boxed{val}, nil
			}
			return []vmtypes.Boxed{params[2]}, nil
		},
	)
}

func (h *host) dictValues(receiver, result types.Type) *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{receiver.VM()}, Returns: []vmtypes.Type{result.VM()}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			_, vals := mapEntries(i, params[0])
			return allocArray(i, result.VM().(*vmtypes.ArrayType), vals)
		},
	)
}

func (h *host) dictItems(receiver, result types.Type) *interp.HostFunction {
	tupleType := result.(*types.List).Elem.VM().(*vmtypes.StructType)
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{receiver.VM()}, Returns: []vmtypes.Type{result.VM()}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			keys, vals := mapEntries(i, params[0])
			items := make([]vmtypes.Boxed, 0, len(keys))
			for idx := range keys {
				addr, err := i.Alloc(vmtypes.NewStruct(tupleType, keys[idx], vals[idx]))
				if err != nil {
					return nil, err
				}
				items = append(items, vmtypes.BoxRef(addr))
			}
			return allocArray(i, result.VM().(*vmtypes.ArrayType), items)
		},
	)
}

// dictRest returns a new dict holding receiver minus the keys in the second
// argument. It backs mapping-pattern `**rest` captures.
func (h *host) dictRest(receiver types.Type) *interp.HostFunction {
	keys := types.NewList(receiver.(*types.Dict).Key)
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{receiver.VM(), keys.VM()}, Returns: []vmtypes.Type{receiver.VM()}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			src, err := i.Load(params[0].Ref())
			if err != nil {
				return nil, err
			}
			mt, ok := src.Type().(*vmtypes.MapType)
			if !ok {
				return nil, fmt.Errorf("dict rest on non-map value")
			}
			ks, vs := mapEntries(i, params[0])
			_, exclude := arrayElems(i, params[1])
			out := vmtypes.NewMapForType(mt, len(ks))
			for idx, k := range ks {
				skip := false
				for _, ex := range exclude {
					if boxedEqual(i, k, ex) {
						skip = true
						break
					}
				}
				if !skip {
					mapSet(out, k, vs[idx])
				}
			}
			addr, err := i.Alloc(out)
			if err != nil {
				return nil, err
			}
			return []vmtypes.Boxed{vmtypes.BoxRef(addr)}, nil
		},
	)
}

// mapSet inserts (key, value) into a map value, dispatching on its concrete
// representation (mirrors mapGet).
func mapSet(m vmtypes.Value, key, val vmtypes.Boxed) {
	switch mm := m.(type) {
	case *vmtypes.TypedMap[bool]:
		mm.Set(key.Bool(), val)
	case *vmtypes.TypedMap[int32]:
		mm.Set(key.I32(), val)
	case *vmtypes.TypedMap[int64]:
		mm.Set(key.I64(), val)
	case *vmtypes.TypedMap[float32]:
		mm.Set(key.F32(), val)
	case *vmtypes.TypedMap[float64]:
		mm.Set(key.F64(), val)
	case *vmtypes.Map:
		mm.Set(mapKey(key), vmtypes.MapEntry{Key: key, Value: val})
	}
}

func (h *host) listContains(elem, receiver types.Type) *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{receiver.VM(), elem.VM()}, Returns: []vmtypes.Type{vmtypes.TypeI32}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			_, elems := arrayElems(i, params[0])
			for _, elem := range elems {
				if boxedEqual(i, elem, params[1]) {
					return []vmtypes.Boxed{vmtypes.BoxI32(1)}, nil
				}
			}
			return []vmtypes.Boxed{vmtypes.BoxI32(0)}, nil
		},
	)
}

func (h *host) listSlice(receiver types.Type) *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{receiver.VM(), vmtypes.TypeI64, vmtypes.TypeI64, vmtypes.TypeI64}, Returns: []vmtypes.Type{receiver.VM()}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			typ, elems := arrayElems(i, params[0])
			indexes, err := sliceIndexes(len(elems), loadI64(i, params[1]), loadI64(i, params[2]), loadI64(i, params[3]))
			if err != nil {
				return nil, err
			}
			out := make([]vmtypes.Boxed, 0, len(indexes))
			for _, idx := range indexes {
				out = append(out, elems[idx])
			}
			return allocArray(i, typ, out)
		},
	)
}

func (h *host) listExtend(receiver types.Type) *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{receiver.VM(), receiver.VM()}, Returns: []vmtypes.Type{receiver.VM()}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			typ, left := arrayElems(i, params[0])
			_, right := arrayElems(i, params[1])
			out := append(left, right...)
			return allocArray(i, typ, out)
		},
	)
}

func (h *host) dictMerge(receiver types.Type) *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{receiver.VM(), receiver.VM()}, Returns: []vmtypes.Type{receiver.VM()}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			src, err := i.Load(params[0].Ref())
			if err != nil {
				return nil, err
			}
			mt, ok := src.Type().(*vmtypes.MapType)
			if !ok {
				return nil, fmt.Errorf("dict merge on non-map value")
			}
			leftKeys, leftVals := mapEntries(i, params[0])
			rightKeys, rightVals := mapEntries(i, params[1])
			out := vmtypes.NewMapForType(mt, len(leftKeys)+len(rightKeys))
			for idx, key := range leftKeys {
				mapSet(out, key, leftVals[idx])
			}
			for idx, key := range rightKeys {
				mapSet(out, key, rightVals[idx])
			}
			addr, err := i.Alloc(out)
			if err != nil {
				return nil, err
			}
			return []vmtypes.Boxed{vmtypes.BoxRef(addr)}, nil
		},
	)
}

func (h *host) enumerate(result types.Type) *interp.HostFunction {
	list := result.(*types.List)
	tupleType := list.Elem.VM().(*vmtypes.StructType)
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{types.NewList(list.Elem.(*types.Tuple).Elems[1]).VM()}, Returns: []vmtypes.Type{result.VM()}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			_, elems := arrayElems(i, params[0])
			out := make([]vmtypes.Boxed, 0, len(elems))
			for idx, elem := range elems {
				addr, err := i.Alloc(vmtypes.NewStruct(tupleType, vmtypes.BoxI64(int64(idx)), elem))
				if err != nil {
					return nil, err
				}
				out = append(out, vmtypes.BoxRef(addr))
			}
			return allocArray(i, result.VM().(*vmtypes.ArrayType), out)
		},
	)
}

func (h *host) zip(result types.Type) *interp.HostFunction {
	list := result.(*types.List)
	tupleType := list.Elem.VM().(*vmtypes.StructType)
	tuple := list.Elem.(*types.Tuple)
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{types.NewList(tuple.Elems[0]).VM(), types.NewList(tuple.Elems[1]).VM()}, Returns: []vmtypes.Type{result.VM()}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			_, a := arrayElems(i, params[0])
			_, b := arrayElems(i, params[1])
			n := len(a)
			if len(b) < n {
				n = len(b)
			}
			out := make([]vmtypes.Boxed, 0, n)
			for idx := 0; idx < n; idx++ {
				addr, err := i.Alloc(vmtypes.NewStruct(tupleType, a[idx], b[idx]))
				if err != nil {
					return nil, err
				}
				out = append(out, vmtypes.BoxRef(addr))
			}
			return allocArray(i, result.VM().(*vmtypes.ArrayType), out)
		},
	)
}

func (h *host) listIter(arg types.Type) *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{arg.VM()}, Returns: []vmtypes.Type{vmtypes.TypeRef}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			_, elems := arrayElems(i, params[0])
			addr, err := i.Alloc(newBoxedIterator("list.iterator", elems))
			if err != nil {
				return nil, err
			}
			return []vmtypes.Boxed{vmtypes.BoxRef(addr)}, nil
		},
	)
}

func (h *host) strIter() *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{vmtypes.TypeString}, Returns: []vmtypes.Type{vmtypes.TypeRef}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			s := loadStr(i, params[0])
			values := make([]vmtypes.Boxed, 0, len([]rune(s)))
			for _, r := range s {
				addr, err := i.Alloc(vmtypes.String(string(r)))
				if err != nil {
					return nil, err
				}
				values = append(values, vmtypes.BoxRef(addr))
			}
			addr, err := i.Alloc(newBoxedIterator("str.iterator", values))
			if err != nil {
				return nil, err
			}
			return []vmtypes.Boxed{vmtypes.BoxRef(addr)}, nil
		},
	)
}

func (h *host) format(t types.Type) *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{t.VM(), vmtypes.TypeString}, Returns: []vmtypes.Type{vmtypes.TypeString}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			return allocString(i, pyFormat(i, params[0], loadStr(i, params[1])))
		},
	)
}

// isBuiltin reports whether name is a builtin.
func isBuiltin(name string) bool {
	switch name {
	case "print", "str", "int", "float", "bool", "abs", "len", "enumerate", "zip", "range", "iter", "next":
		return true
	default:
		return false
	}
}

// builtinReturn returns the result type of a builtin call, or ok=false when the
// argument types are unsupported.
func builtinReturn(name string, args []types.Type) (types.Type, bool) {
	switch name {
	case "print", "str":
		if len(args) != 1 {
			return types.Invalid, false
		}
		arg := args[0]
		if types.Printable(arg) {
			if name == "print" {
				return types.None, true
			}
			return types.Str, true
		}
	case "int", "float", "bool", "abs":
		if len(args) != 1 {
			return types.Invalid, false
		}
		arg := args[0]
		switch name {
		case "int":
			if convertible(arg) {
				return types.Int, true
			}
		case "float":
			if convertible(arg) {
				return types.Float, true
			}
		case "bool":
			if convertible(arg) || isContainer(arg) {
				return types.Bool, true
			}
		case "abs":
			if types.Equal(arg, types.Int) || types.Equal(arg, types.Float) {
				return arg, true
			}
		}
	case "len":
		if len(args) != 1 {
			return types.Invalid, false
		}
		if _, ok := args[0].(*types.List); ok {
			return types.Int, true
		}
		if _, ok := args[0].(*types.Dict); ok {
			return types.Int, true
		}
		if _, ok := args[0].(*types.Set); ok {
			return types.Int, true
		}
		if _, ok := args[0].(*types.Tuple); ok {
			return types.Int, true
		}
		if types.Equal(args[0], types.Str) {
			return types.Int, true
		}
	case "enumerate":
		if len(args) != 1 {
			return types.Invalid, false
		}
		if list, ok := args[0].(*types.List); ok {
			return types.NewList(types.NewTuple(types.Int, list.Elem)), true
		}
	case "zip":
		if len(args) != 2 {
			return types.Invalid, false
		}
		a, aok := args[0].(*types.List)
		b, bok := args[1].(*types.List)
		if aok && bok {
			return types.NewList(types.NewTuple(a.Elem, b.Elem)), true
		}
	case "range":
		if len(args) < 1 || len(args) > 3 {
			return types.Invalid, false
		}
		for _, arg := range args {
			if !types.Equal(arg, types.Int) {
				return types.Invalid, false
			}
		}
		return types.NewIterator(types.Int), true
	case "iter":
		if len(args) != 1 {
			return types.Invalid, false
		}
		elem := iterableElem(args[0])
		if elem != types.Invalid {
			return types.NewIterator(elem), true
		}
	case "next":
		if len(args) != 1 {
			return types.Invalid, false
		}
		if it, ok := args[0].(*types.Iterator); ok {
			return it.Elem, true
		}
	}
	return types.Invalid, false
}

func convertible(t types.Type) bool {
	return types.Equal(t, types.Int) || types.Equal(t, types.Float) || types.Equal(t, types.Bool) || types.Equal(t, types.Str)
}

func isContainer(t types.Type) bool {
	switch t.(type) {
	case *types.List, *types.Dict, *types.Set, *types.Tuple, *types.Iterator:
		return true
	default:
		return false
	}
}

// newHost builds the host-function set, binding print's output to out.
func newHost(out io.Writer, classes map[string]*class) *host {
	excType := classes["BaseException"].typ.VM().(*vmtypes.StructType)
	classID := func(name string) int64 { return int64(classes[name].classID) }
	return &host{
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
		rangeIter: interp.NewHostFunction(
			&vmtypes.FunctionType{Params: []vmtypes.Type{vmtypes.TypeI64, vmtypes.TypeI64, vmtypes.TypeI64}, Returns: []vmtypes.Type{vmtypes.TypeRef}},
			func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
				step := loadI64(i, params[2])
				if step == 0 {
					return nil, fmt.Errorf("range() step must not be zero")
				}
				addr, err := i.Alloc(newRangeIterator(loadI64(i, params[0]), loadI64(i, params[1]), step))
				if err != nil {
					return nil, err
				}
				return []vmtypes.Boxed{vmtypes.BoxRef(addr)}, nil
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
		),
		powFloat: interp.NewHostFunction(
			&vmtypes.FunctionType{Params: []vmtypes.Type{vmtypes.TypeF64, vmtypes.TypeF64}, Returns: []vmtypes.Type{vmtypes.TypeF64}},
			func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
				return []vmtypes.Boxed{vmtypes.BoxF64(math.Pow(params[0].F64(), params[1].F64()))}, nil
			},
		),
		strIndex: interp.NewHostFunction(
			&vmtypes.FunctionType{Params: []vmtypes.Type{vmtypes.TypeString, vmtypes.TypeI64}, Returns: []vmtypes.Type{vmtypes.TypeString}},
			func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
				s := []rune(loadStr(i, params[0]))
				idx := int(loadI64(i, params[1]))
				if idx < 0 {
					idx += len(s)
				}
				if idx < 0 || idx >= len(s) {
					return nil, interp.ErrIndexOutOfRange
				}
				return allocString(i, string(s[idx]))
			},
		),
		strUpper: interp.NewHostFunction(
			&vmtypes.FunctionType{Params: []vmtypes.Type{vmtypes.TypeString}, Returns: []vmtypes.Type{vmtypes.TypeString}},
			func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
				return allocString(i, strings.ToUpper(loadStr(i, params[0])))
			},
		),
		strLower: interp.NewHostFunction(
			&vmtypes.FunctionType{Params: []vmtypes.Type{vmtypes.TypeString}, Returns: []vmtypes.Type{vmtypes.TypeString}},
			func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
				return allocString(i, strings.ToLower(loadStr(i, params[0])))
			},
		),
		strFind: interp.NewHostFunction(
			&vmtypes.FunctionType{Params: []vmtypes.Type{vmtypes.TypeString, vmtypes.TypeString}, Returns: []vmtypes.Type{vmtypes.TypeI64}},
			func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
				return []vmtypes.Boxed{vmtypes.BoxI64(int64(strings.Index(loadStr(i, params[0]), loadStr(i, params[1]))))}, nil
			},
		),
		strSlice: interp.NewHostFunction(
			&vmtypes.FunctionType{Params: []vmtypes.Type{vmtypes.TypeString, vmtypes.TypeI64, vmtypes.TypeI64, vmtypes.TypeI64}, Returns: []vmtypes.Type{vmtypes.TypeString}},
			func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
				runes := []rune(loadStr(i, params[0]))
				indexes, err := sliceIndexes(len(runes), loadI64(i, params[1]), loadI64(i, params[2]), loadI64(i, params[3]))
				if err != nil {
					return nil, err
				}
				var b strings.Builder
				for _, idx := range indexes {
					b.WriteRune(runes[idx])
				}
				return allocString(i, b.String())
			},
		),
		strContains: interp.NewHostFunction(
			&vmtypes.FunctionType{Params: []vmtypes.Type{vmtypes.TypeString, vmtypes.TypeString}, Returns: []vmtypes.Type{vmtypes.TypeI32}},
			func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
				if strings.Contains(loadStr(i, params[0]), loadStr(i, params[1])) {
					return []vmtypes.Boxed{vmtypes.BoxI32(1)}, nil
				}
				return []vmtypes.Boxed{vmtypes.BoxI32(0)}, nil
			},
		),
		strSplit: interp.NewHostFunction(
			&vmtypes.FunctionType{Params: []vmtypes.Type{vmtypes.TypeString, vmtypes.TypeString}, Returns: []vmtypes.Type{vmtypes.NewArrayType(vmtypes.TypeString)}},
			func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
				parts := strings.Split(loadStr(i, params[0]), loadStr(i, params[1]))
				out := make([]vmtypes.Boxed, 0, len(parts))
				for _, part := range parts {
					box, err := allocString(i, part)
					if err != nil {
						return nil, err
					}
					out = append(out, box[0])
				}
				return allocArray(i, vmtypes.NewArrayType(vmtypes.TypeString), out)
			},
		),
		strJoin: interp.NewHostFunction(
			&vmtypes.FunctionType{Params: []vmtypes.Type{vmtypes.TypeString, vmtypes.NewArrayType(vmtypes.TypeString)}, Returns: []vmtypes.Type{vmtypes.TypeString}},
			func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
				_, elems := arrayElems(i, params[1])
				parts := make([]string, len(elems))
				for idx, elem := range elems {
					parts[idx] = loadStr(i, elem)
				}
				return allocString(i, strings.Join(parts, loadStr(i, params[0])))
			},
		),
		excInstance: interp.NewHostFunction(
			&vmtypes.FunctionType{Params: []vmtypes.Type{vmtypes.TypeRef}, Returns: []vmtypes.Type{classes["BaseException"].typ.VM()}},
			func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
				class := classID("RuntimeError")
				message := ""
				if params[0].Kind() == vmtypes.KindRef && params[0].Ref() != 0 {
					if val, err := i.Load(params[0].Ref()); err == nil {
						if exc, ok := val.(*vmtypes.Error); ok {
							message = exc.Error()
							switch {
							case errors.Is(exc.Unwrap(), interp.ErrDivideByZero):
								class = classID("ZeroDivisionError")
							case errors.Is(exc.Unwrap(), interp.ErrIndexOutOfRange):
								class = classID("IndexError")
							case errors.Is(exc.Unwrap(), interp.ErrTypeMismatch):
								class = classID("TypeError")
							}
						}
					}
				}
				msg, err := allocString(i, message)
				if err != nil {
					return nil, err
				}
				addr, err := i.Alloc(vmtypes.NewStruct(excType, vmtypes.BoxI64(class), msg[0]))
				if err != nil {
					return nil, err
				}
				return []vmtypes.Boxed{vmtypes.BoxRef(addr)}, nil
			},
		),
	}
}

// formatScalar renders a boxed scalar the way Python's str()/print() would.
func formatScalar(i *interp.Interpreter, v vmtypes.Boxed) string {
	switch v.Kind() {
	// bool lowers to i1; literals still arrive as i32 (shared representation).
	case vmtypes.KindI1, vmtypes.KindI32:
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

func pyFormat(i *interp.Interpreter, v vmtypes.Boxed, spec string) string {
	if spec == "" {
		return formatScalar(i, v)
	}
	if v.Kind() == vmtypes.KindI64 && strings.HasSuffix(spec, "d") {
		widthSpec := strings.TrimSuffix(spec, "d")
		pad := byte(' ')
		if strings.HasPrefix(widthSpec, "0") {
			pad = '0'
			widthSpec = strings.TrimPrefix(widthSpec, "0")
		}
		width, _ := strconv.Atoi(widthSpec)
		s := strconv.FormatInt(loadI64(i, v), 10)
		if width > len(s) {
			s = strings.Repeat(string(pad), width-len(s)) + s
		}
		return s
	}
	return formatScalar(i, v)
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

const omittedSliceBound = math.MinInt64

func sliceIndexes(length int, rawStart, rawStop, rawStep int64) ([]int, error) {
	step := rawStep
	if step == omittedSliceBound {
		step = 1
	}
	if step == 0 {
		return nil, fmt.Errorf("slice step cannot be zero")
	}
	startOmitted := rawStart == omittedSliceBound
	stopOmitted := rawStop == omittedSliceBound
	start, stop := int(rawStart), int(rawStop)
	if step > 0 {
		if startOmitted {
			start = 0
		} else if start < 0 {
			start += length
		}
		if stopOmitted {
			stop = length
		} else if stop < 0 {
			stop += length
		}
		if start < 0 {
			start = 0
		}
		if start > length {
			start = length
		}
		if stop < 0 {
			stop = 0
		}
		if stop > length {
			stop = length
		}
		var out []int
		for i := start; i < stop; i += int(step) {
			out = append(out, i)
		}
		return out, nil
	}
	if startOmitted {
		start = length - 1
	} else if start < 0 {
		start += length
	}
	if stopOmitted {
		stop = -1
	} else if stop < 0 {
		stop += length
	}
	if start < -1 {
		start = -1
	}
	if start >= length {
		start = length - 1
	}
	if stop < -1 {
		stop = -1
	}
	if stop >= length {
		stop = length - 1
	}
	var out []int
	for i := start; i > stop; i += int(step) {
		out = append(out, i)
	}
	return out, nil
}

func allocArray(i *interp.Interpreter, typ *vmtypes.ArrayType, elems []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
	addr, err := i.Alloc(vmtypes.NewArray(typ, elems...))
	if err != nil {
		return nil, err
	}
	return []vmtypes.Boxed{vmtypes.BoxRef(addr)}, nil
}

func arrayElems(i *interp.Interpreter, ref vmtypes.Boxed) (*vmtypes.ArrayType, []vmtypes.Boxed) {
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

func mapGet(i *interp.Interpreter, ref vmtypes.Boxed, key vmtypes.Boxed) (vmtypes.Boxed, bool) {
	val, err := i.Load(ref.Ref())
	if err != nil {
		return 0, false
	}
	switch m := val.(type) {
	case *vmtypes.TypedMap[bool]:
		return m.Get(key.Bool())
	case *vmtypes.TypedMap[int32]:
		return m.Get(key.I32())
	case *vmtypes.TypedMap[int64]:
		return m.Get(loadI64(i, key))
	case *vmtypes.TypedMap[float32]:
		return m.Get(key.F32())
	case *vmtypes.TypedMap[float64]:
		return m.Get(key.F64())
	case *vmtypes.Map:
		entry, ok := m.Get(mapKey(key))
		return entry.Value, ok
	default:
		return 0, false
	}
}

func mapEntries(i *interp.Interpreter, ref vmtypes.Boxed) ([]vmtypes.Boxed, []vmtypes.Boxed) {
	val, err := i.Load(ref.Ref())
	if err != nil {
		return nil, nil
	}
	var keys, vals []vmtypes.Boxed
	switch m := val.(type) {
	case *vmtypes.TypedMap[bool]:
		m.Range(func(k bool, v vmtypes.Boxed) {
			keys = append(keys, vmtypes.BoxI1(k))
			vals = append(vals, v)
		})
	case *vmtypes.TypedMap[int32]:
		m.Range(func(k int32, v vmtypes.Boxed) {
			keys = append(keys, vmtypes.BoxI32(k))
			vals = append(vals, v)
		})
	case *vmtypes.TypedMap[int64]:
		m.Range(func(k int64, v vmtypes.Boxed) {
			keys = append(keys, vmtypes.BoxI64(k))
			vals = append(vals, v)
		})
	case *vmtypes.TypedMap[float32]:
		m.Range(func(k float32, v vmtypes.Boxed) {
			keys = append(keys, vmtypes.BoxF32(k))
			vals = append(vals, v)
		})
	case *vmtypes.TypedMap[float64]:
		m.Range(func(k float64, v vmtypes.Boxed) {
			keys = append(keys, vmtypes.BoxF64(k))
			vals = append(vals, v)
		})
	case *vmtypes.Map:
		m.Range(func(_ vmtypes.MapKey, entry vmtypes.MapEntry) {
			keys = append(keys, entry.Key)
			vals = append(vals, entry.Value)
		})
	}
	return keys, vals
}

func mapKey(v vmtypes.Boxed) vmtypes.MapKey {
	switch v.Kind() {
	// bool keys may arrive as i1 (comparisons) or i32 (literals); canonicalize
	// to one i32 key so the two representations hash and compare alike.
	case vmtypes.KindI1, vmtypes.KindI32:
		return vmtypes.MapKey{Kind: vmtypes.KindI32, Bits: uint64(uint32(v.I32()))}
	case vmtypes.KindI64:
		return vmtypes.MapKey{Kind: vmtypes.KindI64, Bits: uint64(v.I64())}
	case vmtypes.KindF32:
		return vmtypes.MapKey{Kind: vmtypes.KindF32, Bits: uint64(math.Float32bits(v.F32()))}
	case vmtypes.KindF64:
		return vmtypes.MapKey{Kind: vmtypes.KindF64, Bits: math.Float64bits(v.F64())}
	default:
		return vmtypes.MapKey{Kind: vmtypes.KindRef, Bits: uint64(v.Ref())}
	}
}

func boxedEqual(i *interp.Interpreter, a, b vmtypes.Boxed) bool {
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
