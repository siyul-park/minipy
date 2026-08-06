package compiler

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestCompileScratchIsPerFrame pins the property that makes scratch temporaries
// frame locals rather than module globals: a slot written by one activation must
// survive a nested activation that lowers the same emitted code. When scratch
// lived in module globals, every one of these silently produced a wrong value —
// no diagnostic, no trap.
func TestCompileScratchIsPerFrame(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "recursive call inside a for body keeps the loop index",
			src: "def walk(xs: list[int], depth: int) -> int:\n" +
				"    total = 0\n" +
				"    for x in xs:\n" +
				"        if depth > 0:\n" +
				"            total = total + walk(xs, depth - 1)\n" +
				"        total = total + x\n" +
				"    return total\n" +
				"print(walk([1, 2, 3], 1))\n",
			want: "24\n",
		},
		{
			name: "recursive call inside a list comprehension keeps the accumulator",
			src: "def spread(n: int) -> list[int]:\n" +
				"    if n <= 0:\n" +
				"        return [0]\n" +
				"    return [v + n for v in spread(n - 1)]\n" +
				"print(spread(3))\n",
			want: "[6]\n",
		},
		{
			name: "recursive call in a match guard keeps the subject",
			src: "def rank(n: int) -> int:\n" +
				"    match n:\n" +
				"        case 0:\n" +
				"            return 0\n" +
				"        case _:\n" +
				"            return rank(n - 1) + n\n" +
				"print(rank(4))\n",
			want: "10\n",
		},
		{
			// The receiver and index slots stay live across the recursive call
			// on the right. CPython loads xs[0] before evaluating the right
			// side, so the nested mutations are overwritten by the outer
			// store — 1, not 4.
			name: "recursive call on the right of a subscript augassign keeps the receiver",
			src: "def fill(xs: list[int], i: int) -> int:\n" +
				"    if i <= 0:\n" +
				"        return 1\n" +
				"    xs[0] += fill(xs, i - 1)\n" +
				"    return xs[0]\n" +
				"buf: list[int] = [0]\n" +
				"print(fill(buf, 3))\n",
			want: "1\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, run(t, tt.src))
		})
	}
}

// TestCompileScratchStaysOutOfGlobals pins the global table's contents: it holds
// the module's named bindings and nothing else. Scratch temporaries inflating it
// is what made them shared across activations in the first place.
func TestCompileScratchStaysOutOfGlobals(t *testing.T) {
	// Three named globals, and many list-index sites, each of which reserves
	// scratch while lowering.
	src := "xs: list[int] = [1, 2, 3]\n" +
		"i: int = 0\n" +
		"total: int = 0\n" +
		"total = xs[0] + xs[1] + xs[2] + xs[i] + xs[i + 1]\n" +
		"print(total)\n"

	prog, err := Compile(strings.NewReader(src))
	require.NoError(t, err)

	require.Len(t, prog.Globals, 3, "global table holds only the module's named bindings")
	require.NotEmpty(t, prog.Locals, "scratch temporaries are entry-frame locals")
}
