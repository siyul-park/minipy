// Benchmarks in this file measure compile and run cost for the corpus under
// testdata/benchmark. They are ordinary Go benchmarks: `go test ./...`
// never executes a Benchmark func without `-bench`, so this file adds no
// cost to routine verification; run it explicitly with
// `go test ./conformance -bench . -benchtime 1x -run '^$'` (the corpus
// programs run for seconds each under CPython, so a small -benchtime is
// appropriate; see docs/benchmarks.md for the cross-implementation numbers
// this file is not a substitute for).
package conformance

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/siyul-park/minipy/compiler"
	"github.com/siyul-park/minivm/interp"
)

// BenchmarkCompileCorpus measures Compile cost for each benchmark corpus
// program. Source is read before ResetTimer so file I/O is not attributed
// to compilation.
func BenchmarkCompileCorpus(b *testing.B) {
	for _, c := range loadBenchmarkCorpus(b) {
		b.Run(c.Name, func(b *testing.B) {
			source, err := os.ReadFile(c.Path)
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := compiler.Compile(bytes.NewReader(source), compiler.WithOutput(io.Discard)); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkRunCorpus measures steady-state execution cost for each benchmark
// corpus program: compiled once outside the timed loop, then a fresh
// interp.New/Run/Close per iteration, matching how a caller actually runs a
// compiled program repeatedly (compilation is a one-time cost, execution is
// not).
func BenchmarkRunCorpus(b *testing.B) {
	for _, c := range loadBenchmarkCorpus(b) {
		b.Run(c.Name, func(b *testing.B) {
			source, err := os.Open(c.Path)
			if err != nil {
				b.Fatal(err)
			}
			prog, err := compiler.Compile(source, compiler.WithOutput(io.Discard))
			source.Close()
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

// BenchmarkInterpreterNew isolates interp.New/Close's own per-iteration cost
// against a trivial program, so a reader comparing it to BenchmarkRunCorpus
// can see how much of a corpus program's reported time is interpreter setup
// versus the workload itself.
func BenchmarkInterpreterNew(b *testing.B) {
	prog, err := compiler.Compile(strings.NewReader("x = 1\n"), compiler.WithOutput(io.Discard))
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		vm := interp.New(prog)
		vm.Close()
	}
}

// loadBenchmarkCorpus loads testdata/benchmark the same way TestConformance
// loads testdata/conformance, failing the benchmark run (not a silent empty
// set) if the corpus cannot be read.
func loadBenchmarkCorpus(b *testing.B) []Case {
	b.Helper()
	cases, err := Load("testdata/benchmark")
	if err != nil {
		b.Fatal(err)
	}
	if len(cases) == 0 {
		b.Fatal("benchmark: no cases found under testdata/benchmark")
	}
	return cases
}
