package compiler

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/siyul-park/minipy/builtins"
	"github.com/siyul-park/minipy/hostabi"
	"github.com/siyul-park/minipy/types"

	"github.com/siyul-park/minivm/interp"
	vmtypes "github.com/siyul-park/minivm/types"
)

func (c *lowerer) dictGet(receiver, result types.Type) *interp.HostFunction {
	dict := receiver.(*types.Dict)
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{receiver.VM(), dict.Key.VM(), result.VM()}, Returns: []vmtypes.Type{result.VM()}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			val, ok, err := mapGet(i, params[0], params[1])
			if err != nil {
				return nil, err
			}
			if ok {
				return []vmtypes.Boxed{val}, nil
			}
			return []vmtypes.Boxed{params[2]}, nil
		},
	)
}

func (c *lowerer) dictValues(receiver, result types.Type) *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{receiver.VM()}, Returns: []vmtypes.Type{result.VM()}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			_, vals, err := mapEntries(i, params[0])
			if err != nil {
				return nil, err
			}
			return hostabi.AllocArray(i, result.VM().(*vmtypes.ArrayType), vals)
		},
	)
}

func (c *lowerer) dictItems(receiver, result types.Type) *interp.HostFunction {
	tupleType := result.(*types.List).Elem.VM().(*vmtypes.StructType)
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{receiver.VM()}, Returns: []vmtypes.Type{result.VM()}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			keys, vals, err := mapEntries(i, params[0])
			if err != nil {
				return nil, err
			}
			items := make([]vmtypes.Boxed, 0, len(keys))
			for idx := range keys {
				addr, err := i.Alloc(vmtypes.NewStruct(tupleType, keys[idx], vals[idx]))
				if err != nil {
					return nil, err
				}
				items = append(items, vmtypes.BoxRef(addr))
			}
			return hostabi.AllocArray(i, result.VM().(*vmtypes.ArrayType), items)
		},
	)
}

// dictRest returns a new dict holding receiver minus the keys in the second
// argument. It backs mapping-pattern `**rest` captures.
func (c *lowerer) dictRest(receiver types.Type) *interp.HostFunction {
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
			ks, vs, err := mapEntries(i, params[0])
			if err != nil {
				return nil, err
			}
			_, exclude, err := hostabi.ArrayElems(i, params[1])
			if err != nil {
				return nil, err
			}
			out := vmtypes.NewMapForType(mt, len(ks))
			for idx, k := range ks {
				skip := false
				for _, ex := range exclude {
					equal, err := hostabi.BoxedEqual(i, k, ex)
					if err != nil {
						return nil, err
					}
					if equal {
						skip = true
						break
					}
				}
				if !skip {
					if err := mapSet(out, k, vs[idx]); err != nil {
						return nil, err
					}
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
func mapSet(m vmtypes.Value, key, val vmtypes.Boxed) error {
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
	default:
		return interp.ErrTypeMismatch
	}
	return nil
}

// arraySlice builds `receiver[a:b:c]` for any array-backed VM type (list or
// bytes): it works on the generic array element view (hostabi.ArrayElems),
// so it stays receiver-agnostic and returns a freshly allocated array of the
// same VM type rather than mutating the receiver.
func (c *lowerer) arraySlice(receiver types.Type) *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{receiver.VM(), vmtypes.TypeI64, vmtypes.TypeI64, vmtypes.TypeI64}, Returns: []vmtypes.Type{receiver.VM()}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			typ, elems, err := hostabi.ArrayElems(i, params[0])
			if err != nil {
				return nil, err
			}
			start, stop, step, err := loadSliceBounds(i, params)
			if err != nil {
				return nil, err
			}
			indexes, err := sliceIndexes(len(elems), start, stop, step)
			if err != nil {
				return nil, err
			}
			out := make([]vmtypes.Boxed, 0, len(indexes))
			for _, idx := range indexes {
				out = append(out, elems[idx])
			}
			return hostabi.AllocArray(i, typ, out)
		},
	)
}

func (c *lowerer) listSliceAssign(receiver types.Type) *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{receiver.VM(), vmtypes.TypeI64, vmtypes.TypeI64, vmtypes.TypeI64, receiver.VM()}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			typ, elems, err := hostabi.ArrayElems(i, params[0])
			if err != nil {
				return nil, err
			}
			_, values, err := hostabi.ArrayElems(i, params[4])
			if err != nil {
				return nil, err
			}
			rawStart, rawStop, rawStep, err := loadSliceBounds(i, params)
			if err != nil {
				return nil, err
			}
			start, stop, err := normalizeSliceRange(len(elems), rawStart, rawStop, rawStep)
			if err != nil {
				return nil, err
			}
			if len(values) != stop-start {
				return nil, errListSliceLength
			}
			if err := retainBoxes(i, values); err != nil {
				return nil, err
			}
			if err := releaseBoxes(i, elems[start:stop]); err != nil {
				return nil, err
			}
			out := append([]vmtypes.Boxed(nil), elems...)
			copy(out[start:stop], values)
			if err := i.Store(params[0].Ref(), vmtypes.NewArray(typ, out...)); err != nil {
				return nil, err
			}
			return nil, nil
		},
	)
}

func (c *lowerer) listSliceDelete(receiver types.Type) *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{receiver.VM(), vmtypes.TypeI64, vmtypes.TypeI64, vmtypes.TypeI64}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			typ, elems, err := hostabi.ArrayElems(i, params[0])
			if err != nil {
				return nil, err
			}
			rawStart, rawStop, rawStep, err := loadSliceBounds(i, params)
			if err != nil {
				return nil, err
			}
			start, stop, err := normalizeSliceRange(len(elems), rawStart, rawStop, rawStep)
			if err != nil {
				return nil, err
			}
			if err := releaseBoxes(i, elems[start:stop]); err != nil {
				return nil, err
			}
			out := append([]vmtypes.Boxed(nil), elems[:start]...)
			out = append(out, elems[stop:]...)
			if err := i.Store(params[0].Ref(), vmtypes.NewArray(typ, out...)); err != nil {
				return nil, err
			}
			return nil, nil
		},
	)
}

func retainBoxes(i *interp.Interpreter, values []vmtypes.Boxed) error {
	for _, value := range values {
		if value.Kind() == vmtypes.KindRef && value.Ref() != 0 {
			if _, err := i.Retain(value.Ref()); err != nil {
				return err
			}
		}
	}
	return nil
}

func releaseBoxes(i *interp.Interpreter, values []vmtypes.Boxed) error {
	for _, value := range values {
		if value.Kind() == vmtypes.KindRef && value.Ref() != 0 {
			if err := i.Release(value.Ref()); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *lowerer) listIndex(receiver types.Type) *interp.HostFunction {
	elem := receiver.(*types.List).Elem
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{receiver.VM(), elem.VM()}, Returns: []vmtypes.Type{vmtypes.TypeI64}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			_, elems, err := hostabi.ArrayElems(i, params[0])
			if err != nil {
				return nil, err
			}
			for idx, elem := range elems {
				equal, err := hostabi.BoxedEqual(i, elem, params[1])
				if err != nil {
					return nil, err
				}
				if equal {
					return []vmtypes.Boxed{vmtypes.BoxI64(int64(idx))}, nil
				}
			}
			return nil, errListIndexValue
		},
	)
}

func (c *lowerer) listExtend(receiver types.Type) *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{receiver.VM(), receiver.VM()}, Returns: []vmtypes.Type{receiver.VM()}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			typ, left, err := hostabi.ArrayElems(i, params[0])
			if err != nil {
				return nil, err
			}
			_, right, err := hostabi.ArrayElems(i, params[1])
			if err != nil {
				return nil, err
			}
			out := append(left, right...)
			return hostabi.AllocArray(i, typ, out)
		},
	)
}

func (c *lowerer) dictMerge(receiver types.Type) *interp.HostFunction {
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
			leftKeys, leftVals, err := mapEntries(i, params[0])
			if err != nil {
				return nil, err
			}
			rightKeys, rightVals, err := mapEntries(i, params[1])
			if err != nil {
				return nil, err
			}
			out := vmtypes.NewMapForType(mt, len(leftKeys)+len(rightKeys))
			for idx, key := range leftKeys {
				if err := mapSet(out, key, leftVals[idx]); err != nil {
					return nil, err
				}
			}
			for idx, key := range rightKeys {
				if err := mapSet(out, key, rightVals[idx]); err != nil {
					return nil, err
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

func (c *lowerer) listIter(arg types.Type) *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{arg.VM()}, Returns: []vmtypes.Type{vmtypes.TypeRef}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			_, elems, err := hostabi.ArrayElems(i, params[0])
			if err != nil {
				return nil, err
			}
			addr, err := i.Alloc(hostabi.NewIterator("list.iterator", elems))
			if err != nil {
				return nil, err
			}
			return []vmtypes.Boxed{vmtypes.BoxRef(addr)}, nil
		},
	)
}

func (c *lowerer) strIter() *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{vmtypes.TypeString}, Returns: []vmtypes.Type{vmtypes.TypeRef}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			s, err := hostabi.LoadStr(i, params[0])
			if err != nil {
				return nil, err
			}
			values := make([]vmtypes.Boxed, 0, len([]rune(s)))
			for _, r := range s {
				addr, err := i.Alloc(vmtypes.String(string(r)))
				if err != nil {
					return nil, err
				}
				values = append(values, vmtypes.BoxRef(addr))
			}
			addr, err := i.Alloc(hostabi.NewIterator("str.iterator", values))
			if err != nil {
				return nil, err
			}
			return []vmtypes.Boxed{vmtypes.BoxRef(addr)}, nil
		},
	)
}

func (c *lowerer) format(t types.Type) *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{t.VM(), vmtypes.TypeString}, Returns: []vmtypes.Type{vmtypes.TypeString}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			spec, err := hostabi.LoadStr(i, params[1])
			if err != nil {
				return nil, err
			}
			text, err := pyFormat(i, params[0], spec)
			if err != nil {
				return nil, err
			}
			return hostabi.AllocString(i, text)
		},
	)
}

// reprHost renders a value with repr()/ascii() rules: strings gain quotes and
// escapes, other scalars render like str(). It is static per source type, not a
// runtime __repr__ dispatch.
func (c *lowerer) reprHost(t types.Type, ascii bool) *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{t.VM()}, Returns: []vmtypes.Type{vmtypes.TypeString}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			text, err := pyRepr(i, params[0], ascii)
			if err != nil {
				return nil, err
			}
			return hostabi.AllocString(i, text)
		},
	)
}

func (c *lowerer) strIndex() *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{vmtypes.TypeString, vmtypes.TypeI64}, Returns: []vmtypes.Type{vmtypes.TypeString}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			text, err := hostabi.LoadStr(i, params[0])
			if err != nil {
				return nil, err
			}
			index, err := hostabi.LoadI64(i, params[1])
			if err != nil {
				return nil, err
			}
			s := []rune(text)
			idx := int(index)
			if idx < 0 {
				idx += len(s)
			}
			if idx < 0 || idx >= len(s) {
				return nil, interp.ErrIndexOutOfRange
			}
			return hostabi.AllocString(i, string(s[idx]))
		},
	)
}

func (c *lowerer) strUpper() *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{vmtypes.TypeString}, Returns: []vmtypes.Type{vmtypes.TypeString}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			text, err := hostabi.LoadStr(i, params[0])
			if err != nil {
				return nil, err
			}
			return hostabi.AllocString(i, strings.ToUpper(text))
		},
	)
}

func (c *lowerer) strLower() *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{vmtypes.TypeString}, Returns: []vmtypes.Type{vmtypes.TypeString}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			text, err := hostabi.LoadStr(i, params[0])
			if err != nil {
				return nil, err
			}
			return hostabi.AllocString(i, strings.ToLower(text))
		},
	)
}

func (c *lowerer) strFind() *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{vmtypes.TypeString, vmtypes.TypeString}, Returns: []vmtypes.Type{vmtypes.TypeI64}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			text, err := hostabi.LoadStr(i, params[0])
			if err != nil {
				return nil, err
			}
			needle, err := hostabi.LoadStr(i, params[1])
			if err != nil {
				return nil, err
			}
			return []vmtypes.Boxed{vmtypes.BoxI64(int64(strings.Index(text, needle)))}, nil
		},
	)
}

func (c *lowerer) strSlice() *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{vmtypes.TypeString, vmtypes.TypeI64, vmtypes.TypeI64, vmtypes.TypeI64}, Returns: []vmtypes.Type{vmtypes.TypeString}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			text, err := hostabi.LoadStr(i, params[0])
			if err != nil {
				return nil, err
			}
			start, stop, step, err := loadSliceBounds(i, params)
			if err != nil {
				return nil, err
			}
			runes := []rune(text)
			indexes, err := sliceIndexes(len(runes), start, stop, step)
			if err != nil {
				return nil, err
			}
			var b strings.Builder
			for _, idx := range indexes {
				b.WriteRune(runes[idx])
			}
			return hostabi.AllocString(i, b.String())
		},
	)
}

func (c *lowerer) strSplit() *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{vmtypes.TypeString, vmtypes.TypeString}, Returns: []vmtypes.Type{vmtypes.NewArrayType(vmtypes.TypeString)}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			text, err := hostabi.LoadStr(i, params[0])
			if err != nil {
				return nil, err
			}
			separator, err := hostabi.LoadStr(i, params[1])
			if err != nil {
				return nil, err
			}
			parts := strings.Split(text, separator)
			out := make([]vmtypes.Boxed, 0, len(parts))
			for _, part := range parts {
				box, err := hostabi.AllocString(i, part)
				if err != nil {
					return nil, err
				}
				out = append(out, box[0])
			}
			return hostabi.AllocArray(i, vmtypes.NewArrayType(vmtypes.TypeString), out)
		},
	)
}

func (c *lowerer) strJoin() *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{vmtypes.TypeString, vmtypes.NewArrayType(vmtypes.TypeString)}, Returns: []vmtypes.Type{vmtypes.TypeString}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			separator, err := hostabi.LoadStr(i, params[0])
			if err != nil {
				return nil, err
			}
			_, elems, err := hostabi.ArrayElems(i, params[1])
			if err != nil {
				return nil, err
			}
			parts := make([]string, len(elems))
			for idx, elem := range elems {
				parts[idx], err = hostabi.LoadStr(i, elem)
				if err != nil {
					return nil, err
				}
			}
			return hostabi.AllocString(i, strings.Join(parts, separator))
		},
	)
}

func (c *lowerer) exc() *interp.HostFunction {
	excType := c.classes["BaseException"].typ.VM().(*vmtypes.StructType)
	classID := func(name string) int64 { return int64(c.classes[name].classID) }
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{vmtypes.TypeRef}, Returns: []vmtypes.Type{c.classes["BaseException"].typ.VM()}},
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
						case errors.Is(exc.Unwrap(), errListIndexValue):
							class = classID("ValueError")
						case errors.Is(exc.Unwrap(), errListSliceLength):
							class = classID("ValueError")
						case errors.Is(exc.Unwrap(), builtins.ErrOrdValue):
							class = classID("ValueError")
						case errors.Is(exc.Unwrap(), builtins.ErrChrValue):
							class = classID("ValueError")
						}
					}
				}
			}
			msg, err := hostabi.AllocString(i, message)
			if err != nil {
				return nil, err
			}
			addr, err := i.Alloc(vmtypes.NewStruct(excType, vmtypes.BoxI64(class), msg[0]))
			if err != nil {
				return nil, err
			}
			return []vmtypes.Boxed{vmtypes.BoxRef(addr)}, nil
		},
	)
}

func parseFormatSpec(spec string) formatSpec {
	f := formatSpec{fill: ' ', precision: -1}
	i := 0
	// [[fill]align]
	if len(spec) >= 2 && isAlign(spec[1]) {
		f.fill, f.align = spec[0], spec[1]
		i = 2
	} else if len(spec) >= 1 && isAlign(spec[0]) {
		f.align = spec[0]
		i = 1
	}
	// [sign]
	if i < len(spec) && (spec[i] == '+' || spec[i] == '-' || spec[i] == ' ') {
		f.sign = spec[i]
		i++
	}
	// ['#'] alternate form — accepted but not applied
	if i < len(spec) && spec[i] == '#' {
		i++
	}
	// ['0'] zero-padding implies '=' alignment with '0' fill
	if i < len(spec) && spec[i] == '0' {
		f.zero = true
		if f.align == 0 {
			f.align, f.fill = '=', '0'
		}
		i++
	}
	// [width]
	for i < len(spec) && spec[i] >= '0' && spec[i] <= '9' {
		f.width = f.width*10 + int(spec[i]-'0')
		i++
	}
	// [grouping] — accepted but not applied
	if i < len(spec) && (spec[i] == ',' || spec[i] == '_') {
		i++
	}
	// ['.'precision]
	if i < len(spec) && spec[i] == '.' {
		i++
		f.precision = 0
		for i < len(spec) && spec[i] >= '0' && spec[i] <= '9' {
			f.precision = f.precision*10 + int(spec[i]-'0')
			i++
		}
	}
	// [type]
	if i < len(spec) {
		f.typ = spec[i]
	}
	return f
}

func isAlign(b byte) bool { return b == '<' || b == '>' || b == '^' || b == '=' }

// pyFormat applies a Python format spec to a boxed scalar. It supports the v1
// scalar subset: fill/alignment, sign, zero-padding, width, precision, and the
// common presentation types (d, b, o, x/X, f/F, e/E, g/G, %, s, c).
func pyFormat(i *interp.Interpreter, v vmtypes.Boxed, spec string) (string, error) {
	if spec == "" {
		return hostabi.FormatScalar(i, v), nil
	}
	f := parseFormatSpec(spec)
	body, sign, numeric, err := formatBody(i, v, f)
	if err != nil {
		return "", err
	}
	return padFormat(body, sign, f, numeric), nil
}

// formatBody renders the value's digits/text without width padding, returning
// the unsigned body, the sign prefix, and whether the value is numeric (so the
// caller can apply '=' zero-padding between the sign and the digits).
func formatBody(i *interp.Interpreter, v vmtypes.Boxed, f formatSpec) (body, sign string, numeric bool, err error) {
	switch f.typ {
	case 'd', 'b', 'o', 'x', 'X', 'c':
		var n int64
		if v.Kind() == vmtypes.KindI1 {
			n = int64(v.I32())
		} else {
			n, err = hostabi.LoadI64(i, v)
			if err != nil {
				return "", "", false, err
			}
		}
		if f.typ == 'c' {
			return string(rune(n)), "", false, nil
		}
		body, sign, numeric = intBody(n, f)
		return body, sign, numeric, nil
	case 'f', 'F', 'e', 'E', 'g', 'G', '%':
		var value float64
		value, err = floatValue(i, v)
		if err != nil {
			return "", "", false, err
		}
		body, sign, numeric = floatBody(value, f)
		return body, sign, numeric, nil
	case 's', 0:
		if isNumericKind(v) && f.typ == 0 {
			if v.Kind() == vmtypes.KindF32 || v.Kind() == vmtypes.KindF64 {
				var value float64
				value, err = floatValue(i, v)
				if err != nil {
					return "", "", false, err
				}
				body, sign, numeric = floatBody(value, f)
				return body, sign, numeric, nil
			}
			if v.Kind() == vmtypes.KindI64 {
				var n int64
				n, err = hostabi.LoadI64(i, v)
				if err != nil {
					return "", "", false, err
				}
				body, sign, numeric = intBody(n, f)
				return body, sign, numeric, nil
			}
		}
		text := hostabi.FormatScalar(i, v)
		if f.precision >= 0 && f.precision < len(text) {
			text = text[:f.precision]
		}
		return text, "", false, nil
	default:
		return hostabi.FormatScalar(i, v), "", false, nil
	}
}

func intBody(n int64, f formatSpec) (body, sign string, numeric bool) {
	sign = signPrefix(n < 0, f)
	if n < 0 {
		n = -n
	}
	switch f.typ {
	case 'b':
		body = strconv.FormatInt(n, 2)
	case 'o':
		body = strconv.FormatInt(n, 8)
	case 'x':
		body = strconv.FormatInt(n, 16)
	case 'X':
		body = strings.ToUpper(strconv.FormatInt(n, 16))
	default:
		body = strconv.FormatInt(n, 10)
	}
	return body, sign, true
}

func floatBody(x float64, f formatSpec) (body, sign string, numeric bool) {
	sign = signPrefix(math.Signbit(x) && x != 0 || x < 0, f)
	x = math.Abs(x)
	prec := f.precision
	verb := byte('f')
	switch f.typ {
	case 'e', 'E', 'g', 'G':
		verb = f.typ
		if prec < 0 && (f.typ == 'e' || f.typ == 'E') {
			prec = 6
		}
	case '%':
		x *= 100
		verb = 'f'
		if prec < 0 {
			prec = 6
		}
	default: // 'f','F', or numeric default
		verb = 'f'
		if prec < 0 {
			prec = 6
		}
	}
	body = strconv.FormatFloat(x, verb, prec, 64)
	if f.typ == 'E' || f.typ == 'G' {
		body = strings.ToUpper(body)
	}
	if f.typ == '%' {
		body += "%"
	}
	return body, sign, true
}

func signPrefix(negative bool, f formatSpec) string {
	if negative {
		return "-"
	}
	switch f.sign {
	case '+':
		return "+"
	case ' ':
		return " "
	}
	return ""
}

func floatValue(i *interp.Interpreter, v vmtypes.Boxed) (float64, error) {
	switch v.Kind() {
	case vmtypes.KindF32:
		return float64(v.F32()), nil
	case vmtypes.KindF64:
		return v.F64(), nil
	case vmtypes.KindI1:
		return float64(v.I32()), nil
	default:
		n, err := hostabi.LoadI64(i, v)
		return float64(n), err
	}
}

func isNumericKind(v vmtypes.Boxed) bool {
	switch v.Kind() {
	case vmtypes.KindI64, vmtypes.KindF32, vmtypes.KindF64:
		return true
	default:
		return false
	}
}

// padFormat applies width, fill, and alignment to an already-rendered body.
func padFormat(body, sign string, f formatSpec, numeric bool) string {
	full := sign + body
	pad := f.width - len([]rune(full))
	if pad <= 0 {
		return full
	}
	fill := f.fill
	align := f.align
	if align == 0 {
		if numeric {
			align = '>'
		} else {
			align = '<'
		}
	}
	switch align {
	case '<':
		return full + strings.Repeat(string(fill), pad)
	case '^':
		left := pad / 2
		return strings.Repeat(string(fill), left) + full + strings.Repeat(string(fill), pad-left)
	case '=':
		return sign + strings.Repeat(string(fill), pad) + body
	default: // '>'
		return strings.Repeat(string(fill), pad) + full
	}
}

// pyRepr renders repr(v)/ascii(v) for the supported scalar set. Strings are
// quoted and escaped; other scalars fall back to str()-style rendering.
func pyRepr(i *interp.Interpreter, v vmtypes.Boxed, ascii bool) (string, error) {
	if v.Kind() == vmtypes.KindRef && v.Ref() != 0 {
		val, err := i.Load(v.Ref())
		if err != nil {
			return "", err
		}
		if s, ok := val.(vmtypes.String); ok {
			return hostabi.ReprString(string(s), ascii), nil
		}
	}
	return hostabi.FormatScalar(i, v), nil
}

func loadSliceBounds(i *interp.Interpreter, params []vmtypes.Boxed) (int64, int64, int64, error) {
	start, err := hostabi.LoadI64(i, params[1])
	if err != nil {
		return 0, 0, 0, err
	}
	stop, err := hostabi.LoadI64(i, params[2])
	if err != nil {
		return 0, 0, 0, err
	}
	step, err := hostabi.LoadI64(i, params[3])
	if err != nil {
		return 0, 0, 0, err
	}
	return start, stop, step, nil
}

func normalizeSliceRange(length int, rawStart, rawStop, rawStep int64) (int, int, error) {
	step := rawStep
	if step == omittedSliceBound {
		step = 1
	}
	if step != 1 {
		return 0, 0, fmt.Errorf("extended slice assignment is not supported")
	}
	startOmitted := rawStart == omittedSliceBound
	stopOmitted := rawStop == omittedSliceBound
	start, stop := int(rawStart), int(rawStop)
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
	if stop < start {
		stop = start
	}
	return start, stop, nil
}

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

func mapGet(i *interp.Interpreter, ref vmtypes.Boxed, key vmtypes.Boxed) (vmtypes.Boxed, bool, error) {
	if ref.Kind() != vmtypes.KindRef || ref.Ref() == 0 {
		return 0, false, interp.ErrTypeMismatch
	}
	val, err := i.Load(ref.Ref())
	if err != nil {
		return 0, false, err
	}
	switch m := val.(type) {
	case *vmtypes.TypedMap[bool]:
		value, ok := m.Get(key.Bool())
		return value, ok, nil
	case *vmtypes.TypedMap[int32]:
		value, ok := m.Get(key.I32())
		return value, ok, nil
	case *vmtypes.TypedMap[int64]:
		n, err := hostabi.LoadI64(i, key)
		if err != nil {
			return 0, false, err
		}
		value, ok := m.Get(n)
		return value, ok, nil
	case *vmtypes.TypedMap[float32]:
		value, ok := m.Get(key.F32())
		return value, ok, nil
	case *vmtypes.TypedMap[float64]:
		value, ok := m.Get(key.F64())
		return value, ok, nil
	case *vmtypes.Map:
		entry, ok := m.Get(mapKey(key))
		return entry.Value, ok, nil
	default:
		return 0, false, interp.ErrTypeMismatch
	}
}

func mapEntries(i *interp.Interpreter, ref vmtypes.Boxed) ([]vmtypes.Boxed, []vmtypes.Boxed, error) {
	if ref.Kind() != vmtypes.KindRef || ref.Ref() == 0 {
		return nil, nil, interp.ErrTypeMismatch
	}
	val, err := i.Load(ref.Ref())
	if err != nil {
		return nil, nil, err
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
	default:
		return nil, nil, interp.ErrTypeMismatch
	}
	return keys, vals, nil
}

func mapKey(v vmtypes.Boxed) vmtypes.MapKey {
	switch v.Kind() {
	// bool lowers to i1 uniformly (literals and comparison results alike).
	case vmtypes.KindI1:
		return vmtypes.MapKey{Kind: vmtypes.KindI1, Bits: uint64(uint32(v.I32()))}
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
