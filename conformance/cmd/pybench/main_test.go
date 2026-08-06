// Tests in this file cover pybench's pure logic only: annotation stripping,
// statistics, report rendering, and implementation filtering. They MUST NOT
// spawn a process (build a compiler, run python3.13, run minipy, ...), so
// `go test ./...` stays hermetic and independent of what is installed on the
// host. Process-spawning behavior (discovery, correctness gating, timing) is
// exercised by hand via `go run ./conformance/cmd/pybench`, not by this
// package's tests.
package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestStripAnnotations(t *testing.T) {
	t.Run("leaves unannotated source untouched", func(t *testing.T) {
		src := "def f(n: int) -> int:\n    return n + 1\n\n\nprint(str(f(1)))\n"
		require.Equal(t, src, stripAnnotations(src))
	})

	t.Run("rewrites a simple annotated assignment", func(t *testing.T) {
		src := "count: int = 0\n"
		require.Equal(t, "count = 0\n", stripAnnotations(src))
	})

	t.Run("rewrites an indented annotated assignment preserving indent", func(t *testing.T) {
		src := "def f():\n    total: int = 0\n    return total\n"
		require.Equal(t, "def f():\n    total = 0\n    return total\n", stripAnnotations(src))
	})

	t.Run("rewrites a generic-typed annotated assignment without losing the value", func(t *testing.T) {
		src := "cols: list[bool] = [False for i in range(n)]\n"
		require.Equal(t, "cols = [False for i in range(n)]\n", stripAnnotations(src))
	})

	t.Run("drops a bare declaration with no value", func(t *testing.T) {
		src := "class Node:\n    item: int\n    left: \"Node | None\"\n\n    def __init__(self):\n        pass\n"
		want := "class Node:\n\n    def __init__(self):\n        pass\n"
		require.Equal(t, want, stripAnnotations(src))
	})

	t.Run("leaves function parameter and return annotations alone", func(t *testing.T) {
		src := "def f(x: int, y: list[bool] = []) -> tuple[int, int]:\n    return (x, x)\n"
		require.Equal(t, src, stripAnnotations(src))
	})

	t.Run("does not mistake a compound-statement colon for an annotation", func(t *testing.T) {
		src := "if x:\n    y = 1\nelse:\n    y = 2\nfor i in range(n):\n    pass\nwhile True:\n    break\n"
		require.Equal(t, src, stripAnnotations(src))
	})

	t.Run("does not mistake a dict literal for an annotation", func(t *testing.T) {
		src := "d = {\"a\": 1, \"b\": 2}\n"
		require.Equal(t, src, stripAnnotations(src))
	})

	t.Run("does not touch a line inside a multi-line bracketed expression", func(t *testing.T) {
		src := "xs = [\n    a: 1,\n]\n"
		require.Equal(t, src, stripAnnotations(src))
	})

	t.Run("does not mistake a comparison operator for assignment", func(t *testing.T) {
		src := "ok: bool = x >= 1\n"
		require.Equal(t, "ok = x >= 1\n", stripAnnotations(src))
	})
}

func TestComputeStats(t *testing.T) {
	t.Run("odd count reports the middle value as median", func(t *testing.T) {
		s := computeStats([]time.Duration{5 * time.Millisecond, 1 * time.Millisecond, 3 * time.Millisecond})
		require.Equal(t, 1*time.Millisecond, s.Min)
		require.Equal(t, 3*time.Millisecond, s.Median)
		require.Equal(t, 3, s.N)
	})

	t.Run("even count averages the two central values", func(t *testing.T) {
		s := computeStats([]time.Duration{4 * time.Millisecond, 2 * time.Millisecond, 8 * time.Millisecond, 6 * time.Millisecond})
		require.Equal(t, 2*time.Millisecond, s.Min)
		require.Equal(t, 5*time.Millisecond, s.Median)
	})

	t.Run("single value is both min and median", func(t *testing.T) {
		s := computeStats([]time.Duration{7 * time.Millisecond})
		require.Equal(t, 7*time.Millisecond, s.Min)
		require.Equal(t, 7*time.Millisecond, s.Median)
	})

	t.Run("does not mutate the caller's slice order", func(t *testing.T) {
		in := []time.Duration{5 * time.Millisecond, 1 * time.Millisecond, 3 * time.Millisecond}
		_ = computeStats(in)
		require.Equal(t, []time.Duration{5 * time.Millisecond, 1 * time.Millisecond, 3 * time.Millisecond}, in)
	})

	t.Run("panics on an empty input", func(t *testing.T) {
		require.Panics(t, func() { computeStats(nil) })
	})
}

func TestMinusStartup(t *testing.T) {
	t.Run("subtracts the baseline", func(t *testing.T) {
		require.Equal(t, 40*time.Millisecond, minusStartup(50*time.Millisecond, 10*time.Millisecond))
	})

	t.Run("floors at zero rather than going negative", func(t *testing.T) {
		require.Equal(t, time.Duration(0), minusStartup(5*time.Millisecond, 10*time.Millisecond))
	})

	t.Run("equal values floor at zero", func(t *testing.T) {
		require.Equal(t, time.Duration(0), minusStartup(10*time.Millisecond, 10*time.Millisecond))
	})
}

func TestFilterImplementations(t *testing.T) {
	impls := []Implementation{
		{Info: ImplInfo{Name: "CPython 3.13"}},
		{Info: ImplInfo{Name: "CPython (python3)"}},
		{Info: ImplInfo{Name: "minipy -O0"}},
		{Info: ImplInfo{Name: "minipy -O3"}},
	}

	t.Run("empty filter keeps everything", func(t *testing.T) {
		require.Equal(t, impls, filterImplementations(impls, ""))
	})

	t.Run("single substring keeps only matches, case-insensitively", func(t *testing.T) {
		got := filterImplementations(impls, "minipy")
		require.Len(t, got, 2)
		require.Equal(t, "minipy -O0", got[0].Info.Name)
		require.Equal(t, "minipy -O3", got[1].Info.Name)
	})

	t.Run("comma-separated filters union their matches", func(t *testing.T) {
		got := filterImplementations(impls, "3.13,-O3")
		require.Len(t, got, 2)
		require.Equal(t, "CPython 3.13", got[0].Info.Name)
		require.Equal(t, "minipy -O3", got[1].Info.Name)
	})

	t.Run("no match leaves the result empty", func(t *testing.T) {
		require.Empty(t, filterImplementations(impls, "pypy3"))
	})
}

func TestRenderMarkdown(t *testing.T) {
	env := Environment{
		Date:   "2026-08-06 00:00 UTC",
		Kernel: "Linux 6.18.5",
		CPUs:   8,
		RAMGiB: 16,
		Impls: []ImplInfo{
			{Name: "CPython 3.13", Version: "Python 3.13.12"},
			{Name: "pypy3", Absent: true},
		},
	}
	startups := []StartupRow{
		{Impl: "CPython 3.13", Stats: Stats{Min: 17 * time.Millisecond, Median: 18 * time.Millisecond}},
	}
	rows := []CaseRow{
		{Case: "fib", Impl: "CPython 3.13", Status: StatusPass, Stats: &Stats{Min: 166 * time.Millisecond, Median: 170 * time.Millisecond}},
		{Case: "fib", Impl: "minipy -O3", Status: StatusFail, Detail: "expected \"14930352\", got \"boom\""},
		{Case: "fib", Impl: "gpython", Status: StatusNA, Detail: "syntax error"},
	}

	out := renderMarkdown(env, startups, rows)

	t.Run("includes header facts", func(t *testing.T) {
		require.Contains(t, out, "Date: 2026-08-06 00:00 UTC")
		require.Contains(t, out, "Kernel: Linux 6.18.5")
		require.Contains(t, out, "CPUs: 8")
		require.Contains(t, out, "CPython 3.13: Python 3.13.12")
		require.Contains(t, out, "pypy3: not found on PATH")
	})

	t.Run("includes the startup table", func(t *testing.T) {
		require.Contains(t, out, "Startup baseline")
		require.Contains(t, out, "17.0ms")
	})

	t.Run("includes a pass row with timing and the minus-startup column", func(t *testing.T) {
		require.Contains(t, out, "| CPython 3.13 | PASS | 166.0ms | 170.0ms | 149.0ms | 152.0ms |")
	})

	t.Run("includes a fail row with its detail and no timing", func(t *testing.T) {
		require.Contains(t, out, "FAIL")
		require.Contains(t, out, "boom")
	})

	t.Run("includes an n/a row", func(t *testing.T) {
		require.Contains(t, out, "N/A")
		require.Contains(t, out, "syntax error")
	})
}

func TestFormatDuration(t *testing.T) {
	require.Equal(t, "5.0ms", formatDuration(5*time.Millisecond))
	require.Equal(t, "1500.0ms", formatDuration(1500*time.Millisecond))
	require.Equal(t, "0.5ms", formatDuration(500*time.Microsecond))
}

func TestOneLine(t *testing.T) {
	require.Equal(t, "a b c", oneLine("a\nb\n  c  "))
	require.Equal(t, "", oneLine("\n\n"))
}
