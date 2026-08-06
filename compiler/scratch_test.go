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
			name: "recursive call in a comprehension iterable keeps the accumulator",
			src: "def spread(n: int) -> list[int]:\n" +
				"    if n <= 0:\n" +
				"        return [0]\n" +
				"    return [v + n for v in spread(n - 1)]\n" +
				"print(spread(3))\n",
			want: "[6]\n",
		},
		{
			name: "recursive call in a match case body keeps the subject",
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

// TestCompileScalarScratchPaths pins the lowering paths where a scratch slot
// carries a value that is a scalar at the VM level rather than a reference.
// Their slots are declared as references because the value is only stored and
// loaded, never fed to a scalar opcode, so the declaration is what the verifier
// accepts — but that is a property of the emitted shape, not of the values, and
// a change to either would break these silently.
func TestCompileScalarScratchPaths(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "integer-key dict deletion",
			src:  "d: dict[int, int] = {1: 10, 2: 20}\ndel d[1]\nprint(len(d))\n",
			want: "1\n",
		},
		{
			name: "integer list element assignment",
			src:  "xs: list[int] = [1, 2, 3]\nxs[1] = 99\nprint(xs)\n",
			want: "[1, 99, 3]\n",
		},
		{
			name: "integer list insert",
			src:  "xs: list[int] = [1, 2]\nxs.insert(1, 42)\nprint(xs)\n",
			want: "[1, 42, 2]\n",
		},
		{
			name: "integer list reverse",
			src:  "xs: list[int] = [1, 2, 3]\nxs.reverse()\nprint(xs)\n",
			want: "[3, 2, 1]\n",
		},
		{
			name: "chained assignment of an integer",
			src:  "a: int = 0\nb: int = 0\na = b = 7\nprint(a + b)\n",
			want: "14\n",
		},
		{
			name: "augmented assignment into an integer list element",
			src:  "xs: list[int] = [1, 2]\nxs[0] += 5\nprint(xs)\n",
			want: "[6, 2]\n",
		},
		{
			name: "augmented assignment into a float list element",
			src:  "xs: list[float] = [1.0]\nxs[0] += 0.5\nprint(xs)\n",
			want: "[1.5]\n",
		},
		{
			name: "mapping pattern binding an integer",
			src: "d: dict[str, int] = {\"a\": 1}\n" +
				"match d:\n" +
				"    case {\"a\": v}:\n" +
				"        print(v)\n" +
				"    case _:\n" +
				"        print(\"no\")\n",
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
