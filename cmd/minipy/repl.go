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
)

// runFile compiles and runs a minipy source file, writing program output to out.
func runFile(path string, out io.Writer, level optimize.Level, paths []string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	opts := []compiler.Option{
		compiler.WithOutput(out),
		compiler.WithOptimizationLevel(level),
		compiler.WithModules(os.DirFS(filepath.Dir(abs))),
	}
	opts = append(opts, modulePathOptions(paths)...)
	prog, err := compiler.Compile(f, opts...)
	if err != nil {
		return err
	}
	vm := interp.New(prog)
	defer vm.Close()
	return vm.Run(context.Background())
}

// repl runs the interactive loop. It persists declarations and assignments as
// session state, and runs bare expressions and print(...) lines transiently —
// re-running the accumulated state each time so prior side effects do not
// repeat. A bare printable expression is auto-echoed via str()+print.
func repl(in io.Reader, out io.Writer, level optimize.Level, paths []string) error {
	fmt.Fprintln(out, "minipy REPL — type Ctrl-D to exit")
	scanner := bufio.NewScanner(in)
	var state strings.Builder
	for {
		fmt.Fprint(out, ">>> ")
		if !scanner.Scan() {
			fmt.Fprintln(out)
			return scanner.Err()
		}
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		evalLine(&state, line, out, level, paths)
	}
}

// evalLine classifies one REPL entry and either extends the session state or
// runs a transient program.
func evalLine(state *strings.Builder, line string, out io.Writer, level optimize.Level, paths []string) {
	mod, err := parser.Parse(strings.NewReader(line))
	if err != nil {
		fmt.Fprintln(out, pyError(err))
		return
	}
	if len(mod.Body) == 0 {
		return
	}

	switch stmt := mod.Body[0].(type) {
	case *ast.AnnAssign, *ast.Assign, *ast.AugAssign, *ast.Import, *ast.ImportFrom:
		candidate := state.String() + line + "\n"
		opts := replOptions(io.Discard, level, paths)
		if _, err := compiler.Compile(strings.NewReader(candidate), opts...); err != nil {
			fmt.Fprintln(out, pyError(err))
			return
		}
		state.WriteString(line + "\n")
	case *ast.ExprStmt:
		runTransient(state.String(), line, stmt.X, out, level, paths)
	}
}

// runTransient compiles and runs `state + line`, auto-wrapping a bare expression
// in str()+print so its value is echoed.
func runTransient(state, line string, x ast.Expr, out io.Writer, level optimize.Level, paths []string) {
	src := state
	if isPrintCall(x) {
		src += line + "\n"
	} else {
		src += "print(str(" + strings.TrimSpace(line) + "))\n"
	}

	opts := replOptions(out, level, paths)
	prog, err := compiler.Compile(strings.NewReader(src), opts...)
	if err != nil {
		fmt.Fprintln(out, pyError(err))
		return
	}
	vm := interp.New(prog)
	defer vm.Close()
	if err := vm.Run(context.Background()); err != nil {
		fmt.Fprintln(out, pyError(err))
	}
}

func replOptions(out io.Writer, level optimize.Level, paths []string) []compiler.Option {
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}
	opts := []compiler.Option{
		compiler.WithOutput(out),
		compiler.WithOptimizationLevel(level),
		compiler.WithModules(os.DirFS(cwd)),
	}
	opts = append(opts, modulePathOptions(paths)...)
	return opts
}

func isPrintCall(x ast.Expr) bool {
	call, ok := x.(*ast.CallExpr)
	if !ok {
		return false
	}
	name, ok := call.Fn.(*ast.Name)
	return ok && name.Name == "print"
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
