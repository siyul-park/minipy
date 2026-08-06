# Cross-Implementation Benchmarks

Wall-clock comparison of minipy against CPython, pypy3, and gpython on a
shared corpus of compute-heavy programs, plus the tooling that produces the
comparison.

**Read [Execution mode](#execution-mode-only-pypy3-is-jit-compiled) first.**
Every minipy number here is interpreted, not JIT-compiled: minivm's JIT is
arm64-only and this host is amd64. CPython 3.13, CPython 3.11 and gpython are
likewise interpreters, so those comparisons are like-for-like. pypy3 is the
only JIT here, and its lead should be read accordingly.

## When to Read

Read this before adding a benchmark case, before changing
`conformance/cmd/pybench`, or when asked how minipy's runtime performance
compares to CPython's.

## Purpose

`docs/compatibility.md` and the conformance corpus (`docs/conformance.md`)
answer "does minipy behave like CPython". This document answers a different
question: "how fast is minipy compared to CPython, pypy3, and gpython, on
programs that are expensive enough for the answer to matter". The corpus
under `conformance/testdata/benchmark/` and the `pybench` runner exist to
answer that with real, reproducible measurements rather than an impression.

## The Corpus

Each case is a `<name>.py` / `<name>.expected` pair under
`conformance/testdata/benchmark/`, in the same two-file shape as the
conformance corpus (`docs/conformance.md`) minus the divergence machinery:
a benchmark program is expected to produce **identical** output on every
implementation, since identical output across implementations doubles as the
correctness gate — there is no such thing as an intentionally divergent
benchmark case.

| Case | What it exercises |
|---|---|
| `fib` | naive recursive Fibonacci: function-call overhead, integer arithmetic |
| `nqueens` | backtracking search: recursion depth, `list[bool]` state |
| `fannkuch` | pancake-flip permutation search: recursion, in-place list swaps |
| `nbody` | planetary gravity simulation: float arithmetic, `math.sqrt`, flat-array pairwise loops |
| `spectralnorm` | power iteration on an implicit matrix: float division, nested index loops |
| `mandelbrot` | escape-time set membership: float arithmetic in a tight per-pixel loop |
| `binarytrees` | build-and-checksum many trees: class allocation, recursive construction/traversal |
| `matmul` | dense matrix multiply: float multiply-accumulate over flat row-major arrays |
| `sortstress` | generate-then-`sort()` many lists: builtin sort, a hand-rolled PRNG |
| `strbuild` | build and transform many strings: concatenation, `str` methods |

Each program is sized to run in roughly 1-3 seconds under CPython 3.13 (see
each file's header comment for the exact figure) and rewritten from its
original benchmarks-game-style source to fit minipy's subset:

- `print()` takes exactly one argument, so each program prints exactly one
  final checksum line — never intermediate progress — so that identical
  output across implementations is itself the correctness signal.
- No `itertools`, `time`, `pyperf`, or any stdlib module beyond `math`; no
  program times itself.
- Flat `list[float]`/`list[int]` with index loops in place of tuple-of-tuples
  or `sum(genexp)`/`zip`, per the corpus's own style (see `matmul` and
  `nbody`).
- No `if __name__ == "__main__":` — `__name__` is not defined in minipy;
  every program calls `main()` directly.
- Annotation-free wherever minipy's inference allows it (`xs = [1, 2, 3]`,
  `total = 0`, `class P` all work unannotated). A **self-recursive**
  function needs an explicit return annotation, because minipy infers an
  unannotated return type by joining the function's own value-return
  branches, and a self-recursive call inside the body would need that very
  type before it is known. Every case's header comment says which of its
  functions needed one and why. A parameter with no default value and no
  annotation infers to `Any` regardless of the call site's argument types,
  and `Any` does not support the indexing/arithmetic most of these programs
  need — so in practice most function *parameters* end up annotated even
  when locals and recursion-free returns do not.

### Algorithm changes forced by real minipy bugs

Porting this corpus surfaced five reproducible minipy defects, each
confirmed against a clean build of this repository's `HEAD` (not a
work-in-progress tree) with both CPython and minipy's own output captured.
Two corpus programs (`fannkuch`, `matmul`) were rewritten to a different,
still-idiomatic algorithm to avoid the buggy pattern rather than being
dropped, since a working (if differently shaped) program is more useful to
this corpus than no program; the other findings are noted in the relevant
file's header and reported here in full for whoever picks them up.

1. **`fannkuch`: a long-lived `while` loop reading a list element written
   earlier in the same loop can observe a stale value.** The classic
   fannkuch-redux algorithm regenerates each permutation in place with a
   single reusable `count`/`perm1` array pair inside one big `while True:`
   loop. Under minipy, after roughly 50-90 iterations of that loop
   (interleaved reads and writes to the same list), a read of `perm1[0]`
   returns a value from several iterations earlier instead of the value most
   recently written, corrupting the permutation into an invalid state (a
   repeated digit, e.g. `03021` instead of the correct `03421`) — confirmed
   with a full state trace comparing minipy against CPython 3.13 at each
   step, reproducible at both `-O 0` and `-O 3` (each diverges at a
   different iteration, ruling out one specific optimizer pass as the sole
   cause). `conformance/testdata/benchmark/fannkuch.py` instead generates
   permutations recursively (Heap's algorithm), which does not trigger it.
2. **`fannkuch` at `-O 3`: compiling a call whose integer argument comes
   through a local variable instead of a literal makes the GVN/CSE pass take
   minutes instead of seconds.** `fannkuch(n)` where `n = 9` is a local
   variable compiles in minutes at `-O 3` (a compile-time blowup, observed
   directly, not merely inferred from a timeout); `fannkuch(9)` with the
   literal inlined at the call site compiles in seconds and runs correctly.
   The corpus file calls with a literal for this reason (see its header).
3. **`matmul`: accumulating into the same list slot across an inner loop,
   interleaved with writes to other slots, can produce a numerically wrong
   value in exactly one cell.** The natural `i, k, j` loop order for a dense
   matrix multiply accumulates directly into `out[i*n+j]` across the `k`
   loop — a read-modify-write of the same list slot several times per output
   cell, with other cells' slots also being written in between. For `n` as
   small as 6 (216 total multiply-adds), exactly one of the 36 output cells
   comes out wrong (`38.0` instead of `36.0`), confirmed against CPython
   3.13 output for the full matrix. `conformance/testdata/benchmark/matmul.py`
   instead uses the standard `i, j, k` order, accumulating each cell into a
   fresh local before writing `out[i*n+j]` exactly once, which does not
   trigger it (and is the more common formulation anyway).
4. **`matmul` at `-O 3`: previously observed to crash on the safe `i, j, k`
   form; not reproduced by this measurement.** An earlier pass over this
   corpus recorded `-O 3` (GVN/CSE) miscompiling the `a[i*n+k]` / `b[k*n+j]`
   index arithmetic in the safe form and crashing at runtime with
   `index out of range`, said to be reproducible from `n` as small as 10.
   This measurement's own run (`n = 200`, rev `f523af2`) does not reproduce
   it: the results table below shows minipy `-O 3` on `matmul` as `PASS`
   across the correctness gate and all 5 timing runs, and a direct,
   independent check outside pybench (`minipy run -O 3 matmul.py`, 5
   consecutive invocations) matched `matmul.expected` every time. Recorded
   here rather than silently dropped, per this document's own rule of
   reporting only what was actually measured: either the earlier report was
   itself in error, or the defect is narrower (compiler-version- or
   environment-sensitive) than first described. Nothing in this
   measurement's evidence supports asserting it is fixed, only that it did
   not occur here.
5. **`str.split(sep)` segfaults the VM on a string built from many
   variable-length tokens once the input is large enough.** Reproducible
   from as few as a few hundred space-separated numeral tokens (a
   `str(n)`-style loop, not a literal), independent of any other string
   method in the chain — `s.split(" ")` alone segfaults where an equivalent
   plain string built from fixed-length tokens does not.
   `conformance/testdata/benchmark/strbuild.py` never calls `split`.

None of these five are worked around by picking an easier *problem* — every
corpus program still does the same real computation and produces a checksum
that CPython, minipy, pypy3 (and, where feasible, gpython) all agree on. They
are worked around by picking a different *implementation* of that
computation, the same way an application author hitting one of these bugs
would have to.

### A sixth defect, not worked around: `nbody`

Unlike the five above, `nbody` was **not** rewritten to route around a
defect, because no alternative formulation was found that avoided it: this
measurement's own run shows minipy failing `nbody`'s correctness gate at
both `-O 0` and `-O 3`, reproducibly, and the results table below reports it
as a real `FAIL` rather than a passing time.

`nbody` runs 250,000 `advance()` steps of pairwise gravitational float
arithmetic (`math.sqrt`, multiply-accumulate) over 5 bodies, then prints a
final energy checksum. CPython 3.13 (and CPython 3.11 and pypy3, both of
which also pass) produce `-0.171931230`; minipy at both optimization levels
produces `-0.171846486` — a difference starting at the fifth significant
digit, not a gross logic error, consistent with floating-point arithmetic
accumulating differently over 250,000 sequential steps (operation order,
`math.sqrt` precision, or fused-multiply-add differences are all plausible
causes) rather than an obviously wrong formula. That distinction does not
change the correctness-gate outcome: `pybench` requires byte-identical
output, minipy's does not match, and it is reported `FAIL` at both `-O 0`
and `-O 3` — identically, which is itself informative: whatever causes the
divergence is not something the `-O 3` optimizer passes introduce or fix, so
it is present already at `-O 0`, in either the interpreter's float pipeline
or `nbody.py`'s own arithmetic as minipy evaluates it. No workaround was
applied because, unlike the five defects above, no alternative
*formulation* of this computation was found that both stays idiomatic and
avoids the divergence — the program already uses flat arrays and index
loops, the corpus's preferred style. Diagnosing the exact source (interpreter
float pipeline vs. a specific operation) is outside this document's scope;
it is recorded here as a real, reproducible, currently unresolved gap.

## `pybench`, the Cross-Implementation Runner

`conformance/cmd/pybench` discovers every implementation it knows how to
drive, runs the full corpus under each, and reports correctness and timing.

### What it does

1. **Discovers implementations** on `PATH`: `python3.13`, `python3` (whatever
   version that resolves to on the host — 3.11 on the machine this document's
   numbers were measured on), `pypy3`, and `gpython`, plus minipy built fresh
   from this checkout at `-O 0` and `-O 3`. An implementation not found on
   `PATH` is reported absent in the header, never silently omitted.
2. **Runs the correctness gate first.** Every implementation must reproduce
   a case's CPython-derived `.expected` golden exactly before pybench
   records a time for it. Wrong output is `FAIL` (or `N/A` for gpython — see
   below), never a fast time next to a wrong answer.
3. **Handles gpython's ceiling explicitly.** gpython implements Python 3.4:
   it accepts PEP 3107 function parameter/return annotations
   (`def f(x: int) -> int:`) but not PEP 526 variable annotations
   (`x: int = 0`), which several corpus programs use for locals or class
   fields. For those, pybench mechanically strips just the PEP 526 forms
   (`conformance/cmd/pybench/strip.go`) into a temporary copy and runs
   gpython against that instead, still gated against the same `.expected`
   golden as everyone else. A program gpython cannot run at all even after
   stripping — a real syntax gap, a semantic difference from 3.13, or
   simply too slow to finish inside the runner's per-process timeout — is
   reported `N/A`, never dropped from the table.
4. **Measures a startup baseline.** A trivial one-line program
   (`x = 1`) is timed under every implementation the same way a benchmark
   case is, so each case's own table can report both raw wall-clock time and
   wall-clock **minus** that implementation's own startup cost — otherwise a
   short-lived program's number would be dominated by process-start
   overhead that has nothing to do with the workload minipy actually
   compiled and ran.
5. **Reports min and median over `-runs` repetitions** (default 5). Min is
   reported because process-timing noise (scheduler jitter, page faults, GC
   pauses) is one-sided — it can only slow a run down, never speed one up —
   so the fastest observed run is the closest estimate of true cost. Median
   is reported alongside as a noise-resistant central estimate.
6. **Emits Markdown**: a header with the measurement date, kernel, CPU
   count, RAM, and each implementation's `--version` output (or gpython's
   startup banner, which is the closest it has to one), a startup-baseline
   table, then one correctness/timing table per benchmark case.
7. **Fixes the hash seed.** Every process runs with `PYTHONHASHSEED=0` in
   its environment, so an implementation whose string/object hashing is
   randomized by default (CPython, pypy3) cannot introduce run-to-run
   iteration-order or timing noise from that source; minipy and gpython are
   unaffected by the variable but receive it uniformly regardless.
8. **Runs single-threaded.** `pybench` drives every implementation as one
   plain OS process at a time — no run overlaps another — and none of the
   corpus's ten programs themselves use threads, `multiprocessing`, or
   `asyncio` (see [Infeasible Benchmarks](#infeasible-benchmarks) for why
   async programs are excluded entirely), so both the harness and the
   workload it measures are single-threaded end to end.

### Flags

| Flag | Default | Meaning |
|---|---|---|
| `-corpus` | `conformance/testdata/benchmark` | directory of `<name>.py`/`<name>.expected` cases |
| `-runs` | `5` | timing repetitions per (case, implementation) pair, after the gate passes |
| `-impl` | (all discovered) | comma-separated substrings; only implementations whose name contains one are run |
| `-format` | `markdown` | report format (only Markdown is implemented) |
| `-timeout` | `40s` | per-process kill timeout; an implementation that cannot finish within it is `FAIL`/`N/A`, not left to hang the run |

### Running it

```bash
go run ./conformance/cmd/pybench -runs 5
```

`pybench`'s own pure logic (annotation stripping, statistics, implementation
filtering, Markdown table rendering) is unit-tested in
`conformance/cmd/pybench/main_test.go`. Discovery, correctness gating, and
timing all spawn real processes and are deliberately **not** unit-tested —
`go test ./...` stays hermetic and independent of what happens to be
installed on the host running it; those paths are exercised by hand, by
actually running the tool.

## Installing the Implementations

CPython 3.13 and `python3` are assumed already present (they are required
for the conformance corpus's own goldens; see `docs/conformance.md`).

```bash
# pypy3 (Debian/Ubuntu)
apt-get install -y pypy3

# gpython — found on PATH, not a Go module dependency. It MUST NOT be added
# to go.mod/go.sum: it is a sibling interpreter this repo measures against,
# not a package minipy imports.
go install github.com/go-python/gpython@v0.2.0
```

`go install` places `gpython` in `$(go env GOBIN)` (or `$GOPATH/bin`); make
sure that directory is on `PATH` before running `pybench`. gpython has no
`--version` flag; its REPL banner (captured by feeding it a closed stdin) is
reported in the header in its place.

## Environment and Availability (this measurement)

- Date: 2026-08-06 09:24 UTC
- Kernel: Linux 6.18.5-fc-v18
- CPUs: 4
- RAM: 15.7 GiB
- Implementations:
  - CPython 3.13: Python 3.13.12
  - CPython (python3): Python 3.11.15
  - pypy3: Python 3.9.18 (7.3.15+dfsg-1build3, Apr 01 2024, 03:12:48) [PyPy 7.3.15 with GCC 13.2.0]
  - gpython: Python 3.4.0 (none, unknown); [Gpython dev]
  - minipy -O0: built from this checkout, rev f523af2
  - minipy -O3: built from this checkout, rev f523af2

### Execution mode: only pypy3 is JIT-compiled

This is the single most important caveat for reading the tables below.

| Implementation | Execution | Why |
|---|---|---|
| minipy `-O0`/`-O3` | interpreter | minivm's JIT backend is **arm64-only** (`interp/jit_arm64.go`); on every other architecture `interp/jit_stub.go` (`//go:build !arm64`) returns a nil compiler and the interpreter runs unassisted. This host is amd64, so **no minipy number here is JIT-compiled**. `-O0`/`-O3` select minivm's ahead-of-time optimizer level, not a JIT. |
| CPython 3.13 | interpreter | 3.13's copy-and-patch JIT is opt-in at build time and this build does not have it (`--enable-experimental-jit` absent from `CONFIG_ARGS`). |
| CPython 3.11 | interpreter | no JIT exists in 3.11. |
| gpython | interpreter | tree-walking interpreter, no JIT. |
| **pypy3** | **tracing JIT** | RPython tracing JIT, on by default. |

So minipy against CPython 3.13, CPython 3.11 and gpython is a like-for-like
interpreter comparison, and those ratios say something real about minipy's
interpreter.

**minipy against pypy3 is not like-for-like.** pypy3's 3x-20x lead is mostly
its JIT compiling hot loops to machine code while minipy interprets bytecode.
Read that column as "what a mature JIT buys on this workload", not as a
verdict on minipy's design. The interesting comparison for minipy is CPython.

A JIT-enabled minipy measurement would need an arm64 host and is not
represented here at all.

All six implementations pybench knows how to discover were present on this
host; none is reported absent. `-runs 5` was used (the tool's own default),
so every table below is a full, unreduced run of the corpus.

### Startup baseline (empty program)

| Implementation | Min | Median |
|---|---:|---:|
| CPython 3.13 | 13.2ms | 13.6ms |
| CPython (python3) | 11.1ms | 11.8ms |
| pypy3 | 28.2ms | 29.2ms |
| gpython | 3.5ms | 4.0ms |
| minipy -O0 | 3.8ms | 4.1ms |
| minipy -O3 | 3.7ms | 3.9ms |

## Results

### binarytrees

| Implementation | Status | Min | Median | Min - startup | Median - startup |
|---|---|---:|---:|---:|---:|
| CPython 3.13 | PASS | 825.9ms | 900.5ms | 812.6ms | 886.9ms |
| CPython (python3) | PASS | 877.0ms | 885.6ms | 865.9ms | 873.8ms |
| pypy3 | PASS | 131.4ms | 197.6ms | 103.2ms | 168.4ms |
| gpython | PASS | 8707.9ms | 8841.8ms | 8704.4ms | 8837.8ms |
| minipy -O0 | PASS | 1679.6ms | 1701.2ms | 1675.8ms | 1697.1ms |
| minipy -O3 | PASS | 1661.2ms | 1731.8ms | 1657.5ms | 1727.9ms |

### fannkuch

| Implementation | Status | Min | Median | Min - startup | Median - startup |
|---|---|---:|---:|---:|---:|
| CPython 3.13 | PASS | 1282.8ms | 1295.9ms | 1269.6ms | 1282.3ms |
| CPython (python3) | PASS | 1065.1ms | 1072.9ms | 1054.0ms | 1061.1ms |
| pypy3 | PASS | 231.5ms | 241.4ms | 203.3ms | 212.1ms |
| gpython | N/A (exit status 1) | - | - | - | - |
| minipy -O0 | FAIL (timed out after 40s) | - | - | - | - |
| minipy -O3 | PASS | 6388.5ms | 6613.5ms | 6384.8ms | 6609.6ms |

### fib

| Implementation | Status | Min | Median | Min - startup | Median - startup |
|---|---|---:|---:|---:|---:|
| CPython 3.13 | PASS | 2096.7ms | 2109.1ms | 2083.5ms | 2095.5ms |
| CPython (python3) | PASS | 1941.1ms | 1961.9ms | 1930.0ms | 1950.1ms |
| pypy3 | PASS | 208.4ms | 225.5ms | 180.2ms | 196.3ms |
| gpython | PASS | 25389.4ms | 25504.2ms | 25386.0ms | 25500.2ms |
| minipy -O0 | PASS | 2680.4ms | 2777.1ms | 2676.6ms | 2773.0ms |
| minipy -O3 | PASS | 2673.6ms | 2694.8ms | 2669.9ms | 2690.9ms |

### mandelbrot

| Implementation | Status | Min | Median | Min - startup | Median - startup |
|---|---|---:|---:|---:|---:|
| CPython 3.13 | PASS | 1621.4ms | 1641.9ms | 1608.2ms | 1628.3ms |
| CPython (python3) | PASS | 1377.3ms | 1399.1ms | 1366.2ms | 1387.4ms |
| pypy3 | PASS | 136.9ms | 141.8ms | 108.7ms | 112.6ms |
| gpython | PASS | 6581.6ms | 6738.6ms | 6578.1ms | 6734.7ms |
| minipy -O0 | PASS | 1673.5ms | 1675.8ms | 1669.7ms | 1671.7ms |
| minipy -O3 | PASS | 1684.0ms | 1706.9ms | 1680.2ms | 1703.0ms |

### matmul

| Implementation | Status | Min | Median | Min - startup | Median - startup |
|---|---|---:|---:|---:|---:|
| CPython 3.13 | PASS | 1058.3ms | 1098.9ms | 1045.0ms | 1085.3ms |
| CPython (python3) | PASS | 1160.0ms | 1249.9ms | 1148.9ms | 1238.1ms |
| pypy3 | PASS | 53.5ms | 55.0ms | 25.3ms | 25.8ms |
| gpython | N/A (exit status 1) | - | - | - | - |
| minipy -O0 | PASS | 1839.8ms | 1853.0ms | 1836.0ms | 1848.9ms |
| minipy -O3 | PASS | 1824.9ms | 1865.3ms | 1821.1ms | 1861.4ms |

### nbody

| Implementation | Status | Min | Median | Min - startup | Median - startup |
|---|---|---:|---:|---:|---:|
| CPython 3.13 | PASS | 1336.9ms | 1338.5ms | 1323.6ms | 1324.9ms |
| CPython (python3) | PASS | 1304.6ms | 1333.8ms | 1293.5ms | 1322.1ms |
| pypy3 | PASS | 169.5ms | 178.4ms | 141.3ms | 149.2ms |
| gpython | N/A (exit status 1) | - | - | - | - |
| minipy -O0 | FAIL (expected "-0.171931230", got "-0.171846486") | - | - | - | - |
| minipy -O3 | FAIL (expected "-0.171931230", got "-0.171846486") | - | - | - | - |

### nqueens

| Implementation | Status | Min | Median | Min - startup | Median - startup |
|---|---|---:|---:|---:|---:|
| CPython 3.13 | PASS | 991.0ms | 993.0ms | 977.8ms | 979.4ms |
| CPython (python3) | PASS | 794.5ms | 806.1ms | 783.4ms | 794.3ms |
| pypy3 | PASS | 297.1ms | 336.7ms | 268.9ms | 307.4ms |
| gpython | N/A (exit status 1) | - | - | - | - |
| minipy -O0 | PASS | 2932.0ms | 2937.4ms | 2928.2ms | 2933.3ms |
| minipy -O3 | PASS | 2859.3ms | 2909.6ms | 2855.6ms | 2905.7ms |

### sortstress

| Implementation | Status | Min | Median | Min - startup | Median - startup |
|---|---|---:|---:|---:|---:|
| CPython 3.13 | PASS | 745.7ms | 845.3ms | 732.5ms | 831.7ms |
| CPython (python3) | PASS | 674.3ms | 682.8ms | 663.2ms | 671.0ms |
| pypy3 | PASS | 217.9ms | 218.9ms | 189.8ms | 189.6ms |
| gpython | N/A (exit status 1) | - | - | - | - |
| minipy -O0 | PASS | 3285.0ms | 3412.0ms | 3281.2ms | 3407.9ms |
| minipy -O3 | PASS | 3244.1ms | 3334.9ms | 3240.4ms | 3331.0ms |

### spectralnorm

| Implementation | Status | Min | Median | Min - startup | Median - startup |
|---|---|---:|---:|---:|---:|
| CPython 3.13 | PASS | 1330.4ms | 1369.0ms | 1317.2ms | 1355.4ms |
| CPython (python3) | PASS | 1360.0ms | 1377.4ms | 1348.9ms | 1365.6ms |
| pypy3 | PASS | 91.8ms | 233.8ms | 63.6ms | 204.6ms |
| gpython | N/A (exit status 1) | - | - | - | - |
| minipy -O0 | PASS | 2493.1ms | 2518.0ms | 2489.3ms | 2513.9ms |
| minipy -O3 | PASS | 2454.3ms | 2531.9ms | 2450.6ms | 2528.0ms |

### strbuild

| Implementation | Status | Min | Median | Min - startup | Median - startup |
|---|---|---:|---:|---:|---:|
| CPython 3.13 | PASS | 1234.2ms | 1239.0ms | 1220.9ms | 1225.4ms |
| CPython (python3) | PASS | 1224.5ms | 1244.4ms | 1213.4ms | 1232.6ms |
| pypy3 | PASS | 568.5ms | 589.2ms | 540.4ms | 560.0ms |
| gpython | N/A (exit status 1) | - | - | - | - |
| minipy -O0 | PASS | 6317.5ms | 7337.6ms | 6313.8ms | 7333.6ms |
| minipy -O3 | PASS | 5728.7ms | 6417.3ms | 5724.9ms | 6413.4ms |

The tables above are pybench's own Markdown output, unedited (only the
duplicate top-level heading and header block it also prints were removed,
since this document supplies those itself, and both source in this section
are byte-identical to what `go run ./conformance/cmd/pybench -runs 5`
printed on this run). `N/A (exit status 1)` for gpython on a case means it
exited with an error immediately (not a `-timeout` kill) — most often the
stripped source still uses a construct gpython's Python-3.4 parser or
runtime rejects.

## Reading the Results

All ratios below use each case's `minipy` **min** against CPython 3.13's own
**min**, since min is the noise-resistant estimate `pybench` itself prefers
(see [What it does](#what-it-does), item 5); "tied" means within about 1%,
smaller than run-to-run noise on this host.

- **minipy `-O0` vs CPython 3.13**, on the 8 cases where both pass the
  correctness gate: 1.03x (`mandelbrot`) up to 5.12x (`strbuild`), with most
  cases in the 1.7x-3.0x band (`matmul` 1.74x, `spectralnorm` 1.87x,
  `binarytrees` 2.03x, `nqueens` 2.96x, `sortstress` 4.41x). `fib` alone
  landed at 1.28x, matching this machine's earlier hand-timed fib(30) spot
  check (0.166s CPython / 0.209s minipy `-O0`, ~1.26x) almost exactly — but
  that spot check was only ever representative of function-call-heavy
  integer recursion, not the corpus as a whole; the wider spread above is
  the more complete picture. `fannkuch` at `-O 0` did not pass at all: it
  did not finish within the 40s per-process timeout in *both* the `-runs 1`
  smoke test and this `-runs 5` measurement, so its `-O0`/CPython ratio is
  unbounded, not merely large — see the next point.
- **minipy `-O0` vs `-O3`**: contrary to this document's own prior
  expectation (recorded further down as a deliberate correction — see
  [defect 4](#algorithm-changes-forced-by-real-minipy-bugs)), `-O 3` did not
  crash any case in this run and was never more than about 1% slower than
  `-O 0` on any case that passed at both levels (`mandelbrot`, essentially
  tied). It was consistently at or slightly ahead of `-O 0` elsewhere
  (`strbuild` 9.3% faster, `nqueens` 2.5% faster, `spectralnorm` 1.6%
  faster, `sortstress` 1.2% faster, `binarytrees`/`matmul`/`fib` tied), and
  on `fannkuch` the difference was categorical, not incremental: `-O 3`
  passed in 6.4-6.6s where `-O 0` did not finish in 40s. `-O 3` on `matmul`
  also passed cleanly here, which the corpus's own header comment and an
  earlier version of this document did not expect — see defect 4 above for
  the full account of that discrepancy. None of this licenses "always pass
  `-O 3`" as a general rule from one host's one run; it says only what this
  measurement showed.
- **minipy vs CPython on `nbody`**: neither optimization level passes the
  correctness gate (see [the sixth defect](#a-sixth-defect-not-worked-around-nbody)
  above), so `nbody` has no minipy timing to compare — the table reports
  `FAIL`, not a fast wrong answer.
- **pypy3**: fastest implementation on every single case that it could run,
  by a wide and consistent margin (roughly 3x-20x faster than CPython 3.13's
  min, e.g. 19.8x on `matmul`, 14.5x on `spectralnorm`, down to 2.2x on
  `strbuild`). Each `pybench` invocation is a fresh process, so these
  numbers include JIT warmup rather than pypy3's warmed-up steady state,
  and pypy3 was still decisively fastest despite that handicap.
- **gpython**: passed only 3 of 10 cases (`binarytrees`, `fib`,
  `mandelbrot`) and was slower than CPython 3.13 on all three by a wide
  margin (10.5x on `binarytrees`, 12.1x on `fib`, 4.1x on `mandelbrot`,
  consistent with "one to two orders of magnitude slower"). The other 7
  cases are `N/A`, and every one of those was an immediate
  `exit status 1` on the annotation-stripped source, not a `-timeout` kill
  — meaning gpython's Python-3.4 ceiling (a real syntax or semantic gap
  beyond what PEP 526 stripping fixes) is the reason it did not run those
  cases at all, not that it ran out of time.

## Infeasible Benchmarks

Standard benchmarks-game / pyperformance programs that were considered and
rejected for this corpus, and why:

| Benchmark | Reason infeasible |
|---|---|
| `pidigits` | needs arbitrary-precision (bignum) arithmetic; minipy `int` is fixed-width 64-bit |
| `richards`, `deltablue` | need polymorphic (virtual) method dispatch across an open set of classes/interfaces. minipy resolves method calls statically, not through a runtime vtable, and only 3 dunders participate in any form of interface-shaped dispatch at all (`__len__`, `__getitem__`, `__setitem__`, per `docs/spec/04-static-semantics.md`'s "Restricted special methods") — out of minipy's supported OOP subset |
| `regex_dna`, `regex_effbot`, `json_loads`, `json_dumps`, `pickle`, `xml_etree` | need stdlib modules (`re`, `json`, `pickle`, `xml.etree`) minipy does not implement; the corpus is restricted to `math` beyond builtins |
| `chaos`, `hexiom` | need `random.Random` as a seeded, algorithm-specified stream (Wichmann-Hill/Mersenne Twister) whose exact sequence the reference output depends on; minipy has no `random` module, and a hand-rolled PRNG would not reproduce the reference program's actual output |
| `django_template`, `tornado_http` | need a real templating engine or async HTTP framework (Django, Tornado); minipy has no framework or stdlib surface for either and is not a hosting target for one |
| `async_tree_*`, `asyncio_*` | `async`/`await` is rejected by the checker before lowering; minipy does not support coroutines |
| `deepcopy` | needs reflection over arbitrary, previously-unknown object graphs (`copy.deepcopy`'s generic traversal); minipy's type model is static and closed, with no runtime type introspection to walk an arbitrary graph generically |

## Adding a Case

Follow `docs/conformance.md`'s "Adding a Case" shape: a `<name>.py` plus a
`<name>.expected` captured by actually running CPython 3.13
(`python3.13 name.py > name.expected`), sized to run in roughly 1-3 seconds
under CPython, respecting the rewrite rules under [The Corpus](#the-corpus)
above. Verify it produces byte-identical output under minipy
(`go run ./cmd/minipy run conformance/testdata/benchmark/name.py`) before
adding it — if minipy's output differs, that is a bug to report (see
[Algorithm changes forced by real minipy bugs](#algorithm-changes-forced-by-real-minipy-bugs)
for the shape that documentation should take), not a case to add as-is.
