// This file regression-tests bug A1 (container comparisons panicking in
// CmpOpcode) end to end, compiling and running real minipy source through
// the full pipeline rather than only exercising Comparable/EmitCompareStack
// in isolation. It lives in operator/ rather than compiler/ because A1's
// fix is scoped to the operator package (docs/spec/04-static-semantics.md,
// docs/spec/05-codegen.md); compiler is a read-only dependency here, mirroring
// the compile/run helper already used by compiler/compiler_test.go and
// conformance/conformance_test.go.
package operator_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/siyul-park/minipy/compiler"

	"github.com/siyul-park/minivm/interp"
	"github.com/stretchr/testify/require"
)

// run compiles and executes src, returning captured stdout.
func run(t *testing.T, src string) string {
	t.Helper()
	var buf bytes.Buffer
	prog, err := compiler.Compile(strings.NewReader(src), compiler.WithOutput(&buf))
	require.NoError(t, err)

	vm := interp.New(prog)
	defer vm.Close()
	require.NoError(t, vm.Run(context.Background()))
	return buf.String()
}

// compileErr compiles src and returns the resulting error without running
// it, for asserting a clean diagnostic instead of a panic.
func compileErr(t *testing.T, src string) error {
	t.Helper()
	_, err := compiler.Compile(strings.NewReader(src))
	return err
}

// TestContainerComparisons reproduces every expression from the A1 bug
// report plus the CPython parity checks from the task's verification list.
// Every want value was cross-checked against /usr/bin/python3.13.
func TestContainerComparisons(t *testing.T) {
	tests := []struct {
		name string
		expr string
		want string
	}{
		{"list eq equal", "[1, 2] == [1, 2]", "True\n"},
		{"list eq unequal", "[1, 2] == [1, 3]", "False\n"},
		{"list ne", "[1, 2] != [1, 3]", "True\n"},
		{"list lt", "[1, 2] < [1, 3]", "True\n"},
		{"list lt prefix", "[1] < [1, 2]", "True\n"},
		{"tuple eq", "(1, 2) == (1, 2)", "True\n"},
		{"tuple eq heterogeneous", "(1, \"a\") == (1, \"a\")", "True\n"},
		{"set eq", "{1, 2} == {1, 2}", "True\n"},
		{"set eq unordered", "{1, 2} == {2, 1}", "True\n"},
		{"dict eq", "{\"a\": 1} == {\"a\": 1}", "True\n"},
		{"str list eq", "[\"a\"] == [\"a\"]", "True\n"},
		{"nested list eq", "[[1], [2]] == [[1], [2]]", "True\n"},
		{"nested list eq differs", "[[1], [2]] == [[1], [3]]", "False\n"},
		{"list eq different length", "[1, 2] == [1, 2, 3]", "False\n"},
		{"dict eq different value", "{\"a\": 1} == {\"a\": 2}", "False\n"},
		{"dict eq different key", "{\"a\": 1} == {\"b\": 1}", "False\n"},
		{"set eq different size", "{1, 2} == {1, 2, 3}", "False\n"},
		{"set union", "sorted([v for v in ({1, 2} | {2, 3})])", "[1, 2, 3]\n"},
		{"set intersection", "sorted([v for v in ({1, 2} & {2, 3})])", "[2]\n"},
		{"set difference", "sorted([v for v in ({1, 2} - {2, 3})])", "[1]\n"},
		{"set symmetric difference", "sorted([v for v in ({1, 2} ^ {2, 3})])", "[1, 3]\n"},
		{"set subset", "{1, 2} <= {1, 2, 3}", "True\n"},
		{"set proper subset", "{1, 2} < {1, 2, 3}", "True\n"},
		{"set superset", "{1, 2, 3} >= {1, 2}", "True\n"},
		{"set proper superset", "{1, 2, 3} > {1, 2}", "True\n"},
		{"tuple lt", "(1, 2) < (1, 3)", "True\n"},
		{"str list lt", "[\"a\", \"b\"] < [\"a\", \"c\"]", "True\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, run(t, "print("+tt.expr+")\n"))
		})
	}
}

// TestContainerOrderingRejected confirms unsupported container ordering is a
// clean TypeError-shaped diagnostic rather than a panic.
func TestContainerOrderingRejected(t *testing.T) {
	tests := []struct {
		name string
		expr string
	}{
		{"dict lt", "{\"a\": 1} < {\"b\": 2}"},
		{"dict le", "{\"a\": 1} <= {\"b\": 2}"},
		{"nested list lt", "[[1], [2]] < [[1], [3]]"},
		{"tuple with list field lt", "([1],) < ([2],)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.NotPanics(t, func() {
				err := compileErr(t, tt.expr+"\n")
				require.Error(t, err)
				require.Contains(t, err.Error(), "not supported between instances of")
			})
		})
	}
}
