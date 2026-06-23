// Command minipy is the M0 command-line interface: it runs a minipy source file
// or, with no argument, starts an interactive REPL.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
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
	root := &cobra.Command{
		Use:           "minipy [file]",
		Short:         "minipy — a statically-typed Python subset on minivm (M0)",
		Args:          cobra.MaximumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return repl(cmd.InOrStdin(), cmd.OutOrStdout())
			}
			return runFile(args[0], cmd.OutOrStdout())
		},
	}

	root.AddCommand(&cobra.Command{
		Use:           "run <file>",
		Short:         "compile and run a minipy file",
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFile(args[0], cmd.OutOrStdout())
		},
	})

	return root
}
