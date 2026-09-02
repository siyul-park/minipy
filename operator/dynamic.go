package operator

import (
	"fmt"
	"math"
	"strings"

	"github.com/siyul-park/minipy/hostabi"
	"github.com/siyul-park/minipy/token"

	"github.com/siyul-park/minivm/interp"
	vmtypes "github.com/siyul-park/minivm/types"
)

// dynBinaryOp returns a host function that performs a binary arithmetic
// operation on two dynamically-typed operands. The operands arrive as
// self-describing Boxed values; the result is returned in the same form.
func dynBinaryOp(op token.Type) *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{
			Params:  []vmtypes.Type{vmtypes.TypeAny, vmtypes.TypeAny},
			Returns: []vmtypes.Type{vmtypes.TypeAny},
		},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			return dynBinary(i, params[0], params[1], op)
		},
	)
}

// dynCompare returns a host function that performs a comparison on two
// dynamically-typed operands and returns a bool (i1).
func dynCompare(op token.Type) *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{
			Params:  []vmtypes.Type{vmtypes.TypeAny, vmtypes.TypeAny},
			Returns: []vmtypes.Type{vmtypes.TypeI1},
		},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			result, err := dynCmp(i, params[0], params[1], op)
			if err != nil {
				return nil, err
			}
			return []vmtypes.Boxed{vmtypes.BoxI1(result)}, nil
		},
	)
}

// dynContains returns a host function for `in` / `not in` with a dynamic RHS.
func dynContains(op token.Type) *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{
			Params:  []vmtypes.Type{vmtypes.TypeAny, vmtypes.TypeAny},
			Returns: []vmtypes.Type{vmtypes.TypeI1},
		},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			// params[0] = needle (left of 'in'), params[1] = haystack (right of 'in')
			needle, haystack := params[0], params[1]
			found, err := dynIn(i, needle, haystack)
			if err != nil {
				return nil, err
			}
			if op == token.NOTIN {
				found = !found
			}
			return []vmtypes.Boxed{vmtypes.BoxI1(found)}, nil
		},
	)
}

// DynBool returns a host function that computes the truthiness of a dynamic
// value, following Python semantics: 0, 0.0, False, None, "", empty containers
// are falsy; everything else is truthy.
func DynBool() *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{
			Params:  []vmtypes.Type{vmtypes.TypeAny},
			Returns: []vmtypes.Type{vmtypes.TypeI1},
		},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			result := dynTruth(i, params[0])
			return []vmtypes.Boxed{vmtypes.BoxI1(result)}, nil
		},
	)
}

// DynStr returns a host function that converts a dynamic value to a string.
func DynStr() *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{
			Params:  []vmtypes.Type{vmtypes.TypeAny},
			Returns: []vmtypes.Type{vmtypes.TypeString},
		},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			s := hostabi.FormatDynamic(i, params[0])
			return hostabi.AllocString(i, s)
		},
	)
}

// DynPrint returns a host function that prints a dynamic value with a newline.
func DynPrint(out interface{ Write([]byte) (int, error) }) *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{
			Params: []vmtypes.Type{vmtypes.TypeAny},
		},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			s := hostabi.FormatDynamic(i, params[0])
			_, err := fmt.Fprintln(out, s)
			return nil, err
		},
	)
}

// DynLen returns a host function that computes len() on a dynamic value.
func DynLen() *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{
			Params:  []vmtypes.Type{vmtypes.TypeAny},
			Returns: []vmtypes.Type{vmtypes.TypeI64},
		},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			n, err := dynLength(i, params[0])
			if err != nil {
				return nil, err
			}
			return []vmtypes.Boxed{vmtypes.BoxI64(n)}, nil
		},
	)
}

// DynIter returns a host function that creates an iterator from a dynamic value.
func DynIter() *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{
			Params:  []vmtypes.Type{vmtypes.TypeAny},
			Returns: []vmtypes.Type{vmtypes.TypeAny},
		},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			return dynIterator(i, params[0])
		},
	)
}

// DynGetItem returns a host function for dynamic subscript access (obj[key]).
func DynGetItem() *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{
			Params:  []vmtypes.Type{vmtypes.TypeAny, vmtypes.TypeAny},
			Returns: []vmtypes.Type{vmtypes.TypeAny},
		},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			return dynIndex(i, params[0], params[1])
		},
	)
}

// dynUnaryNeg returns a host function for unary negation (-x) on a dynamic value.
func dynUnaryNeg() *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{
			Params:  []vmtypes.Type{vmtypes.TypeAny},
			Returns: []vmtypes.Type{vmtypes.TypeAny},
		},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			v := params[0]
			switch v.Kind() {
			case vmtypes.KindI64:
				return []vmtypes.Boxed{vmtypes.BoxI64(-v.I64())}, nil
			case vmtypes.KindF64:
				return []vmtypes.Boxed{vmtypes.BoxF64(-v.F64())}, nil
			case vmtypes.KindF32:
				return []vmtypes.Boxed{vmtypes.BoxF32(-v.F32())}, nil
			case vmtypes.KindI1:
				if v.I32() != 0 {
					return []vmtypes.Boxed{vmtypes.BoxI64(-1)}, nil
				}
				return []vmtypes.Boxed{vmtypes.BoxI64(0)}, nil
			default:
				return nil, interp.ErrTypeMismatch
			}
		},
	)
}

// dynUnaryPos returns a host function for unary positive (+x) on a dynamic value.
func dynUnaryPos() *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{
			Params:  []vmtypes.Type{vmtypes.TypeAny},
			Returns: []vmtypes.Type{vmtypes.TypeAny},
		},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			v := params[0]
			switch v.Kind() {
			case vmtypes.KindI64, vmtypes.KindF64, vmtypes.KindF32:
				return []vmtypes.Boxed{v}, nil
			case vmtypes.KindI1:
				if v.I32() != 0 {
					return []vmtypes.Boxed{vmtypes.BoxI64(1)}, nil
				}
				return []vmtypes.Boxed{vmtypes.BoxI64(0)}, nil
			default:
				return nil, interp.ErrTypeMismatch
			}
		},
	)
}

// dynUnaryInvert returns a host function for bitwise inversion (~x) on a dynamic value.
func dynUnaryInvert() *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{
			Params:  []vmtypes.Type{vmtypes.TypeAny},
			Returns: []vmtypes.Type{vmtypes.TypeAny},
		},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			v := params[0]
			switch v.Kind() {
			case vmtypes.KindI64:
				return []vmtypes.Boxed{vmtypes.BoxI64(^v.I64())}, nil
			case vmtypes.KindI1:
				if v.I32() != 0 {
					return []vmtypes.Boxed{vmtypes.BoxI64(^int64(1))}, nil
				}
				return []vmtypes.Boxed{vmtypes.BoxI64(^int64(0))}, nil
			default:
				return nil, interp.ErrTypeMismatch
			}
		},
	)
}

// dynBinary dispatches a binary operation by examining the runtime kinds.
func dynBinary(i *interp.Interpreter, left, right vmtypes.Boxed, op token.Type) ([]vmtypes.Boxed, error) {
	lk, rk := left.Kind(), right.Kind()

	// String concatenation: str + str
	if op == token.PLUS && lk == vmtypes.KindRef && rk == vmtypes.KindRef {
		ls, lErr := hostabi.LoadStr(i, left)
		rs, rErr := hostabi.LoadStr(i, right)
		if lErr == nil && rErr == nil {
			return hostabi.AllocString(i, ls+rs)
		}
	}

	// String repetition: str * int or int * str
	if op == token.STAR {
		if lk == vmtypes.KindRef && rk == vmtypes.KindI64 {
			if s, err := hostabi.LoadStr(i, left); err == nil {
				n := int(right.I64())
				if n < 0 {
					n = 0
				}
				return hostabi.AllocString(i, strings.Repeat(s, n))
			}
		}
		if lk == vmtypes.KindI64 && rk == vmtypes.KindRef {
			if s, err := hostabi.LoadStr(i, right); err == nil {
				n := int(left.I64())
				if n < 0 {
					n = 0
				}
				return hostabi.AllocString(i, strings.Repeat(s, n))
			}
		}
	}

	lf, rf := toFloat(left), toFloat(right)
	lint, lisInt := toInt(left)
	rint, risInt := toInt(right)

	// Non-numeric ref values (string, list, dict) cannot do arithmetic
	// beyond concatenation/repetition handled above.
	if lk == vmtypes.KindRef || rk == vmtypes.KindRef {
		return nil, interp.ErrTypeMismatch
	}

	// Both operands are integers: perform integer arithmetic.
	if lisInt && risInt {
		result, err := intBinary(lint, rint, op)
		if err != nil {
			return nil, err
		}
		return []vmtypes.Boxed{result}, nil
	}

	// At least one is float (or both are numeric): float arithmetic.
	result, err := floatBinary(lf, rf, op)
	if err != nil {
		return nil, err
	}
	return []vmtypes.Boxed{result}, nil
}

// intBinary performs integer binary operations following Python semantics.
func intBinary(left, right int64, op token.Type) (vmtypes.Boxed, error) {
	switch op {
	case token.PLUS:
		return vmtypes.BoxI64(left + right), nil
	case token.MINUS:
		return vmtypes.BoxI64(left - right), nil
	case token.STAR:
		return vmtypes.BoxI64(left * right), nil
	case token.SLASH:
		if right == 0 {
			return 0, interp.ErrDivideByZero
		}
		return vmtypes.BoxF64(float64(left) / float64(right)), nil
	case token.DOUBLESLASH:
		if right == 0 {
			return 0, interp.ErrDivideByZero
		}
		return vmtypes.BoxI64(pyFloorDiv(left, right)), nil
	case token.PERCENT:
		if right == 0 {
			return 0, interp.ErrDivideByZero
		}
		return vmtypes.BoxI64(pyMod(left, right)), nil
	case token.DOUBLESTAR:
		if right < 0 {
			return vmtypes.BoxF64(math.Pow(float64(left), float64(right))), nil
		}
		return vmtypes.BoxI64(intPow(left, right)), nil
	case token.AMP:
		return vmtypes.BoxI64(left & right), nil
	case token.PIPE:
		return vmtypes.BoxI64(left | right), nil
	case token.CARET:
		return vmtypes.BoxI64(left ^ right), nil
	case token.LSHIFT:
		return vmtypes.BoxI64(left << uint(right)), nil
	case token.RSHIFT:
		return vmtypes.BoxI64(left >> uint(right)), nil
	default:
		return 0, fmt.Errorf("unsupported int op: %s", op)
	}
}

// floatBinary performs float binary operations following Python semantics.
func floatBinary(left, right float64, op token.Type) (vmtypes.Boxed, error) {
	switch op {
	case token.PLUS:
		return vmtypes.BoxF64(left + right), nil
	case token.MINUS:
		return vmtypes.BoxF64(left - right), nil
	case token.STAR:
		return vmtypes.BoxF64(left * right), nil
	case token.SLASH:
		if right == 0 {
			return 0, interp.ErrDivideByZero
		}
		return vmtypes.BoxF64(left / right), nil
	case token.DOUBLESLASH:
		if right == 0 {
			return 0, interp.ErrDivideByZero
		}
		return vmtypes.BoxF64(math.Floor(left / right)), nil
	case token.PERCENT:
		if right == 0 {
			return 0, interp.ErrDivideByZero
		}
		return vmtypes.BoxF64(left - math.Floor(left/right)*right), nil
	case token.DOUBLESTAR:
		return vmtypes.BoxF64(math.Pow(left, right)), nil
	default:
		return 0, fmt.Errorf("unsupported float op: %s", op)
	}
}

// dynCmp compares two dynamic values and returns true/false.
func dynCmp(i *interp.Interpreter, left, right vmtypes.Boxed, op token.Type) (bool, error) {
	lk, rk := left.Kind(), right.Kind()

	// String comparison
	if lk == vmtypes.KindRef && rk == vmtypes.KindRef {
		ls, lErr := hostabi.LoadStr(i, left)
		rs, rErr := hostabi.LoadStr(i, right)
		if lErr == nil && rErr == nil {
			return cmpStrings(ls, rs, op), nil
		}
		// Both refs but not both strings: compare by identity for eq/ne.
		if op == token.EQ {
			return left == right, nil
		}
		if op == token.NE {
			return left != right, nil
		}
		return false, fmt.Errorf("'%s' not supported between these types", op)
	}

	// None comparison
	if lk == vmtypes.KindRef && left.Ref() == 0 || rk == vmtypes.KindRef && right.Ref() == 0 {
		if op == token.EQ {
			eq := (lk == vmtypes.KindRef && left.Ref() == 0) && (rk == vmtypes.KindRef && right.Ref() == 0)
			return eq, nil
		}
		if op == token.NE {
			eq := (lk == vmtypes.KindRef && left.Ref() == 0) && (rk == vmtypes.KindRef && right.Ref() == 0)
			return !eq, nil
		}
		return false, fmt.Errorf("'%s' not supported with None", op)
	}

	// Cross-kind guard: if one operand is a non-null ref (string, list, dict)
	// and the other is numeric, they are never equal in Python.
	if lk == vmtypes.KindRef || rk == vmtypes.KindRef {
		if op == token.EQ {
			return false, nil
		}
		if op == token.NE {
			return true, nil
		}
		return false, fmt.Errorf("'%s' not supported between these types", op)
	}

	// Numeric comparison (int/float/bool)
	lf, rf := toFloat(left), toFloat(right)
	return cmpFloats(lf, rf, op), nil
}

// dynTruth returns the Python truthiness of a dynamic value.
func dynTruth(i *interp.Interpreter, v vmtypes.Boxed) bool {
	switch v.Kind() {
	case vmtypes.KindI1:
		return v.I32() != 0
	case vmtypes.KindI64:
		return v.I64() != 0
	case vmtypes.KindF64:
		return v.F64() != 0
	case vmtypes.KindF32:
		return v.F32() != 0
	case vmtypes.KindRef:
		if v.Ref() == 0 {
			return false
		}
		// Use ArrayElems to normalize all array representations including TypedArray.
		_, elems, err := hostabi.ArrayElems(i, v)
		if err == nil {
			return len(elems) > 0
		}
		val, lErr := i.Load(v.Ref())
		if lErr != nil {
			return true
		}
		switch obj := val.(type) {
		case vmtypes.String:
			return len(string(obj)) > 0
		default:
			if _, ok := obj.Type().(*vmtypes.MapType); ok {
				length, err := hostabi.MapLen(obj)
				return err == nil && length > 0
			}
			return true
		}
	default:
		return true
	}
}

// dynLength returns the length of a dynamic value.
func dynLength(i *interp.Interpreter, v vmtypes.Boxed) (int64, error) {
	if v.Kind() != vmtypes.KindRef || v.Ref() == 0 {
		return 0, interp.ErrTypeMismatch
	}
	// Try string.
	if s, err := hostabi.LoadStr(i, v); err == nil {
		return int64(len([]rune(s))), nil
	}
	// Try array/list.
	_, elems, err := hostabi.ArrayElems(i, v)
	if err == nil {
		return int64(len(elems)), nil
	}
	// Try dict/set (map).
	val, lErr := i.Load(v.Ref())
	if lErr == nil {
		if _, ok := val.Type().(*vmtypes.MapType); ok {
			length, mErr := hostabi.MapLen(val)
			return int64(length), mErr
		}
	}
	return 0, interp.ErrTypeMismatch
}

// dynIterator creates an iterator from a dynamic value.
func dynIterator(i *interp.Interpreter, v vmtypes.Boxed) ([]vmtypes.Boxed, error) {
	if v.Kind() != vmtypes.KindRef || v.Ref() == 0 {
		return nil, interp.ErrTypeMismatch
	}
	// Try string.
	if s, err := hostabi.LoadStr(i, v); err == nil {
		chars := []rune(s)
		elems := make([]vmtypes.Boxed, 0, len(chars))
		for _, r := range chars {
			addr, aErr := i.Alloc(vmtypes.String(string(r)))
			if aErr != nil {
				return nil, aErr
			}
			elems = append(elems, vmtypes.BoxRef(addr))
		}
		addr, err := i.Alloc(hostabi.NewIterator("str.iterator", elems))
		if err != nil {
			return nil, err
		}
		return []vmtypes.Boxed{vmtypes.BoxRef(addr)}, nil
	}
	// Try array/list.
	_, elems, err := hostabi.ArrayElems(i, v)
	if err == nil {
		addr, aErr := i.Alloc(hostabi.NewIterator("list.iterator", elems))
		if aErr != nil {
			return nil, aErr
		}
		return []vmtypes.Boxed{vmtypes.BoxRef(addr)}, nil
	}
	// Try dict/set (map): iterate over keys.
	val, lErr := i.Load(v.Ref())
	if lErr == nil {
		if _, ok := val.Type().(*vmtypes.MapType); ok {
			// The iterator holds the keys for its own lifetime and traces
			// them, so the reference MapEntries hands over is transferred to
			// it rather than released here, matching the list case above.
			keys, _, kErr := hostabi.MapEntries(i, val)
			if kErr != nil {
				return nil, kErr
			}
			addr, aErr := i.Alloc(hostabi.NewIterator("dict.iterator", keys))
			if aErr != nil {
				return nil, aErr
			}
			return []vmtypes.Boxed{vmtypes.BoxRef(addr)}, nil
		}
	}
	return nil, interp.ErrTypeMismatch
}

// dynIndex performs dynamic subscript access: obj[key].
func dynIndex(i *interp.Interpreter, receiver, key vmtypes.Boxed) ([]vmtypes.Boxed, error) {
	if receiver.Kind() != vmtypes.KindRef || receiver.Ref() == 0 {
		return nil, interp.ErrTypeMismatch
	}
	// Try string indexing.
	if s, err := hostabi.LoadStr(i, receiver); err == nil {
		idx, ok := toInt(key)
		if !ok {
			return nil, interp.ErrTypeMismatch
		}
		runes := []rune(s)
		index := int(idx)
		if index < 0 {
			index += len(runes)
		}
		if index < 0 || index >= len(runes) {
			return nil, interp.ErrIndexOutOfRange
		}
		return hostabi.AllocString(i, string(runes[index]))
	}
	// Try array/list indexing.
	_, elems, err := hostabi.ArrayElems(i, receiver)
	if err == nil {
		idx, ok := toInt(key)
		if !ok {
			return nil, interp.ErrTypeMismatch
		}
		index := int(idx)
		if index < 0 {
			index += len(elems)
		}
		if index < 0 || index >= len(elems) {
			return nil, interp.ErrIndexOutOfRange
		}
		return []vmtypes.Boxed{elems[index]}, nil
	}
	// Try dict/set (map) indexing.
	val, lErr := i.Load(receiver.Ref())
	if lErr == nil {
		if _, ok := val.Type().(*vmtypes.MapType); ok {
			result, found, gErr := hostabi.MapGet(i, val, key)
			if gErr != nil {
				return nil, gErr
			}
			if !found {
				return nil, interp.ErrIndexOutOfRange
			}
			return []vmtypes.Boxed{result}, nil
		}
	}
	return nil, interp.ErrTypeMismatch
}

// dynIn checks membership: needle in haystack.
func dynIn(i *interp.Interpreter, needle, haystack vmtypes.Boxed) (bool, error) {
	if haystack.Kind() != vmtypes.KindRef || haystack.Ref() == 0 {
		return false, interp.ErrTypeMismatch
	}
	// Try string containment first.
	if s, err := hostabi.LoadStr(i, haystack); err == nil {
		ns, nErr := hostabi.LoadStr(i, needle)
		if nErr != nil {
			return false, interp.ErrTypeMismatch
		}
		return strings.Contains(s, ns), nil
	}
	// Try array/list containment via ArrayElems.
	_, elems, err := hostabi.ArrayElems(i, haystack)
	if err == nil {
		for _, elem := range elems {
			eq, eErr := hostabi.BoxedEqual(i, elem, needle)
			if eErr != nil {
				return false, eErr
			}
			if eq {
				return true, nil
			}
		}
		return false, nil
	}
	// Try dict/set (map) key membership.
	val, lErr := i.Load(haystack.Ref())
	if lErr == nil {
		if _, ok := val.Type().(*vmtypes.MapType); ok {
			_, found, gErr := hostabi.MapGet(i, val, needle)
			return found, gErr
		}
	}
	return false, interp.ErrTypeMismatch
}

// toFloat converts a Boxed numeric value to float64 for mixed-type operations.
func toFloat(v vmtypes.Boxed) float64 {
	switch v.Kind() {
	case vmtypes.KindI64:
		return float64(v.I64())
	case vmtypes.KindF64:
		return v.F64()
	case vmtypes.KindF32:
		return float64(v.F32())
	case vmtypes.KindI1:
		if v.I32() != 0 {
			return 1
		}
		return 0
	default:
		return 0
	}
}

// toInt converts a Boxed value to int64 if it is an integer kind.
func toInt(v vmtypes.Boxed) (int64, bool) {
	switch v.Kind() {
	case vmtypes.KindI64:
		return v.I64(), true
	case vmtypes.KindI1:
		if v.I32() != 0 {
			return 1, true
		}
		return 0, true
	default:
		return 0, false
	}
}

// pyFloorDiv implements Python's floor division for integers.
func pyFloorDiv(a, b int64) int64 {
	q := a / b
	r := a % b
	if r != 0 && (a^b) < 0 {
		q--
	}
	return q
}

// pyMod implements Python's modulo for integers (result has sign of divisor).
func pyMod(a, b int64) int64 {
	r := a % b
	if r != 0 && (a^b) < 0 {
		r += b
	}
	return r
}

// intPow computes integer exponentiation for non-negative exponents.
func intPow(base, exp int64) int64 {
	result := int64(1)
	for exp > 0 {
		if exp&1 == 1 {
			result *= base
		}
		base *= base
		exp >>= 1
	}
	return result
}

func cmpStrings(left, right string, op token.Type) bool {
	switch op {
	case token.EQ:
		return left == right
	case token.NE:
		return left != right
	case token.LT:
		return left < right
	case token.LE:
		return left <= right
	case token.GT:
		return left > right
	case token.GE:
		return left >= right
	default:
		return false
	}
}

func cmpFloats(left, right float64, op token.Type) bool {
	switch op {
	case token.EQ:
		return left == right
	case token.NE:
		return left != right
	case token.LT:
		return left < right
	case token.LE:
		return left <= right
	case token.GT:
		return left > right
	case token.GE:
		return left >= right
	default:
		return false
	}
}
