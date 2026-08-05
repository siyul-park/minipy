package conformance

import (
	"flag"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// update regenerates every corpus case's .expected golden from CPython 3.13
// output instead of comparing against it. It is a manual maintenance switch
// (`go test ./conformance -run TestGoldensMatchCPython -update`) and MUST NOT
// run as part of ordinary verification: verification always compares against
// the committed goldens, never regenerates them.
var update = flag.Bool("update", false, "regenerate .expected goldens from CPython 3.13")

const wantMajorMinor = "3 13"

// TestGoldensMatchCPython re-derives every corpus case's CPython golden by
// running the real interpreter, so a golden can never quietly drift from what
// CPython 3.13 actually produces. It requires a python3.13 interpreter on
// PATH and is skipped, not failed, when one is unavailable or -short is set.
func TestGoldensMatchCPython(t *testing.T) {
	if testing.Short() {
		t.Skip("conformance: skipping CPython golden verification in -short mode")
	}
	python := requirePython313(t)

	cases, err := Load("testdata/conformance")
	require.NoError(t, err)
	require.NotEmpty(t, cases)

	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			got := runCPython(t, python, c.Path)
			if *update {
				require.NoError(t, os.WriteFile(strings.TrimSuffix(c.Path, ".py")+".expected", []byte(got), 0o644))
				return
			}
			require.Equal(t, c.Expected, got)
		})
	}
}

// requirePython313 skips the test when no python3.13 interpreter is on PATH
// and otherwise asserts that the resolved interpreter reports version 3.13.
func requirePython313(t *testing.T) string {
	t.Helper()
	python, err := exec.LookPath("python3.13")
	if err != nil {
		t.Skip("conformance: skipping, python3.13 not found on PATH")
	}

	out, err := exec.Command(python, "-c", "import sys; print(sys.version_info[0], sys.version_info[1])").Output()
	require.NoError(t, err)
	require.Equal(t, wantMajorMinor, strings.TrimSpace(string(out)))
	return python
}

// runCPython runs a corpus source under CPython with a fixed hash seed, for
// output that does not depend on per-process hash randomization, and returns
// its captured stdout.
func runCPython(t *testing.T, python, path string) string {
	t.Helper()
	cmd := exec.Command(python, path)
	cmd.Env = append(os.Environ(), "PYTHONHASHSEED=0")
	out, err := cmd.Output()
	require.NoError(t, err)
	return string(out)
}
