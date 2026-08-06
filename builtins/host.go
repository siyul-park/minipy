package builtins

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/siyul-park/minipy/hostabi"
	"github.com/siyul-park/minipy/types"

	"github.com/siyul-park/minivm/interp"
	vmtypes "github.com/siyul-park/minivm/types"
)

// Runtime ValueError cases exposed by builtin host functions. The compiler
// boundary can classify these by identity without matching message text.
var (
	ErrIntValue         = errors.New("invalid literal for int() with base 10")
	ErrFloatValue       = errors.New("could not convert string to float")
	ErrRangeStep        = errors.New("range() step must not be zero")
	ErrOrdValue         = errors.New("ord() expected a single Unicode character")
	ErrChrValue         = errors.New("chr() argument out of range")
	ErrMinMaxEmpty      = errors.New("min()/max() arg is an empty sequence")
	ErrDivisionByZero   = errors.New("division by zero")
	ErrNegativeExponent = errors.New("negative exponent not supported for integer pow")
)

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

func intParseHost() *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{vmtypes.TypeString}, Returns: []vmtypes.Type{vmtypes.TypeI64}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			s, err := hostabi.LoadStr(i, params[0])
			if err != nil {
				return nil, err
			}
			n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
			if err != nil {
				return nil, fmt.Errorf("%w: %q", ErrIntValue, s)
			}
			boxed, err := hostabi.BoxInt(i, n)
			if err != nil {
				return nil, err
			}
			return []vmtypes.Boxed{boxed}, nil
		},
	)
}

func floatParseHost() *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{vmtypes.TypeString}, Returns: []vmtypes.Type{vmtypes.TypeF64}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			s, err := hostabi.LoadStr(i, params[0])
			if err != nil {
				return nil, err
			}
			f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
			if err != nil {
				return nil, fmt.Errorf("%w: %q", ErrFloatValue, s)
			}
			return []vmtypes.Boxed{hostabi.BoxFloat(f)}, nil
		},
	)
}

func rangeIterHost() *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{vmtypes.TypeI64, vmtypes.TypeI64, vmtypes.TypeI64}, Returns: []vmtypes.Type{vmtypes.TypeRef}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			start, err := hostabi.LoadI64(i, params[0])
			if err != nil {
				return nil, err
			}
			stop, err := hostabi.LoadI64(i, params[1])
			if err != nil {
				return nil, err
			}
			step, err := hostabi.LoadI64(i, params[2])
			if err != nil {
				return nil, err
			}
			if step == 0 {
				return nil, ErrRangeStep
			}
			addr, err := i.Alloc(newRangeIterator(start, stop, step))
			if err != nil {
				return nil, err
			}
			return []vmtypes.Boxed{vmtypes.BoxRef(addr)}, nil
		},
	)
}

func enumerateHost(result types.Type) *interp.HostFunction {
	list := result.(*types.List)
	tupleType := list.Elem.VM().(*vmtypes.StructType)
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{types.NewList(list.Elem.(*types.Tuple).Elems[1]).VM()}, Returns: []vmtypes.Type{result.VM()}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			_, elems, err := hostabi.ArrayElems(i, params[0])
			if err != nil {
				return nil, err
			}
			out := make([]vmtypes.Boxed, 0, len(elems))
			for idx, elem := range elems {
				// elem is borrowed from the input array; the struct becomes
				// its second owner, so it needs its own retain (see
				// hostabi's package doc comment).
				fields := []vmtypes.Boxed{vmtypes.BoxI64(int64(idx)), elem}
				if err := hostabi.RetainBoxes(i, fields); err != nil {
					return nil, err
				}
				addr, err := i.Alloc(vmtypes.NewStruct(tupleType, fields...))
				if err != nil {
					_ = hostabi.ReleaseBoxes(i, fields)
					return nil, err
				}
				out = append(out, vmtypes.BoxRef(addr))
			}
			// out holds structs this call already owns outright (they were
			// just allocated); AllocArray retains them again on the result
			// array's behalf, so release our own stake once it succeeds.
			res, err := hostabi.AllocArray(i, result.VM().(*vmtypes.ArrayType), out)
			if err != nil {
				return nil, err
			}
			if err := hostabi.ReleaseBoxes(i, out); err != nil {
				return nil, err
			}
			return res, nil
		},
	)
}

func zipHost(result types.Type) *interp.HostFunction {
	list := result.(*types.List)
	tupleType := list.Elem.VM().(*vmtypes.StructType)
	tuple := list.Elem.(*types.Tuple)
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{types.NewList(tuple.Elems[0]).VM(), types.NewList(tuple.Elems[1]).VM()}, Returns: []vmtypes.Type{result.VM()}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			_, a, err := hostabi.ArrayElems(i, params[0])
			if err != nil {
				return nil, err
			}
			_, b, err := hostabi.ArrayElems(i, params[1])
			if err != nil {
				return nil, err
			}
			n := len(a)
			if len(b) < n {
				n = len(b)
			}
			out := make([]vmtypes.Boxed, 0, n)
			for idx := 0; idx < n; idx++ {
				// a[idx]/b[idx] are borrowed from the input arrays; the
				// struct becomes their second owner, so they need their own
				// retain (see hostabi's package doc comment).
				fields := []vmtypes.Boxed{a[idx], b[idx]}
				if err := hostabi.RetainBoxes(i, fields); err != nil {
					return nil, err
				}
				addr, err := i.Alloc(vmtypes.NewStruct(tupleType, fields...))
				if err != nil {
					_ = hostabi.ReleaseBoxes(i, fields)
					return nil, err
				}
				out = append(out, vmtypes.BoxRef(addr))
			}
			// out holds structs this call already owns outright (they were
			// just allocated); AllocArray retains them again on the result
			// array's behalf, so release our own stake once it succeeds.
			res, err := hostabi.AllocArray(i, result.VM().(*vmtypes.ArrayType), out)
			if err != nil {
				return nil, err
			}
			if err := hostabi.ReleaseBoxes(i, out); err != nil {
				return nil, err
			}
			return res, nil
		},
	)
}

func listIter(arg types.Type) *interp.HostFunction {
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

func ordHost() *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{vmtypes.TypeString}, Returns: []vmtypes.Type{vmtypes.TypeI64}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			s, err := hostabi.LoadStr(i, params[0])
			if err != nil {
				return nil, err
			}
			runes := []rune(s)
			if len(runes) != 1 {
				return nil, fmt.Errorf("%w: %q has %d codepoints", ErrOrdValue, string(runes), len(runes))
			}
			return []vmtypes.Boxed{vmtypes.BoxI64(int64(runes[0]))}, nil
		},
	)
}

// strLenHost implements len(str) by codepoint, not by the raw byte count
// STRING_LEN reports for the underlying UTF-8 storage. String iteration
// (strIter, below), indexing, and slicing (strIndex/strSlice,
// compiler/runtime.go) already count/index by codepoint via []rune; len
// disagreeing with them for any non-ASCII string is the bug this closes.
func strLenHost() *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{vmtypes.TypeString}, Returns: []vmtypes.Type{vmtypes.TypeI64}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			s, err := hostabi.LoadStr(i, params[0])
			if err != nil {
				return nil, err
			}
			return []vmtypes.Boxed{vmtypes.BoxI64(int64(utf8.RuneCountInString(s)))}, nil
		},
	)
}

func chrHost() *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{vmtypes.TypeI64}, Returns: []vmtypes.Type{vmtypes.TypeString}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			n, err := hostabi.LoadI64(i, params[0])
			if err != nil {
				return nil, err
			}
			if n < 0 || n > 0x10FFFF || (n >= 0xD800 && n <= 0xDFFF) {
				return nil, fmt.Errorf("%w: %d not a Unicode scalar value", ErrChrValue, n)
			}
			return hostabi.AllocString(i, string(rune(n)))
		},
	)
}

func strIter() *interp.HostFunction {
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

// bytesIter yields each byte as an int in 0..255, reinterpreting the
// underlying signed i8 storage as unsigned (so 0x80 and 0xff yield 128/255,
// not negative values).
func bytesIter() *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{types.Bytes.VM()}, Returns: []vmtypes.Type{vmtypes.TypeRef}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			_, elems, err := hostabi.ArrayElems(i, params[0])
			if err != nil {
				return nil, err
			}
			values := make([]vmtypes.Boxed, len(elems))
			for idx, e := range elems {
				values[idx] = vmtypes.BoxI64(int64(uint8(e.I32())))
			}
			addr, err := i.Alloc(hostabi.NewIterator("bytes.iterator", values))
			if err != nil {
				return nil, err
			}
			return []vmtypes.Boxed{vmtypes.BoxRef(addr)}, nil
		},
	)
}

func sortedHost(arg types.Type) *interp.HostFunction {
	list := arg.(*types.List)
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{arg.VM()}, Returns: []vmtypes.Type{arg.VM()}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			typ, elems, err := hostabi.ArrayElems(i, params[0])
			if err != nil {
				return nil, err
			}
			copied := append([]vmtypes.Boxed(nil), elems...)
			var sortErr error
			sort.SliceStable(copied, func(a, b int) bool {
				if sortErr != nil {
					return false
				}
				return boxedLess(i, copied[a], copied[b], list.Elem, &sortErr)
			})
			if sortErr != nil {
				return nil, sortErr
			}
			return hostabi.AllocArray(i, typ, copied)
		},
	)
}

func reversedHost(arg types.Type) *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{arg.VM()}, Returns: []vmtypes.Type{arg.VM()}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			typ, elems, err := hostabi.ArrayElems(i, params[0])
			if err != nil {
				return nil, err
			}
			copied := make([]vmtypes.Boxed, len(elems))
			for idx, e := range elems {
				copied[len(elems)-1-idx] = e
			}
			return hostabi.AllocArray(i, typ, copied)
		},
	)
}

func minListHost(arg types.Type) *interp.HostFunction {
	list := arg.(*types.List)
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{arg.VM()}, Returns: []vmtypes.Type{list.Elem.VM()}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			_, elems, err := hostabi.ArrayElems(i, params[0])
			if err != nil {
				return nil, err
			}
			if len(elems) == 0 {
				return nil, ErrMinMaxEmpty
			}
			result := elems[0]
			for _, e := range elems[1:] {
				if boxedLess(i, e, result, list.Elem, &err) {
					result = e
				}
				if err != nil {
					return nil, err
				}
			}
			// result is borrowed from the input array; the caller's array
			// argument is released once this call returns (its box is not
			// among the returned values), so the returned element needs its
			// own retain to outlive that release (see hostabi's package doc
			// comment).
			if err := hostabi.RetainBoxes(i, []vmtypes.Boxed{result}); err != nil {
				return nil, err
			}
			return []vmtypes.Boxed{result}, nil
		},
	)
}

func maxListHost(arg types.Type) *interp.HostFunction {
	list := arg.(*types.List)
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{arg.VM()}, Returns: []vmtypes.Type{list.Elem.VM()}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			_, elems, err := hostabi.ArrayElems(i, params[0])
			if err != nil {
				return nil, err
			}
			if len(elems) == 0 {
				return nil, ErrMinMaxEmpty
			}
			result := elems[0]
			for _, e := range elems[1:] {
				if boxedLess(i, result, e, list.Elem, &err) {
					result = e
				}
				if err != nil {
					return nil, err
				}
			}
			// result is borrowed from the input array; the caller's array
			// argument is released once this call returns (its box is not
			// among the returned values), so the returned element needs its
			// own retain to outlive that release (see hostabi's package doc
			// comment).
			if err := hostabi.RetainBoxes(i, []vmtypes.Boxed{result}); err != nil {
				return nil, err
			}
			return []vmtypes.Boxed{result}, nil
		},
	)
}

func minArgsHost(elem types.Type, n int) *interp.HostFunction {
	params := make([]vmtypes.Type, n)
	for idx := range params {
		params[idx] = elem.VM()
	}
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: params, Returns: []vmtypes.Type{elem.VM()}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			result := params[0]
			for _, p := range params[1:] {
				var err error
				if boxedLess(i, p, result, elem, &err) {
					result = p
				}
				if err != nil {
					return nil, err
				}
			}
			return []vmtypes.Boxed{result}, nil
		},
	)
}

func maxArgsHost(elem types.Type, n int) *interp.HostFunction {
	params := make([]vmtypes.Type, n)
	for idx := range params {
		params[idx] = elem.VM()
	}
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: params, Returns: []vmtypes.Type{elem.VM()}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			result := params[0]
			for _, p := range params[1:] {
				var err error
				if boxedLess(i, result, p, elem, &err) {
					result = p
				}
				if err != nil {
					return nil, err
				}
			}
			return []vmtypes.Boxed{result}, nil
		},
	)
}

func sumHost(arg types.Type) *interp.HostFunction {
	list := arg.(*types.List)
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{arg.VM()}, Returns: []vmtypes.Type{list.Elem.VM()}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			_, elems, err := hostabi.ArrayElems(i, params[0])
			if err != nil {
				return nil, err
			}
			if types.Equal(list.Elem, types.Int) {
				var total int64
				for _, e := range elems {
					n, err := hostabi.LoadI64(i, e)
					if err != nil {
						return nil, err
					}
					total += n
				}
				boxed, err := hostabi.BoxInt(i, total)
				if err != nil {
					return nil, err
				}
				return []vmtypes.Boxed{boxed}, nil
			}
			var total float64
			for _, e := range elems {
				total += e.F64()
			}
			return []vmtypes.Boxed{hostabi.BoxFloat(total)}, nil
		},
	)
}

func anyHost() *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{types.NewList(types.Bool).VM()}, Returns: []vmtypes.Type{vmtypes.TypeI1}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			_, elems, err := hostabi.ArrayElems(i, params[0])
			if err != nil {
				return nil, err
			}
			for _, e := range elems {
				if e.Bool() {
					return []vmtypes.Boxed{vmtypes.BoxI1(true)}, nil
				}
			}
			return []vmtypes.Boxed{vmtypes.BoxI1(false)}, nil
		},
	)
}

func allHost() *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{types.NewList(types.Bool).VM()}, Returns: []vmtypes.Type{vmtypes.TypeI1}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			_, elems, err := hostabi.ArrayElems(i, params[0])
			if err != nil {
				return nil, err
			}
			for _, e := range elems {
				if !e.Bool() {
					return []vmtypes.Boxed{vmtypes.BoxI1(false)}, nil
				}
			}
			return []vmtypes.Boxed{vmtypes.BoxI1(true)}, nil
		},
	)
}

func roundHost() *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{vmtypes.TypeF64}, Returns: []vmtypes.Type{vmtypes.TypeI64}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			f := params[0].F64()
			return []vmtypes.Boxed{vmtypes.BoxI64(int64(math.RoundToEven(f)))}, nil
		},
	)
}

func roundDigitsHost() *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{vmtypes.TypeF64, vmtypes.TypeI64}, Returns: []vmtypes.Type{vmtypes.TypeF64}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			f := params[0].F64()
			n := params[1].I64()
			return []vmtypes.Boxed{hostabi.BoxFloat(roundDigits(f, n))}, nil
		},
	)
}

// maxRoundDigits bounds the ndigits magnitude roundDigits scales by. Beyond
// it, rounding cannot change the result: float64 has at most 323 significant
// decimal places (denormal minimum ~4.9e-324), so a larger positive ndigits
// leaves f unchanged and a smaller (more negative) ndigits collapses any
// finite f to a signed zero, matching CPython's round().
const maxRoundDigits = 323

// roundDigits rounds f to n decimal digits the way CPython's round(float, int)
// does: round-half-to-even applied to f's exact binary value, not to a
// float64-scaled approximation of it. Scaling f by 10**n in float64 (the
// naive shift-round-unshift approach) loses precision before rounding ever
// happens, which is why round(2.675, 2) must land on 2.67 — 2.675 is not
// exactly representable, and its nearest double is already below the
// halfway point. big.Rat holds f and the decimal scale exactly, so the
// halfway comparison in roundRatToEven sees f's true value.
func roundDigits(f float64, n int64) float64 {
	if math.IsNaN(f) || math.IsInf(f, 0) || f == 0 {
		return f
	}
	if n > maxRoundDigits {
		return f
	}
	if n < -maxRoundDigits {
		return math.Copysign(0, f)
	}

	magnitude := n
	if magnitude < 0 {
		magnitude = -magnitude
	}
	scale := new(big.Rat).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(magnitude), nil))

	scaled := new(big.Rat).SetFloat64(f)
	if n >= 0 {
		scaled.Mul(scaled, scale)
	} else {
		scaled.Quo(scaled, scale)
	}
	rounded := new(big.Rat).SetInt(roundRatToEven(scaled))
	if n >= 0 {
		rounded.Quo(rounded, scale)
	} else {
		rounded.Mul(rounded, scale)
	}
	result, _ := rounded.Float64()
	return result
}

// roundRatToEven rounds r to the nearest integer, breaking exact halfway
// ties toward the even neighbor (banker's rounding), matching CPython's
// round().
func roundRatToEven(r *big.Rat) *big.Int {
	num, den := r.Num(), r.Denom()
	quotient, remainder := new(big.Int).QuoRem(num, den, new(big.Int))
	twiceRemainder := new(big.Int).Lsh(remainder.Abs(remainder), 1)
	switch twiceRemainder.Cmp(den) {
	case -1:
		return quotient
	case 0:
		if quotient.Bit(0) == 0 {
			return quotient
		}
	}
	if num.Sign() < 0 {
		return quotient.Sub(quotient, big.NewInt(1))
	}
	return quotient.Add(quotient, big.NewInt(1))
}

func divmodHost(elem types.Type) *interp.HostFunction {
	if types.Equal(elem, types.Int) {
		tupleType := types.NewTuple(types.Int, types.Int).VM().(*vmtypes.StructType)
		return interp.NewHostFunction(
			&vmtypes.FunctionType{Params: []vmtypes.Type{vmtypes.TypeI64, vmtypes.TypeI64}, Returns: []vmtypes.Type{vmtypes.TypeRef}},
			func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
				a := params[0].I64()
				b := params[1].I64()
				if b == 0 {
					return nil, ErrDivisionByZero
				}
				q := a / b
				r := a % b
				if (r != 0) && ((r ^ b) < 0) {
					q--
					r += b
				}
				addr, err := i.Alloc(vmtypes.NewStruct(tupleType, vmtypes.BoxI64(q), vmtypes.BoxI64(r)))
				if err != nil {
					return nil, err
				}
				return []vmtypes.Boxed{vmtypes.BoxRef(addr)}, nil
			},
		)
	}
	tupleType := types.NewTuple(types.Float, types.Float).VM().(*vmtypes.StructType)
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{vmtypes.TypeF64, vmtypes.TypeF64}, Returns: []vmtypes.Type{vmtypes.TypeRef}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			a := params[0].F64()
			b := params[1].F64()
			if b == 0 {
				return nil, ErrDivisionByZero
			}
			q := math.Floor(a / b)
			r := a - q*b
			addr, err := i.Alloc(vmtypes.NewStruct(tupleType, hostabi.BoxFloat(q), hostabi.BoxFloat(r)))
			if err != nil {
				return nil, err
			}
			return []vmtypes.Boxed{vmtypes.BoxRef(addr)}, nil
		},
	)
}

func powHost(base, exp types.Type) *interp.HostFunction {
	if types.Equal(base, types.Int) && types.Equal(exp, types.Int) {
		return interp.NewHostFunction(
			&vmtypes.FunctionType{Params: []vmtypes.Type{vmtypes.TypeI64, vmtypes.TypeI64}, Returns: []vmtypes.Type{vmtypes.TypeI64}},
			func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
				b := params[0].I64()
				e := params[1].I64()
				result, err := intPow(b, e)
				if err != nil {
					return nil, err
				}
				boxed, err := hostabi.BoxInt(i, result)
				if err != nil {
					return nil, err
				}
				return []vmtypes.Boxed{boxed}, nil
			},
		)
	}
	paramTypes := make([]vmtypes.Type, 2)
	if types.Equal(base, types.Int) {
		paramTypes[0] = vmtypes.TypeI64
	} else {
		paramTypes[0] = vmtypes.TypeF64
	}
	if types.Equal(exp, types.Int) {
		paramTypes[1] = vmtypes.TypeI64
	} else {
		paramTypes[1] = vmtypes.TypeF64
	}
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: paramTypes, Returns: []vmtypes.Type{vmtypes.TypeF64}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			var bf, ef float64
			if types.Equal(base, types.Int) {
				bf = float64(params[0].I64())
			} else {
				bf = params[0].F64()
			}
			if types.Equal(exp, types.Int) {
				ef = float64(params[1].I64())
			} else {
				ef = params[1].F64()
			}
			return []vmtypes.Boxed{hostabi.BoxFloat(math.Pow(bf, ef))}, nil
		},
	)
}

func hexHost() *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{vmtypes.TypeI64}, Returns: []vmtypes.Type{vmtypes.TypeString}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			n := params[0].I64()
			var s string
			if n < 0 {
				s = fmt.Sprintf("-0x%x", -n)
			} else {
				s = fmt.Sprintf("0x%x", n)
			}
			return hostabi.AllocString(i, s)
		},
	)
}

func octHost() *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{vmtypes.TypeI64}, Returns: []vmtypes.Type{vmtypes.TypeString}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			n := params[0].I64()
			var s string
			if n < 0 {
				s = fmt.Sprintf("-0o%o", -n)
			} else {
				s = fmt.Sprintf("0o%o", n)
			}
			return hostabi.AllocString(i, s)
		},
	)
}

func binHost() *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{vmtypes.TypeI64}, Returns: []vmtypes.Type{vmtypes.TypeString}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			n := params[0].I64()
			var s string
			if n < 0 {
				s = "-0b" + strconv.FormatInt(-n, 2)
			} else {
				s = "0b" + strconv.FormatInt(n, 2)
			}
			return hostabi.AllocString(i, s)
		},
	)
}

func reprHost(t types.Type) *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{t.VM()}, Returns: []vmtypes.Type{vmtypes.TypeString}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			if params[0].Kind() == vmtypes.KindRef && params[0].Ref() != 0 {
				val, err := i.Load(params[0].Ref())
				if err != nil {
					return nil, err
				}
				if s, ok := val.(vmtypes.String); ok {
					return hostabi.AllocString(i, hostabi.ReprString(string(s), false))
				}
			}
			return hostabi.AllocString(i, hostabi.FormatScalar(i, params[0]))
		},
	)
}

func boxedLess(i *interp.Interpreter, a, b vmtypes.Boxed, elem types.Type, errp *error) bool {
	switch {
	case types.Equal(elem, types.Int):
		// Read through LoadI64: an int past the inline payload arrives as a
		// heap ref, and comparing the raw boxes would order the truncations.
		na, e := hostabi.LoadI64(i, a)
		if e != nil {
			*errp = e
			return false
		}
		nb, e := hostabi.LoadI64(i, b)
		if e != nil {
			*errp = e
			return false
		}
		return na < nb
	case types.Equal(elem, types.Float):
		return a.F64() < b.F64()
	case types.Equal(elem, types.Bool):
		return !a.Bool() && b.Bool()
	case types.Equal(elem, types.Str):
		sa, e := hostabi.LoadStr(i, a)
		if e != nil {
			*errp = e
			return false
		}
		sb, e := hostabi.LoadStr(i, b)
		if e != nil {
			*errp = e
			return false
		}
		return sa < sb
	}
	return false
}

func intPow(base, exp int64) (int64, error) {
	if exp < 0 {
		return 0, ErrNegativeExponent
	}
	result := int64(1)
	for exp > 0 {
		if exp&1 == 1 {
			result *= base
		}
		base *= base
		exp >>= 1
	}
	return result, nil
}
