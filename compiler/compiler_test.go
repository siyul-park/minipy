package compiler

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/siyul-park/minipy/token"
	"github.com/siyul-park/minivm/interp"
	"github.com/stretchr/testify/require"
)

func run(t *testing.T, src string) string {
	t.Helper()
	var buf bytes.Buffer
	prog, err := Compile(strings.NewReader(src), WithOutput(&buf))
	require.NoError(t, err)

	vm, err := interp.New(prog)
	require.NoError(t, err)
	defer vm.Close()
	require.NoError(t, vm.Run(context.Background()))
	return buf.String()
}

func hasCode(t *testing.T, err error, code token.Code) {
	t.Helper()
	el, ok := err.(token.ErrorList)
	require.Truef(t, ok, "expected token.ErrorList, got %T", err)
	for _, e := range el {
		if e.Code == code {
			return
		}
	}
	t.Fatalf("expected diagnostic %s, got %v", code, err)
}

func TestCompile(t *testing.T) {
	t.Run("worked example", func(t *testing.T) {
		require.Equal(t, "42\n", run(t, "x: int = 6\ny: int = 7\nprint(str(x * y))\n"))
	})

	t.Run("integer arithmetic and precedence", func(t *testing.T) {
		require.Equal(t, "7\n", run(t, "print(str(1 + 2 * 3))\n"))
		require.Equal(t, "9\n", run(t, "print(str((1 + 2) * 3))\n"))
		require.Equal(t, "1\n", run(t, "print(str(7 % 3))\n"))
		require.Equal(t, "3\n", run(t, "print(str(7 // 2))\n"))
		require.Equal(t, "1024\n", run(t, "print(str(2 ** 10))\n"))
	})

	t.Run("true division yields float", func(t *testing.T) {
		require.Equal(t, "3.5\n", run(t, "print(str(7 / 2))\n"))
	})

	t.Run("float arithmetic", func(t *testing.T) {
		require.Equal(t, "4.0\n", run(t, "print(str(1.5 + 2.5))\n"))
		require.Equal(t, "2.5\n", run(t, "print(str(5.0 / 2.0))\n"))
	})

	t.Run("bitwise and shift", func(t *testing.T) {
		require.Equal(t, "6\n", run(t, "print(str(2 | 4))\n"))
		require.Equal(t, "8\n", run(t, "print(str(1 << 3))\n"))
		require.Equal(t, "-6\n", run(t, "print(str(~5))\n"))
	})

	t.Run("unary minus", func(t *testing.T) {
		require.Equal(t, "-5\n", run(t, "print(str(-5))\n"))
		require.Equal(t, "-2.5\n", run(t, "print(str(-2.5))\n"))
	})

	t.Run("boolean short-circuit", func(t *testing.T) {
		require.Equal(t, "False\n", run(t, "print(str(True and False))\n"))
		require.Equal(t, "True\n", run(t, "print(str(False or True))\n"))
		require.Equal(t, "True\n", run(t, "print(str(not False))\n"))
	})

	t.Run("comparison including chains", func(t *testing.T) {
		require.Equal(t, "True\n", run(t, "print(str(3 < 5))\n"))
		require.Equal(t, "True\n", run(t, "print(str(1 < 2 < 3))\n"))
		require.Equal(t, "False\n", run(t, "print(str(1 < 2 < 2))\n"))
	})

	t.Run("string concatenation and comparison", func(t *testing.T) {
		require.Equal(t, "ab\n", run(t, `print("a" + "b")`+"\n"))
		require.Equal(t, "True\n", run(t, `print(str("a" < "b"))`+"\n"))
	})

	t.Run("builtin conversions", func(t *testing.T) {
		require.Equal(t, "3\n", run(t, "print(str(int(3.9)))\n"))
		require.Equal(t, "3.0\n", run(t, "print(str(float(3)))\n"))
		require.Equal(t, "False\n", run(t, "print(str(bool(0)))\n"))
		require.Equal(t, "True\n", run(t, `print(str(bool("x")))`+"\n"))
		require.Equal(t, "5\n", run(t, "print(str(abs(-5)))\n"))
		require.Equal(t, "2.5\n", run(t, "print(str(abs(-2.5)))\n"))
		require.Equal(t, "42\n", run(t, `print(str(int("42")))`+"\n"))
	})

	t.Run("globals and augmented assignment", func(t *testing.T) {
		require.Equal(t, "15\n", run(t, "x: int = 10\nx += 5\nprint(str(x))\n"))
		require.Equal(t, "20\n", run(t, "n: int = 4\nn *= 5\nprint(str(n))\n"))
	})

	t.Run("plain reassignment after declaration", func(t *testing.T) {
		require.Equal(t, "2\n", run(t, "x: int = 1\nx = 2\nprint(str(x))\n"))
	})

	t.Run("float floor, modulo, power", func(t *testing.T) {
		require.Equal(t, "3.0\n", run(t, "print(str(7.0 // 2.0))\n"))
		require.Equal(t, "1.5\n", run(t, "print(str(5.5 % 2.0))\n"))
		require.Equal(t, "8.0\n", run(t, "print(str(2.0 ** 3.0))\n"))
	})

	t.Run("equality and float comparison", func(t *testing.T) {
		require.Equal(t, "True\n", run(t, "print(str(True == True))\n"))
		require.Equal(t, "True\n", run(t, "print(str(1.5 < 2.5))\n"))
		require.Equal(t, "True\n", run(t, `print(str("a" != "b"))`+"\n"))
	})

	t.Run("conversions across all scalar inputs", func(t *testing.T) {
		require.Equal(t, "1\n", run(t, "print(str(int(True)))\n"))
		require.Equal(t, "1.0\n", run(t, "print(str(float(True)))\n"))
		require.Equal(t, "False\n", run(t, "print(str(bool(0.0)))\n"))
		require.Equal(t, "3.14\n", run(t, `print(str(float("3.14")))`+"\n"))
	})

	t.Run("none value", func(t *testing.T) {
		require.Equal(t, "None\n", run(t, "print(None)\n"))
		require.Equal(t, "None\n", run(t, "x: None = None\nprint(str(x))\n"))
	})

	t.Run("roadmap M1 sample sums even numbers", func(t *testing.T) {
		src := `total: int = 0
for i in range(1, 101):
    if i % 2 == 0:
        total = total + i
print(str(total))
`
		require.Equal(t, "2550\n", run(t, src))
	})

	t.Run("if elif else selects a branch", func(t *testing.T) {
		src := `x: int = 2
if x == 1:
    print("one")
elif x == 2:
    print("two")
else:
    print("other")
`
		require.Equal(t, "two\n", run(t, src))
	})

	t.Run("inline block", func(t *testing.T) {
		src := `x: int = 1
if x == 1: print("yes")
`
		require.Equal(t, "yes\n", run(t, src))
	})

	t.Run("while with break", func(t *testing.T) {
		src := `i: int = 0
while i < 10:
    if i == 3:
        break
    i = i + 1
print(str(i))
`
		require.Equal(t, "3\n", run(t, src))
	})

	t.Run("while with continue sums even numbers", func(t *testing.T) {
		src := `i: int = 0
total: int = 0
while i < 10:
    i = i + 1
    if i % 2 == 1:
        continue
    total = total + i
print(str(total))
`
		require.Equal(t, "30\n", run(t, src))
	})

	t.Run("for with descending step", func(t *testing.T) {
		src := `total: int = 0
for i in range(10, 0, -1):
    total = total + i
print(str(total))
`
		require.Equal(t, "55\n", run(t, src))
	})

	t.Run("for else runs without break", func(t *testing.T) {
		src := `for i in range(3):
    print(str(i))
else:
    print("done")
`
		require.Equal(t, "0\n1\n2\ndone\n", run(t, src))
	})

	t.Run("for else skipped after break", func(t *testing.T) {
		src := `for i in range(5):
    if i == 2:
        break
else:
    print("done")
print("after")
`
		require.Equal(t, "after\n", run(t, src))
	})

	t.Run("while else runs without break", func(t *testing.T) {
		src := `i: int = 0
while i < 2:
    i = i + 1
else:
    print("loopdone")
`
		require.Equal(t, "loopdone\n", run(t, src))
	})

	t.Run("nested loop break leaves outer running", func(t *testing.T) {
		src := `total: int = 0
for i in range(3):
    for j in range(3):
        if j == 1:
            break
        total = total + 1
print(str(total))
`
		require.Equal(t, "3\n", run(t, src))
	})

	t.Run("conditional expression", func(t *testing.T) {
		big := `x: int = 5
print("big" if x > 3 else "small")
`
		small := `x: int = 1
print("big" if x > 3 else "small")
`
		require.Equal(t, "big\n", run(t, big))
		require.Equal(t, "small\n", run(t, small))
	})

	t.Run("pass is a no-op", func(t *testing.T) {
		src := `x: int = 1
if x == 1:
    pass
print(str(x))
`
		require.Equal(t, "1\n", run(t, src))
	})

	t.Run("range single argument", func(t *testing.T) {
		src := `total: int = 0
for i in range(5):
    total = total + i
print(str(total))
`
		require.Equal(t, "10\n", run(t, src))
	})
}

func TestCompileErrors(t *testing.T) {
	cases := map[string]token.Code{
		"x = 5\n":                             token.MissingAnnotation,
		"x: int = 1.5\n":                      token.TypeMismatch,
		"print(str(1 + 1.5))\n":               token.TypeMismatch,
		"x: int = 99999999999999999999999\n":  token.IntOverflow,
		"print(str(y))\n":                     token.UndefinedName,
		"print()\n":                           token.ArityMismatch,
		"print(1, 2)\n":                       token.ArityMismatch,
		"x: int = True\n":                     token.TypeMismatch,
		"print(str(True + 1))\n":              token.TypeMismatch,
		"x: int\nprint(str(x))\n":             token.UseBeforeDefinition,
		"x: int = 1\nx: str = \"a\"\n":        token.TypeMismatch,
		"print(str(not 1))\n":                 token.TypeMismatch,
		"print(str(1.5 & 2))\n":               token.TypeMismatch,
		"x: int = 1\nprint(str(x == None))\n": token.UnsupportedFeature,
		"print(str(True and 1))\n":            token.TypeMismatch,
		"print(str(1 < \"a\"))\n":             token.NotComparable,
		"z += 1\n":                            token.UndefinedName,
		// M1 control flow
		"x: int = 1\nif x:\n    pass\n":              token.TypeMismatch,
		"for i in 5:\n    pass\n":                    token.UnsupportedFeature,
		"break\n":                                    token.SyntaxError,
		"continue\n":                                 token.SyntaxError,
		"for i in range(1.5):\n    pass\n":           token.TypeMismatch,
		"for i in range():\n    pass\n":              token.ArityMismatch,
		"a: int = 1\nfor i in range(0, 9, a):\n p\n": token.UnsupportedFeature,
		"for i in range(0, 9, 0):\n    pass\n":       token.SyntaxError,
		"x: int = 1 if True else \"a\"\n":            token.TypeMismatch,
	}
	for src, code := range cases {
		_, err := Compile(strings.NewReader(src), WithOutput(&bytes.Buffer{}))
		require.Errorf(t, err, "src=%q", src)
		hasCode(t, err, code)
	}
}
