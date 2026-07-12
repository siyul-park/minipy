package hostabi

import (
	"fmt"
	"io"
	"sort"
	"strings"

	pytypes "github.com/siyul-park/minipy/types"

	"github.com/siyul-park/minivm/interp"
	vmtypes "github.com/siyul-park/minivm/types"
)

// FormatValue renders a checked minipy value using its source type. Strings are
// unquoted at the top level and use repr-style quoting inside containers.
func FormatValue(i *interp.Interpreter, value vmtypes.Boxed, typ pytypes.Type) (string, error) {
	f := formatter{i: i, seen: map[vmtypes.Ref]bool{}}
	return f.value(value, pytypes.Erase(typ), false)
}

type formatter struct {
	i    *interp.Interpreter
	seen map[vmtypes.Ref]bool
}

func (f formatter) value(value vmtypes.Boxed, typ pytypes.Type, nested bool) (string, error) {
	switch {
	case pytypes.Equal(typ, pytypes.Int):
		n, err := f.integer(value)
		return fmt.Sprint(n), err
	case pytypes.Equal(typ, pytypes.Float):
		n, err := f.float(value)
		return PyFloat(n), err
	case pytypes.Equal(typ, pytypes.Bool):
		b, err := f.boolean(value)
		if err != nil {
			return "", err
		}
		if b {
			return "True", nil
		}
		return "False", nil
	case pytypes.Equal(typ, pytypes.Str):
		s, err := f.string(value)
		if err != nil || !nested {
			return s, err
		}
		return ReprString(s, false), nil
	case pytypes.Equal(typ, pytypes.None):
		return "None", nil
	}

	switch typ := typ.(type) {
	case *pytypes.List:
		return f.list(value, typ)
	case *pytypes.Tuple:
		return f.tuple(value, typ)
	case *pytypes.Dict:
		return f.dict(value, typ)
	case *pytypes.Set:
		return f.set(value, typ)
	case *pytypes.Union:
		for _, member := range typ.Members {
			if f.matches(value, member) {
				return f.value(value, member, nested)
			}
		}
	}
	return f.dynamic(value, nested)
}

func (f formatter) list(value vmtypes.Boxed, typ *pytypes.List) (string, error) {
	values, ref, err := f.array(value)
	if err != nil {
		return "", err
	}
	if f.seen[ref] {
		return "[...]", nil
	}
	f.seen[ref] = true
	defer delete(f.seen, ref)

	parts := make([]string, len(values))
	for idx, value := range values {
		parts[idx], err = f.value(value, typ.Elem, true)
		if err != nil {
			return "", err
		}
	}
	return "[" + strings.Join(parts, ", ") + "]", nil
}

func (f formatter) tuple(value vmtypes.Boxed, typ *pytypes.Tuple) (string, error) {
	object, ref, err := f.object(value)
	if err != nil {
		return "", err
	}
	tuple, ok := object.(*vmtypes.Struct)
	if !ok || len(tuple.Typ.Fields) != len(typ.Elems) {
		return "", fmt.Errorf("format tuple: %w", interp.ErrTypeMismatch)
	}
	if f.seen[ref] {
		return "(...)", nil
	}
	f.seen[ref] = true
	defer delete(f.seen, ref)

	parts := make([]string, len(typ.Elems))
	for idx, elem := range typ.Elems {
		parts[idx], err = f.value(tuple.Field(idx), elem, true)
		if err != nil {
			return "", err
		}
	}
	if len(parts) == 1 {
		parts[0] += ","
	}
	return "(" + strings.Join(parts, ", ") + ")", nil
}

func (f formatter) dict(value vmtypes.Boxed, typ *pytypes.Dict) (string, error) {
	keys, values, ref, err := f.entries(value)
	if err != nil {
		return "", err
	}
	if f.seen[ref] {
		return "{...}", nil
	}
	f.seen[ref] = true
	defer delete(f.seen, ref)

	parts := make([]string, len(keys))
	for idx := range keys {
		key, err := f.value(keys[idx], typ.Key, true)
		if err != nil {
			return "", err
		}
		val, err := f.value(values[idx], typ.Value, true)
		if err != nil {
			return "", err
		}
		parts[idx] = key + ": " + val
	}
	sort.Strings(parts)
	return "{" + strings.Join(parts, ", ") + "}", nil
}

func (f formatter) set(value vmtypes.Boxed, typ *pytypes.Set) (string, error) {
	keys, _, ref, err := f.entries(value)
	if err != nil {
		return "", err
	}
	if len(keys) == 0 {
		return "set()", nil
	}
	if f.seen[ref] {
		return "{...}", nil
	}
	f.seen[ref] = true
	defer delete(f.seen, ref)

	parts := make([]string, len(keys))
	for idx, key := range keys {
		parts[idx], err = f.value(key, typ.Elem, true)
		if err != nil {
			return "", err
		}
	}
	sort.Strings(parts)
	return "{" + strings.Join(parts, ", ") + "}", nil
}

func (f formatter) dynamic(value vmtypes.Boxed, nested bool) (string, error) {
	switch value.Kind() {
	case vmtypes.KindI1:
		return f.value(value, pytypes.Bool, nested)
	case vmtypes.KindI64:
		return f.value(value, pytypes.Int, nested)
	case vmtypes.KindF32, vmtypes.KindF64:
		return f.value(value, pytypes.Float, nested)
	case vmtypes.KindRef:
		if value.Ref() == 0 {
			return "None", nil
		}
	}
	object, _, err := f.object(value)
	if err != nil {
		return "", err
	}
	if value, ok := object.(vmtypes.String); ok {
		if nested {
			return ReprString(string(value), false), nil
		}
		return string(value), nil
	}
	return object.String(), nil
}

func (f formatter) matches(value vmtypes.Boxed, typ pytypes.Type) bool {
	if pytypes.Equal(typ, pytypes.None) {
		return value.Kind() == vmtypes.KindRef && value.Ref() == 0
	}
	vm := typ.VM()
	if vm == nil {
		return false
	}
	if value.Kind() != vmtypes.KindRef {
		return value.Kind() == vm.Kind()
	}
	object, _, err := f.object(value)
	return err == nil && object.Type().Equals(vm)
}

func (f formatter) integer(value vmtypes.Boxed) (int64, error) {
	if value.Kind() == vmtypes.KindI64 {
		return value.I64(), nil
	}
	object, _, err := f.object(value)
	if err != nil {
		return 0, err
	}
	n, ok := object.(vmtypes.I64)
	if !ok {
		return 0, fmt.Errorf("format int: %w", interp.ErrTypeMismatch)
	}
	return int64(n), nil
}

func (f formatter) float(value vmtypes.Boxed) (float64, error) {
	switch value.Kind() {
	case vmtypes.KindF32:
		return float64(value.F32()), nil
	case vmtypes.KindF64:
		return value.F64(), nil
	}
	object, _, err := f.object(value)
	if err != nil {
		return 0, err
	}
	switch n := object.(type) {
	case vmtypes.F32:
		return float64(n), nil
	case vmtypes.F64:
		return float64(n), nil
	default:
		return 0, fmt.Errorf("format float: %w", interp.ErrTypeMismatch)
	}
}

func (f formatter) boolean(value vmtypes.Boxed) (bool, error) {
	if value.Kind() == vmtypes.KindI1 {
		return value.Bool(), nil
	}
	object, _, err := f.object(value)
	if err != nil {
		return false, err
	}
	b, ok := object.(vmtypes.I1)
	if !ok {
		return false, fmt.Errorf("format bool: %w", interp.ErrTypeMismatch)
	}
	return bool(b), nil
}

func (f formatter) string(value vmtypes.Boxed) (string, error) {
	object, _, err := f.object(value)
	if err != nil {
		return "", err
	}
	s, ok := object.(vmtypes.String)
	if !ok {
		return "", fmt.Errorf("format string: %w", interp.ErrTypeMismatch)
	}
	return string(s), nil
}

func (f formatter) array(value vmtypes.Boxed) ([]vmtypes.Boxed, vmtypes.Ref, error) {
	object, ref, err := f.object(value)
	if err != nil {
		return nil, 0, err
	}
	var values []vmtypes.Boxed
	switch array := object.(type) {
	case *vmtypes.Array:
		values = append(values, array.Elems...)
	case vmtypes.TypedArray[bool]:
		for _, value := range array {
			values = append(values, vmtypes.BoxI1(value))
		}
	case vmtypes.TypedArray[int8]:
		for _, value := range array {
			values = append(values, vmtypes.BoxI32(int32(value)))
		}
	case vmtypes.TypedArray[int32]:
		for _, value := range array {
			values = append(values, vmtypes.BoxI32(value))
		}
	case vmtypes.TypedArray[int64]:
		for _, value := range array {
			values = append(values, vmtypes.BoxI64(value))
		}
	case vmtypes.TypedArray[float32]:
		for _, value := range array {
			values = append(values, vmtypes.BoxF32(value))
		}
	case vmtypes.TypedArray[float64]:
		for _, value := range array {
			values = append(values, vmtypes.BoxF64(value))
		}
	default:
		return nil, 0, fmt.Errorf("format list: %w", interp.ErrTypeMismatch)
	}
	return values, ref, nil
}

func (f formatter) entries(value vmtypes.Boxed) ([]vmtypes.Boxed, []vmtypes.Boxed, vmtypes.Ref, error) {
	object, ref, err := f.object(value)
	if err != nil {
		return nil, nil, 0, err
	}
	var keys, values []vmtypes.Boxed
	switch m := object.(type) {
	case *vmtypes.TypedMap[int8]:
		m.Range(func(key int8, value vmtypes.Boxed) {
			keys = append(keys, vmtypes.BoxI32(int32(key)))
			values = append(values, value)
		})
	case *vmtypes.TypedMap[bool]:
		m.Range(func(key bool, value vmtypes.Boxed) {
			keys = append(keys, vmtypes.BoxI1(key))
			values = append(values, value)
		})
	case *vmtypes.TypedMap[int32]:
		m.Range(func(key int32, value vmtypes.Boxed) {
			keys = append(keys, vmtypes.BoxI32(key))
			values = append(values, value)
		})
	case *vmtypes.TypedMap[int64]:
		m.Range(func(key int64, value vmtypes.Boxed) {
			keys = append(keys, vmtypes.BoxI64(key))
			values = append(values, value)
		})
	case *vmtypes.TypedMap[float32]:
		m.Range(func(key float32, value vmtypes.Boxed) {
			keys = append(keys, vmtypes.BoxF32(key))
			values = append(values, value)
		})
	case *vmtypes.TypedMap[float64]:
		m.Range(func(key float64, value vmtypes.Boxed) {
			keys = append(keys, vmtypes.BoxF64(key))
			values = append(values, value)
		})
	case *vmtypes.Map:
		m.Range(func(_ vmtypes.MapKey, entry vmtypes.MapEntry) {
			keys = append(keys, entry.Key)
			values = append(values, entry.Value)
		})
	default:
		return nil, nil, 0, fmt.Errorf("format map: %w", interp.ErrTypeMismatch)
	}
	return keys, values, ref, nil
}

func (f formatter) object(value vmtypes.Boxed) (vmtypes.Value, vmtypes.Ref, error) {
	if value.Kind() != vmtypes.KindRef || value.Ref() == 0 {
		return nil, 0, fmt.Errorf("load object: %w", interp.ErrTypeMismatch)
	}
	ref := vmtypes.Ref(value.Ref())
	object, err := f.i.Load(int(ref))
	if err != nil {
		return nil, 0, err
	}
	return object, ref, nil
}

// ReprString quotes and escapes a string using Python-style repr rules.
func ReprString(s string, ascii bool) string {
	quote := byte('\'')
	if strings.Contains(s, "'") && !strings.Contains(s, "\"") {
		quote = '"'
	}
	var out strings.Builder
	out.WriteByte(quote)
	for _, r := range s {
		switch r {
		case rune(quote):
			out.WriteByte('\\')
			out.WriteRune(r)
		case '\\':
			out.WriteString(`\\`)
		case '\n':
			out.WriteString(`\n`)
		case '\r':
			out.WriteString(`\r`)
		case '\t':
			out.WriteString(`\t`)
		default:
			writeReprRune(&out, r, ascii)
		}
	}
	out.WriteByte(quote)
	return out.String()
}

func writeReprRune(out *strings.Builder, r rune, ascii bool) {
	switch {
	case r < 0x20 || r == 0x7f:
		fmt.Fprintf(out, `\x%02x`, r)
	case ascii && r > 0x7f:
		switch {
		case r > 0xffff:
			fmt.Fprintf(out, `\U%08x`, r)
		case r > 0xff:
			fmt.Fprintf(out, `\u%04x`, r)
		default:
			fmt.Fprintf(out, `\x%02x`, r)
		}
	default:
		out.WriteRune(r)
	}
}

// PrintFunction returns a host function that writes one typed value and a
// trailing newline to out.
func PrintFunction(out io.Writer, typ pytypes.Type) *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{vmtypes.TypeRef}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			text, err := FormatValue(i, params[0], typ)
			if err != nil {
				return nil, err
			}
			_, err = fmt.Fprintln(out, text)
			return nil, err
		},
	)
}

// StringFunction returns a host function that converts one typed value to a
// minivm string.
func StringFunction(typ pytypes.Type) *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{vmtypes.TypeRef}, Returns: []vmtypes.Type{vmtypes.TypeString}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			text, err := FormatValue(i, params[0], typ)
			if err != nil {
				return nil, err
			}
			return AllocString(i, text)
		},
	)
}
