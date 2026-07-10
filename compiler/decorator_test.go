package compiler

import (
	"testing"
	"testing/fstest"

	"github.com/siyul-park/minipy/token"
	"github.com/stretchr/testify/require"
)

func TestFunctionDecorators(t *testing.T) {
	t.Run("bare decorator with matching signature type-checks and applies", func(t *testing.T) {
		src := `def identity(f: Callable[[int], int]) -> Callable[[int], int]:
    return f

@identity
def add_one(x: int) -> int:
    return x + 1

print(str(add_one(4)))
`
		require.Empty(t, checkOnly(t, src))
		require.Equal(t, "5\n", run(t, src))
	})

	t.Run("wrapper decorator changes the observed result", func(t *testing.T) {
		src := `def double(f: Callable[[int], int]) -> Callable[[int], int]:
    def wrapper(x: int) -> int:
        return f(x) * 2
    return wrapper

@double
def add_one(x: int) -> int:
    return x + 1

print(str(add_one(4)))
`
		require.Equal(t, "10\n", run(t, src))
	})

	t.Run("factory decorator arguments are evaluated once at definition time", func(t *testing.T) {
		src := `log: str = ""

def make(tag: str) -> Callable[[Callable[[int], int]], Callable[[int], int]]:
    global log
    log = log + "make(" + tag + ")"
    def deco(f: Callable[[int], int]) -> Callable[[int], int]:
        return f
    return deco

@make("x")
def add_one(x: int) -> int:
    return x + 1

print(str(add_one(4)))
print(str(add_one(9)))
print(log)
`
		require.Equal(t, "5\n10\nmake(x)\n", run(t, src))
	})

	t.Run("stacked decorators evaluate top to bottom and apply bottom to top", func(t *testing.T) {
		src := `log: str = ""

def make(tag: str) -> Callable[[Callable[[int], int]], Callable[[int], int]]:
    global log
    log = log + "eval(" + tag + ")"
    def deco(f: Callable[[int], int]) -> Callable[[int], int]:
        global log
        log = log + "apply(" + tag + ")"
        return f
    return deco

@make("outer")
@make("inner")
def f(x: int) -> int:
    return x

print(log)
`
		require.Equal(t, "eval(outer)eval(inner)apply(inner)apply(outer)\n", run(t, src))
	})

	t.Run("module-qualified decorator and factory execute correctly", func(t *testing.T) {
		fsys := fstest.MapFS{
			"deco.py": {Data: []byte(`def identity(f: Callable[[int], int]) -> Callable[[int], int]:
    return f

def make(n: int) -> Callable[[Callable[[int], int]], Callable[[int], int]]:
    def deco(f: Callable[[int], int]) -> Callable[[int], int]:
        def wrapper(x: int) -> int:
            return f(x) + n
        return wrapper
    return deco
`)},
		}
		src := `import deco

@deco.identity
def add_one(x: int) -> int:
    return x + 1

@deco.make(10)
def add_two(x: int) -> int:
    return x + 2

print(str(add_one(4)))
print(str(add_two(4)))
`
		require.Equal(t, "5\n16\n", runFS(t, src, fsys))
	})

	t.Run("nested function decorator applies each time the enclosing def executes", func(t *testing.T) {
		src := `log: str = ""

def make() -> Callable[[Callable[[], int]], Callable[[], int]]:
    def deco(f: Callable[[], int]) -> Callable[[], int]:
        global log
        log = log + "apply"
        return f
    return deco

def outer() -> Callable[[], int]:
    @make()
    def inner() -> int:
        return 1
    return inner

outer()
outer()
print(log)
`
		require.Equal(t, "applyapply\n", run(t, src))
	})

	t.Run("decorated function with a union parameter cannot bypass the wrapper via specialization", func(t *testing.T) {
		src := `log: str = ""

def track(f: Callable[[int | str], str]) -> Callable[[int | str], str]:
    def wrapper(x: int | str) -> str:
        global log
        log = log + "call"
        return f(x)
    return wrapper

@track
def describe(x: int | str) -> str:
    if isinstance(x, int):
        return "int"
    return "str"

print(describe(1))
print(describe("a"))
print(log)
`
		require.Equal(t, "int\nstr\ncallcall\n", run(t, src))
	})

	t.Run("recursive calls resolve through the final decorated binding", func(t *testing.T) {
		src := `log: str = ""

def track(f: Callable[[int], int]) -> Callable[[int], int]:
    def wrapper(x: int) -> int:
        global log
        log = log + "c"
        return f(x)
    return wrapper

@track
def fact(n: int) -> int:
    if n <= 1:
        return 1
    return n * fact(n - 1)

print(str(fact(4)))
print(log)
`
		require.Equal(t, "24\ncccc\n", run(t, src))
	})
}

func TestFunctionDecoratorDiagnostics(t *testing.T) {
	t.Run("factory call argument type mismatch is reported through ordinary call rules", func(t *testing.T) {
		src := `def make(n: str) -> Callable[[Callable[[int], int]], Callable[[int], int]]:
    def deco(f: Callable[[int], int]) -> Callable[[int], int]:
        return f
    return deco

@make(5)
def f(x: int) -> int:
    return x
`
		code(t, checkOnly(t, src), token.TypeMismatch)
	})

	t.Run("factory returning a non-callable value is rejected", func(t *testing.T) {
		src := `def make(n: int) -> int:
    return n

@make(5)
def f(x: int) -> int:
    return x
`
		code(t, checkOnly(t, src), token.TypeMismatch)
	})

	t.Run("decorator returning a different signature is rejected", func(t *testing.T) {
		src := `def bad(f: Callable[[int], int]) -> Callable[[int], str]:
    def wrapper(x: int) -> str:
        return str(x)
    return wrapper

@bad
def f(x: int) -> int:
    return x
`
		code(t, checkOnly(t, src), token.TypeMismatch)
	})

	t.Run("a decorator with Any type is rejected", func(t *testing.T) {
		src := `d: Any = None

@d
def f(x: int) -> int:
    return x
`
		code(t, checkOnly(t, src), token.TypeMismatch)
	})

	t.Run("decorator resolved through an instance attribute is rejected", func(t *testing.T) {
		src := `@dataclass
class Box:
    d: int

box: Box = Box(1)

@box.d
def f(x: int) -> int:
    return x
`
		code(t, checkOnly(t, src), token.UnsupportedFeature)
	})

	t.Run("subscript decorator expression is rejected", func(t *testing.T) {
		src := `xs: list[int] = [1, 2]

@xs[0]
def f(x: int) -> int:
    return x
`
		code(t, checkOnly(t, src), token.UnsupportedFeature)
	})

	t.Run("boolop decorator expression is rejected", func(t *testing.T) {
		src := `a: bool = True
b: bool = False

@(a or b)
def f(x: int) -> int:
    return x
`
		code(t, checkOnly(t, src), token.UnsupportedFeature)
	})

	t.Run("a decorator referring to its own function is use-before-definition", func(t *testing.T) {
		src := `@f
def f(x: int) -> int:
    return x
`
		code(t, checkOnly(t, src), token.UseBeforeDefinition)
	})
}

func TestClassDecorators(t *testing.T) {
	t.Run("dataclass and dataclass call form behave identically", func(t *testing.T) {
		bare := `@dataclass
class Point:
    x: int
    y: int
p: Point = Point(1, 2)
print(str(p.x) + "," + str(p.y))
`
		call := `@dataclass()
class Point:
    x: int
    y: int
p: Point = Point(1, 2)
print(str(p.x) + "," + str(p.y))
`
		require.Empty(t, checkOnly(t, bare))
		require.Empty(t, checkOnly(t, call))
		want := run(t, bare)
		require.Equal(t, "1,2\n", want)
		require.Equal(t, want, run(t, call))
	})

	t.Run("dataclass options are rejected", func(t *testing.T) {
		src := `@dataclass(init=False)
class Point:
    x: int
`
		errs := checkOnly(t, src)
		code(t, errs, token.UnsupportedFeature)
		require.Contains(t, errs[0].Msg, "#32")
	})

	t.Run("non-dataclass class decorators are rejected", func(t *testing.T) {
		src := `@other
class C:
    pass
`
		errs := checkOnly(t, src)
		code(t, errs, token.UnsupportedFeature)
		require.Contains(t, errs[0].Msg, "#22")
	})
}

func TestClassKeywords(t *testing.T) {
	t.Run("metaclass keyword is rejected", func(t *testing.T) {
		src := `class C(metaclass=M):
    pass
`
		errs := checkOnly(t, src)
		require.Equal(t, 1, count(t, errs, token.UnsupportedFeature))
		require.Contains(t, errs[0].Msg, "#22")
	})

	t.Run("unknown class keyword is rejected", func(t *testing.T) {
		src := `class C(foo=1):
    pass
`
		errs := checkOnly(t, src)
		require.Equal(t, 1, count(t, errs, token.UnsupportedFeature))
		require.Contains(t, errs[0].Msg, "foo")
	})

	t.Run("dynamic class keywords are rejected", func(t *testing.T) {
		src := `opts: dict[str, int] = {}
class C(**opts):
    pass
`
		errs := checkOnly(t, src)
		require.Equal(t, 1, count(t, errs, token.UnsupportedFeature))
	})

	t.Run("multiple base classes are rejected", func(t *testing.T) {
		src := `class A:
    pass
class B:
    pass
class C(A, B):
    pass
`
		errs := checkOnly(t, src)
		require.Equal(t, 1, count(t, errs, token.UnsupportedFeature))
		require.Contains(t, errs[0].Msg, "#16")
	})
}
