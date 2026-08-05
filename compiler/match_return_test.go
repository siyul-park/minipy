package compiler

import (
	"strings"
	"testing"

	"github.com/siyul-park/minipy/token"
	"github.com/stretchr/testify/require"
)

// TestCompileMatchExhaustiveness covers blockReturns/blockTerminates'
// *ast.Match case (compiler/check_decl.go): a match counts as returning only
// when it has an unconditional wildcard/bare-capture arm and every case body
// returns. These cases exercise the checker's "may fall through" diagnostic
// (compiler/check_decl.go) and, for the accepted programs, prove the emitted
// program (compiler/lower.go) still returns the right value end to end.
func TestCompileMatchExhaustiveness(t *testing.T) {
	t.Run("wildcard arm with every case returning runs and reports no diagnostic", func(t *testing.T) {
		src := "def f(x: int) -> str:\n" +
			"    match x:\n" +
			"        case 0:\n" +
			"            return \"zero\"\n" +
			"        case 1 | 2:\n" +
			"            return \"small\"\n" +
			"        case _:\n" +
			"            return \"big\"\n" +
			"print(f(0))\n" +
			"print(f(1))\n" +
			"print(f(2))\n" +
			"print(f(99))\n"
		require.Equal(t, "zero\nsmall\nsmall\nbig\n", run(t, src))
	})

	t.Run("bare capture arm with every case returning runs", func(t *testing.T) {
		src := "def f(x: int) -> str:\n" +
			"    match x:\n" +
			"        case 0:\n" +
			"            return \"zero\"\n" +
			"        case n:\n" +
			"            return \"other:\" + str(n)\n" +
			"print(f(0))\n" +
			"print(f(5))\n"
		require.Equal(t, "zero\nother:5\n", run(t, src))
	})

	t.Run("guarded wildcard arm does not make the match exhaustive", func(t *testing.T) {
		src := "def f(x: int) -> str:\n" +
			"    match x:\n" +
			"        case 0:\n" +
			"            return \"zero\"\n" +
			"        case _ if x > 0:\n" +
			"            return \"pos\"\n"
		_, err := Compile(strings.NewReader(src))
		require.Error(t, err)
		code(t, err, token.TypeMismatch)
	})

	t.Run("match with no wildcard or capture arm is still reported", func(t *testing.T) {
		src := "def f(x: int) -> str:\n" +
			"    match x:\n" +
			"        case 0:\n" +
			"            return \"zero\"\n" +
			"        case 1:\n" +
			"            return \"one\"\n"
		_, err := Compile(strings.NewReader(src))
		require.Error(t, err)
		code(t, err, token.TypeMismatch)
	})

	t.Run("exhaustive match with one non-returning arm is still reported", func(t *testing.T) {
		src := "def f(x: int) -> str:\n" +
			"    match x:\n" +
			"        case 0:\n" +
			"            return \"zero\"\n" +
			"        case _:\n" +
			"            print(\"no return here\")\n"
		_, err := Compile(strings.NewReader(src))
		require.Error(t, err)
		code(t, err, token.TypeMismatch)
	})

	t.Run("exhaustive non-returning match in a None function keeps the implicit return", func(t *testing.T) {
		src := "def f(x: int) -> None:\n" +
			"    match x:\n" +
			"        case 0:\n" +
			"            print(\"zero\")\n" +
			"        case _:\n" +
			"            print(\"other\")\n" +
			"f(0)\n" +
			"f(5)\n" +
			"print(\"done\")\n"
		require.Equal(t, "zero\nother\ndone\n", run(t, src))
	})

	t.Run("exhaustive match as the last statement of a loop body runs", func(t *testing.T) {
		src := "def classify(items: list[int]) -> list[str]:\n" +
			"    out: list[str] = []\n" +
			"    for x in items:\n" +
			"        match x:\n" +
			"            case 0:\n" +
			"                out.append(\"zero\")\n" +
			"            case _:\n" +
			"                out.append(\"nonzero\")\n" +
			"    return out\n" +
			"print(str(classify([0, 1, 2, 0])))\n"
		require.Equal(t, "['zero', 'nonzero', 'nonzero', 'zero']\n", run(t, src))
	})
}

// TestCompileRaiseTerminates covers blockReturns' *ast.Raise case: a
// function whose branch ends in an unconditional raise does not fall
// through, so it needs no trailing return.
func TestCompileRaiseTerminates(t *testing.T) {
	t.Run("branch ending in raise runs without a fall-through diagnostic", func(t *testing.T) {
		src := "def g(x: int) -> str:\n" +
			"    if x > 0:\n" +
			"        return \"pos\"\n" +
			"    raise ValueError(\"bad\")\n" +
			"print(g(1))\n" +
			"try:\n" +
			"    g(-1)\n" +
			"except ValueError:\n" +
			"    print(\"caught\")\n"
		require.Equal(t, "pos\ncaught\n", run(t, src))
	})

	t.Run("match arm raising combined with returning arms runs", func(t *testing.T) {
		src := "def f(x: int) -> str:\n" +
			"    match x:\n" +
			"        case 0:\n" +
			"            return \"zero\"\n" +
			"        case n if n < 0:\n" +
			"            raise ValueError(\"negative\")\n" +
			"        case _:\n" +
			"            return \"pos\"\n" +
			"print(f(0))\n" +
			"print(f(5))\n" +
			"try:\n" +
			"    f(-1)\n" +
			"except ValueError:\n" +
			"    print(\"caught\")\n"
		require.Equal(t, "zero\npos\ncaught\n", run(t, src))
	})
}

// TestCompileIfElseReturnsRegression guards the *ast.If case that predates
// this change: both branches returning still counts as returning on every
// path.
func TestCompileIfElseReturnsRegression(t *testing.T) {
	t.Run("if and else both returning runs without a fall-through diagnostic", func(t *testing.T) {
		src := "def h(x: int) -> str:\n" +
			"    if x > 0:\n" +
			"        return \"pos\"\n" +
			"    else:\n" +
			"        return \"nonpos\"\n" +
			"print(h(1))\n" +
			"print(h(-1))\n"
		require.Equal(t, "pos\nnonpos\n", run(t, src))
	})
}
