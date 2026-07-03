// Command minipy is the command-line interface: it runs a minipy source file
// or, with no argument, starts an interactive REPL.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/siyul-park/minipy/compiler"
	"github.com/siyul-park/minivm/optimize"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, pyError(err))
		os.Exit(1)
	}
}

// newRootCmd builds the cobra command tree. The root runs a file when given one
// and otherwise starts the REPL; `run <file>` is the explicit file form.
func newRootCmd() *cobra.Command {
	var opt int
	var paths []string

	root := &cobra.Command{
		Use:           "minipy [file]",
		Short:         "minipy — a statically-typed Python subset on minivm",
		Args:          cobra.MaximumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			level, err := optLevel(opt)
			if err != nil {
				return err
			}
			if len(args) == 0 {
				return repl(cmd.InOrStdin(), cmd.OutOrStdout(), level, paths)
			}
			return runFile(args[0], cmd.OutOrStdout(), level, paths)
		},
	}
	root.PersistentFlags().IntVarP(&opt, "opt", "O", int(optimize.O0),
		"optimization level (0..3); 3 enables global value numbering / CSE")
	root.PersistentFlags().StringArrayVarP(&paths, "path", "p", nil,
		"add a module search path")

	root.AddCommand(&cobra.Command{
		Use:           "run <file>",
		Short:         "compile and run a minipy file",
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			level, err := optLevel(opt)
			if err != nil {
				return err
			}
			return runFile(args[0], cmd.OutOrStdout(), level, paths)
		},
	})

	return root
}

func modulePathOptions(paths []string) []compiler.Option {
	opts := make([]compiler.Option, 0, len(paths))
	for _, p := range paths {
		abs, err := filepath.Abs(p)
		if err != nil {
			abs = p
		}
		opts = append(opts, compiler.WithModules(os.DirFS(abs)))
	}
	return opts
}

func optLevel(opt int) (optimize.Level, error) {
	if opt < int(optimize.O0) || opt > int(optimize.O3) {
		return 0, fmt.Errorf("invalid optimization level %d: must be 0..3", opt)
	}
	return optimize.Level(opt), nil
}
