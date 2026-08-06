package builtins

import (
	"fmt"
	"slices"

	"github.com/siyul-park/minipy/ast"
	"github.com/siyul-park/minipy/hostabi"
	"github.com/siyul-park/minipy/module"
	"github.com/siyul-park/minipy/token"
	"github.com/siyul-park/minipy/types"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/interp"
	vmtypes "github.com/siyul-park/minivm/types"
)

func dynamicSymbols() []module.Symbol {
	return []module.Symbol{
		module.NewSymbol("compile", compileCheck, emitCompile, compileValue),
		module.NewSymbol("eval", evalCheck, emitEval, dynamicValue("eval")),
		module.NewSymbol("exec", execCheck, emitExec, dynamicValue("exec")),
	}
}

func compileCheck(c module.Checker, args []ast.Expr, pos token.Pos) types.Type {
	if len(args) != 3 {
		c.Error(pos, token.ArityMismatch, "compile() takes exactly 3 arguments (%d given)", len(args))
		return types.Invalid
	}
	for _, arg := range args {
		if t := c.Check(arg); t != types.Invalid && !types.Equal(t, types.Str) {
			c.Error(arg.Pos(), token.TypeMismatch, "compile() arguments must be str")
		}
	}
	return types.Code
}

func evalCheck(c module.Checker, args []ast.Expr, pos token.Pos) types.Type {
	return dynamicCheck(c, "eval", args, pos, types.Any)
}

func execCheck(c module.Checker, args []ast.Expr, pos token.Pos) types.Type {
	return dynamicCheck(c, "exec", args, pos, types.None)
}

func dynamicCheck(c module.Checker, name string, args []ast.Expr, pos token.Pos, result types.Type) types.Type {
	if len(args) < 1 || len(args) > 3 {
		c.Error(pos, token.ArityMismatch, "%s() takes 1 to 3 arguments (%d given)", name, len(args))
		return types.Invalid
	}
	source := c.Check(args[0])
	if source != types.Invalid && !types.Equal(source, types.Str) && !types.Equal(source, types.Code) {
		c.Error(args[0].Pos(), token.TypeMismatch, "%s() source must be str or code", name)
	}
	for _, arg := range args[1:] {
		t := c.Check(arg)
		dict, ok := t.(*types.Dict)
		if t != types.Invalid && (!ok || !types.Equal(dict.Key, types.Str) || !types.IsAny(dict.Value)) {
			c.Error(arg.Pos(), token.TypeMismatch, "%s() namespace must be dict[str, Any]", name)
		}
	}
	return result
}

func emitCompile(e module.Emitter, args []ast.Expr) {
	for _, arg := range args {
		e.Expr(arg)
	}
	e.CallHost(e.Host(Name, "compile"))
}

func emitEval(e module.Emitter, args []ast.Expr) { emitDynamic(e, args, "eval") }
func emitExec(e module.Emitter, args []ast.Expr) { emitDynamic(e, args, "exec") }

func emitDynamic(e module.Emitter, args []ast.Expr, name string) {
	e.Expr(args[0])
	for i := 1; i < 3; i++ {
		if i < len(args) {
			e.Expr(args[i])
		} else {
			e.Emit(instr.REF_NULL)
		}
	}
	e.CallHost(e.Host(Name, name))
	e.Emit(instr.CALL)
}

func compileValue(r module.Runtime) vmtypes.Value {
	compiler, ok := r.(module.Compiler)
	if !ok {
		return nil
	}
	return compileHost(compiler)
}

func dynamicValue(mode string) module.ValueFunc {
	return func(r module.Runtime) vmtypes.Value {
		compiler, ok := r.(module.Compiler)
		if !ok {
			return nil
		}
		return dynamicHost(compiler, mode)
	}
}

func compileHost(compiler module.Compiler) *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{
			Params:  []vmtypes.Type{vmtypes.TypeString, vmtypes.TypeString, vmtypes.TypeString},
			Returns: []vmtypes.Type{vmtypes.TypeRef},
		},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			source, err := hostabi.LoadStr(i, params[0])
			if err != nil {
				return nil, err
			}
			filename, err := hostabi.LoadStr(i, params[1])
			if err != nil {
				return nil, err
			}
			mode, err := hostabi.LoadStr(i, params[2])
			if err != nil {
				return nil, err
			}
			code, err := compiler.Compile(source, filename, mode)
			if err != nil {
				return nil, err
			}
			addr, err := i.Alloc(code)
			if err != nil {
				return nil, err
			}
			return []vmtypes.Boxed{vmtypes.BoxRef(addr)}, nil
		},
	)
}

func dynamicHost(compiler module.Compiler, mode string) *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{
			Params:  []vmtypes.Type{vmtypes.TypeRef, vmtypes.TypeRef, vmtypes.TypeRef},
			Returns: []vmtypes.Type{vmtypes.TypeRef},
		},
		func(i *interp.Interpreter, params []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			code, err := loadCode(i, compiler, params[0], mode)
			if err != nil {
				return nil, err
			}
			globals, locals, owned, err := namespaces(i, params[1], params[2])
			if err != nil {
				return nil, err
			}
			captures, err := codeCaptures(i, code, globals, locals, owned)
			if err != nil {
				return nil, err
			}
			fn := code.Function()
			fnAddr, err := i.Alloc(fn)
			if err != nil {
				return nil, err
			}
			closureAddr, err := i.Alloc(vmtypes.NewClosure(fn.Typ, vmtypes.Ref(fnAddr), captures))
			if err != nil {
				return nil, err
			}
			return []vmtypes.Boxed{vmtypes.BoxRef(closureAddr)}, nil
		},
	)
}

func loadCode(i *interp.Interpreter, compiler module.Compiler, value vmtypes.Boxed, mode string) (module.Code, error) {
	if value.Kind() == vmtypes.KindRef {
		if loaded, err := i.Load(value.Ref()); err == nil {
			if code, ok := loaded.(module.Code); ok {
				if code.Mode() != mode {
					return nil, fmt.Errorf("code object mode %q cannot be used with %s()", code.Mode(), mode)
				}
				return code, nil
			}
		}
	}
	source, err := hostabi.LoadStr(i, value)
	if err != nil {
		return nil, err
	}
	return compiler.Compile(source, "<string>", mode)
}

func namespaces(i *interp.Interpreter, globals, locals vmtypes.Boxed) (vmtypes.Boxed, vmtypes.Boxed, bool, error) {
	owned := globals == vmtypes.BoxedNull
	if owned {
		addr, err := i.Alloc(vmtypes.NewMap(vmtypes.NewMapType(vmtypes.TypeString, vmtypes.TypeRef)))
		if err != nil {
			return vmtypes.BoxedNull, vmtypes.BoxedNull, false, err
		}
		globals = vmtypes.BoxRef(addr)
	}
	if locals == vmtypes.BoxedNull {
		locals = globals
	}
	if err := checkNamespace(i, globals); err != nil {
		return vmtypes.BoxedNull, vmtypes.BoxedNull, false, err
	}
	if err := checkNamespace(i, locals); err != nil {
		return vmtypes.BoxedNull, vmtypes.BoxedNull, false, err
	}
	return globals, locals, owned, nil
}

func checkNamespace(i *interp.Interpreter, value vmtypes.Boxed) error {
	loaded, err := i.Load(value.Ref())
	if err != nil {
		return err
	}
	if _, ok := loaded.(*vmtypes.Map); !ok {
		return fmt.Errorf("namespace must be dict[str, Any]")
	}
	return nil
}

func codeCaptures(i *interp.Interpreter, code module.Code, globals, locals vmtypes.Boxed, owned bool) ([]vmtypes.Boxed, error) {
	captures := make([]vmtypes.Boxed, 0, 2+len(code.Constants()))
	if !owned {
		if _, err := i.Retain(globals.Ref()); err != nil {
			return nil, err
		}
	}
	captures = append(captures, globals)
	if _, err := i.Retain(locals.Ref()); err != nil {
		return nil, err
	}
	captures = append(captures, locals)
	for _, constant := range code.Constants() {
		boxed, err := materializeConstant(i, constant)
		if err != nil {
			return nil, err
		}
		captures = append(captures, boxed)
	}
	return captures, nil
}

func materializeConstant(i *interp.Interpreter, constant vmtypes.Value) (vmtypes.Boxed, error) {
	switch value := constant.(type) {
	case *vmtypes.Function:
		clone := *value
		clone.Locals = slices.Clone(value.Locals)
		clone.Captures = slices.Clone(value.Captures)
		clone.Code = slices.Clone(value.Code)
		clone.Handlers = slices.Clone(value.Handlers)
		addr, err := i.Alloc(&clone)
		if err != nil {
			return vmtypes.BoxedNull, err
		}
		return vmtypes.BoxRef(addr), nil
	case *interp.HostFunction:
		clone := *value
		addr, err := i.Alloc(&clone)
		if err != nil {
			return vmtypes.BoxedNull, err
		}
		return vmtypes.BoxRef(addr), nil
	case *vmtypes.Struct:
		clone := vmtypes.NewStruct(value.Typ)
		copy(clone.Data, value.Data)
		addr, err := i.Alloc(clone)
		if err != nil {
			return vmtypes.BoxedNull, err
		}
		return vmtypes.BoxRef(addr), nil
	default:
		if err := i.Push(constant); err != nil {
			return vmtypes.BoxedNull, err
		}
		return i.PopBoxed()
	}
}
