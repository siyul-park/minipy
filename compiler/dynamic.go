package compiler

import (
	"bytes"
	"fmt"

	"github.com/siyul-park/minipy/ast"
	"github.com/siyul-park/minipy/hostabi"
	"github.com/siyul-park/minipy/module"
	"github.com/siyul-park/minipy/parser"
	"github.com/siyul-park/minipy/token"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/interp"
	"github.com/siyul-park/minivm/program"
	vmtypes "github.com/siyul-park/minivm/types"
)

type compiledCode struct {
	filename string
	mode     string
	fn       *vmtypes.Function
	consts   []vmtypes.Value
}

var _ module.Code = (*compiledCode)(nil)

func (c *compiledCode) Kind() vmtypes.Kind          { return vmtypes.KindRef }
func (c *compiledCode) Type() vmtypes.Type          { return vmtypes.TypeRef }
func (c *compiledCode) Mode() string                { return c.mode }
func (c *compiledCode) Function() *vmtypes.Function { return c.fn }
func (c *compiledCode) Constants() []vmtypes.Value  { return c.consts }
func (c *compiledCode) String() string {
	return fmt.Sprintf("<code object %s, file %q>", c.mode, c.filename)
}

func (c config) compileCode(source, filename, mode string) (module.Code, error) {
	if mode != "eval" && mode != "exec" {
		return nil, fmt.Errorf("compile() mode must be 'eval' or 'exec'")
	}
	mod, parseErr := parser.Parse(bytes.NewBufferString(source))
	if mode == "eval" && parseErr == nil {
		if len(mod.Body) != 1 {
			return nil, fmt.Errorf("eval() source must contain one expression")
		}
		if _, ok := mod.Body[0].(*ast.ExprStmt); !ok {
			return nil, fmt.Errorf("eval() source must be an expression")
		}
	}
	checked, err := c.checkDynamic(mod, parseErr)
	if err != nil {
		return nil, err
	}

	fb := vmtypes.NewFunctionBuilder(&vmtypes.FunctionType{Returns: []vmtypes.Type{vmtypes.TypeRef}})
	low := newLowerer(program.NewBuilder(), checked, newNativeRuntime(c))
	low.dynamic = true
	low.code = fnTarget(fb)
	if err := low.lowerCode(mode); err != nil {
		return nil, err
	}
	fb.WithLocals(low.scratch...)
	fb.WithCaptures(codeCaptureTypes(low.consts)...)
	fn, err := fb.Build()
	if err != nil {
		return nil, fmt.Errorf("build dynamic code: %w", err)
	}
	pools, err := low.prog.Build()
	if err != nil {
		return nil, fmt.Errorf("build dynamic pools: %w", err)
	}
	if len(pools.Types) != 0 {
		return nil, fmt.Errorf("dynamic code requiring runtime type metadata is not supported")
	}
	functions := []vmtypes.Value{fn}
	for _, constant := range low.consts {
		if _, ok := constant.(*vmtypes.Function); ok {
			functions = append(functions, constant)
		}
	}
	if err := program.Verify(&program.Program{Constants: functions}); err != nil {
		return nil, fmt.Errorf("verify dynamic code: %w", err)
	}
	return &compiledCode{
		filename: filename,
		mode:     mode,
		fn:       fn,
		consts:   append([]vmtypes.Value(nil), low.consts...),
	}, nil
}

func (c config) checkDynamic(mod *ast.Module, parseErr error) (*checkedProgram, error) {
	ld := newLoader(c.reg, c.paths)
	entry := ld.loadEntry(mod)
	chk := newChecker(ld)
	chk.dynamic = true
	chk.checkProgram(entry)
	var errs token.ErrorList
	if list, ok := parseErr.(token.ErrorList); ok {
		errs = append(errs, list...)
	}
	errs = append(errs, ld.errs...)
	errs = append(errs, chk.errs...)
	if err := errs.Err(); err != nil {
		return nil, err
	}
	return chk.result(entry), nil
}

func (c *lowerer) lowerCode(mode string) error {
	if mode == "eval" {
		c.mod = c.entry
		expr := c.entry.ast.Body[0].(*ast.ExprStmt).X
		// The result slot is dynamic, so the value is returned in its
		// self-describing boxed form without an extra widening step.
		c.expr(expr)
		c.emit(instr.RETURN)
	} else {
		c.module(c.entry)
		c.emit(instr.REF_NULL)
		c.emit(instr.RETURN)
	}
	return c.err
}

func codeCaptureTypes(constants []vmtypes.Value) []vmtypes.Type {
	captures := make([]vmtypes.Type, 0, 2+len(constants))
	captures = append(captures, vmtypes.TypeRef, vmtypes.TypeRef)
	for _, constant := range constants {
		captures = append(captures, constant.Type())
	}
	return captures
}

func (c *lowerer) loadName(name string) {
	found := c.label()
	if c.current == nil {
		c.lookupName(1, name)
		c.brIf(found)
		c.emit(instr.DROP)
	}
	c.lookupName(0, name)
	c.brIf(found)
	c.emit(instr.DROP)
	c.constGet(vmtypes.String(name))
	c.callHost(dynamicNameHost())
	c.bind(found)
}

func (c *lowerer) lookupName(namespace int, name string) {
	c.emit(instr.UPVAL_GET, uint64(namespace))
	c.constGet(vmtypes.String(name))
	c.emit(instr.MAP_LOOKUP)
}

func (c *lowerer) storeName(name string) {
	namespace := 1
	if c.current != nil {
		namespace = 0
	}
	c.emit(instr.UPVAL_GET, uint64(namespace))
	c.emit(instr.SWAP)
	c.constGet(vmtypes.String(name))
	c.emit(instr.SWAP)
	c.emit(instr.MAP_SET)
}

func dynamicNameHost() *interp.HostFunction {
	return interp.NewHostFunction(
		&vmtypes.FunctionType{Params: []vmtypes.Type{vmtypes.TypeString}, Returns: []vmtypes.Type{vmtypes.TypeRef}},
		func(i *interp.Interpreter, p []vmtypes.Boxed) ([]vmtypes.Boxed, error) {
			name, err := hostabi.LoadStr(i, p[0])
			if err != nil {
				return nil, err
			}
			return nil, fmt.Errorf("name %q is not defined", name)
		},
	)
}
