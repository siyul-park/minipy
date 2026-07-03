package builtins

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/siyul-park/minipy/hostabi"
	"github.com/siyul-park/minipy/types"

	"github.com/siyul-park/minivm/interp"
	vmtypes "github.com/siyul-park/minivm/types"
)

func printHost(out io.Writer) *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{vmtypes.TypeRef}, Returns: nil},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			fmt.Fprintln(out, hostabi.FormatScalar(i, params[0]))
			return nil, nil
		},
	)
}

func strHost() *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{vmtypes.TypeRef}, Returns: []vmtypes.Type{vmtypes.TypeString}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			return hostabi.AllocString(i, hostabi.FormatScalar(i, params[0]))
		},
	)
}

func intParseHost() *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{vmtypes.TypeString}, Returns: []vmtypes.Type{vmtypes.TypeI64}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			s := hostabi.LoadStr(i, params[0])
			n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid literal for int() with base 10: %q", s)
			}
			return []vmtypes.Boxed{vmtypes.BoxI64(n)}, nil
		},
	)
}

func floatParseHost() *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{vmtypes.TypeString}, Returns: []vmtypes.Type{vmtypes.TypeF64}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			s := hostabi.LoadStr(i, params[0])
			f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
			if err != nil {
				return nil, fmt.Errorf("could not convert string to float: %q", s)
			}
			return []vmtypes.Boxed{vmtypes.BoxF64(f)}, nil
		},
	)
}

func rangeIterHost() *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{vmtypes.TypeI64, vmtypes.TypeI64, vmtypes.TypeI64}, Returns: []vmtypes.Type{vmtypes.TypeRef}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			step := hostabi.LoadI64(i, params[2])
			if step == 0 {
				return nil, fmt.Errorf("range() step must not be zero")
			}
			addr, err := i.Alloc(newRangeIterator(hostabi.LoadI64(i, params[0]), hostabi.LoadI64(i, params[1]), step))
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
			_, elems := hostabi.ArrayElems(i, params[0])
			out := make([]vmtypes.Boxed, 0, len(elems))
			for idx, elem := range elems {
				addr, err := i.Alloc(vmtypes.NewStruct(tupleType, vmtypes.BoxI64(int64(idx)), elem))
				if err != nil {
					return nil, err
				}
				out = append(out, vmtypes.BoxRef(addr))
			}
			return hostabi.AllocArray(i, result.VM().(*vmtypes.ArrayType), out)
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
			_, a := hostabi.ArrayElems(i, params[0])
			_, b := hostabi.ArrayElems(i, params[1])
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
			return hostabi.AllocArray(i, result.VM().(*vmtypes.ArrayType), out)
		},
	)
}

func listIter(arg types.Type) *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{arg.VM()}, Returns: []vmtypes.Type{vmtypes.TypeRef}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			_, elems := hostabi.ArrayElems(i, params[0])
			addr, err := i.Alloc(hostabi.NewIterator("list.iterator", elems))
			if err != nil {
				return nil, err
			}
			return []vmtypes.Boxed{vmtypes.BoxRef(addr)}, nil
		},
	)
}

func strIter() *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{vmtypes.TypeString}, Returns: []vmtypes.Type{vmtypes.TypeRef}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			s := hostabi.LoadStr(i, params[0])
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
