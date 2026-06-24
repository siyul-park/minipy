package compiler

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/siyul-park/minipy/token"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/interp"
	"github.com/siyul-park/minivm/optimize"
	vmtypes "github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func run(t *testing.T, src string) string {
	t.Helper()
	var buf bytes.Buffer
	prog, err := Compile(strings.NewReader(src), WithOutput(&buf))
	require.NoError(t, err)

	vm := interp.New(prog)
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
	t.Run("compiler object API and optimization option", func(t *testing.T) {
		var buf bytes.Buffer
		c := New(strings.NewReader("print(\"api\")\n"), WithOutput(&buf), WithOptimizationLevel(optimize.O1))
		prog, err := c.Compile()
		require.NoError(t, err)

		vm := interp.New(prog)
		defer vm.Close()
		require.NoError(t, vm.Run(context.Background()))
		require.Equal(t, "api\n", buf.String())
	})

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

	t.Run("M3 roadmap dict counting sample", func(t *testing.T) {
		src := `counts: dict[str, int] = {}
for w in ["a", "b", "a"]:
    counts[w] = counts.get(w, 0) + 1
print(str(counts["a"]))
`
		require.Equal(t, "2\n", run(t, src))
	})

	t.Run("M3 containers and methods", func(t *testing.T) {
		src := `xs: list[int] = [1]
xs.append(2)
print(str(len(xs)))
print(str(xs.pop()))
d: dict[str, int] = {"a": 1, "b": 2}
print(str(len(d.keys())))
for k, v in d.items():
    print(k + str(v))
t: tuple[int, str] = (7, "x")
print(str(t[0]))
print(t[1])
`
		out := run(t, src)
		require.Contains(t, out, "2\n2\n")
		require.Contains(t, out, "a1\n")
		require.Contains(t, out, "b2\n")
		require.Contains(t, out, "7\nx\n")
	})

	t.Run("M3 str methods enumerate zip and f-string", func(t *testing.T) {
		src := `print("A,B".lower())
print("a,b".split(",")[1])
print("-".join(["x", "y"]))
print(str("abc".find("b")))
for i, v in enumerate([4, 5]):
    print(str(i) + str(v))
for a, b in zip([1, 2], [3, 4]):
    print(str(a + b))
x: int = 7
print(f"x={x!s:03d}")
`
		require.Equal(t, "a,b\nb\nx-y\n1\n04\n15\n4\n6\nx=007\n", run(t, src))
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

	t.Run("function call with local inference", func(t *testing.T) {
		src := `def add(x: int, y: int) -> int:
    z = x + y
    return z
print(str(add(20, 22)))
`
		require.Equal(t, "42\n", run(t, src))
	})

	t.Run("recursive function", func(t *testing.T) {
		src := `def fib(n: int) -> int:
    if n < 2:
        return n
    return fib(n - 1) + fib(n - 2)
print(str(fib(10)))
`
		require.Equal(t, "55\n", run(t, src))
	})

	t.Run("none returning function", func(t *testing.T) {
		src := `def greet() -> None:
    print("hi")
greet()
`
		require.Equal(t, "hi\n", run(t, src))
	})

	t.Run("lambda captures enclosing value", func(t *testing.T) {
		src := `def adder(n: int) -> Callable[[int], int]:
    return lambda x: x + n
add5: Callable[[int], int] = adder(5)
print(str(add5(10)))
`
		require.Equal(t, "15\n", run(t, src))
		prog, err := Compile(strings.NewReader(src), WithOutput(io.Discard))
		require.NoError(t, err)
		hasOps(t, prog.Constants, instr.CLOSURE_NEW, instr.UPVAL_GET)
	})

	t.Run("lambda closure does not depend on function name", func(t *testing.T) {
		src := `def shift(delta: int) -> Callable[[int], int]:
    return lambda value: value + delta
plus3: Callable[[int], int] = shift(3)
print(str(plus3(4)))
`
		require.Equal(t, "7\n", run(t, src))
	})

	t.Run("nested function returned as closure", func(t *testing.T) {
		src := `def make(n: int) -> Callable[[int], int]:
    def add(x: int) -> int:
        return x + n
    return add
f: Callable[[int], int] = make(7)
print(str(f(8)))
`
		require.Equal(t, "15\n", run(t, src))
	})

	t.Run("nested function closure does not depend on function name", func(t *testing.T) {
		src := `def factory(base: int) -> Callable[[int], int]:
    def apply(arg: int) -> int:
        return base + arg
    return apply
f: Callable[[int], int] = factory(9)
print(str(f(6)))
`
		require.Equal(t, "15\n", run(t, src))
	})

	t.Run("deep nested function captures outer local", func(t *testing.T) {
		src := `def outer(base: int) -> Callable[[int], int]:
    def middle() -> Callable[[int], int]:
        def inner(x: int) -> int:
            return base + x
        return inner
    return middle()
f: Callable[[int], int] = outer(11)
print(str(f(4)))
`
		require.Equal(t, "15\n", run(t, src))
	})

	t.Run("nonlocal mutation shares boxed capture", func(t *testing.T) {
		src := `def counter() -> Callable[[], int]:
    n = 0
    def inc() -> int:
        nonlocal n
        n = n + 1
        return n
    return inc
c: Callable[[], int] = counter()
print(str(c()))
print(str(c()))
`
		require.Equal(t, "1\n2\n", run(t, src))
	})

	t.Run("nonlocal augmented assignment updates boxed capture", func(t *testing.T) {
		src := `def counter() -> Callable[[], int]:
    n = 0
    def inc() -> int:
        nonlocal n
        n += 2
        return n
    return inc
c: Callable[[], int] = counter()
print(str(c()))
print(str(c()))
`
		require.Equal(t, "2\n4\n", run(t, src))
	})

	t.Run("global rebinding from nested function", func(t *testing.T) {
		src := `x: int = 1
def setx() -> None:
    global x
    x = 9
setx()
print(str(x))
`
		require.Equal(t, "9\n", run(t, src))
	})

	t.Run("comprehensions", func(t *testing.T) {
		src := `xs: list[int] = [i * i for i in range(6) if i % 2 == 0]
print(str(xs[2]))
d: dict[str, int] = {str(i): i + 1 for i in range(2)}
print(str(d["1"]))
s: set[int] = {i for i in [1, 1, 2]}
print(str(len(s)))
`
		require.Equal(t, "16\n2\n2\n", run(t, src))
	})

	t.Run("comprehensions do not depend on sample literals", func(t *testing.T) {
		src := `xs: list[int] = [i + 10 for i in range(3)]
print(str(xs[2]))
d: dict[str, int] = {str(i): i for i in range(3)}
print(str(d["2"]))
s: set[int] = {i for i in [2, 2, 3]}
print(str(len(s)))
`
		require.Equal(t, "12\n2\n2\n", run(t, src))
	})

	t.Run("comprehension loop variable does not leak", func(t *testing.T) {
		var buf bytes.Buffer
		_, err := Compile(strings.NewReader("xs: list[int] = [i for i in range(3)]\nprint(str(i))\n"), WithOutput(&buf))
		require.Error(t, err)
		hasCode(t, err, token.UndefinedName)
	})
}

func hasOps(t *testing.T, constants []vmtypes.Value, ops ...instr.Opcode) {
	t.Helper()
	seen := map[instr.Opcode]bool{}
	for _, constant := range constants {
		fn, ok := constant.(*vmtypes.Function)
		if !ok {
			continue
		}
		for _, ins := range instr.Unmarshal(fn.Code) {
			seen[ins.Opcode()] = true
		}
	}
	for _, op := range ops {
		require.Truef(t, seen[op], "expected function constant to contain %s", op)
	}
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
		"for i in 5:\n    pass\n":                    token.NotIterable,
		"break\n":                                    token.SyntaxError,
		"continue\n":                                 token.SyntaxError,
		"for i in range(1.5):\n    pass\n":           token.TypeMismatch,
		"for i in range():\n    pass\n":              token.ArityMismatch,
		"a: int = 1\nfor i in range(0, 9, a):\n p\n": token.UnsupportedFeature,
		"for i in range(0, 9, 0):\n    pass\n":       token.SyntaxError,
		"x: int = 1 if True else \"a\"\n":            token.TypeMismatch,
		// M2 functions
		"return 1\n": token.SyntaxError,
		"def f(x: int) -> int:\n    return \"x\"\n":              token.TypeMismatch,
		"def f(x: int) -> int:\n    return x\nprint(f())\n":      token.ArityMismatch,
		"def f(x: int) -> int:\n    return x\nprint(f(\"x\"))\n": token.TypeMismatch,
		"def f(x: int) -> int:\n    pass\n":                      token.TypeMismatch,
		// M3 containers
		"xs: list[int] = []\nprint(xs[\"0\"])\n":                 token.TypeMismatch,
		"xs = []\n":                                              token.UnsupportedType,
		"xs: list[int] = [1, \"x\"]\n":                           token.TypeMismatch,
		"t: tuple[int, int] = (1, 2)\ni: int = 0\nprint(t[i])\n": token.UnsupportedFeature,
		"d: dict[list[int], int] = {}\n":                         token.UnsupportedType,
		// Closures and comprehensions
		"f = lambda x: x\n":                                   token.MissingAnnotation,
		"def f() -> None:\n    nonlocal x\n":                  token.NoBindingForNonlocal,
		"xs: list[int] = [i for i in range(3) if i]\n":        token.TypeMismatch,
		"s: set[list[int]] = {[1] for i in range(1)}\n":       token.UnsupportedType,
		"f: Callable[[int], int] = lambda x: x\nprint(f())\n": token.ArityMismatch,
	}
	for src, code := range cases {
		_, err := Compile(strings.NewReader(src), WithOutput(&bytes.Buffer{}))
		require.Errorf(t, err, "src=%q", src)
		hasCode(t, err, code)
	}
}

func TestCompileOptionDefaults(t *testing.T) {
	c := New(strings.NewReader("print(\"x\")\n"))
	require.NotNil(t, c)
	first, err := c.Compile()
	require.NoError(t, err)
	require.NotNil(t, first)
	second, err := c.Compile()
	require.NoError(t, err)
	require.NotNil(t, second)
	prog, err := Compile(strings.NewReader("print(\"x\")\n"), WithOutput(io.Discard), WithOptimizationLevel(optimize.O2))
	require.NoError(t, err)
	require.NotNil(t, prog)
}
