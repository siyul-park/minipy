// Command minipy is the command-line interface: it runs a minipy source file
// or, with no argument, starts an interactive REPL.
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/siyul-park/minipy/compiler"
	"github.com/siyul-park/minivm/optimize"
)

func main() {
	os.Exit(execute(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func execute(args []string, in io.Reader, out, errOut io.Writer) int {
	command := newRootCmd()
	command.SetArgs(append([]string(nil), args...))
	command.SetIn(in)
	command.SetOut(out)
	command.SetErr(errOut)
	if err := command.Execute(); err != nil {
		fmt.Fprintln(errOut, pyError(err))
		return 1
	}
	return 0
}

// newRootCmd builds the cobra command tree. The root runs a file when given one
// and otherwise starts the REPL; `run <file>` is the explicit file form.
func newRootCmd() *cobra.Command {
	var optimization int
	var modulePaths []string

	root := &cobra.Command{
		Use:           "minipy [file]",
		Short:         "minipy — a statically-typed Python subset on minivm",
		Args:          cobra.MaximumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(command *cobra.Command, args []string) error {
			level, err := optLevel(optimization)
			if err != nil {
				return err
			}
			paths := append([]string(nil), modulePaths...)
			if len(args) == 0 {
				return repl(command.Context(), command.InOrStdin(), command.OutOrStdout(), level, paths)
			}
			return runFile(command.Context(), args[0], command.OutOrStdout(), level, paths)
		},
	}
	root.PersistentFlags().IntVarP(&optimization, "opt", "O", int(optimize.O0),
		"optimization level (0..3); 3 enables global value numbering / CSE")
	root.PersistentFlags().StringArrayVarP(&modulePaths, "path", "p", nil,
		"add a module search path")
	root.AddCommand(newRunCommand(&optimization, &modulePaths))
	return root
}

func newRunCommand(optimization *int, modulePaths *[]string) *cobra.Command {
	return &cobra.Command{
		Use:           "run <file>",
		Short:         "compile and run a minipy file",
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(command *cobra.Command, args []string) error {
			level, err := optLevel(*optimization)
			if err != nil {
				return err
			}
			paths := append([]string(nil), (*modulePaths)...)
			return runFile(command.Context(), args[0], command.OutOrStdout(), level, paths)
		},
	}
}

func optLevel(optimization int) (optimize.Level, error) {
	if optimization < int(optimize.O0) || optimization > int(optimize.O3) {
		return 0, fmt.Errorf("invalid optimization level %d: must be 0..3", optimization)
	}
	return optimize.Level(optimization), nil
}

func modulePathOptions(paths []string) ([]compiler.Option, error) {
	options := make([]compiler.Option, 0, len(paths))
	for _, path := range paths {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		options = append(options, compiler.WithModules(os.DirFS(absolute)))
	}
	return options, nil
}
