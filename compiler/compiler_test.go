package compiler

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/siyul-park/minipy/parser"
	"github.com/siyul-park/minipy/token"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/interp"
	"github.com/siyul-park/minivm/optimize"
	"github.com/siyul-park/minivm/program"
	vmtypes "github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

type broken struct{}

func (broken) Read([]byte) (int, error) {
	return 0, io.ErrUnexpectedEOF
}

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

func code(t *testing.T, err error, want token.Code) {
	t.Helper()
	el, ok := err.(token.ErrorList)
	require.Truef(t, ok, "expected token.ErrorList, got %T", err)
	for _, e := range el {
		if e.Code == want {
			return
		}
	}
	require.Failf(t, "missing diagnostic", "expected diagnostic %s, got %v", want, err)
}

func count(t *testing.T, err error, want token.Code) int {
	t.Helper()
	el, ok := err.(token.ErrorList)
	require.Truef(t, ok, "expected token.ErrorList, got %T", err)
	count := 0
	for _, e := range el {
		if e.Code == want {
			count++
		}
	}
	return count
}

// checkOnly runs the parser and type checker without lowering, returning the
// accumulated diagnostics. It lets union/inference checks be tested before the
// codegen stage exists.
func checkOnly(t *testing.T, src string) token.ErrorList {
	t.Helper()
	mod, parseErr := parser.Parse(strings.NewReader(src))
	chk := newChecker()
	chk.check(mod)
	var errs token.ErrorList
	if pl, ok := parseErr.(token.ErrorList); ok {
		errs = append(errs, pl...)
	}
	return append(errs, chk.errs...)
}

func TestCompileUnions(t *testing.T) {
	t.Run("isinstance dispatch on a union runs", func(t *testing.T) {
		src := "def describe(x: int | str) -> str:\n" +
			"    if isinstance(x, int):\n" +
			"        return \"int:\" + str(x)\n" +
			"    return \"str:\" + x\n" +
			"print(describe(3))\n" +
			"print(describe(\"hi\"))\n"
		require.Equal(t, "int:3\nstr:hi\n", run(t, src))
	})

	t.Run("Optional narrowing with is not None runs", func(t *testing.T) {
		src := "def f(x: int | None) -> int:\n" +
			"    if x is not None:\n" +
			"        return x + 1\n" +
			"    return 0\n" +
			"v: int | None = 41\n" +
			"print(str(f(v)))\n" +
			"w: int | None = None\n" +
			"print(str(f(w)))\n"
		require.Equal(t, "42\n0\n", run(t, src))
	})

	t.Run("isinstance lowers to REF_TEST and narrowing to REF_CAST", func(t *testing.T) {
		src := "def describe(x: int | str) -> str:\n" +
			"    if isinstance(x, int):\n" +
			"        return str(x)\n" +
			"    return x\n" +
			"print(describe(1))\n"
		prog, err := Compile(strings.NewReader(src), WithOutput(&bytes.Buffer{}))
		require.NoError(t, err)
		ops(t, prog.Constants, instr.REF_TEST, instr.REF_CAST)
	})

	t.Run("concrete calls specialize isinstance branches", func(t *testing.T) {
		src := "def describe(x: int | str) -> str:\n" +
			"    if isinstance(x, int):\n" +
			"        return \"int:\" + str(x)\n" +
			"    return \"str:\" + x\n" +
			"print(describe(3))\n" +
			"print(describe(\"hi\"))\n"
		var buf bytes.Buffer
		prog, err := Compile(strings.NewReader(src), WithOutput(&buf))
		require.NoError(t, err)

		vm := interp.New(prog)
		defer vm.Close()
		require.NoError(t, vm.Run(context.Background()))
		require.Equal(t, "int:3\nstr:hi\n", buf.String())

		requireFuncParam(t, prog.Constants, vmtypes.TypeI64, false, instr.REF_TEST, instr.REF_CAST)
		requireFuncParam(t, prog.Constants, vmtypes.TypeString, false, instr.REF_TEST, instr.REF_CAST)
		requireFuncParam(t, prog.Constants, vmtypes.TypeRef, true, instr.REF_TEST, instr.REF_CAST)
	})

	t.Run("specialized forward function call runs", func(t *testing.T) {
		src := "def g() -> str:\n" +
			"    return describe(3)\n" +
			"def describe(x: int | str) -> str:\n" +
			"    if isinstance(x, int):\n" +
			"        return \"int:\" + str(x)\n" +
			"    return \"str:\" + x\n" +
			"print(g())\n"
		require.Equal(t, "int:3\n", run(t, src))
	})
}

func TestCompileInference(t *testing.T) {
	t.Run("unannotated function compiles and runs via inference", func(t *testing.T) {
		src := "def identity(x):\n" +
			"    return x\n" +
			"print(str(identity(3)))\n" +
			"print(identity(\"hi\"))\n"
		require.Equal(t, "3\nhi\n", run(t, src))
	})

	t.Run("unannotated concrete calls specialize by argument type", func(t *testing.T) {
		src := "def identity(x):\n" +
			"    return x\n" +
			"print(str(identity(3)))\n" +
			"print(identity(\"hi\"))\n"
		prog, err := Compile(strings.NewReader(src), WithOutput(io.Discard))
		require.NoError(t, err)

		requireFuncParam(t, prog.Constants, vmtypes.TypeI64, true)
		requireFuncParam(t, prog.Constants, vmtypes.TypeString, true)
		requireFuncParam(t, prog.Constants, vmtypes.TypeRef, true)
	})

	t.Run("inferred concrete return type", func(t *testing.T) {
		src := "def two():\n" +
			"    return 2\n" +
			"x = two()\n" +
			"print(str(x + 1))\n"
		require.Equal(t, "3\n", run(t, src))
	})

	t.Run("unannotated parameter narrowed with isinstance", func(t *testing.T) {
		src := "def kind(x):\n" +
			"    if isinstance(x, int):\n" +
			"        return \"int\"\n" +
			"    return \"other\"\n" +
			"print(kind(1))\n" +
			"print(kind(\"s\"))\n"
		require.Equal(t, "int\nother\n", run(t, src))
	})
}

func TestCheckUnions(t *testing.T) {
	t.Run("union annotation and isinstance narrowing type-check", func(t *testing.T) {
		errs := checkOnly(t, "def describe(x: int | str) -> str:\n"+
			"    if isinstance(x, int):\n"+
			"        return \"int:\" + str(x)\n"+
			"    return \"str:\" + x\n")
		require.Empty(t, errs)
	})

	t.Run("is-not-None narrowing on Optional", func(t *testing.T) {
		errs := checkOnly(t, "def f(x: int | None) -> int:\n"+
			"    if x is not None:\n"+
			"        return x\n"+
			"    return 0\n")
		require.Empty(t, errs)
	})

	t.Run("operating on an un-narrowed union is an error", func(t *testing.T) {
		errs := checkOnly(t, "def f(x: int | str) -> str:\n    return \"v:\" + x\n")
		require.NotEmpty(t, errs)
	})

	t.Run("global inference needs no annotation", func(t *testing.T) {
		require.Empty(t, checkOnly(t, "x = 5\nprint(str(x))\n"))
	})

	t.Run("isinstance arity is checked", func(t *testing.T) {
		errs := checkOnly(t, "x: int = 1\nprint(str(isinstance(x)))\n")
		require.NotEmpty(t, errs)
		require.Equal(t, token.ArityMismatch, errs[0].Code)
	})
}

func TestTypingAnnotations(t *testing.T) {
	t.Run("string forward references resolve without future import", func(t *testing.T) {
		errs := checkOnly(t, "class Node:\n    next: \"Node | None\"\nxs: list[\"Node\"] = []\n")
		require.Empty(t, errs)
	})

	t.Run("Annotated erases to base type", func(t *testing.T) {
		src := "from typing import Annotated\nx: Annotated[int, \"meta\"] = 2\nprint(str(x + 1))\n"
		require.Equal(t, "3\n", run(t, src))
	})

	t.Run("Literal accepts exact literals and erases to base type", func(t *testing.T) {
		src := "from typing import Literal\nx: Literal[1] = 1\ny: int = x\nprint(str(y + 1))\n"
		require.Equal(t, "2\n", run(t, src))
	})

	t.Run("Literal supports multiple string values", func(t *testing.T) {
		errs := checkOnly(t, "from typing import Literal\nx: Literal[\"a\", \"b\"] = \"b\"\n")
		require.Empty(t, errs)
	})

	t.Run("Literal works inside union hints", func(t *testing.T) {
		errs := checkOnly(t, "from typing import Literal\nx: Literal[1] | str = 1\ny: Literal[1] | str = \"ok\"\n")
		require.Empty(t, errs)
	})

	t.Run("typing module attribute annotations resolve", func(t *testing.T) {
		errs := checkOnly(t, "import typing\nx: typing.Literal[True] = True\ny: typing.Optional[int] = None\n")
		require.Empty(t, errs)
	})

	t.Run("TypeAlias annotated assignment declares alias", func(t *testing.T) {
		src := "from typing import TypeAlias\nVec: TypeAlias = list[int]\nxs: Vec = []\nprint(str(len(xs)))\n"
		require.Equal(t, "0\n", run(t, src))
	})

	t.Run("Literal rejects nonmatching literal", func(t *testing.T) {
		errs := checkOnly(t, "from typing import Literal\nx: Literal[1] = 2\n")
		require.NotEmpty(t, errs)
		require.Equal(t, token.TypeMismatch, errs[0].Code)
	})

	t.Run("typing symbols are annotation-only", func(t *testing.T) {
		_, err := Compile(strings.NewReader("from typing import Literal\nprint(Literal)\n"), WithOutput(&bytes.Buffer{}))
		require.Error(t, err)
		code(t, err, token.UnsupportedFeature)
	})

	t.Run("invalid typing annotations diagnose precisely", func(t *testing.T) {
		cases := map[string]token.Code{
			"x: \"Missing\" = None\n":                               token.UnsupportedType,
			"from typing import Literal\nx: Literal[[1]] = [1]\n":   token.UnsupportedType,
			"from typing import Annotated\nx: Annotated[int] = 1\n": token.UnsupportedType,
			"from typing import TypeAlias\nVec: TypeAlias = 1\n":    token.UnsupportedType,
			"from typing import TypeAlias\nVec: TypeAlias\n":        token.MissingAnnotation,
			"@dataclass\nclass Node:\n    next: \"Node\"\n":         token.UnsupportedType,
			"x: Literal[1] = 1\n":                                   token.UnsupportedType,
		}
		for src, want := range cases {
			errs := checkOnly(t, src)
			require.NotEmptyf(t, errs, "src=%q", src)
			require.Equalf(t, want, errs[0].Code, "src=%q", src)
		}
	})
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

	t.Run("chained comparison evaluates middle operand once", func(t *testing.T) {
		src := `calls: int = 0
def mid() -> int:
    global calls
    calls += 1
    return 2
def last() -> int:
    global calls
    calls += 10
    return 3
print(str(1 < mid() < 3))
print(str(calls))
calls = 0
print(str(3 < mid() < last()))
print(str(calls))
`
		require.Equal(t, "True\n1\nFalse\n1\n", run(t, src))
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

	t.Run("roadmap control-flow sample sums even numbers", func(t *testing.T) {
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

	t.Run("roadmap dict counting sample", func(t *testing.T) {
		src := `counts: dict[str, int] = {}
for w in ["a", "b", "a"]:
    counts[w] = counts.get(w, 0) + 1
print(str(counts["a"]))
`
		require.Equal(t, "2\n", run(t, src))
	})

	t.Run("containers and methods", func(t *testing.T) {
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

	t.Run("list search and mutation methods", func(t *testing.T) {
		src := `xs: list[int] = [10, 20, 10]
print(str(xs.index(10)))
print(str(xs.index(20)))
xs.insert(1, 15)
xs.insert(-99, 5)
xs.insert(99, 30)
print(str(len(xs)))
print(str(xs[0]))
print(str(xs[2]))
print(str(xs[5]))
ys: list[int] = [40, 50]
xs.extend(ys)
print(str(xs[7]))
ys.extend(ys)
print(str(len(ys)))
print(str(ys[2]))
xs.reverse()
print(str(xs[0]))
print(str(xs[7]))
`
		require.Equal(t, "0\n1\n6\n5\n15\n30\n50\n4\n40\n50\n5\n", run(t, src))
	})

	t.Run("list index missing raises ValueError", func(t *testing.T) {
		src := `xs: list[int] = [1, 2]
try:
    print(str(xs.index(3)))
except ValueError:
    print("missing")
`
		require.Equal(t, "missing\n", run(t, src))
	})

	t.Run("list slice assignment and deletion", func(t *testing.T) {
		src := `xs: list[int] = [1, 2, 3, 4]
xs[1:3] = [20, 30]
print(str(xs[0]))
print(str(xs[1]))
print(str(xs[2]))
print(str(xs[3]))
xs[-3:-1] = [200, 300]
print(str(xs[1]))
print(str(xs[2]))
ys: list[int] = [9, 8]
ys[:] = ys
print(str(ys[0]))
print(str(ys[1]))
del xs[:2]
print(str(len(xs)))
print(str(xs[0]))
print(str(xs[1]))
del xs[1:]
print(str(len(xs)))
del xs[-10:10]
print(str(len(xs)))
`
		require.Equal(t, "1\n20\n30\n4\n200\n300\n9\n8\n2\n300\n4\n1\n0\n", run(t, src))
	})

	t.Run("list slice assignment length mismatch raises ValueError", func(t *testing.T) {
		src := `xs: list[int] = [1, 2, 3]
try:
    xs[1:3] = [9]
except ValueError:
    print("mismatch")
`
		require.Equal(t, "mismatch\n", run(t, src))
	})

	t.Run("container membership indexing and mutable fields", func(t *testing.T) {
		src := `@dataclass
class Box:
    n: int
box: Box = Box(1)
box.n += 4
pair: tuple[int, str] = (box.n, "z")
a: int
b: str
a, b = pair
xs: list[int] = [a, 9]
d: dict[str, int] = {"x": a}
print(str(9 in xs))
print(str("x" in d))
print(str("e" in "hello"))
print("hello"[1])
print(str(len(d.values())))
print(str(bool(xs)))
print(str(a))
print(b)
`
		require.Equal(t, "True\nTrue\nTrue\ne\n1\nTrue\n5\nz\n", run(t, src))
	})

	t.Run("str methods enumerate zip and f-string", func(t *testing.T) {
		src := `print("A,B".lower())
print("a,b".split(",")[1])
print("-".join(["x", "y"]))
print(str("abc".find("b")))
for i, v in enumerate([4, 5]):
    print(str(i) + str(v))
for a, b in zip([1, 2], [3, 4]):
    print(str(a + b))
x: int = 7
print(f"x={x:03d}")
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

	t.Run("default parameters and keyword calls", func(t *testing.T) {
		src := `def mix(a: int, /, b: int = 2, *, c: int = 3) -> int:
    return a * 100 + b * 10 + c
print(str(mix(1)))
print(str(mix(1, 4)))
print(str(mix(1, c=8)))
print(str(mix(1, b=5, c=6)))
`
		require.Equal(t, "123\n143\n128\n156\n", run(t, src))
	})

	t.Run("walrus assigns and yields value", func(t *testing.T) {
		src := `if (n := 4) > 2:
    print(str(n))
print(str((m := n + 3)))
print(str(m))
`
		require.Equal(t, "4\n7\n7\n", run(t, src))
	})

	t.Run("list and string slicing", func(t *testing.T) {
		src := `xs: list[int] = [0, 1, 2, 3, 4, 5]
ys: list[int] = xs[1:5:2]
print(str(len(ys)))
print(str(ys[0]))
print(str(ys[1]))
print("abcdef"[1:5:2])
print("abcdef"[:3])
print("abcdef"[3:])
`
		require.Equal(t, "2\n1\n3\nbd\nabc\ndef\n", run(t, src))
	})

	t.Run("raise from evaluates cause and raises exception", func(t *testing.T) {
		src := `try:
    raise ValueError("outer") from ValueError("inner")
except ValueError:
    print("ok")
`
		require.Equal(t, "ok\n", run(t, src))
	})

	t.Run("starred assignment and display unpacking", func(t *testing.T) {
		src := `head, *middle, tail = [1, 2, 3, 4]
print(str(head))
print(str(len(middle)))
print(str(middle[0]))
print(str(middle[1]))
print(str(tail))
xs: list[int] = [0, *middle, 5]
print(str(len(xs)))
print(str(xs[2]))
d: dict[str, int] = {"a": 1}
e: dict[str, int] = {**d, "b": 2}
print(str(len(e)))
print(str(e["b"]))
s: set[int] = {1, 2}
t: set[int] = {*s, 3}
print(str(len(t)))
`
		require.Equal(t, "1\n2\n2\n3\n4\n4\n3\n2\n2\n3\n", run(t, src))
	})

	t.Run("static tuple unpack in list literal lowers without host helper", func(t *testing.T) {
		src := `xs: list[int] = [0, *(1, 2), 3]
`
		prog, err := Compile(strings.NewReader(src), WithOutput(io.Discard))
		require.NoError(t, err)
		require.Empty(t, hostConstants(prog.Constants))
		programOps(t, prog, instr.ARRAY_APPEND, instr.STRUCT_GET)
	})

	t.Run("starred tuple calls and keyword methods constructors", func(t *testing.T) {
		src := `def add(a: int, b: int, c: int = 5) -> int:
    return a + b + c
args: tuple[int, int] = (2, 3)
print(str(add(*args)))
@dataclass
class Point:
    x: int
    y: int
    def shift(self, dx: int, *, dy: int = 1) -> int:
        return self.x + dx + self.y + dy
p: Point = Point(y=4, x=3)
print(str(p.shift(10, dy=20)))
`
		require.Equal(t, "10\n37\n", run(t, src))
	})

	t.Run("keyword constructor type mismatch reports diagnostic", func(t *testing.T) {
		src := `@dataclass
class Point:
    x: int
p: Point = Point(x="bad")
`
		_, err := Compile(strings.NewReader(src), WithOutput(io.Discard))
		code(t, err, token.TypeMismatch)
	})

	t.Run("generator expressions and type aliases", func(t *testing.T) {
		src := `type Num = int
total: Num = 0
for value in (i * i for i in range(5) if i > 1):
    total = total + value
print(str(total))
`
		require.Equal(t, "29\n", run(t, src))
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
		ops(t, prog.Constants, instr.CLOSURE_NEW, instr.UPVAL_GET)
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

	t.Run("nonlocal assignment expression is checked once", func(t *testing.T) {
		src := `def outer() -> None:
    x = 0
    def inner() -> None:
        nonlocal x
        x = missing
`
		_, err := Compile(strings.NewReader(src), WithOutput(io.Discard))
		require.Error(t, err)
		require.Equal(t, 1, count(t, err, token.UndefinedName))
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
		code(t, err, token.UndefinedName)
	})

	t.Run("comprehension target does not overwrite outer binding", func(t *testing.T) {
		src := `x: int = 7
xs: list[int] = [1, 2]
ys: list[int] = [x for x in xs]
print(str(x))
print(str(ys[1]))
`
		require.Equal(t, "7\n2\n", run(t, src))
	})

	t.Run("generator roadmap sample", func(t *testing.T) {
		src := `def upto(n: int) -> Iterator[int]:
    i: int = 0
    while i < n:
        yield i
        i = i + 1
total: int = 0
for v in upto(5):
    total = total + v
print(str(total))
`
		require.Equal(t, "10\n", run(t, src))
	})

	t.Run("generator loop control and else", func(t *testing.T) {
		src := `def upto(n: int) -> Iterator[int]:
    i: int = 0
    while i < n:
        yield i
        i = i + 1
total: int = 0
for v in upto(5):
    if v == 1:
        continue
    if v == 4:
        break
    total = total + v
else:
    total = 99
print(str(total))
`
		require.Equal(t, "5\n", run(t, src))
	})

	t.Run("range value and direct next", func(t *testing.T) {
		src := `r: Iterator[int] = range(2, 8, 3)
print(str(next(r)))
print(str(next(r)))
`
		require.Equal(t, "2\n5\n", run(t, src))
	})

	t.Run("exhausted next reports runtime error", func(t *testing.T) {
		prog, err := Compile(strings.NewReader("r: Iterator[int] = range(1)\nprint(str(next(r)))\nprint(str(next(r)))\n"), WithOutput(io.Discard))
		require.NoError(t, err)
		vm := interp.New(prog)
		defer vm.Close()
		require.Error(t, vm.Run(context.Background()))
	})

	t.Run("iter over list dict set and str", func(t *testing.T) {
		src := `xs: list[int] = [4, 5]
d: dict[str, int] = {"a": 1}
s: set[int] = {7}
text: str = "xy"
print(str(next(iter(xs))))
print(next(iter(d)))
print(str(next(iter(s))))
print(next(iter(text)))
`
		require.Equal(t, "4\na\n7\nx\n", run(t, src))
	})

	t.Run("dict and set loops use map iterator", func(t *testing.T) {
		src := `d: dict[str, int] = {"a": 1}
s: set[int] = {2}
for k in d:
    print(k)
for v in s:
    print(str(v))
`
		prog, err := Compile(strings.NewReader(src), WithOutput(io.Discard))
		require.NoError(t, err)
		require.True(t, opcode(prog, instr.MAP_ITER))
		require.False(t, opcode(prog, instr.MAP_KEYS))
	})

	t.Run("dict and set comprehensions use map iterator", func(t *testing.T) {
		src := `d: dict[str, int] = {"a": 1}
s: set[int] = {2}
ks: list[str] = [k for k in d]
vs: set[int] = {v for v in s}
print(ks[0])
print(str(len(vs)))
`
		prog, err := Compile(strings.NewReader(src), WithOutput(io.Discard))
		require.NoError(t, err)
		require.True(t, opcode(prog, instr.MAP_ITER))
		require.False(t, opcode(prog, instr.MAP_KEYS))
	})

	t.Run("nested generator captures outer local", func(t *testing.T) {
		src := `def outer(base: int) -> Iterator[int]:
    def inner() -> Iterator[int]:
        yield base + 1
    for v in inner():
        yield v
print(str(next(outer(9))))
`
		require.Equal(t, "10\n", run(t, src))
	})

	t.Run("class explicit init fields and method", func(t *testing.T) {
		src := `class Point:
    x: int
    y: int
    def __init__(self, x: int, y: int) -> None:
        self.x = x
        self.y = y
    def norm2(self) -> int:
        return self.x * self.x + self.y * self.y
print(str(Point(3, 4).norm2()))
`
		require.Equal(t, "25\n", run(t, src))
	})

	t.Run("inherited method call", func(t *testing.T) {
		src := `class Base:
    def value(self) -> int:
        return 3
class Child(Base):
    pass
print(str(Child().value()))
`
		require.Equal(t, "3\n", run(t, src))
	})

	t.Run("dataclass constructor defaults and inherited fields", func(t *testing.T) {
		src := `@dataclass
class Base:
    x: int
    y: int = 5
@dataclass
class Point(Base):
    z: int = 7
    def total(self) -> int:
        return self.x + self.y + self.z
p: Point = Point(3)
print(str(p.x))
print(str(p.total()))
`
		require.Equal(t, "3\n15\n", run(t, src))
	})

	t.Run("del dict key and list item", func(t *testing.T) {
		require.Equal(t, "1\n", run(t, "d: dict[str, int] = {\"a\": 1, \"b\": 2}\ndel d[\"a\"]\nprint(str(len(d)))\n"))
		require.Equal(t, "2\n3\n", run(t, "xs: list[int] = [1, 2, 3]\ndel xs[1]\nprint(str(len(xs)))\nprint(str(xs[1]))\n"))
	})

	t.Run("del attribute zeroes the field", func(t *testing.T) {
		src := "@dataclass\nclass P:\n    x: int\n    y: int\np: P = P(1, 2)\ndel p.x\nprint(str(p.x))\n"
		require.Equal(t, "0\n", run(t, src))
	})

	t.Run("del then reassign", func(t *testing.T) {
		require.Equal(t, "9\n", run(t, "n: int = 5\ndel n\nn = 9\nprint(str(n))\n"))
	})

	t.Run("assert passes silently", func(t *testing.T) {
		require.Equal(t, "ok\n", run(t, "assert True\nprint(\"ok\")\n"))
	})

	t.Run("assert false raises uncaught", func(t *testing.T) {
		prog, err := Compile(strings.NewReader("assert False, \"boom\"\n"), WithOutput(&bytes.Buffer{}))
		require.NoError(t, err)
		vm := interp.New(prog)
		defer vm.Close()
		require.Error(t, vm.Run(context.Background()))
	})

	t.Run("match literals and or-pattern", func(t *testing.T) {
		src := `def kind(n: int) -> str:
    match n:
        case 0:
            return "zero"
        case 1 | 2 | 3:
            return "small"
    return "big"
print(kind(0))
print(kind(2))
print(kind(9))
`
		require.Equal(t, "zero\nsmall\nbig\n", run(t, src))
	})

	t.Run("match tuple, guard, and as", func(t *testing.T) {
		src := `pt: tuple[int, int] = (1, 5)
match pt:
    case (a, b) as whole if a < b:
        print(str(a))
        print(str(b))
`
		require.Equal(t, "1\n5\n", run(t, src))
	})

	t.Run("match list with star capture", func(t *testing.T) {
		src := `xs: list[int] = [1, 2, 3, 4]
match xs:
    case [first, *rest]:
        print(str(first))
        print(str(len(rest)))
        print(str(rest[0]))
`
		require.Equal(t, "1\n3\n2\n", run(t, src))
	})

	t.Run("match mapping with rest capture", func(t *testing.T) {
		src := `d: dict[str, int] = {"a": 1, "b": 2, "c": 3}
match d:
    case {"a": x, **rest}:
        print(str(x))
        print(str(len(rest)))
`
		require.Equal(t, "1\n2\n", run(t, src))
	})

	t.Run("match tuple with star capture", func(t *testing.T) {
		src := `t: tuple[int, int, int, int] = (1, 2, 3, 4)
match t:
    case (first, *rest):
        print(str(first))
        print(str(len(rest)))
        print(str(rest[0]))
        print(str(rest[2]))
`
		require.Equal(t, "1\n3\n2\n4\n", run(t, src))
	})

	t.Run("match tuple star between prefix and suffix", func(t *testing.T) {
		src := `t: tuple[int, int, int, int] = (1, 2, 3, 4)
match t:
    case (a, *mid, z):
        print(str(a))
        print(str(z))
        print(str(len(mid)))
        print(str(mid[0]))
`
		require.Equal(t, "1\n4\n2\n2\n", run(t, src))
	})

	t.Run("match tuple star with no binding", func(t *testing.T) {
		src := `t: tuple[int, int, int] = (1, 2, 3)
match t:
    case (a, *_, z):
        print(str(a))
        print(str(z))
`
		require.Equal(t, "1\n3\n", run(t, src))
	})

	t.Run("match class pattern with positional and keyword", func(t *testing.T) {
		src := `@dataclass
class Point:
    x: int
    y: int
p: Point = Point(3, 4)
match p:
    case Point(x, y=4):
        print(str(x))
`
		require.Equal(t, "3\n", run(t, src))
	})

	t.Run("roadmap safe div catches vm trap", func(t *testing.T) {
		src := `def safe_div(a: int, b: int) -> int:
    try:
        return a // b
    except ZeroDivisionError:
        return 0
print(str(safe_div(1, 0)))
print(str(safe_div(6, 2)))
`
		require.Equal(t, "0\n3\n", run(t, src))
	})

	t.Run("guest raise matches type and superclass", func(t *testing.T) {
		src := `def f(n: int) -> str:
    try:
        if n == 0:
            raise ValueError("bad")
        raise KeyError("key")
    except ValueError as e:
        return "value"
    except Exception:
        return "exception"
print(f(0))
print(f(1))
`
		require.Equal(t, "value\nexception\n", run(t, src))
	})

	t.Run("custom exception subclass catches as base", func(t *testing.T) {
		src := `class MyError(Exception):
    pass
try:
    raise MyError("mine")
except Exception:
    print("caught")
`
		require.Equal(t, "caught\n", run(t, src))
	})

	t.Run("finally runs on normal exception and return", func(t *testing.T) {
		src := `def normal() -> None:
    try:
        print("body")
    finally:
        print("finally-normal")
def caught() -> None:
    try:
        raise ValueError("x")
    except ValueError:
        print("caught")
    finally:
        print("finally-exception")
def ret() -> int:
    try:
        return 7
    finally:
        print("finally-return")
normal()
caught()
print(str(ret()))
`
		require.Equal(t, "body\nfinally-normal\ncaught\nfinally-exception\nfinally-return\n7\n", run(t, src))
	})

	t.Run("with enter exit normal and exception", func(t *testing.T) {
		src := `class Ctx:
    def __enter__(self) -> int:
        print("enter")
        return 9
    def __exit__(self) -> None:
        print("exit")
with Ctx() as value:
    print(str(value))
try:
    with Ctx():
        raise ValueError("boom")
except ValueError:
    print("caught")
`
		require.Equal(t, "enter\n9\nexit\nenter\nexit\ncaught\n", run(t, src))
	})

	t.Run("unmatched exception reraises", func(t *testing.T) {
		prog, err := Compile(strings.NewReader(`try:
    raise ValueError("x")
except KeyError:
    print("wrong")
`), WithOutput(io.Discard))
		require.NoError(t, err)
		vm := interp.New(prog)
		defer vm.Close()
		require.Error(t, vm.Run(context.Background()))
	})

	t.Run("identity comparisons", func(t *testing.T) {
		src := `x: None = None
y: None = None
print(str(x is None))
print(str(x is not y))
`
		require.Equal(t, "True\nFalse\n", run(t, src))
	})

	t.Run("try else and bare reraises", func(t *testing.T) {
		src := `try:
    print("body")
except ValueError:
    print("except")
else:
    print("else")
finally:
    print("finally")
try:
    try:
        raise ValueError("x")
    except ValueError as e:
        raise
except ValueError:
    print("reraised")
`
		require.Equal(t, "body\nelse\nfinally\nreraised\n", run(t, src))
	})

	t.Run("finally assignment is definitely initialized", func(t *testing.T) {
		src := `x: int
try:
    pass
finally:
    x = 5
print(str(x))
`
		require.Equal(t, "5\n", run(t, src))
	})

	t.Run("finally runs before loop break and continue", func(t *testing.T) {
		src := `i: int = 0
while i < 3:
    i = i + 1
    try:
        if i == 1:
            continue
        if i == 2:
            break
    finally:
        print("finally")
print(str(i))
`
		require.Equal(t, "finally\nfinally\n2\n", run(t, src))
	})

	t.Run("with multiple context managers nests exits", func(t *testing.T) {
		src := `class Ctx:
    label: str
    def __init__(self, label: str) -> None:
        self.label = label
    def __enter__(self) -> str:
        print("enter " + self.label)
        return self.label
    def __exit__(self) -> None:
        print("exit " + self.label)
with Ctx("a") as a, Ctx("b") as b:
    print(a + b)
`
		require.Equal(t, "enter a\nenter b\nab\nexit b\nexit a\n", run(t, src))
	})

	t.Run("compiler reused across multiple Compile calls with default optimization level", func(t *testing.T) {
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
	})

	t.Run("read error is returned from Compile", func(t *testing.T) {
		_, err := New(broken{}).Compile()
		require.Error(t, err)
		require.ErrorContains(t, err, "read source")
	})
}

func TestCompileFString(t *testing.T) {
	t.Run("debug, conversion, and nested format specs run", func(t *testing.T) {
		cases := []struct {
			src  string
			want string
		}{
			{"x: int = 5\nprint(f\"{x=}\")", "x=5\n"},
			{"x: int = 5\nprint(f\"{x = }\")", "x = 5\n"},
			{"x: int = 5\nprint(f\"{x=!s}\")", "x=5\n"},
			{"x: int = 5\nprint(f\"{x=:03d}\")", "x=005\n"},
			{"s: str = \"hi\"\nprint(f\"{s=}\")", "s='hi'\n"},
			{"x: int = 5\nprint(f\"{x!s}\")", "5\n"},
			{"x: int = 5\nprint(f\"{x!r}\")", "5\n"},
			{"s: str = \"hi\"\nprint(f\"{s!r}\")", "'hi'\n"},
			{"s: str = \"hi\"\nprint(f\"{s!a}\")", "'hi'\n"},
			{"x: float = 3.14159\nw: int = 8\np: int = 2\nprint(f\"{x:{w}.{p}f}\")", "    3.14\n"},
			{"x: int = 42\nprint(f\"{x:>6}\")", "    42\n"},
			{"x: int = 42\nprint(f\"{x:<6}|\")", "42    |\n"},
			{"x: int = 42\nprint(f\"{x:^6}|\")", "  42  |\n"},
			{"x: int = 255\nprint(f\"{x:x}\")", "ff\n"},
			{"x: float = 1.5\nprint(f\"{x:+.2f}\")", "+1.50\n"},
			{"x: int = 5\nprint(f\"{{literal}} {x}\")", "{literal} 5\n"},
		}
		for _, tc := range cases {
			require.Equalf(t, tc.want, run(t, tc.src), "src: %s", tc.src)
		}
	})

	t.Run("nested format specs preserve left-to-right evaluation order", func(t *testing.T) {
		src := `log: str = ""
def w() -> int:
    global log
    log = log + "w"
    return 6
def p() -> int:
    global log
    log = log + "p"
    return 2
x: float = 3.14159
print(f"{x:{w()}.{p()}f}")
print(log)
`
		require.Equal(t, "  3.14\nwp\n", run(t, src))
	})

	t.Run("unsupported conversion is rejected", func(t *testing.T) {
		src := "x: int = 5\nprint(f\"{x!z}\")\n"
		_, err := Compile(strings.NewReader(src), WithOutput(io.Discard))
		code(t, err, token.UnsupportedFeature)
	})

	t.Run("deeply nested format spec is rejected", func(t *testing.T) {
		src := "x: int = 5\nw: int = 4\nprint(f\"{x:{w:{w}}}\")\n"
		_, err := Compile(strings.NewReader(src), WithOutput(io.Discard))
		code(t, err, token.UnsupportedFeature)
	})
}

func opcode(prog *program.Program, op instr.Opcode) bool {
	for _, ins := range instr.Unmarshal(prog.Code) {
		if ins.Opcode() == op {
			return true
		}
	}
	for _, constant := range prog.Constants {
		function, ok := constant.(*vmtypes.Function)
		if !ok {
			continue
		}
		for _, ins := range instr.Unmarshal(function.Code) {
			if ins.Opcode() == op {
				return true
			}
		}
	}
	return false
}

func ops(t *testing.T, constants []vmtypes.Value, ops ...instr.Opcode) {
	t.Helper()
	seen := map[instr.Opcode]bool{}
	for _, constant := range constants {
		function, ok := constant.(*vmtypes.Function)
		if !ok {
			continue
		}
		for _, ins := range instr.Unmarshal(function.Code) {
			seen[ins.Opcode()] = true
		}
	}
	for _, op := range ops {
		require.Truef(t, seen[op], "expected function constant to contain %s", op)
	}
}

func programOps(t *testing.T, prog *program.Program, ops ...instr.Opcode) {
	t.Helper()
	seen := map[instr.Opcode]bool{}
	for _, ins := range instr.Unmarshal(prog.Code) {
		seen[ins.Opcode()] = true
	}
	for _, op := range ops {
		require.Truef(t, seen[op], "expected program code to contain %s", op)
	}
}

func hostConstants(constants []vmtypes.Value) []*interp.HostFunction {
	var out []*interp.HostFunction
	for _, constant := range constants {
		if host, ok := constant.(*interp.HostFunction); ok {
			out = append(out, host)
		}
	}
	return out
}

func requireFuncParam(t *testing.T, constants []vmtypes.Value, parameter vmtypes.Type, wantOps bool, ops ...instr.Opcode) {
	t.Helper()
	for _, constant := range constants {
		function, ok := constant.(*vmtypes.Function)
		if !ok || len(function.Typ.Params) != 1 || !function.Typ.Params[0].Equals(parameter) {
			continue
		}
		if len(ops) == 0 {
			return
		}
		seen := map[instr.Opcode]bool{}
		for _, ins := range instr.Unmarshal(function.Code) {
			seen[ins.Opcode()] = true
		}
		for _, op := range ops {
			require.Equalf(t, wantOps, seen[op], "function with parameter %s opcode %s", parameter, op)
		}
		return
	}
	require.Failf(t, "missing function constant", "expected function constant with parameter %s", parameter)
}

func TestCompileErrors(t *testing.T) {
	cases := map[string]token.Code{
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
		// control flow
		"x: int = 1\nif x:\n    pass\n":                           token.TypeMismatch,
		"for i in 5:\n    pass\n":                                 token.NotIterable,
		"break\n":                                                 token.SyntaxError,
		"continue\n":                                              token.SyntaxError,
		"for i in range(1.5):\n    pass\n":                        token.TypeMismatch,
		"for i in range():\n    pass\n":                           token.ArityMismatch,
		"for i in range(0, 9, 0):\n    pass\n":                    token.SyntaxError,
		"x: int = 1 if True else \"a\"\n":                         token.TypeMismatch,
		"b: bool = False\nif b:\n    x: int = 1\nprint(str(x))\n": token.UseBeforeDefinition,
		// functions
		"return 1\n": token.SyntaxError,
		"def f(x: int) -> int:\n    return \"x\"\n":              token.TypeMismatch,
		"def f(x: int) -> int:\n    return x\nprint(f())\n":      token.ArityMismatch,
		"def f(x: int) -> int:\n    return x\nprint(f(\"x\"))\n": token.TypeMismatch,
		"def f(x: int) -> int:\n    pass\n":                      token.TypeMismatch,
		"def f(x: int) -> int:\n    try:\n        if x == 0:\n            raise ValueError(\"bad\")\n        return 1\n    except ValueError:\n        pass\n": token.TypeMismatch,
		// containers
		"xs: list[int] = []\nprint(xs[\"0\"])\n":                        token.TypeMismatch,
		"xs: list[int] = [1]\nxs[0] += 1\n":                             token.UnsupportedFeature,
		"xs = []\n":                                                     token.UnsupportedType,
		"xs: list[int] = [1, \"x\"]\n":                                  token.TypeMismatch,
		"xs: list[int] = [1]\nprint(str(xs.index(\"x\")))\n":            token.TypeMismatch,
		"xs: list[int] = [1]\nxs.insert(0, \"x\")\n":                    token.TypeMismatch,
		"xs: list[int] = [1]\nys: list[str] = [\"x\"]\nxs.extend(ys)\n": token.TypeMismatch,
		"xs: list[int] = [1]\nxs.reverse(1)\n":                          token.ArityMismatch,
		"xs: list[int] = [1]\nxs[0:1] = [\"x\"]\n":                      token.TypeMismatch,
		"xs: list[int] = [1]\nxs[0:\"x\"] = [1]\n":                      token.TypeMismatch,
		"xs: list[int] = [1]\nxs[::2] = [1]\n":                          token.UnsupportedFeature,
		"xs: list[int] = [1]\ndel xs[::2]\n":                            token.UnsupportedFeature,
		"\"abc\"[1:2] = \"x\"\n":                                        token.UnsupportedFeature,
		"(1, 2, 3)[1:2] = [9]\n":                                        token.UnsupportedFeature,
		"t: tuple[int, int] = (1, 2)\ni: int = 0\nprint(t[i])\n":        token.UnsupportedFeature,
		"d: dict[list[int], int] = {}\n":                                token.UnsupportedType,
		// Closures and comprehensions
		"f = lambda x: x\n":                                   token.MissingAnnotation,
		"def f() -> None:\n    nonlocal x\n":                  token.NoBindingForNonlocal,
		"xs: list[int] = [i for i in range(3) if i]\n":        token.TypeMismatch,
		"s: set[list[int]] = {[1] for i in range(1)}\n":       token.UnsupportedType,
		"f: Callable[[int], int] = lambda x: x\nprint(f())\n": token.ArityMismatch,
		// generators and iterators
		"yield 1\n":                                                token.SyntaxError,
		"def g() -> int:\n    yield 1\n":                           token.TypeMismatch,
		"def g() -> Iterator[int]:\n    yield \"x\"\n":             token.TypeMismatch,
		"def g() -> Iterator[int]:\n    return 1\n":                token.TypeMismatch,
		"def g() -> Iterator[int]:\n    yield 1\nprint(next(1))\n": token.TypeMismatch,
		// classes
		"@dataclass\nclass Point:\n    x: int\nprint(Point(\"x\"))\n":                          token.TypeMismatch,
		"@dataclass\nclass Point:\n    x: int\np: Point = Point(1)\np.x = \"x\"\n":             token.TypeMismatch,
		"@dataclass\nclass Point:\n    x: int\np: Point = Point(1)\nprint(p.y)\n":              token.UndefinedName,
		"@dataclass\nclass Point:\n    x: int\np: Point = Point(1)\nprint(p.missing())\n":      token.UnsupportedFeature,
		"class Point:\n    x: int\n    def __init__(self, x: int) -> int:\n        return x\n": token.TypeMismatch,
		// M9: del, assert, match
		"n: int = 1\ndel n\nprint(str(n))\n": token.UseBeforeDefinition,
		"assert 1\n":                         token.TypeMismatch,
		"x: int = 1\nmatch x:\n    case 1 if 2:\n        pass\n":                                  token.TypeMismatch,
		"v: int = 0\nmatch v:\n    case 1 as x:\n        pass\nprint(str(x))\n":                   token.UseBeforeDefinition,
		"v: tuple[int, str] = (1, \"a\")\nmatch v:\n    case (x, _) | (_, x):\n        pass\n":    token.PatternError,
		"v: tuple[int, str, int] = (1, \"a\", 2)\nmatch v:\n    case (a, *rest):\n        pass\n": token.TypeMismatch,
		"s: str = \"a\"\nmatch s:\n    case 1:\n        pass\n":                                   token.PatternError,
		// Exceptions and context managers
		"try:\n    pass\nexcept int:\n    pass\n": token.TypeMismatch,
		"raise\n":   token.SyntaxError,
		"raise 1\n": token.TypeMismatch,
		"class C:\n    pass\nwith C():\n    pass\n":                 token.UnsupportedFeature,
		"x: int = 1\nprint(str(x is 1))\n":                          token.TypeMismatch,
		"try:\n    x = 1\nexcept ValueError:\n    pass\nprint(x)\n": token.UseBeforeDefinition,
		// Python 3.13 parse-only forms
		"import math\n":                                    token.ModuleNotFound,
		"from math import sqrt\n":                          token.ModuleNotFound,
		"async def f():\n    return None\n":                token.UnsupportedFeature,
		"async for x in xs:\n    pass\n":                   token.UnsupportedFeature,
		"async with cm:\n    pass\n":                       token.UnsupportedFeature,
		"def f(x):\n    return await x\n":                  token.UnsupportedFeature,
		"def f(*a: int, *b: int):\n    return None\n":      token.SyntaxError,
		"def f(*xs: int):\n    return None\nf(1, \"a\")\n": token.TypeMismatch,
		"xs = [*ys]\n":                                     token.UndefinedName,
		"g = (x for x in xs)\n":                            token.UndefinedName,
		"print(str(1 @ 2))\n":                              token.UnsupportedFeature,
		"try:\n    pass\nexcept* ValueError:\n    pass\n":  token.UnsupportedFeature,
	}
	for src, want := range cases {
		_, err := Compile(strings.NewReader(src), WithOutput(&bytes.Buffer{}))
		require.Errorf(t, err, "src=%q", src)
		code(t, err, want)
	}
}

// TestCompileBoolI1 pins down that Python bool is a uniformly i1-kinded value
// across literals, comparisons, membership, conversions, and dict keys — the
// value renders as True/False (KindI1) rather than leaking an i32 as None.
func TestCompileBoolI1(t *testing.T) {
	cases := map[string]string{
		"bool literal":        "print(str(True))\nprint(str(False))\n",
		"comparison":          "print(str(1 == 1))\nprint(str(1 < 0))\n",
		"list membership":     "print(str(2 in [1, 2]))\nprint(str(3 in [1, 2]))\n",
		"list not in":         "print(str(3 not in [1, 2]))\n",
		"str membership":      "print(str('b' in 'abc'))\n",
		"dict membership":     "d: dict[str, int] = {'x': 1}\nprint(str('x' in d))\nprint(str('y' in d))\n",
		"bool of empty tuple": "print(str(bool(())))\n",
		"bool of int":         "print(str(bool(0)))\nprint(str(bool(5)))\n",
		"bool dict key":       "d: dict[bool, int] = {True: 1, False: 2}\nprint(str(d[True]))\nprint(str(d[False]))\n",
	}
	want := map[string]string{
		"bool literal":        "True\nFalse\n",
		"comparison":          "True\nFalse\n",
		"list membership":     "True\nFalse\n",
		"list not in":         "True\n",
		"str membership":      "True\n",
		"dict membership":     "True\nFalse\n",
		"bool of empty tuple": "False\n",
		"bool of int":         "False\nTrue\n",
		"bool dict key":       "1\n2\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, want[name], run(t, src))
		})
	}
}

// TestCompileVariadic covers variadic *args / **kwargs parameters and their
// interaction with positional, keyword-only, and default arguments (#9).
func TestCompileVariadic(t *testing.T) {
	cases := map[string]string{
		"args collects surplus positionals": "def f(*xs: int) -> int:\n" +
			"    total: int = 0\n    for x in xs:\n        total = total + x\n    return total\n" +
			"print(str(f(1, 2, 3)))\nprint(str(f()))\n",
		"kwargs collects surplus keywords": "def g(**kw: int) -> int:\n    return len(kw)\n" +
			"print(str(g(a=1, b=2)))\nprint(str(g()))\n",
		"positional then args": "def h(a: int, *rest: int) -> int:\n    return a + len(rest)\n" +
			"print(str(h(10, 1, 2)))\nprint(str(h(10)))\n",
		"keyword only after bare star": "def k(a: int, *, b: int) -> int:\n    return a + b\n" +
			"print(str(k(1, b=2)))\n",
		"defaults with args": "def d(a: int, b: int = 5, *rest: int) -> int:\n    return a + b + len(rest)\n" +
			"print(str(d(1)))\nprint(str(d(1, 2)))\nprint(str(d(1, 2, 9, 9)))\n",
		"args and kwargs together": "def m(a: int, *rest: int, **kw: int) -> int:\n" +
			"    return a + len(rest) + len(kw)\n" +
			"print(str(m(1, 2, 3, x=4)))\n",
		"static tuple unpacks into fixed arity": "def p(a: int, b: int) -> int:\n    return a + b\n" +
			"t: tuple[int, int] = (3, 4)\nprint(str(p(*t)))\n",
		"exception keyword argument": "try:\n    raise ValueError(message=\"boom\")\nexcept ValueError as e:\n    print(e.message)\n",
		"exception default argument": "try:\n    raise ValueError()\nexcept ValueError as e:\n    print(str(len(e.message)))\n",
	}
	want := map[string]string{
		"args collects surplus positionals":     "6\n0\n",
		"kwargs collects surplus keywords":      "2\n0\n",
		"positional then args":                  "12\n10\n",
		"keyword only after bare star":          "3\n",
		"defaults with args":                    "6\n3\n5\n",
		"args and kwargs together":              "4\n",
		"static tuple unpacks into fixed arity": "7\n",
		"exception keyword argument":            "boom\n",
		"exception default argument":            "0\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, want[name], run(t, src))
		})
	}
}
