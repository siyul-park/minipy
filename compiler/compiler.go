// Package compiler turns minipy source into a runnable minivm program for the
// supported subset (docs/spec): it parses, type-checks, and lowers a module of
// scalar statements, control flow, and functions. Compile returns a
// *program.Program; run it with minivm's interp.New(prog).Run(ctx).
package compiler

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"

	"github.com/siyul-park/minipy/ast"
	"github.com/siyul-park/minipy/module"
	"github.com/siyul-park/minipy/parser"
	"github.com/siyul-park/minipy/token"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/optimize"
	"github.com/siyul-park/minivm/program"
	vmtypes "github.com/siyul-park/minivm/types"
)

// Option configures a Compile call.
type Option func(*config)

// Compiler turns minipy source into a runnable minivm program. It mirrors the
// package-level Compile convenience function while keeping options reusable for
// one source stream. Compiler itself only orchestrates the pipeline (parse,
// check, lower, optimize, verify); type-checking state lives in checker and
// lowering state lives in lowerer, both created fresh per Compile call.
type Compiler struct {
	src    []byte
	err    error
	config config
}

// config holds compile-time options.
type config struct {
	out   io.Writer
	level optimize.Level
	paths []searchEntry
	reg   *module.Registry
}

// WithOutput binds the sink the compiled program's `print` writes to. It
// defaults to os.Stdout; tests and the REPL pass their own writer.
func WithOutput(w io.Writer) Option {
	return func(c *config) { c.out = w }
}

// WithOptimizationLevel selects the minivm optimizer pipeline used after
// lowering. It defaults to optimize.O0.
func WithOptimizationLevel(level optimize.Level) Option {
	return func(c *config) { c.level = level }
}

// WithModules adds one sys.path-style module search root.
func WithModules(fsys fs.FS) Option {
	return func(c *config) { c.paths = append(c.paths, searchEntry{fsys: fsys, dir: "."}) }
}

// WithNativeModules adds native modules to the default registry. The builtins,
// operator, and typing modules remain registered; duplicate module names panic
// as a configuration error.
func WithNativeModules(modules ...module.Module) Option {
	return func(c *config) {
		registered := append(c.reg.Modules(), modules...)
		c.reg = module.NewRegistry(registered, module.WithFallback(c.reg.FallbackName()))
	}
}

// WithModulePath adds directories inside fsys as ordered module search roots.
func WithModulePath(fsys fs.FS, dirs ...string) Option {
	return func(c *config) {
		if len(dirs) == 0 {
			dirs = []string{"."}
		}
		for _, dir := range dirs {
			c.paths = append(c.paths, searchEntry{fsys: fsys, dir: cleanDir(dir)})
		}
	}
}

// New returns a Compiler over source read from r.
func New(r io.Reader, opts ...Option) *Compiler {
	config := defaultConfig()
	for _, opt := range opts {
		opt(&config)
	}
	src, err := io.ReadAll(r)
	if err != nil {
		// Keep constructor parser-like and error-free; Compile reports the read
		// failure as a regular error.
		src = []byte{}
	}
	return &Compiler{src: src, err: err, config: config}
}

// Compile reads minipy source from r, type-checks it, and lowers it into a
// minivm program. On any lexical, syntactic, or semantic error it returns a
// token.ErrorList describing every diagnostic found and a nil program.
func Compile(r io.Reader, opts ...Option) (*program.Program, error) {
	return New(r, opts...).Compile()
}

// Compile parses, type-checks, lowers, optimizes, and verifies c's source.
func (c *Compiler) Compile() (*program.Program, error) {
	if c.err != nil {
		return nil, fmt.Errorf("read source: %w", c.err)
	}
	mod, parseErr := parser.Parse(bytes.NewReader(c.src))

	checked, err := c.check(mod, parseErr)
	if err != nil {
		return nil, err
	}

	native := newNativeRuntime(c.config.reg, c.config.out)
	low := newLowerer(program.NewBuilder(), checked, native)
	prog, err := low.lower()
	if err != nil {
		return nil, err
	}

	typesPool := append([]vmtypes.Type(nil), prog.Types...)
	handlers := append([]instr.Handler(nil), prog.Handlers...)
	globals := append([]vmtypes.Type(nil), prog.Globals...)
	optimized, err := optimize.NewOptimizer(c.config.level).Optimize(prog)
	if err != nil {
		return nil, fmt.Errorf("optimize program: %w", err)
	}
	optimized.Types = typesPool
	optimized.Handlers = handlers
	optimized.Globals = globals
	if err := program.Verify(optimized); err != nil {
		return nil, fmt.Errorf("verify program: %w", err)
	}
	return optimized, nil
}

func defaultConfig() config {
	return config{out: os.Stdout, level: optimize.O0, reg: defaultRegistry()}
}

// check loads and type-checks mod (merging any parser diagnostics), returning
// the entry module and checker on success or a token.ErrorList describing
// every parse, load, and type error found.
func (c *Compiler) check(mod *ast.Module, parseErr error) (*checkedProgram, error) {
	ld := newLoader(c.config.reg, c.config.paths)
	entry := ld.loadEntry(mod)
	chk := newChecker(ld)
	chk.checkProgram(entry)

	var errs token.ErrorList
	if pl, ok := parseErr.(token.ErrorList); ok {
		errs = append(errs, pl...)
	}
	errs = append(errs, ld.errs...)
	errs = append(errs, chk.errs...)
	if err := errs.Err(); err != nil {
		return nil, err
	}
	return chk.result(entry), nil
}
