// Command minipy is the command-line interface: it runs a minipy source file
// or, with no argument, starts an interactive REPL.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

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

	optLevel := func() (optimize.Level, error) {
		if opt < int(optimize.O0) || opt > int(optimize.O3) {
			return 0, fmt.Errorf("invalid optimization level %d: must be 0..3", opt)
		}
		return optimize.Level(opt), nil
	}

	root := &cobra.Command{
		Use:           "minipy [file]",
		Short:         "minipy — a statically-typed Python subset on minivm",
		Args:          cobra.MaximumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			level, err := optLevel()
			if err != nil {
				return err
			}
			if len(args) == 0 {
				return repl(cmd.InOrStdin(), cmd.OutOrStdout(), level)
			}
			return runFile(args[0], cmd.OutOrStdout(), level)
		},
	}
	root.PersistentFlags().IntVarP(&opt, "opt", "O", int(optimize.O0),
		"optimization level (0..3); 3 enables global value numbering / CSE")

	root.AddCommand(&cobra.Command{
		Use:           "run <file>",
		Short:         "compile and run a minipy file",
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			level, err := optLevel()
			if err != nil {
				return err
			}
			return runFile(args[0], cmd.OutOrStdout(), level)
		},
	})

	return root
}
