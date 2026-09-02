// Package random provides Python's random module as a native module:
// pseudo-random number generation functions backed by host functions that use
// Go's math/rand/v2 package. Static types are preferred; dynamic/Any arguments
// are supported with runtime dispatch.
package random

import (
	"errors"
	"math/rand/v2"
	"sync"

	"github.com/siyul-park/minipy/ast"
	"github.com/siyul-park/minipy/hostabi"
	"github.com/siyul-park/minipy/module"
	"github.com/siyul-park/minipy/token"
	"github.com/siyul-park/minipy/types"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/interp"
	vmtypes "github.com/siyul-park/minivm/types"
)

// Name is the module's registered name.
const Name = "random"

// Runtime errors exposed by random host functions.
var (
	ErrEmptySequence = errors.New("cannot choose from an empty sequence")
	ErrEmptyRange    = errors.New("empty range for randrange()")
)

// rng is the module-level random source, protected by a mutex for
// concurrent safety. Note: seed() is process-global; all minipy programs in
// the same process share this RNG state. This is acceptable because minipy
// does not yet support concurrent program execution (see coding-patterns S10).
var (
	rng   *rand.Rand
	rngMu sync.Mutex
)

func init() {
	rng = rand.New(rand.NewPCG(0, 0))
}

// New builds the random native module. Host functions are pre-allocated once
// here and passed into emit closures to avoid redundant allocations per emit.
func New() *module.NativeModule {
	randomHF := randomHost()
	randintHF := randintHost()
	randrangeOneHF := randrangeOneHost()
	randrangeTwoHF := randrangeTwoHost()
	uniformHF := uniformHost()
	seedHF := seedHost()

	return module.NewNative(Name,
		module.NewSymbol("random", checkRandom, emitRandomFn(randomHF), nil),
		module.NewSymbol("randint", checkRandint, emitRandintFn(randintHF), nil),
		module.NewSymbol("randrange", checkRandrange, emitRandrangeFn(randrangeOneHF, randrangeTwoHF), nil),
		module.NewSymbol("uniform", checkUniform, emitUniformFn(uniformHF), nil),
		module.NewSymbol("choice", checkChoice, emitChoice, nil),
		module.NewSymbol("shuffle", checkShuffle, emitShuffle, nil),
		module.NewSymbol("seed", checkSeed, emitSeedFn(seedHF), nil),
	)
}

// --- Check functions (callers) ---

// checkRandom type-checks random(): 0 args, returns float.
func checkRandom(c module.Checker, args []ast.Expr, pos token.Pos) types.Type {
	if len(args) != 0 {
		c.Error(pos, token.ArityMismatch, "random() takes no arguments (%d given)", len(args))
		return types.Invalid
	}
	return types.Float
}

// checkRandint type-checks randint(a, b): 2 int args, returns int.
func checkRandint(c module.Checker, args []ast.Expr, pos token.Pos) types.Type {
	if len(args) != 2 {
		c.Error(pos, token.ArityMismatch, "randint() takes exactly 2 arguments (%d given)", len(args))
		for _, a := range args {
			c.Check(a)
		}
		return types.Invalid
	}
	t0 := c.Check(args[0])
	t1 := c.Check(args[1])
	if types.IsDynamic(t0) || types.IsDynamic(t1) {
		return types.Any
	}
	if !types.Equal(t0, types.Int) {
		c.Error(args[0].Pos(), token.TypeMismatch, "randint() arguments must be int")
		return types.Invalid
	}
	if !types.Equal(t1, types.Int) {
		c.Error(args[1].Pos(), token.TypeMismatch, "randint() arguments must be int")
		return types.Invalid
	}
	return types.Int
}

// checkRandrange type-checks randrange(stop) or randrange(start, stop): 1 or 2
// int args, returns int.
func checkRandrange(c module.Checker, args []ast.Expr, pos token.Pos) types.Type {
	if len(args) < 1 || len(args) > 2 {
		c.Error(pos, token.ArityMismatch, "randrange() takes 1 or 2 arguments (%d given)", len(args))
		for _, a := range args {
			c.Check(a)
		}
		return types.Invalid
	}
	for _, a := range args {
		t := c.Check(a)
		if types.IsDynamic(t) {
			return types.Any
		}
		if !types.Equal(t, types.Int) {
			c.Error(a.Pos(), token.TypeMismatch, "randrange() arguments must be int")
			return types.Invalid
		}
	}
	return types.Int
}

// checkUniform type-checks uniform(a, b): 2 float/int args, returns float.
func checkUniform(c module.Checker, args []ast.Expr, pos token.Pos) types.Type {
	if len(args) != 2 {
		c.Error(pos, token.ArityMismatch, "uniform() takes exactly 2 arguments (%d given)", len(args))
		for _, a := range args {
			c.Check(a)
		}
		return types.Invalid
	}
	t0 := c.Check(args[0])
	t1 := c.Check(args[1])
	if types.IsDynamic(t0) || types.IsDynamic(t1) {
		return types.Any
	}
	if !isNumeric(t0) {
		c.Error(args[0].Pos(), token.TypeMismatch, "uniform() arguments must be int or float")
		return types.Invalid
	}
	if !isNumeric(t1) {
		c.Error(args[1].Pos(), token.TypeMismatch, "uniform() arguments must be int or float")
		return types.Invalid
	}
	return types.Float
}

// checkChoice type-checks choice(xs): 1 list arg, returns element type.
func checkChoice(c module.Checker, args []ast.Expr, pos token.Pos) types.Type {
	if len(args) != 1 {
		c.Error(pos, token.ArityMismatch, "choice() takes exactly 1 argument (%d given)", len(args))
		for _, a := range args {
			c.Check(a)
		}
		return types.Invalid
	}
	t := c.Check(args[0])
	if types.IsDynamic(t) {
		return types.Any
	}
	list, ok := t.(*types.List)
	if !ok {
		c.Error(args[0].Pos(), token.TypeMismatch, "choice() argument must be a list")
		return types.Invalid
	}
	if types.IsDynamic(list.Elem) {
		return types.Any
	}
	return list.Elem
}

// checkShuffle type-checks shuffle(xs): 1 list arg, returns None.
func checkShuffle(c module.Checker, args []ast.Expr, pos token.Pos) types.Type {
	if len(args) != 1 {
		c.Error(pos, token.ArityMismatch, "shuffle() takes exactly 1 argument (%d given)", len(args))
		for _, a := range args {
			c.Check(a)
		}
		return types.Invalid
	}
	t := c.Check(args[0])
	if types.IsDynamic(t) {
		return types.None
	}
	if _, ok := t.(*types.List); !ok {
		c.Error(args[0].Pos(), token.TypeMismatch, "shuffle() argument must be a list")
		return types.Invalid
	}
	return types.None
}

// checkSeed type-checks seed(n): 1 int arg, returns None.
func checkSeed(c module.Checker, args []ast.Expr, pos token.Pos) types.Type {
	if len(args) != 1 {
		c.Error(pos, token.ArityMismatch, "seed() takes exactly 1 argument (%d given)", len(args))
		for _, a := range args {
			c.Check(a)
		}
		return types.Invalid
	}
	t := c.Check(args[0])
	if types.IsDynamic(t) {
		return types.None
	}
	if !types.Equal(t, types.Int) {
		c.Error(args[0].Pos(), token.TypeMismatch, "seed() argument must be int")
		return types.Invalid
	}
	return types.None
}

// --- Emit functions (callers) ---

// emitRandomFn emits random() call using a pre-allocated host function.
func emitRandomFn(host *interp.HostFunction) module.EmitFunc {
	return func(e module.Emitter, args []ast.Expr) {
		e.CallHost(host)
	}
}

// emitRandintFn emits randint(a, b) call using a pre-allocated host function.
// Handles dynamic types by dispatching to a host function that unboxes refs.
func emitRandintFn(host *interp.HostFunction) module.EmitFunc {
	return func(e module.Emitter, args []ast.Expr) {
		t0 := e.Type(args[0])
		t1 := e.Type(args[1])
		if types.IsDynamic(t0) || types.IsDynamic(t1) {
			e.Expr(args[0])
			e.Expr(args[1])
			e.CallHost(dynRandintHost(t0, t1))
			return
		}
		e.Expr(args[0])
		e.Expr(args[1])
		e.CallHost(host)
	}
}

// emitRandrangeFn emits randrange(stop) or randrange(start, stop) call using
// pre-allocated host functions. Handles dynamic types with runtime dispatch.
func emitRandrangeFn(oneHost, twoHost *interp.HostFunction) module.EmitFunc {
	return func(e module.Emitter, args []ast.Expr) {
		if len(args) == 1 {
			t0 := e.Type(args[0])
			if types.IsDynamic(t0) {
				e.Expr(args[0])
				e.CallHost(dynRandrangeOneHost())
				return
			}
			e.Expr(args[0])
			e.CallHost(oneHost)
		} else {
			t0 := e.Type(args[0])
			t1 := e.Type(args[1])
			if types.IsDynamic(t0) || types.IsDynamic(t1) {
				e.Expr(args[0])
				e.Expr(args[1])
				e.CallHost(dynRandrangeTwoHost(t0, t1))
				return
			}
			e.Expr(args[0])
			e.Expr(args[1])
			e.CallHost(twoHost)
		}
	}
}

// emitUniformFn emits uniform(a, b) call with int-to-float promotion using a
// pre-allocated host function. Handles dynamic types with runtime dispatch.
func emitUniformFn(host *interp.HostFunction) module.EmitFunc {
	return func(e module.Emitter, args []ast.Expr) {
		t0 := e.Type(args[0])
		t1 := e.Type(args[1])
		if types.IsDynamic(t0) || types.IsDynamic(t1) {
			e.Expr(args[0])
			e.Expr(args[1])
			e.CallHost(dynUniformHost(t0, t1))
			return
		}
		e.Expr(args[0])
		if types.Equal(t0, types.Int) {
			e.Emit(instr.I64_TO_F64_S)
		}
		e.Expr(args[1])
		if types.Equal(t1, types.Int) {
			e.Emit(instr.I64_TO_F64_S)
		}
		e.CallHost(host)
	}
}

// emitChoice emits choice(xs) call.
func emitChoice(e module.Emitter, args []ast.Expr) {
	e.Expr(args[0])
	t := e.Type(args[0])
	e.CallHost(choiceHost(t))
}

// emitShuffle emits shuffle(xs) call (void: returns None).
func emitShuffle(e module.Emitter, args []ast.Expr) {
	e.Expr(args[0])
	t := e.Type(args[0])
	e.CallHostVoid(shuffleHost(t))
}

// emitSeedFn emits seed(n) call (void: returns None) using a pre-allocated
// host function. Handles dynamic types with runtime dispatch.
func emitSeedFn(host *interp.HostFunction) module.EmitFunc {
	return func(e module.Emitter, args []ast.Expr) {
		t := e.Type(args[0])
		if types.IsDynamic(t) {
			e.Expr(args[0])
			e.CallHostVoid(dynSeedHost())
			return
		}
		e.Expr(args[0])
		e.CallHostVoid(host)
	}
}

// --- Host functions (callees) ---

func randomHost() *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{}, Returns: []vmtypes.Type{vmtypes.TypeF64}},
		func(_ *interp.Interpreter, _ []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			rngMu.Lock()
			v := rng.Float64()
			rngMu.Unlock()
			return []vmtypes.Boxed{vmtypes.BoxF64(v)}, nil
		},
	)
}

func randintHost() *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{vmtypes.TypeI64, vmtypes.TypeI64}, Returns: []vmtypes.Type{vmtypes.TypeI64}},
		func(_ *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			a := params[0].I64()
			b := params[1].I64()
			if b < a {
				return nil, ErrEmptyRange
			}
			rngMu.Lock()
			// IntN gives [0, n), so we need [a, b] inclusive
			v := a + rng.Int64N(b-a+1)
			rngMu.Unlock()
			return []vmtypes.Boxed{vmtypes.BoxI64(v)}, nil
		},
	)
}

func randrangeOneHost() *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{vmtypes.TypeI64}, Returns: []vmtypes.Type{vmtypes.TypeI64}},
		func(_ *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			stop := params[0].I64()
			if stop <= 0 {
				return nil, ErrEmptyRange
			}
			rngMu.Lock()
			v := rng.Int64N(stop)
			rngMu.Unlock()
			return []vmtypes.Boxed{vmtypes.BoxI64(v)}, nil
		},
	)
}

func randrangeTwoHost() *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{vmtypes.TypeI64, vmtypes.TypeI64}, Returns: []vmtypes.Type{vmtypes.TypeI64}},
		func(_ *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			start := params[0].I64()
			stop := params[1].I64()
			if stop <= start {
				return nil, ErrEmptyRange
			}
			rngMu.Lock()
			v := start + rng.Int64N(stop-start)
			rngMu.Unlock()
			return []vmtypes.Boxed{vmtypes.BoxI64(v)}, nil
		},
	)
}

func uniformHost() *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{vmtypes.TypeF64, vmtypes.TypeF64}, Returns: []vmtypes.Type{vmtypes.TypeF64}},
		func(_ *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			a := params[0].F64()
			b := params[1].F64()
			rngMu.Lock()
			v := a + rng.Float64()*(b-a)
			rngMu.Unlock()
			return []vmtypes.Boxed{vmtypes.BoxF64(v)}, nil
		},
	)
}

func choiceHost(arg types.Type) *interp.HostFunction {
	list, ok := arg.(*types.List)
	var retType vmtypes.Type
	if ok {
		retType = list.Elem.VM()
	} else {
		retType = vmtypes.TypeAny
	}
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{arg.VM()}, Returns: []vmtypes.Type{retType}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			_, elems, err := hostabi.ArrayElems(i, params[0])
			if err != nil {
				return nil, err
			}
			if len(elems) == 0 {
				return nil, ErrEmptySequence
			}
			rngMu.Lock()
			idx := rng.IntN(len(elems))
			rngMu.Unlock()
			return []vmtypes.Boxed{elems[idx]}, nil
		},
	)
}

func shuffleHost(arg types.Type) *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{arg.VM()}, Returns: []vmtypes.Type{}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			if params[0].Kind() != vmtypes.KindRef || params[0].Ref() == 0 {
				return nil, interp.ErrTypeMismatch
			}
			val, err := i.Load(params[0].Ref())
			if err != nil {
				return nil, err
			}
			rngMu.Lock()
			defer rngMu.Unlock()
			switch a := val.(type) {
			case *vmtypes.Array:
				rng.Shuffle(len(a.Elems), func(x, y int) {
					a.Elems[x], a.Elems[y] = a.Elems[y], a.Elems[x]
				})
			case vmtypes.TypedArray[int64]:
				rng.Shuffle(len(a), func(x, y int) {
					a[x], a[y] = a[y], a[x]
				})
			case vmtypes.TypedArray[float64]:
				rng.Shuffle(len(a), func(x, y int) {
					a[x], a[y] = a[y], a[x]
				})
			case vmtypes.TypedArray[int32]:
				rng.Shuffle(len(a), func(x, y int) {
					a[x], a[y] = a[y], a[x]
				})
			case vmtypes.TypedArray[float32]:
				rng.Shuffle(len(a), func(x, y int) {
					a[x], a[y] = a[y], a[x]
				})
			case vmtypes.TypedArray[int8]:
				rng.Shuffle(len(a), func(x, y int) {
					a[x], a[y] = a[y], a[x]
				})
			case vmtypes.TypedArray[bool]:
				rng.Shuffle(len(a), func(x, y int) {
					a[x], a[y] = a[y], a[x]
				})
			default:
				return nil, interp.ErrTypeMismatch
			}
			return nil, nil
		},
	)
}

func seedHost() *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{vmtypes.TypeI64}, Returns: []vmtypes.Type{}},
		func(_ *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			n := params[0].I64()
			rngMu.Lock()
			rng = rand.New(rand.NewPCG(uint64(n), 0))
			rngMu.Unlock()
			return nil, nil
		},
	)
}

// --- Dynamic dispatch host functions ---
// These accept vmtypes.TypeAny parameters for dynamic-typed arguments, unbox
// the value at runtime, and perform the random operation.

func dynUniformHost(t0, t1 types.Type) *interp.HostFunction {
	p0 := hostabi.VMParamType(t0)
	p1 := hostabi.VMParamType(t1)
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{p0, p1}, Returns: []vmtypes.Type{vmtypes.TypeF64}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			a, err := hostabi.UnboxFloat(i, params[0])
			if err != nil {
				return nil, err
			}
			b, err := hostabi.UnboxFloat(i, params[1])
			if err != nil {
				return nil, err
			}
			rngMu.Lock()
			v := a + rng.Float64()*(b-a)
			rngMu.Unlock()
			return []vmtypes.Boxed{vmtypes.BoxF64(v)}, nil
		},
	)
}

func dynRandintHost(t0, t1 types.Type) *interp.HostFunction {
	p0 := hostabi.VMParamType(t0)
	p1 := hostabi.VMParamType(t1)
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{p0, p1}, Returns: []vmtypes.Type{vmtypes.TypeI64}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			a, err := hostabi.UnboxInt(i, params[0])
			if err != nil {
				return nil, err
			}
			b, err := hostabi.UnboxInt(i, params[1])
			if err != nil {
				return nil, err
			}
			if b < a {
				return nil, ErrEmptyRange
			}
			rngMu.Lock()
			v := a + rng.Int64N(b-a+1)
			rngMu.Unlock()
			return []vmtypes.Boxed{vmtypes.BoxI64(v)}, nil
		},
	)
}

func dynRandrangeOneHost() *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{vmtypes.TypeAny}, Returns: []vmtypes.Type{vmtypes.TypeI64}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			stop, err := hostabi.UnboxInt(i, params[0])
			if err != nil {
				return nil, err
			}
			if stop <= 0 {
				return nil, ErrEmptyRange
			}
			rngMu.Lock()
			v := rng.Int64N(stop)
			rngMu.Unlock()
			return []vmtypes.Boxed{vmtypes.BoxI64(v)}, nil
		},
	)
}

func dynRandrangeTwoHost(t0, t1 types.Type) *interp.HostFunction {
	p0 := hostabi.VMParamType(t0)
	p1 := hostabi.VMParamType(t1)
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{p0, p1}, Returns: []vmtypes.Type{vmtypes.TypeI64}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			start, err := hostabi.UnboxInt(i, params[0])
			if err != nil {
				return nil, err
			}
			stop, err := hostabi.UnboxInt(i, params[1])
			if err != nil {
				return nil, err
			}
			if stop <= start {
				return nil, ErrEmptyRange
			}
			rngMu.Lock()
			v := start + rng.Int64N(stop-start)
			rngMu.Unlock()
			return []vmtypes.Boxed{vmtypes.BoxI64(v)}, nil
		},
	)
}

func dynSeedHost() *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{vmtypes.TypeAny}, Returns: []vmtypes.Type{}},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			n, err := hostabi.UnboxInt(i, params[0])
			if err != nil {
				return nil, err
			}
			rngMu.Lock()
			rng = rand.New(rand.NewPCG(uint64(n), 0))
			rngMu.Unlock()
			return nil, nil
		},
	)
}

// --- Helpers ---

func isNumeric(t types.Type) bool {
	return types.Equal(t, types.Int) || types.Equal(t, types.Float)
}
