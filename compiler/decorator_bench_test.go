package compiler

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/siyul-park/minivm/interp"
)

// BenchmarkCompileFunctionDecorator compares compile cost for an undecorated
// function against identity- and factory-decorated forms, to confirm
// decorator lowering does not regress the undecorated fast path.
func BenchmarkCompileFunctionDecorator(b *testing.B) {
	cases := map[string]string{
		"undecorated": `def f(x: int) -> int:
    return x + 1
`,
		"identity": `def identity(f: Callable[[int], int]) -> Callable[[int], int]:
    return f

@identity
def f(x: int) -> int:
    return x + 1
`,
		"factory": `def make(n: int) -> Callable[[Callable[[int], int]], Callable[[int], int]]:
    def deco(f: Callable[[int], int]) -> Callable[[int], int]:
        def wrapper(x: int) -> int:
            return f(x) + n
        return wrapper
    return deco

@make(1)
def f(x: int) -> int:
    return x + 1
`,
	}
	for name, src := range cases {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := Compile(strings.NewReader(src), WithOutput(io.Discard)); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkRunIdentityDecorator compares repeated-call steady-state cost
// between an undecorated function and one wrapped by an identity decorator,
// to confirm the decorator infrastructure itself does not add per-call
// allocations.
func BenchmarkRunIdentityDecorator(b *testing.B) {
	cases := map[string]string{
		"undecorated": `def f(x: int) -> int:
    return x + 1
total: int = 0
for i in range(1000):
    total = total + f(i)
print(str(total))
`,
		"identity": `def identity(f: Callable[[int], int]) -> Callable[[int], int]:
    return f

@identity
def f(x: int) -> int:
    return x + 1

total: int = 0
for i in range(1000):
    total = total + f(i)
print(str(total))
`,
	}
	for name, src := range cases {
		b.Run(name, func(b *testing.B) {
			prog, err := Compile(strings.NewReader(src), WithOutput(io.Discard))
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				vm := interp.New(prog)
				if err := vm.Run(context.Background()); err != nil {
					b.Fatal(err)
				}
				vm.Close()
			}
		})
	}
}
