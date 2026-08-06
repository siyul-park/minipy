// This file regression-tests `in`/`not in` over list[T] comparing elements
// by identity instead of CPython's structural `==` (docs/spec/04-static-
// semantics.md, docs/spec/05-codegen.md). It reuses the run helper already
// defined in compare_e2e_test.go for the same reason that file compiles and
// runs real minipy source rather than only exercising emitContains in
// isolation: the bug is only visible once a fresh (non-identical) container
// value crosses the host ABI boundary.
package operator_test

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestListContainsStructural reproduces the bug report's `tuple in
// list[tuple[...]]` case plus nested-list and negative variants. Every want
// value was cross-checked against /usr/bin/python3.13.
func TestListContainsStructural(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "tuple in list of tuples matches by value",
			src: "pts: list[tuple[int,int]] = [(0,0),(1,1)]\n" +
				"a: tuple[int,int] = (1,1)\n" +
				"print(a in pts)\n",
			want: "True\n",
		},
		{
			name: "tuple not in list of tuples when absent",
			src: "pts: list[tuple[int,int]] = [(0,0),(1,1)]\n" +
				"b: tuple[int,int] = (5,5)\n" +
				"print(b in pts)\n",
			want: "False\n",
		},
		{
			name: "not in over list of tuples",
			src: "pts: list[tuple[int,int]] = [(0,0),(1,1)]\n" +
				"b: tuple[int,int] = (5,5)\n" +
				"print(b not in pts)\n",
			want: "True\n",
		},
		{
			name: "list in list of lists matches by value",
			src: "lls: list[list[int]] = [[1,2],[3,4]]\n" +
				"q: list[int] = [3,4]\n" +
				"print(q in lls)\n",
			want: "True\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, run(t, tt.src))
		})
	}
}
