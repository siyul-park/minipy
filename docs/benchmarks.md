# Cross-Implementation Benchmarks

Wall-clock comparison of minipy against CPython, pypy3, and gpython on a
shared corpus of compute-heavy programs, plus the tooling that produces the
comparison.

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

### A sixth defect, now fixed: `nbody`

Unlike the five above, `nbody` was **not** rewritten to route around its
defect, because no alternative formulation was found that avoided it. It is
recorded here because the diagnosis turned out to matter: the defect was not
what this document previously assumed.

`nbody` runs 250,000 `advance()` steps of pairwise gravitational float
arithmetic (`math.sqrt`, multiply-accumulate) over 5 bodies, then prints a
final energy checksum. CPython 3.13, CPython 3.11, and pypy3 all produce
`-0.171931230`; minipy produced `-0.171846486` at both `-O 0` and `-O 3`, a
difference starting at the fifth significant digit. This document read that as
floating-point arithmetic accumulating differently over 250,000 sequential
steps — operation order, `math.sqrt` precision, or fused-multiply-add — and
noted that the divergence was identical at both optimization levels, so no
optimizer pass introduced it.

That reading was wrong. It was a **scratch-slot aliasing bug**: temporaries
were module globals, shared by every activation, so `advance()` and the
functions it calls clobbered each other's list-index temporaries. Moving
scratch to frame locals fixes it, and the fix is visible across builds
independently of the minivm bump:

| build | output |
|---|---|
| CPython 3.13 | `-0.171931230` |
| minipy, scratch in globals | `-0.171846486` |
| minipy, scratch in frame locals, old VM | `-0.171931230` |
| minipy, scratch in frame locals, new VM | `-0.171931230` |

`nbody` now passes `pybench`'s correctness gate at both levels and the results
table below reports real times for it.

The lesson generalizes to the five defects above: a wrong value that looks like
accumulated float error can be a compiler bug, and "identical at `-O 0` and
`-O 3`" rules out the optimizer but not the lowerer. Those five were **not**
re-verified after this fix; they are still described as first observed, and
whether any of them shared this root cause is untested.

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

- Date: 2026-08-06 23:20 UTC
- Kernel: Linux 6.18.5-fc-v18
- CPUs: 4
- RAM: 15.7 GiB
- Implementations:
  - CPython 3.13: Python 3.13.12
  - CPython (python3): Python 3.11.15
  - pypy3: **not found on PATH** — absent on this host, reported rather than
    silently dropped. The rows it carried in the previous measurement are gone
    from the tables below; they are not claims that have been re-verified.
  - gpython: **not found on PATH**, same caveat.
  - minipy -O0: built from this checkout, rev cd54764
  - minipy -O3: built from this checkout, rev cd54764

`-runs 5` was used (the tool's own default), so every table below is a full,
unreduced run of the corpus for the implementations that were present.

### Regressions in this measurement, and what causes them

Three builds were timed to separate the two changes that landed together — the
scratch-temporaries refactor and the minivm bump — since the bump could not land
on its own (the new verifier propagates a slot's declared type through
`GLOBAL_GET` where the old one pushed `KindAny`, so the old uniformly-`TypeRef`
global table stops verifying). Min-of-3 wall clock at `-O 0`:

| case | base + old VM | refactor + old VM | refactor + new VM |
|---|---:|---:|---:|
| binarytrees | 1782ms | 1872ms | **2501ms** |
| sortstress | 3279ms | 3142ms | **1971ms** |
| strbuild | 5966ms | 5573ms | 5372ms |
| matmul | 1844ms | 1744ms | 1754ms |
| nbody | 6736ms | 6452ms | 6536ms |
| fib | 2688ms | 2690ms | 2787ms |
| fannkuch `-O 3` | 6681ms | 5812ms | **times out** |

The middle column is the refactor alone. It is neutral to slightly positive
everywhere — strbuild -7%, matmul -5%, nbody -4%, sortstress -4%, fib unchanged,
binarytrees +5% — so moving scratch into frame locals does not cost meaningful
performance.

**Both regressions come from the minivm bump, not from the refactor.**

1. **`fannkuch` no longer finishes at any optimization level.** It passed at
   6.2-6.7s on the old VM at both `-O 0` and `-O 3`; on the new VM it exceeds
   45s at `-O 0`, `-O 1`, `-O 2`, and `-O 3` alike. This is *runtime*, not
   compile time — compiling `fannkuch.py` takes 1.5ms at `-O 0` and 2.5ms at
   `-O 3` — and it is level-independent, so it is not an optimizer pass. Filed
   upstream; tracked here as #62.
2. **`binarytrees` is 34% slower** (1872ms -> 2501ms) purely across the VM
   bump. `sortstress` is 37% *faster* across the same bump, so the new VM is not
   uniformly slower; something specific to this workload regressed.

`strbuild` is faster than the previous measurement, not slower. An earlier
reading of this measurement attributed both regressions to the refactor; the
three-build comparison above supersedes it.

The refactor's own trade is still deliberate: scratch in module globals was
shared by every activation, so a recursive call silently corrupted its caller's
temporaries (see `docs/spec/05-codegen.md`, Scratch slots).

The defects recorded under
[Algorithm changes forced by real minipy bugs](#algorithm-changes-forced-by-real-minipy-bugs)
were **not** re-verified in this measurement. They are still described as first
observed.

### Startup baseline (empty program)

| Implementation | Min | Median |
|---|---:|---:|
| CPython 3.13 | 13.0ms | 14.8ms |
| CPython (python3) | 11.5ms | 11.9ms |
| minipy -O0 | 3.9ms | 4.3ms |
| minipy -O3 | 3.4ms | 3.7ms |

## Results

### binarytrees

| Implementation | Status | Min | Median | Min - startup | Median - startup |
|---|---|---:|---:|---:|---:|
| CPython 3.13 | PASS | 847.2ms | 877.8ms | 834.2ms | 863.1ms |
| CPython (python3) | PASS | 1032.5ms | 1053.7ms | 1020.9ms | 1041.9ms |
| minipy -O0 | PASS | 2530.2ms | 3251.2ms | 2526.2ms | 3246.9ms |
| minipy -O3 | PASS | 2144.9ms | 2401.4ms | 2141.5ms | 2397.7ms |

### fannkuch

| Implementation | Status | Min | Median | Min - startup | Median - startup |
|---|---|---:|---:|---:|---:|
| CPython 3.13 | PASS | 1253.7ms | 1267.6ms | 1240.8ms | 1252.9ms |
| CPython (python3) | PASS | 1052.2ms | 1061.4ms | 1040.6ms | 1049.6ms |
| minipy -O0 | FAIL (timed out after 40s) | - | - | - | - |
| minipy -O3 | FAIL (timed out after 40s) | - | - | - | - |

### fib

| Implementation | Status | Min | Median | Min - startup | Median - startup |
|---|---|---:|---:|---:|---:|
| CPython 3.13 | PASS | 2093.9ms | 2147.3ms | 2080.9ms | 2132.5ms |
| CPython (python3) | PASS | 1945.9ms | 1971.9ms | 1934.4ms | 1960.0ms |
| minipy -O0 | PASS | 2766.9ms | 2847.2ms | 2762.9ms | 2842.9ms |
| minipy -O3 | PASS | 2812.1ms | 2830.6ms | 2808.6ms | 2826.9ms |

### mandelbrot

| Implementation | Status | Min | Median | Min - startup | Median - startup |
|---|---|---:|---:|---:|---:|
| CPython 3.13 | PASS | 1700.5ms | 1712.6ms | 1687.5ms | 1697.8ms |
| CPython (python3) | PASS | 1407.1ms | 1583.5ms | 1395.5ms | 1571.6ms |
| minipy -O0 | PASS | 1560.2ms | 1564.1ms | 1556.2ms | 1559.8ms |
| minipy -O3 | PASS | 1563.3ms | 1596.5ms | 1559.9ms | 1592.8ms |

### matmul

| Implementation | Status | Min | Median | Min - startup | Median - startup |
|---|---|---:|---:|---:|---:|
| CPython 3.13 | PASS | 1067.8ms | 1193.1ms | 1054.8ms | 1178.4ms |
| CPython (python3) | PASS | 1222.7ms | 1297.9ms | 1211.2ms | 1286.0ms |
| minipy -O0 | PASS | 1766.1ms | 1791.7ms | 1762.1ms | 1787.4ms |
| minipy -O3 | PASS | 1768.5ms | 1795.0ms | 1765.0ms | 1791.3ms |

### nbody

| Implementation | Status | Min | Median | Min - startup | Median - startup |
|---|---|---:|---:|---:|---:|
| CPython 3.13 | PASS | 1326.8ms | 1385.1ms | 1313.8ms | 1370.4ms |
| CPython (python3) | PASS | 1250.0ms | 1280.4ms | 1238.5ms | 1268.6ms |
| minipy -O0 | PASS | 6529.9ms | 6662.1ms | 6525.9ms | 6657.8ms |
| minipy -O3 | PASS | 6542.8ms | 6642.3ms | 6539.4ms | 6638.6ms |

### nqueens

| Implementation | Status | Min | Median | Min - startup | Median - startup |
|---|---|---:|---:|---:|---:|
| CPython 3.13 | PASS | 967.2ms | 999.4ms | 954.2ms | 984.6ms |
| CPython (python3) | PASS | 787.0ms | 790.2ms | 775.4ms | 778.3ms |
| minipy -O0 | PASS | 2999.9ms | 3039.6ms | 2996.0ms | 3035.3ms |
| minipy -O3 | PASS | 2976.1ms | 2998.8ms | 2972.7ms | 2995.1ms |

### sortstress

| Implementation | Status | Min | Median | Min - startup | Median - startup |
|---|---|---:|---:|---:|---:|
| CPython 3.13 | PASS | 721.7ms | 739.5ms | 708.8ms | 724.7ms |
| CPython (python3) | PASS | 660.7ms | 676.0ms | 649.1ms | 664.1ms |
| minipy -O0 | PASS | 1975.9ms | 2010.0ms | 1972.0ms | 2005.7ms |
| minipy -O3 | PASS | 1988.6ms | 2040.3ms | 1985.2ms | 2036.6ms |

### spectralnorm

| Implementation | Status | Min | Median | Min - startup | Median - startup |
|---|---|---:|---:|---:|---:|
| CPython 3.13 | PASS | 1335.8ms | 1358.6ms | 1322.8ms | 1343.8ms |
| CPython (python3) | PASS | 1280.0ms | 1324.1ms | 1268.5ms | 1312.3ms |
| minipy -O0 | PASS | 2497.3ms | 2519.1ms | 2493.4ms | 2514.8ms |
| minipy -O3 | PASS | 2421.9ms | 2495.9ms | 2418.5ms | 2492.2ms |

### strbuild

| Implementation | Status | Min | Median | Min - startup | Median - startup |
|---|---|---:|---:|---:|---:|
| CPython 3.13 | PASS | 1195.3ms | 1231.4ms | 1182.4ms | 1216.7ms |
| CPython (python3) | PASS | 1228.1ms | 1236.3ms | 1216.6ms | 1224.4ms |
| minipy -O0 | PASS | 5536.8ms | 5887.0ms | 5532.9ms | 5882.7ms |
| minipy -O3 | PASS | 5072.4ms | 5162.7ms | 5069.0ms | 5158.9ms |

## Reading the Results

All ratios below use each case's `minipy` **min** against CPython 3.13's own
**min**, since min is the noise-resistant estimate `pybench` itself prefers
(see [What it does](#what-it-does), item 5); "tied" means within about 1%,
smaller than run-to-run noise on this host.

- **minipy `-O0` vs CPython 3.13**, over the cases where both pass the
  correctness gate: `mandelbrot` is the one case minipy wins. The rest run
  roughly 1.3x-4.6x slower, `strbuild` worst at about 4.6x (5536.8ms against
  1195.3ms). Read the per-case tables above for exact numbers rather than a
  summarized band; the regressions described under
  [Regressions in this measurement](#regressions-in-this-measurement-and-what-causes-them)
  move several of these against the previous run.
- **minipy `-O0` vs `-O3`**: `-O 3` is at or slightly ahead of `-O 0` on the
  cases that pass at both levels. The one categorical difference the previous
  measurement recorded — `fannkuch` passing at `-O 3` in 6.4s where `-O 0`
  times out — **no longer holds**: `fannkuch` now times out at every level from
  `-O 0` to `-O 3`. That is a regression in the minivm bump, not a property of
  the corpus; see above. Nothing here licenses "always pass `-O 3`" as a general rule; it says
  only what this measurement showed.
- **`nbody` now passes** at both levels, where the previous measurement failed
  the correctness gate. That was a compiler bug, not float drift — see
  [the sixth defect](#a-sixth-defect-now-fixed-nbody) above. It runs about 4.9x
  slower than CPython 3.13.
- **Startup**: minipy starts in roughly 3.4-4.3ms against CPython 3.13's
  13.0-14.8ms, about 3-4x faster. On the short cases that is a visible share of
  wall clock, which is why every table carries a "minus startup" column.
- **pypy3 and gpython** were not present on this host, so this measurement says
  nothing about them. The previous measurement's findings — pypy3 fastest on
  every case it ran, gpython passing only 3 of 10 and slower than CPython on
  all three — stand as previously recorded and were not re-verified here.

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
