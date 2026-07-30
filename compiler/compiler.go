// Package compiler turns minipy source into verified minivm programs.
package compiler

import (
	"errors"
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

// Option configures a Compiler.
type Option func(*config)

// Compiler owns reusable compilation policy and infrastructure. Mutable source,
// diagnostics, module graph, checker state, and lowerer state belong to the
// fresh compilation created by each Compile call.
type Compiler struct {
	config config
}

type config struct {
	out   io.Writer
	level optimize.Level
	paths []searchEntry
	reg   *module.Registry
}

type compilation struct {
	source io.Reader
	config config
}

// ErrNoSource reports a compile call without a source reader.
var ErrNoSource = errors.New("compiler: no source reader")

// WithOutput binds the sink the compiled program's print writes to. It defaults
// to os.Stdout.
func WithOutput(writer io.Writer) Option {
	return func(config *config) { config.out = writer }
}

// WithOptimizationLevel selects the minivm optimizer pipeline used after
// lowering. It defaults to optimize.O0.
func WithOptimizationLevel(level optimize.Level) Option {
	return func(config *config) { config.level = level }
}

// WithModules adds one sys.path-style module search root.
func WithModules(files fs.FS) Option {
	return func(config *config) {
		config.paths = append(config.paths, searchEntry{fsys: files, dir: "."})
	}
}

// WithNativeModules adds native modules to the default registry. The builtins,
// operator, and typing modules remain registered; duplicate module names panic
// as an invalid startup catalogue.
func WithNativeModules(modules ...module.Module) Option {
	return func(config *config) {
		registered := append(config.reg.Modules(), modules...)
		config.reg = module.NewRegistry(registered, module.WithFallback(config.reg.FallbackName()))
	}
}

// WithModulePath adds directories inside files as ordered module search roots.
func WithModulePath(files fs.FS, directories ...string) Option {
	return func(config *config) {
		if len(directories) == 0 {
			directories = []string{"."}
		}
		for _, directory := range directories {
			config.paths = append(config.paths, searchEntry{fsys: files, dir: cleanDir(directory)})
		}
	}
}

// Compile parses, checks, lowers, optimizes, and verifies source using a compiler
// configured by options.
func Compile(source io.Reader, options ...Option) (*program.Program, error) {
	return New(options...).Compile(source)
}

// New returns a reusable Compiler configured by options.
func New(options ...Option) *Compiler {
	config := defaultConfig()
	for _, option := range options {
		option(&config)
	}
	return &Compiler{config: config}
}

// Compile compiles source in a fresh session. A failed invocation cannot affect
// later calls on the same Compiler.
func (c *Compiler) Compile(source io.Reader) (*program.Program, error) {
	if source == nil {
		return nil, ErrNoSource
	}
	return (&compilation{source: source, config: c.config}).compile()
}

func (c *compilation) compile() (*program.Program, error) {
	module, parseErr := parser.Parse(c.source)

	checked, err := c.check(module, parseErr)
	if err != nil {
		return nil, err
	}

	lowered, err := c.lower(checked)
	if err != nil {
		return nil, err
	}

	optimized, err := c.optimize(lowered)
	if err != nil {
		return nil, err
	}
	if err := program.Verify(optimized); err != nil {
		return nil, fmt.Errorf("verify program: %w", err)
	}
	return optimized, nil
}

func (c *compilation) check(module *ast.Module, parseErr error) (*checkedProgram, error) {
	var diagnostics token.ErrorList
	switch parseErr := parseErr.(type) {
	case nil:
	case token.ErrorList:
		diagnostics = append(diagnostics, parseErr...)
	default:
		return nil, parseErr
	}

	loader := newLoader(c.config.reg, c.config.paths)
	entry := loader.loadEntry(module)
	checker := newChecker(loader)
	checker.checkProgram(entry)

	diagnostics = append(diagnostics, loader.errs...)
	diagnostics = append(diagnostics, checker.errs...)
	if err := diagnostics.Err(); err != nil {
		return nil, err
	}
	return checker.result(entry), nil
}

func (c *compilation) lower(checked *checkedProgram) (*program.Program, error) {
	runtime := newNativeRuntime(c.config.reg, c.config.out)
	lowerer := newLowerer(program.NewBuilder(), checked, runtime)
	return lowerer.lower()
}

func (c *compilation) optimize(lowered *program.Program) (*program.Program, error) {
	typesPool := append([]vmtypes.Type(nil), lowered.Types...)
	handlers := append([]instr.Handler(nil), lowered.Handlers...)
	globals := append([]vmtypes.Type(nil), lowered.Globals...)

	optimized, err := optimize.NewOptimizer(c.config.level).Optimize(lowered)
	if err != nil {
		return nil, fmt.Errorf("optimize program: %w", err)
	}
	optimized.Types = typesPool
	optimized.Handlers = handlers
	optimized.Globals = globals
	return optimized, nil
}

func defaultConfig() config {
	return config{out: os.Stdout, level: optimize.O0, reg: defaultRegistry()}
}
