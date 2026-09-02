// The corpus is file-based, deviating from the coding standard's default of
// keeping source and expected output visible beside the assertion (§11.2).
// The invariant that requires it: every corpus golden must be a byte-identical
// artifact produced by an external CPython 3.13 process, so that the expected
// output is external evidence rather than a restatement of minipy's own
// behavior. Go string literals cannot satisfy that. Inline fixtures remain the
// default for all unit tests.
package conformance

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/siyul-park/minipy/compiler"
	"github.com/siyul-park/minivm/interp"
	"github.com/siyul-park/minivm/optimize"
	"github.com/stretchr/testify/require"
)

// levels are the optimization levels every corpus case must produce identical
// output under. A level changes which minivm passes rewrite the emitted program,
// never what the program means, so the whole corpus is a behavior contract for
// each one rather than for the default alone.
var levels = []struct {
	name  string
	level optimize.Level
}{
	{"O0", optimize.O0},
	{"O1", optimize.O1},
	{"O2", optimize.O2},
	{"O3", optimize.O3},
}

// run compiles and executes a corpus case's source at one optimization level,
// mirroring the compile/run helper in compiler/compiler_test.go, and returns its
// captured stdout.
func run(t *testing.T, path string, level optimize.Level) string {
	t.Helper()
	source, err := os.Open(path)
	require.NoError(t, err)
	defer source.Close()

	var buf bytes.Buffer
	prog, err := compiler.Compile(source, compiler.WithOutput(&buf), compiler.WithOptimizationLevel(level))
	require.NoError(t, err)

	vm := interp.New(prog)
	defer vm.Close()
	require.NoError(t, vm.Run(context.Background()))
	return buf.String()
}

func TestConformance(t *testing.T) {
	cases, err := Load("testdata/conformance")
	require.NoError(t, err)
	require.NotEmpty(t, cases)

	for _, level := range levels {
		t.Run(level.name, func(t *testing.T) {
			for _, c := range cases {
				t.Run(c.Name, func(t *testing.T) {
					want := c.Expected
					if c.Divergent {
						want = c.MinipyExpected
					}
					require.Equal(t, want, run(t, c.Path, level.level))
				})
			}
		})
	}
}
