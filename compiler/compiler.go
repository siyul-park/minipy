// Package compiler turns minipy source into a runnable minivm program for the
// M0 subset (docs/spec): it parses, type-checks, and lowers a module of
// top-level scalar statements. Compile returns a *program.Program; run it with
// minivm's interp.New(prog).Run(ctx).
package compiler

import (
	"fmt"
	"io"
	"os"

	"github.com/siyul-park/minipy/parser"
	"github.com/siyul-park/minipy/token"

	"github.com/siyul-park/minivm/optimize"
	"github.com/siyul-park/minivm/program"
)

// config holds compile-time options.
type config struct {
	out io.Writer
}

// Option configures a Compile call.
type Option func(*config)

// WithOutput binds the sink the compiled program's `print` writes to. It
// defaults to os.Stdout; tests and the REPL pass their own writer.
func WithOutput(w io.Writer) Option {
	return func(c *config) { c.out = w }
}

// Compile reads minipy source from r, type-checks it, and lowers it into a
// minivm program. On any lexical, syntactic, or semantic error it returns a
// token.ErrorList describing every diagnostic found and a nil program.
func Compile(r io.Reader, opts ...Option) (*program.Program, error) {
	cfg := &config{out: os.Stdout}
	for _, opt := range opts {
		opt(cfg)
	}

	mod, parseErr := parser.Parse(r)

	chk := newChecker()
	chk.check(mod)

	var errs token.ErrorList
	if pl, ok := parseErr.(token.ErrorList); ok {
		errs = append(errs, pl...)
	}
	errs = append(errs, chk.errs...)
	if err := errs.Err(); err != nil {
		return nil, err
	}

	host := newHostFuncs(cfg.out)
	b := program.NewBuilder()
	newEmitter(b, chk.exprType, chk.globals, host).module(mod)

	prog, err := b.Build()
	if err != nil {
		return nil, fmt.Errorf("assemble program: %w", err)
	}

	optimized, err := optimize.NewOptimizer(optimize.O1).Optimize(prog)
	if err != nil {
		return nil, fmt.Errorf("optimize program: %w", err)
	}
	return optimized, nil
}
