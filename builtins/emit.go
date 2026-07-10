package builtins

import (
	"math"

	"github.com/siyul-park/minipy/ast"
	"github.com/siyul-park/minipy/module"
	"github.com/siyul-park/minipy/types"

	"github.com/siyul-park/minivm/instr"
)

func emitPrint(e module.Emitter, args []ast.Expr) {
	e.Expr(args[0])
	e.CallHostVoid(e.Host(Name, "print"))
}

func emitStr(e module.Emitter, args []ast.Expr) {
	e.Expr(args[0])
	if e.Type(args[0]) != types.Str {
		e.CallHost(e.Host(Name, "str"))
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
	switch typ := e.Type(arg); typ {
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
			e.Emit(instr.STRING_LEN)
		}
	}
	e.Emit(instr.I32_TO_I64_S)
}

func emitEnumerate(e module.Emitter, args []ast.Expr) {
	e.Expr(args[0])
	result, _ := enumerateResult([]types.Type{e.Type(args[0])})
	e.CallHost(enumerateHost(result))
}

func emitZip(e module.Emitter, args []ast.Expr) {
	e.Expr(args[0])
	e.Expr(args[1])
	result, _ := zipResult([]types.Type{e.Type(args[0]), e.Type(args[1])})
	e.CallHost(zipHost(result))
}

func emitRange(e module.Emitter, args []ast.Expr) {
	switch len(args) {
	case 1:
		e.Emit(instr.I64_CONST, 0)
		e.Expr(args[0])
		e.Emit(instr.I64_CONST, 1)
	case 2:
		e.Expr(args[0])
		e.Expr(args[1])
		e.Emit(instr.I64_CONST, 1)
	default:
		e.Expr(args[0])
		e.Expr(args[1])
		e.Expr(args[2])
	}
	e.CallHost(e.Host(Name, "range"))
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
		e.CallHost(listIter(typ))
	default:
		if types.Equal(typ, types.Str) {
			e.CallHost(strIter())
		} else if types.Equal(typ, types.Bytes) {
			e.CallHost(bytesIter())
		}
	}
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
	valSlot := e.Tmp()
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
