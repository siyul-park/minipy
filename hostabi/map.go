package hostabi

import (
	"math"

	"github.com/siyul-park/minivm/interp"
	vmtypes "github.com/siyul-park/minivm/types"
)

// This file owns minipy's view of a minivm map value.
//
// minivm picks a map's concrete representation from its declared key type: a
// TypedMap[T] holds unboxed scalar keys, a TypedMap[string] holds string keys
// by content, and a generic Map holds boxed keys indexed by a MapKey. Host
// code must not care which it got, so these functions are the one place that
// dispatch happens. A string key is the reason the interpreter is a parameter:
// its content lives on the heap, and content is what indexes the entry.

// LoadMap reads the map value a dict/set receiver reference points at.
func LoadMap(i *interp.Interpreter, ref vmtypes.Boxed) (vmtypes.Value, error) {
	if ref.Kind() != vmtypes.KindRef || ref.Ref() == 0 {
		return nil, interp.ErrTypeMismatch
	}
	return i.Load(ref.Ref())
}

// MapLen returns the number of entries in a map value.
func MapLen(m vmtypes.Value) (int, error) {
	switch m := m.(type) {
	case *vmtypes.TypedMap[int8]:
		return m.Len(), nil
	case *vmtypes.TypedMap[bool]:
		return m.Len(), nil
	case *vmtypes.TypedMap[int32]:
		return m.Len(), nil
	case *vmtypes.TypedMap[int64]:
		return m.Len(), nil
	case *vmtypes.TypedMap[float32]:
		return m.Len(), nil
	case *vmtypes.TypedMap[float64]:
		return m.Len(), nil
	case *vmtypes.TypedMap[string]:
		return m.Len(), nil
	case *vmtypes.Map:
		return m.Len(), nil
	default:
		return 0, interp.ErrTypeMismatch
	}
}

// MapGet looks key up in a map value. The returned value is borrowed: it still
// belongs to the map, so a caller storing it into a second owner must retain it
// first.
func MapGet(i *interp.Interpreter, m vmtypes.Value, key vmtypes.Boxed) (vmtypes.Boxed, bool, error) {
	switch m := m.(type) {
	case *vmtypes.TypedMap[int8]:
		value, ok := m.Get(int8(key.I32()))
		return value, ok, nil
	case *vmtypes.TypedMap[bool]:
		value, ok := m.Get(key.Bool())
		return value, ok, nil
	case *vmtypes.TypedMap[int32]:
		value, ok := m.Get(key.I32())
		return value, ok, nil
	case *vmtypes.TypedMap[int64]:
		n, err := LoadI64(i, key)
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
	case *vmtypes.TypedMap[string]:
		text, err := LoadStr(i, key)
		if err != nil {
			return 0, false, err
		}
		value, ok := m.Get(text)
		return value, ok, nil
	case *vmtypes.Map:
		mapKey, err := mapKeyOf(i, key)
		if err != nil {
			return 0, false, err
		}
		entry, ok := m.Get(mapKey)
		return entry.Value, ok, nil
	default:
		return 0, false, interp.ErrTypeMismatch
	}
}

// MapSet stores value under key. The map takes no reference of its own, so the
// caller transfers ownership of both boxes exactly as an interpreter MAP_SET
// does; the displaced entry, if any, is returned so the caller can release it.
func MapSet(i *interp.Interpreter, m vmtypes.Value, key, value vmtypes.Boxed) (vmtypes.Boxed, bool, error) {
	switch m := m.(type) {
	case *vmtypes.TypedMap[int8]:
		old, ok := m.Set(int8(key.I32()), value)
		return old, ok, nil
	case *vmtypes.TypedMap[bool]:
		old, ok := m.Set(key.Bool(), value)
		return old, ok, nil
	case *vmtypes.TypedMap[int32]:
		old, ok := m.Set(key.I32(), value)
		return old, ok, nil
	case *vmtypes.TypedMap[int64]:
		n, err := LoadI64(i, key)
		if err != nil {
			return 0, false, err
		}
		old, ok := m.Set(n, value)
		return old, ok, nil
	case *vmtypes.TypedMap[float32]:
		old, ok := m.Set(key.F32(), value)
		return old, ok, nil
	case *vmtypes.TypedMap[float64]:
		old, ok := m.Set(key.F64(), value)
		return old, ok, nil
	case *vmtypes.TypedMap[string]:
		text, err := LoadStr(i, key)
		if err != nil {
			return 0, false, err
		}
		old, ok := m.Set(text, value)
		return old, ok, nil
	case *vmtypes.Map:
		mapKey, err := mapKeyOf(i, key)
		if err != nil {
			return 0, false, err
		}
		old, ok := m.Set(mapKey, vmtypes.MapEntry{Key: key, Value: value})
		return old.Value, ok, nil
	default:
		return 0, false, interp.ErrTypeMismatch
	}
}

// MapDelete removes key and reports the value it held. The returned value is
// no longer owned by the map, so the caller owns it.
func MapDelete(i *interp.Interpreter, m vmtypes.Value, key vmtypes.Boxed) (vmtypes.Boxed, bool, error) {
	switch m := m.(type) {
	case *vmtypes.TypedMap[int8]:
		value, ok := m.Delete(int8(key.I32()))
		return value, ok, nil
	case *vmtypes.TypedMap[bool]:
		value, ok := m.Delete(key.Bool())
		return value, ok, nil
	case *vmtypes.TypedMap[int32]:
		value, ok := m.Delete(key.I32())
		return value, ok, nil
	case *vmtypes.TypedMap[int64]:
		n, err := LoadI64(i, key)
		if err != nil {
			return 0, false, err
		}
		value, ok := m.Delete(n)
		return value, ok, nil
	case *vmtypes.TypedMap[float32]:
		value, ok := m.Delete(key.F32())
		return value, ok, nil
	case *vmtypes.TypedMap[float64]:
		value, ok := m.Delete(key.F64())
		return value, ok, nil
	case *vmtypes.TypedMap[string]:
		text, err := LoadStr(i, key)
		if err != nil {
			return 0, false, err
		}
		value, ok := m.Delete(text)
		return value, ok, nil
	case *vmtypes.Map:
		mapKey, err := mapKeyOf(i, key)
		if err != nil {
			return 0, false, err
		}
		entry, ok := m.Delete(mapKey)
		return entry.Value, ok, nil
	default:
		return 0, false, interp.ErrTypeMismatch
	}
}

// MapClear drops every entry of a map value.
func MapClear(m vmtypes.Value) error {
	switch m := m.(type) {
	case *vmtypes.TypedMap[int8]:
		m.Clear(func(vmtypes.Boxed) {})
	case *vmtypes.TypedMap[bool]:
		m.Clear(func(vmtypes.Boxed) {})
	case *vmtypes.TypedMap[int32]:
		m.Clear(func(vmtypes.Boxed) {})
	case *vmtypes.TypedMap[int64]:
		m.Clear(func(vmtypes.Boxed) {})
	case *vmtypes.TypedMap[float32]:
		m.Clear(func(vmtypes.Boxed) {})
	case *vmtypes.TypedMap[float64]:
		m.Clear(func(vmtypes.Boxed) {})
	case *vmtypes.TypedMap[string]:
		m.Clear(func(vmtypes.Boxed) {})
	case *vmtypes.Map:
		m.Clear(func(vmtypes.MapEntry) {})
	default:
		return interp.ErrTypeMismatch
	}
	return nil
}

// MapEntries reads a map value's keys and values in matching order.
//
// The keys are owned by the caller and MUST be released with ReleaseBoxes once
// the caller is done with them — after any container the caller stores them
// into has taken its own reference. A string-keyed map holds no boxed key at
// all, only the content, so its key has to be allocated here; every other
// representation is retained so that one release rule covers them all. The
// values are borrowed: they still belong to the map.
func MapEntries(i *interp.Interpreter, m vmtypes.Value) ([]vmtypes.Boxed, []vmtypes.Boxed, error) {
	var keys, values []vmtypes.Boxed
	switch m := m.(type) {
	case *vmtypes.TypedMap[int8]:
		m.Range(func(k int8, v vmtypes.Boxed) {
			keys = append(keys, vmtypes.BoxI32(int32(k)))
			values = append(values, v)
		})
	case *vmtypes.TypedMap[bool]:
		m.Range(func(k bool, v vmtypes.Boxed) {
			keys = append(keys, vmtypes.BoxI1(k))
			values = append(values, v)
		})
	case *vmtypes.TypedMap[int32]:
		m.Range(func(k int32, v vmtypes.Boxed) {
			keys = append(keys, vmtypes.BoxI32(k))
			values = append(values, v)
		})
	case *vmtypes.TypedMap[int64]:
		m.Range(func(k int64, v vmtypes.Boxed) {
			keys = append(keys, vmtypes.BoxI64(k))
			values = append(values, v)
		})
	case *vmtypes.TypedMap[float32]:
		m.Range(func(k float32, v vmtypes.Boxed) {
			keys = append(keys, vmtypes.BoxF32(k))
			values = append(values, v)
		})
	case *vmtypes.TypedMap[float64]:
		m.Range(func(k float64, v vmtypes.Boxed) {
			keys = append(keys, vmtypes.BoxF64(k))
			values = append(values, v)
		})
	case *vmtypes.TypedMap[string]:
		var err error
		m.Range(func(k string, v vmtypes.Boxed) {
			if err != nil {
				return
			}
			var key []vmtypes.Boxed
			if key, err = AllocString(i, k); err != nil {
				return
			}
			keys = append(keys, key[0])
			values = append(values, v)
		})
		if err != nil {
			_ = ReleaseBoxes(i, keys)
			return nil, nil, err
		}
		return keys, values, nil
	case *vmtypes.Map:
		m.Range(func(_ vmtypes.MapKey, entry vmtypes.MapEntry) {
			keys = append(keys, entry.Key)
			values = append(values, entry.Value)
		})
	default:
		return nil, nil, interp.ErrTypeMismatch
	}
	if err := RetainBoxes(i, keys); err != nil {
		return nil, nil, err
	}
	return keys, values, nil
}

// mapKeyOf builds the index a generic Map stores an entry under, mirroring the
// interpreter's own rule so a key published by MAP_SET and a key published by a
// host function reach the same entry. A scalar keys by value, i1 through its i32
// representation and a heap-spilled int through its numeric value; a string keys
// by content under KindText, so equal strings index one entry however each was
// published; every other reference keys by heap address.
func mapKeyOf(i *interp.Interpreter, key vmtypes.Boxed) (vmtypes.MapKey, error) {
	switch key.Kind() {
	case vmtypes.KindI1, vmtypes.KindI8, vmtypes.KindI32:
		return vmtypes.MapKey{Kind: vmtypes.KindI32, Bits: uint64(uint32(key.I32()))}, nil
	case vmtypes.KindI64:
		n, err := LoadI64(i, key)
		if err != nil {
			return vmtypes.MapKey{}, err
		}
		return vmtypes.MapKey{Kind: vmtypes.KindI64, Bits: uint64(n)}, nil
	case vmtypes.KindF32:
		bits := math.Float32bits(key.F32())
		if bits == negZeroF32 {
			bits = 0
		}
		return vmtypes.MapKey{Kind: vmtypes.KindF32, Bits: uint64(bits)}, nil
	case vmtypes.KindF64:
		bits := math.Float64bits(key.F64())
		if bits == negZeroF64 {
			bits = 0
		}
		return vmtypes.MapKey{Kind: vmtypes.KindF64, Bits: bits}, nil
	case vmtypes.KindRef:
		if key.Ref() == 0 {
			return vmtypes.MapKey{Kind: vmtypes.KindRef}, nil
		}
		value, err := i.Load(key.Ref())
		if err != nil {
			return vmtypes.MapKey{}, err
		}
		switch value := value.(type) {
		case vmtypes.I64:
			return vmtypes.MapKey{Kind: vmtypes.KindI64, Bits: uint64(value)}, nil
		case vmtypes.String:
			return vmtypes.MapKey{Kind: vmtypes.KindText, Text: string(value)}, nil
		}
		return vmtypes.MapKey{Kind: vmtypes.KindRef, Bits: uint64(key.Ref())}, nil
	default:
		return vmtypes.MapKey{}, interp.ErrTypeMismatch
	}
}

// negZero* are the bit patterns the interpreter folds into positive zero so a
// map has one entry for 0.0 and -0.0, as Python requires.
const (
	negZeroF32 = uint32(1) << 31
	negZeroF64 = uint64(1) << 63
)
