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
	"github.com/stretchr/testify/require"
)

// run compiles and executes a corpus case's source, mirroring the compile/run
// helper in compiler/compiler_test.go, and returns its captured stdout.
func run(t *testing.T, path string) string {
	t.Helper()
	source, err := os.Open(path)
	require.NoError(t, err)
	defer source.Close()

	var buf bytes.Buffer
	prog, err := compiler.Compile(source, compiler.WithOutput(&buf))
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

	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			want := c.Expected
			if c.Divergent {
				want = c.MinipyExpected
			}
			require.Equal(t, want, run(t, c.Path))
		})
	}
}
