# Code Generation Quality

What "better emitted code" means for minipy, how it is measured, and the corpus
that pins it.

## When to Read

Read this before changing the lowerer, adding a native symbol's emitter, or
claiming a change makes minipy faster or smaller.

For what each construct lowers to, read `docs/spec/05-codegen.md`. For behavior
against CPython, read `docs/conformance.md`.

## Source of Truth

| Concern | Source |
|---|---|
| what each construct lowers to | `docs/spec/05-codegen.md` |
| the emitted-program corpus | `codegen/testdata/` |
| the golden listing format | `codegen/format.go` |
| opcode semantics and fusion patterns | minivm `docs/instruction-set.md` |
| producer-consumer fusion | minivm `docs/fusion.md` |
| boxing and slot kinds | minivm `docs/value-representation.md` |
| optimizer passes and levels | minivm `docs/pass-system.md` |
| measured performance | `docs/benchmarks.md` |

## Summary

minipy lowers a checked AST straight to minivm bytecode. There is no
intermediate representation and no minipy-owned optimizer, so **the quality of
the emitted program is decided entirely at the point of emission**. A lowering
choice is not something a later pass will clean up.

Two things follow. First, emitted-code quality has to be observable, or it
silently rots — that is what `codegen/` is for. Second, the levers are minivm's,
not minipy's: what the interpreter fuses, what it boxes, and what it has to
guard is what decides whether a sequence is fast.

## Measuring

Every golden in `codegen/testdata/` is headed by the program's metrics:

```
# instructions   90
# functions      0
# host functions 5
# constants      6
# types          1
# globals        2
# locals         4
# handlers       0
```

| Metric | What it says |
|---|---|
| instructions | decoded instructions in the entry code plus every function constant |
| functions | minipy function constants, including specialized clones |
| host functions | native host-function constants — one per *interned value*, not per call |
| constants | constant-pool entries |
| types | runtime type-pool entries |
| globals / locals | declared module global and entry-frame local slots |
| handlers | top-level exception table entries |

`host functions` is the one to watch. A host call is minipy's escape hatch from
the opcode set: it costs a constant-pool entry, a `const.get`, a `call`, and a
Go frame, and it is opaque to both the optimizer and the interpreter's fusion.
Two identical entries in one program mean two call sites that could have shared
one value.

`instructions` is a size measure, not a speed measure. A longer sequence of
fusable opcodes routinely beats a shorter one that ends in a host call.

## Working with the corpus

A case is a `<name>.py` under `codegen/testdata/<category>/` and a sibling
`<name>.masm` golden. `go test ./codegen -update` rewrites every golden from the
compiler's current output; the diff that produces **is** the evidence for a
codegen change and belongs in the commit that makes it.

A golden is a record of what the compiler does, never of what it should do, so
three assertions run beside it and are what keep it honest:

- the compiler verifies every program it returns (`program.Verify`), so a golden
  cannot be blessed into a program the VM would reject;
- every case is executed and must run, so a golden cannot be blessed into a
  program that traps;
- every case is compiled repeatedly and must emit one program, so a golden
  cannot be blessed out of a nondeterministic compiler. Iterating a Go map to
  decide emission order is the way that breaks, and it has broken before.

Add a case when a lowering path has a shape worth pinning, not for every
language feature — behavior belongs in `conformance/`. Keep a case small enough
that its whole listing is readable, and say in a leading comment what the case
is there to show.

## Emission levers

These come from minivm's own contracts. Each names the document that owns it.

### Keep a value on the stack between its producer and its consumer

The threaded interpreter fuses producer-consumer opcode sequences into one
handler, which checks stack room once for the net push instead of once per
source (minivm `docs/fusion.md`). The patterns it covers include:

- a primitive constant feeding a primitive binary operation;
- a typed local feeding a primitive binary operation, with or without a constant;
- a non-trapping primitive binary result stored straight into a typed local;
- a constant index feeding `array.get` or `struct.get`;
- a declared typed-array or struct container in a local, global, or upvalue plus
  a scalar index producer feeding `array.get`/`struct.get`;
- structured-error creation followed by a raise.

The last of these is why a loop puts its test at the bottom. A top-tested loop
has to branch out on the *negation* of its condition, and with no branch-if-false
opcode that means `compare; i32.eqz; br_if exit` — the `i32.eqz` sits between the
compare and the branch and splits a four-instruction fusion into two handlers,
on the hottest path a program has. Entering the loop with a jump to a bottom test
lets the compare feed `br_if` directly and drops the back-edge branch as well.

Parking an intermediate in a scratch slot between the producer and the consumer
breaks the sequence and the fusion with it. Reserve a scratch slot because the
value is needed twice or must outlive a branch — not to make an emitter read
more linearly.

Trapping arithmetic (`div`, `rem`, `mod`) materializes its operands on the stack
regardless, so there is nothing to win by reshaping around it.

### Declare a slot with the kind it will hold

`LOCAL_GET` and `GLOBAL_GET` push the slot's *declared* kind. A slot declared as
a reference makes every read a boxed value, which blocks the numeric fusions
above and forces an unbox before arithmetic. `module.Emitter.Tmp` takes the
minivm type the slot will hold for exactly this reason; naming the concrete
scalar type is both a correctness requirement and the main performance lever
(`docs/spec/05-codegen.md`, "Scratch slots"; minivm
`docs/value-representation.md`).

### Prefer an opcode to a host function

A host function is the right answer when the operation genuinely is not in the
instruction set — string indexing has to decode a rune, a dict read has to
raise on a miss. It is the wrong answer when an opcode sequence exists: the host
call is opaque to fusion, to the optimizer, and to the verifier's type flow.

When a host function is unavoidable, intern **one value per distinct operation
and receiver type** for the whole compilation. A factory called per call site
produces a fresh closure each time, and the constant pool interns by pointer
identity, so it grows linearly with call sites for no behavioral difference.

`compiler/runtime.go` does this through `(*lowerer).host`, keyed by the
operation name and the types that shape it. Native module emitters
(`builtins/`, `operator/`, `math/`, `random/`) do **not** yet: they call their
producer per emission, so a module with several `str(n)` calls still interns one
host function per call. Across the conformance and benchmark corpora, interning
the compiler-side factories took host-function constants from 1653 to 1402 and
the total constant pool from 2232 to 1981; the rest waits on
`module.Emitter` gaining a way for a native symbol to name its operation.

### Recognize a builtin whose shape beats its general form

`for x in range(n)` is the most common loop in Python, and lowering it through
the general iterable path allocated a range iterator on the heap and ran the
`CORO_DONE`/`CORO_VALUE`/`RESUME` protocol once per step. As a counter loop it is
**2.8x faster** — 1277ms to 453ms over 9M iterations, against 425ms for the
hand-written `while` equivalent — and interns one fewer host function.

The rule that made it safe to special-case is that the fast path only applies to
a shape whose semantics it can reproduce exactly: a direct call to the builtin
`range` (identity against the registry's own symbol, so a shadowing definition is
untouched) with a literal step (so the comparison's direction is known at compile
time). Everything else keeps the iterator. A special case that has to guess is
not worth having.

### Let specialization do the narrowing

A direct call whose arguments are concrete resolves to a monomorphic clone, and
the clone lowers its body under concrete types — the `ref.test`/`ref.cast` pair
an `isinstance` guard needs in the union body disappears from it entirely
(`docs/spec/05-codegen.md`, "Specializations"). Emitting a dynamic path where
the checker already produced a specialization throws that away.

## Optimizer levels

`optimize.New(level)` runs minivm's pipeline after lowering:

| Level | Passes |
|---|---|
| O0 | none |
| O1 | fold, dedup |
| O2 | fold, algebraic, dedup, DCE |
| O3 | fold, algebraic, GVN, dedup, DCE |

The passes own every table they touch: dedup compacts the constant and type
pools and renumbers the operands addressing them, and the length-changing passes
repair the exception handler table against the code they relocate. **A
pre-optimization table must never be written back over an optimized program** —
doing so produced an invalid handler range on every `try` at `-O2` and above
(`docs/spec/05-codegen.md`, "Verification and Optimizer Notes").

Every level is a behavior contract: the conformance corpus runs at all four, and
the codegen corpus must still verify at all four.

### Why the default is O0

Measured over the 143 programs of the conformance and benchmark corpora
(entry-code bytes, pool sizes, and the time to compile all of them once):

| Level | code bytes | constants | types | compile |
|---|---:|---:|---:|---:|
| O0 | 52796 | 2000 | 161 | 74ms |
| O1 | 52796 | 2014 | 161 | 93ms |
| O2 | 50630 | 2014 | 161 | 100ms |
| O3 | 50630 | 2014 | 161 | 129ms |

Run time is unchanged: best-of-3 wall clock on `binarytrees` and `matmul` moves
by 2-5% in both directions across the four levels, which is noise.

So O1 is worse than O0 on every axis measured — no smaller code, a slightly
larger constant pool from folding, and a slower compile — and O3 buys nothing
over O2 while costing the most to compile. Only O2's 4.1% code-size reduction is
real, and it comes from a pass with an open defect (below).

### The DCE defect

`transform.NewDCEPass`, which O2 adds, removes the `unreachable` that terminates
an exhausted-iterator block. `unreachable` is a block terminator: without it the
block falls through into the continuation with a shallower stack, and the
program stops verifying —

```
verify: slot 0, ip 79, call: stack underflow
```

`codegen/testdata/control/iterator_next.py` is the smallest reproducer
(`next(iter(xs))`), and running each pass alone over its `-O0` program isolates
DCE as the only one that breaks it. `codegen/codegen_test.go` pins the failure in
`optimizerDefects` rather than skipping it, so the upstream fix turns that test
red and the entry goes away with the defect.

Until then O2 cannot be the default, because it does not compile every valid
program. Raise the default when DCE is fixed; the size win is real and the
correctness net (the conformance corpus at four levels, plus the codegen
corpus's verify pass) is already in place to check it.

Constant folding lives in the optimizer, not the lowerer. `2 + 3 * 4` emits
three constants and two adds at `-O0`; that is expected, and
`codegen/testdata/arithmetic/constant_expression.py` exists to keep it visible.

## Claiming an improvement

`docs/coding-patterns.md` §11.4 requires benchmark evidence for performance
structure. In practice:

- A **size** claim is settled by the golden diff: the metrics header moved, and
  the diff shows what moved it.
- A **speed** claim is settled by `conformance/cmd/pybench` over
  `conformance/testdata/benchmark/`, reported in `docs/benchmarks.md` with the
  machine it was measured on. Do not infer speed from instruction count.
- Note when a measurement is taken on AMD64: minivm's JIT backend is ARM64-only,
  so an AMD64 number is threaded-interpreter throughput and says nothing about
  what a traced loop would do (minivm `docs/instruction-set.md`, "JIT Status").

## Related Docs

- `docs/README.md` — documentation map and ownership guide.
- `docs/spec/05-codegen.md` — what each construct lowers to.
- `docs/conformance.md` — the CPython-differential behavior corpus.
- `docs/benchmarks.md` — measured cross-implementation performance.
- `docs/coding-patterns.md` — the normative coding and architecture standard.
