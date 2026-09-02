package codegen

import (
	"bytes"
	"context"
	"flag"
	"os"
	"strings"
	"testing"

	"github.com/siyul-park/minipy/compiler"
	"github.com/siyul-park/minivm/interp"
	"github.com/siyul-park/minivm/optimize"
	"github.com/siyul-park/minivm/program"
	"github.com/stretchr/testify/require"
)

// levels are the optimization levels a corpus case must still verify under. The
// goldens themselves pin the default level's output; a higher level rewrites
// the program, and what must hold there is that the result is still a valid
// program, not that it looks a particular way.
var levels = []struct {
	name  string
	level optimize.Level
}{
	{"O0", optimize.O0},
	{"O1", optimize.O1},
	{"O2", optimize.O2},
	{"O3", optimize.O3},
}

// update rewrites every golden from the compiler's current output. It is a
// maintenance switch, never part of verification: run it after a deliberate
// codegen change, then read the diff, which is the change's own evidence.
var update = flag.Bool("update", false, "rewrite .masm goldens from the current compiler output")

// TestCodegen pins the program minipy emits for every corpus case.
//
// A golden alone cannot tell a better program from a broken one, so each case
// carries two more assertions that do: the compiler verifies every program it
// returns (compiler.Compile calls program.Verify), and the case is executed
// here. A golden regenerated into a program that does not run fails on the run,
// not on the diff.
func TestCodegen(t *testing.T) {
	cases, err := Load("testdata")
	require.NoError(t, err)
	require.NotEmpty(t, cases)

	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			source, err := os.ReadFile(c.Path)
			require.NoError(t, err)

			var stdout bytes.Buffer
			prog, err := compiler.Compile(bytes.NewReader(source), compiler.WithOutput(&stdout))
			require.NoError(t, err)

			vm := interp.New(prog)
			defer vm.Close()
			require.NoError(t, vm.Run(context.Background()), "the compiled program must run")

			listing := Format(prog)
			if *update {
				require.NoError(t, os.WriteFile(c.Golden, []byte(listing), 0o644))
				return
			}
			require.Equalf(t, c.Expected, listing,
				"emitted program differs from %s; re-run with -update after a deliberate codegen change", c.Golden)
		})
	}
}

// TestCodegenIsDeterministic pins that compiling one source twice emits the
// same program. Goldens are only meaningful if it does, and a checker or
// lowerer that iterated a Go map to decide emission order would break it
// intermittently rather than outright.
func TestCodegenIsDeterministic(t *testing.T) {
	cases, err := Load("testdata")
	require.NoError(t, err)

	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			source, err := os.ReadFile(c.Path)
			require.NoError(t, err)

			first := compileListing(t, string(source))
			for range 4 {
				require.Equal(t, first, compileListing(t, string(source)))
			}
		})
	}
}

// TestCodegenVerifiesAtEveryOptimizationLevel pins that the optimizer leaves a
// program the verifier still accepts, for every corpus case at every level.
// compiler.Compile verifies what it returns, so a level that broke a program
// would surface as a compile error here.
func TestCodegenVerifiesAtEveryOptimizationLevel(t *testing.T) {
	cases, err := Load("testdata")
	require.NoError(t, err)

	for _, level := range levels {
		t.Run(level.name, func(t *testing.T) {
			for _, c := range cases {
				t.Run(c.Name, func(t *testing.T) {
					source, err := os.ReadFile(c.Path)
					require.NoError(t, err)

					prog, err := compiler.Compile(bytes.NewReader(source),
						compiler.WithOutput(&bytes.Buffer{}), compiler.WithOptimizationLevel(level.level))
					require.NoError(t, err)
					require.NoError(t, program.Verify(prog))
				})
			}
		})
	}
}

func compileListing(t *testing.T, source string) string {
	t.Helper()
	prog, err := compiler.Compile(strings.NewReader(source), compiler.WithOutput(&bytes.Buffer{}))
	require.NoError(t, err)
	return Format(prog)
}
