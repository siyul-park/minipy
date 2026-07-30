package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/siyul-park/minipy/ast"
	"github.com/siyul-park/minipy/compiler"
	"github.com/siyul-park/minipy/parser"

	"github.com/siyul-park/minivm/interp"
	"github.com/siyul-park/minivm/optimize"
	"github.com/siyul-park/minivm/program"
)

// runFile compiles and runs a minipy source file, writing program output to out.
func runFile(ctx context.Context, path string, out io.Writer, level optimize.Level, paths []string) (err error) {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, file.Close())
	}()

	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	options := []compiler.Option{
		compiler.WithOutput(out),
		compiler.WithOptimizationLevel(level),
		compiler.WithModules(os.DirFS(filepath.Dir(absolute))),
	}
	pathOptions, err := modulePathOptions(paths)
	if err != nil {
		return err
	}
	options = append(options, pathOptions...)

	compiled, err := compiler.Compile(file, options...)
	if err != nil {
		return err
	}
	return runProgram(ctx, compiled)
}

// repl runs the interactive loop. It persists declarations and assignments as
// session state and runs expressions transiently without repeating prior side
// effects.
func repl(ctx context.Context, in io.Reader, out io.Writer, level optimize.Level, paths []string) error {
	if _, err := fmt.Fprintln(out, "minipy REPL — type Ctrl-D to exit"); err != nil {
		return err
	}

	scanner := bufio.NewScanner(in)
	var state strings.Builder
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, err := fmt.Fprint(out, ">>> "); err != nil {
			return err
		}
		if !scanner.Scan() {
			_, writeErr := fmt.Fprintln(out)
			return errors.Join(scanner.Err(), writeErr)
		}
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		if err := evalLine(ctx, &state, line, out, level, paths); err != nil {
			return err
		}
	}
}

// evalLine classifies one REPL entry and either extends the session state or
// runs a transient program. Language failures are rendered and do not end the
// session; output and process failures are returned.
func evalLine(ctx context.Context, state *strings.Builder, line string, out io.Writer, level optimize.Level, paths []string) error {
	module, err := parser.Parse(strings.NewReader(line))
	if err != nil {
		return report(out, err)
	}
	if len(module.Body) == 0 {
		return nil
	}

	switch statement := module.Body[0].(type) {
	case *ast.AnnAssign, *ast.Assign, *ast.AugAssign, *ast.Import, *ast.ImportFrom:
		candidate := state.String() + line + "\n"
		options, err := replOptions(io.Discard, level, paths)
		if err != nil {
			return err
		}
		if _, err := compiler.Compile(strings.NewReader(candidate), options...); err != nil {
			return report(out, err)
		}
		state.WriteString(line + "\n")
	case *ast.ExprStmt:
		if err := runTransient(ctx, state.String(), line, statement.X, out, level, paths); err != nil {
			return report(out, err)
		}
	}
	return nil
}

// runTransient compiles and runs state plus one expression, auto-wrapping a
// bare expression in str and print so its value is echoed.
func runTransient(ctx context.Context, state, line string, expression ast.Expr, out io.Writer, level optimize.Level, paths []string) error {
	source := state
	if isPrintCall(expression) {
		source += line + "\n"
	} else {
		source += "print(str(" + strings.TrimSpace(line) + "))\n"
	}

	options, err := replOptions(out, level, paths)
	if err != nil {
		return err
	}
	compiled, err := compiler.Compile(strings.NewReader(source), options...)
	if err != nil {
		return err
	}
	return runProgram(ctx, compiled)
}

func runProgram(ctx context.Context, compiled *program.Program) error {
	runtime := interp.New(compiled)
	return errors.Join(runtime.Run(ctx), runtime.Close())
}

func replOptions(out io.Writer, level optimize.Level, paths []string) ([]compiler.Option, error) {
	workingDirectory, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	options := []compiler.Option{
		compiler.WithOutput(out),
		compiler.WithOptimizationLevel(level),
		compiler.WithModules(os.DirFS(workingDirectory)),
	}
	pathOptions, err := modulePathOptions(paths)
	if err != nil {
		return nil, err
	}
	return append(options, pathOptions...), nil
}

func isPrintCall(expression ast.Expr) bool {
	call, ok := expression.(*ast.CallExpr)
	if !ok {
		return false
	}
	name, ok := call.Fn.(*ast.Name)
	return ok && name.Name == "print"
}

func report(out io.Writer, err error) error {
	_, writeErr := fmt.Fprintln(out, pyError(err))
	return writeErr
}

// pyError renders an error in CPython's style. Compile diagnostics already carry
// a Python exception name; common runtime traps are mapped to their Python
// equivalents.
func pyError(err error) string {
	if errors.Is(err, interp.ErrDivideByZero) {
		return "ZeroDivisionError: division by zero"
	}
	return err.Error()
}
