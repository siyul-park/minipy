package builtins

import (
	"math"

	"github.com/siyul-park/minipy/ast"
	"github.com/siyul-park/minipy/hostabi"
	"github.com/siyul-park/minipy/module"
	"github.com/siyul-park/minipy/operator"
	"github.com/siyul-park/minipy/token"
	"github.com/siyul-park/minipy/types"
	"github.com/siyul-park/minivm/interp"

	"github.com/siyul-park/minivm/instr"
	vmtypes "github.com/siyul-park/minivm/types"
)

func emitPrint(e module.Emitter, args []ast.Expr) {
	arg := args[0]
	e.Expr(arg)
	if isDynamicEmit(e.Type(arg)) {
		e.CallHostVoid(e.Once(module.HostKey(Name, "print", "dynamic"), func() *interp.HostFunction { return operator.DynPrint(e.Runtime().Out()) }))
		return
	}
	e.CallHostVoid(e.Once(module.HostKey(Name, "print", e.Type(arg)), func() *interp.HostFunction { return hostabi.PrintFunction(e.Runtime().Out(), e.Type(arg)) }))
}

func emitStr(e module.Emitter, args []ast.Expr) {
	arg := args[0]
	e.Expr(arg)
	if isDynamicEmit(e.Type(arg)) {
		e.CallHost(e.Once(module.HostKey(Name, "str", "dynamic"), operator.DynStr))
		return
	}
	if e.Type(arg) != types.Str {
		e.CallHost(e.Once(module.HostKey(Name, "str", e.Type(arg)), func() *interp.HostFunction { return hostabi.StringFunction(e.Type(arg)) }))
	}
}

func emitInt(e module.Emitter, args []ast.Expr) {
	e.Expr(args[0])
	switch e.Type(args[0]) {
	case types.Float:
		e.Emit(instr.F64_TO_I64_S)
	case types.Bool:
		e.Emit(instr.I32_TO_I64_S)
	case types.Str:
		e.CallHost(e.Host(Name, "int"))
	}
}

func emitFloat(e module.Emitter, args []ast.Expr) {
	e.Expr(args[0])
	switch e.Type(args[0]) {
	case types.Int:
		e.Emit(instr.I64_TO_F64_S)
	case types.Bool:
		e.Emit(instr.I32_TO_F64_S)
	case types.Str:
		e.CallHost(e.Host(Name, "float"))
	}
}

func emitBool(e module.Emitter, args []ast.Expr) {
	arg := args[0]
	e.Expr(arg)
	typ := e.Type(arg)
	if isDynamicEmit(typ) {
		e.CallHost(e.Once(module.HostKey(Name, "bool", "dynamic"), operator.DynBool))
		return
	}
	switch typ {
	case types.Int:
		e.Emit(instr.I64_CONST, 0)
		e.Emit(instr.I64_NE)
	case types.Float:
		e.Emit(instr.F64_CONST, math.Float64bits(0))
		e.Emit(instr.F64_NE)
	case types.Str:
		e.Emit(instr.STRING_LEN)
		e.Emit(instr.I32_CONST, 0)
		e.Emit(instr.I32_NE)
	default:
		switch t := typ.(type) {
		case *types.List:
			e.Emit(instr.ARRAY_LEN)
			e.Emit(instr.I32_CONST, 0)
			e.Emit(instr.I32_NE)
		case *types.Dict, *types.Set:
			e.Emit(instr.MAP_LEN)
			e.Emit(instr.I32_CONST, 0)
			e.Emit(instr.I32_NE)
		case *types.Tuple:
			e.Emit(instr.DROP)
			if len(t.Elems) == 0 {
				e.Emit(instr.I32_CONST, 0)
			} else {
				e.Emit(instr.I32_CONST, 1)
			}
			// Normalize to i1 so bool() is uniformly bool-kinded.
			e.Emit(instr.I32_CONST, 0)
			e.Emit(instr.I32_NE)
		case *types.Iterator, *types.Callable, *types.Class:
			e.Emit(instr.REF_IS_NULL)
			e.Emit(instr.I32_EQZ)
		}
	}
}

func emitAbs(e module.Emitter, args []ast.Expr) {
	arg := args[0]
	if e.Type(arg) == types.Int {
		e.Expr(arg)
		e.Emit(instr.DUP)
		e.Emit(instr.I64_CONST, 0)
		e.Emit(instr.I64_LT_S)
		neg := e.Label()
		end := e.Label()
		e.BrIf(neg)
		e.Br(end)
		e.Bind(neg)
		e.Emit(instr.I64_CONST, 0)
		e.Emit(instr.SWAP)
		e.Emit(instr.I64_SUB)
		e.Bind(end)
		return
	}
	e.Expr(arg)
	e.Emit(instr.F64_ABS)
}

func emitLen(e module.Emitter, args []ast.Expr) {
	arg := args[0]
	e.Expr(arg)
	if isDynamicEmit(e.Type(arg)) {
		e.CallHost(e.Once(module.HostKey(Name, "len", "dynamic"), operator.DynLen))
		return
	}
	switch t := e.Type(arg).(type) {
	case *types.List:
		e.Emit(instr.ARRAY_LEN)
	case *types.Dict, *types.Set:
		e.Emit(instr.MAP_LEN)
	case *types.Tuple:
		e.Emit(instr.I32_CONST, uint64(len(t.Elems)))
	default:
		if types.Equal(e.Type(arg), types.Bytes) {
			e.Emit(instr.ARRAY_LEN)
		} else {
			// STRING_LEN reports the underlying UTF-8 byte count, not the
			// codepoint count str iteration/indexing/slicing already use;
			// strLenHost counts codepoints so len() agrees with them.
			e.CallHost(e.Once(module.HostKey(Name, "len", "str"), strLenHost))
			return
		}
	}
	e.Emit(instr.I32_TO_I64_S)
}

func emitEnumerate(e module.Emitter, args []ast.Expr) {
	e.Expr(args[0])
	result, _ := enumerateResult([]types.Type{e.Type(args[0])})
	e.CallHost(e.Once(module.HostKey(Name, "enumerate", result), func() *interp.HostFunction { return enumerateHost(result) }))
}

func emitZip(e module.Emitter, args []ast.Expr) {
	e.Expr(args[0])
	e.Expr(args[1])
	result, _ := zipResult([]types.Type{e.Type(args[0]), e.Type(args[1])})
	e.CallHost(e.Once(module.HostKey(Name, "zip", result), func() *interp.HostFunction { return zipHost(result) }))
}

func emitRange(e module.Emitter, args []ast.Expr) {
	start, stop, step := RangeBounds(args)
	emitBound(e, start, 0)
	e.Expr(stop)
	emitBound(e, step, 1)
	e.CallHost(e.Host(Name, "range"))
}

// RangeBounds splits a range call's arguments into its start, stop and step,
// with a nil start or step standing for the default the 1- and 2-argument forms
// omit. It is exported because a caller lowering `for x in range(...)` needs the
// same split to emit a counter loop instead of an iterator, and the arity rule
// belongs here rather than duplicated there.
func RangeBounds(args []ast.Expr) (start, stop, step ast.Expr) {
	switch len(args) {
	case 1:
		return nil, args[0], nil
	case 2:
		return args[0], args[1], nil
	default:
		return args[0], args[1], args[2]
	}
}

// emitBound pushes a range bound, or the literal default when it was omitted.
func emitBound(e module.Emitter, bound ast.Expr, def uint64) {
	if bound == nil {
		e.Emit(instr.I64_CONST, def)
		return
	}
	e.Expr(bound)
}

func emitIter(e module.Emitter, args []ast.Expr) {
	arg := args[0]
	typ := e.Type(arg)
	if _, ok := typ.(*types.Iterator); ok {
		e.Expr(arg)
		return
	}
	e.Expr(arg)
	switch typ.(type) {
	case *types.Dict, *types.Set:
		e.Emit(instr.MAP_ITER)
	case *types.List:
		e.CallHost(e.Once(module.HostKey(Name, "iter", typ), func() *interp.HostFunction { return listIter(typ) }))
	default:
		if types.Equal(typ, types.Str) {
			e.CallHost(e.Once(module.HostKey(Name, "iter", "str"), strIter))
		} else if types.Equal(typ, types.Bytes) {
			e.CallHost(e.Once(module.HostKey(Name, "iter", "bytes"), bytesIter))
		}
	}
}

func emitSorted(e module.Emitter, args []ast.Expr) {
	e.Expr(args[0])
	if len(args) == 1 {
		e.Emit(instr.I32_CONST, 0)
	} else {
		e.Expr(args[1])
	}
	e.CallHost(sortedHost(e.Type(args[0])))
}

func emitReversed(e module.Emitter, args []ast.Expr) {
	e.Expr(args[0])
	e.CallHost(reversedHost(e.Type(args[0])))
}

func emitMin(e module.Emitter, args []ast.Expr) {
	if len(args) == 1 {
		e.Expr(args[0])
		e.CallHost(e.Once(module.HostKey(Name, "min", "list", e.Type(args[0])), func() *interp.HostFunction { return minListHost(e.Type(args[0])) }))
		return
	}
	for _, arg := range args {
		e.Expr(arg)
	}
	e.CallHost(e.Once(module.HostKey(Name, "min", e.Type(args[0]), len(args)), func() *interp.HostFunction { return minArgsHost(e.Type(args[0]), len(args)) }))
}

func emitMax(e module.Emitter, args []ast.Expr) {
	if len(args) == 1 {
		e.Expr(args[0])
		e.CallHost(e.Once(module.HostKey(Name, "max", "list", e.Type(args[0])), func() *interp.HostFunction { return maxListHost(e.Type(args[0])) }))
		return
	}
	for _, arg := range args {
		e.Expr(arg)
	}
	e.CallHost(e.Once(module.HostKey(Name, "max", e.Type(args[0]), len(args)), func() *interp.HostFunction { return maxArgsHost(e.Type(args[0]), len(args)) }))
}

func emitSum(e module.Emitter, args []ast.Expr) {
	e.Expr(args[0])
	e.CallHost(e.Once(module.HostKey(Name, "sum", e.Type(args[0])), func() *interp.HostFunction { return sumHost(e.Type(args[0])) }))
}

func emitAny(e module.Emitter, args []ast.Expr) {
	e.Expr(args[0])
	e.CallHost(e.Once(module.HostKey(Name, "any"), anyHost))
}

func emitAll(e module.Emitter, args []ast.Expr) {
	e.Expr(args[0])
	e.CallHost(e.Once(module.HostKey(Name, "all"), allHost))
}

func emitRound(e module.Emitter, args []ast.Expr) {
	e.Expr(args[0])
	if len(args) == 2 {
		e.Expr(args[1])
		e.CallHost(e.Once(module.HostKey(Name, "round", "digits"), roundDigitsHost))
	} else {
		e.CallHost(e.Once(module.HostKey(Name, "round"), roundHost))
	}
}

func emitDivmod(e module.Emitter, args []ast.Expr) {
	e.Expr(args[0])
	e.Expr(args[1])
	e.CallHost(e.Once(module.HostKey(Name, "divmod", e.Type(args[0])), func() *interp.HostFunction { return divmodHost(e.Type(args[0])) }))
}

func emitPow(e module.Emitter, args []ast.Expr) {
	left, right := e.Type(args[0]), e.Type(args[1])
	if operator.PowFloatResult(token.DOUBLESTAR, args[1]) &&
		types.Equal(types.Erase(left), types.Int) && types.Equal(types.Erase(right), types.Int) {
		operator.EmitPowFloat(e, left, right,
			func() { e.Expr(args[0]) },
			func() { e.Expr(args[1]) })
		return
	}
	e.Expr(args[0])
	e.Expr(args[1])
	e.CallHost(e.Once(module.HostKey(Name, "pow", left, right), func() *interp.HostFunction { return powHost(left, right) }))
}

func emitHex(e module.Emitter, args []ast.Expr) {
	e.Expr(args[0])
	e.CallHost(hexHost())
}

func emitOct(e module.Emitter, args []ast.Expr) {
	e.Expr(args[0])
	e.CallHost(e.Once(module.HostKey(Name, "oct"), octHost))
}

func emitBin(e module.Emitter, args []ast.Expr) {
	e.Expr(args[0])
	e.CallHost(e.Once(module.HostKey(Name, "bin"), binHost))
}

func emitRepr(e module.Emitter, args []ast.Expr) {
	e.Expr(args[0])
	e.CallHost(e.Once(module.HostKey(Name, "repr", e.Type(args[0])), func() *interp.HostFunction { return reprHost(e.Type(args[0])) }))
}

func emitOrd(e module.Emitter, args []ast.Expr) {
	e.Expr(args[0])
	e.CallHost(e.Host(Name, "ord"))
}

func emitChr(e module.Emitter, args []ast.Expr) {
	e.Expr(args[0])
	e.CallHost(e.Host(Name, "chr"))
}

func emitNext(e module.Emitter, args []ast.Expr) {
	valSlot := e.Tmp(vmtypes.TypeAny)
	done := e.Label()
	end := e.Label()
	e.Expr(args[0])
	e.Emit(instr.DUP)
	e.Emit(instr.CORO_DONE)
	e.BrIf(done)
	e.Emit(instr.DUP)
	e.Emit(instr.CORO_VALUE)
	e.Emit(instr.GLOBAL_SET, uint64(valSlot))
	e.Emit(instr.REF_NULL)
	e.Emit(instr.RESUME)
	e.Emit(instr.DROP)
	e.Emit(instr.GLOBAL_GET, uint64(valSlot))
	e.Br(end)
	e.Bind(done)
	e.Emit(instr.REF_NULL)
	e.Emit(instr.RESUME)
	e.Emit(instr.DROP)
	e.Emit(instr.UNREACHABLE)
	e.Bind(end)
}

func isDynamicEmit(t types.Type) bool {
	return types.IsDynamic(types.Erase(t))
}
