# CPython Conformance Corpus

Contract for the `conformance` package: a file-based corpus of minipy source
programs pinned against captured CPython 3.13 output.

## When to Read

Read this before adding, editing, or triaging a case under
`conformance/testdata/conformance/`, before touching `conformance/suite.go`,
or when a `conformance` test fails and you need to know whether to fix the
compiler, fix the case, or record a divergence.

## Purpose

`docs/compatibility.md` states, feature by feature, what minipy supports. The
conformance corpus is the executable complement: each case is a real program
that a real CPython 3.13 process actually ran, with its actual stdout
captured byte-for-byte as a golden. Running the same program through minipy
and diffing against that golden catches behavior drift that a hand-written Go
string literal cannot, because a hand-written literal only restates what the
test author believes minipy does — it is not external evidence of what
CPython does.

## The Three-File Scheme

Every case is a `<name>.py` source file under
`conformance/testdata/conformance/<category>/`, paired with sibling golden
files that share its stem:

| File | Meaning |
|---|---|
| `<name>.py` | The program. Must be valid CPython 3.13 **and** valid minipy. |
| `<name>.expected` | CPython 3.13's captured stdout, byte-for-byte. Always present. |
| `<name>.minipy` | minipy's captured stdout. Present **only** for a divergent case. |

`conformance.Load` walks this tree, pairs each `.py` with its goldens, parses
header directives, and returns `conformance.Case` values sorted by name. It
returns an error rather than panicking for any hygiene violation:

- a `.py` with no sibling `.expected`;
- an orphan `.expected` or `.minipy` with no sibling `.py`;
- a `.minipy` golden whose source has no `minipy-divergence` directive, or a
  `minipy-divergence` directive with no sibling `.minipy` golden (the
  biconditional below);
- a `minipy-divergence` directive with no `minipy-divergence-doc`;
- a `minipy-divergence-doc` that resolves to a file that does not exist.

`TestConformance` (`conformance/conformance_test.go`) treats a clean `Load`
as a hygiene assertion in its own right, then compiles and runs every case
through minipy and requires its stdout equal the golden.

Every case runs at **each** optimization level — `O0`, `O1`, `O2`, `O3` — as a
subtest per level. An optimizer level changes which minivm passes rewrite the
emitted program, never what the program means, so the corpus is a behavior
contract for all four rather than for the default alone. Running only the
default hid a miscompile that failed 17 cases at `O2` and `O3`
(`docs/spec/05-codegen.md`, "Verification and Optimizer Notes").

## The Divergence Biconditional

**A `.minipy` golden exists if and only if the source declares a
`# minipy-divergence:` directive.** A case is either:

- **conformant** — no directive, no `.minipy` golden, minipy's output is
  required to equal `.expected` (CPython's output); or
- **divergent** — both a directive and a `.minipy` golden are present,
  minipy's output is required to equal `.minipy` instead, and the directive
  states why.

This is enforced structurally, not by convention: `Load` fails the corpus if
either side of the biconditional is present without the other. A case may
never silently drift into divergence — the moment minipy's output changes,
`TestConformance` fails until a maintainer either fixes the regression or
adds the directive and golden that make the divergence explicit and
documented.

A divergent source's leading comment block carries two directives:

```python
# minipy-divergence: <one-line reason>
# minipy-divergence-doc: <path relative to the repository root, optional #anchor>
```

The doc path is resolved against the repository root (`..` from the
`conformance` package directory) with any `#anchor` fragment stripped before
the existence check; the anchor itself is not verified. It must point at the
spec or compatibility document that already states the divergence as
user-facing behavior — typically `docs/compatibility.md`.

## Authoring Rules

Every case must be valid under **both** CPython 3.13 and minipy. minipy is a
static subset of Python, so the following constraints apply on top of the
compatibility matrix in `docs/compatibility.md`:

- `print()` takes exactly one argument; combine values with an f-string
  instead of comma-separated `print` arguments.
- Container bindings must be annotated: `xs: list[int] = [1, 2, 3]`.
- No `list()`, `dict()`, `set()`, `tuple()`, `type()`, `super()`, or
  `object()` calls.
- `sorted(..., key=None, reverse=bool)` and `sorted(..., key=Callable[[T], K], reverse=bool)` are allowed; callable K must be statically orderable. `key=`/`reverse=` on `min` or `max` remain unsupported.
- No `unittest`; imports are limited to `math`, `string`, `functools`,
  `sys`, `typing`, and `operator`.
- No `if __name__ == "__main__":` — `__name__` is undefined in minipy.
- Avoid constructs with known open compiler work. As of this revision that
  list is empty: `==`/`!=` on `list`/`tuple`/`dict`/`set`, `sorted`/`reversed`/
  `min`/`enumerate`/`zip` over an inline list-of-strings literal, printing a
  `dict` or a `set`, and bare-tuple assignment (`a, b = 1, 2`) were all
  previously excluded here and all now match CPython. Re-verify before
  re-adding an exclusion; a construct that stops matching is a bug to file,
  not a rule to restore.
- Keep cases small and deterministic: arithmetic, string methods, control
  flow, functions, classes, f-strings, and comprehensions over integers are
  all safe ground.
- Corpus cases MUST NOT depend on `dict` or `set` iteration order: `for k in
  d`, `d.keys()`, `d.values()`, `d.items()`, set iteration, and
  comprehensions over a `dict`/`set` are not order-stable across runs (see
  `docs/spec/02-types.md#iteration-order`). Make such a case deterministic by
  sorting first, e.g. `for k in sorted(d.keys()):` instead of `for k in d:`,
  and printing that sorted result. A case that prints a raw `dict` or `set`
  value with more than one entry is invalid, except in
  `conformance/testdata/conformance/divergent/`, where such a case may
  exist specifically to document the ordering divergence itself.

Every case file opens with a provenance comment naming the CPython test
module its semantics were derived from, for example:

```python
# Derived from CPython Lib/test/test_int.py (PSF License, docs/reference/SOURCES.md).
```

## Regenerating Goldens

Never hand-write a golden. Goldens are always produced by running the real
interpreter and capturing its stdout:

```bash
# .expected — CPython's output for one case, generated once when the case is authored
python3.13 conformance/testdata/conformance/<category>/<name>.py > \
  conformance/testdata/conformance/<category>/<name>.expected

# .expected — regenerate every case's golden from CPython in one pass
go test ./conformance -run TestGoldensMatchCPython -update
```

`-update` is a manual maintenance switch. It overwrites `.expected` files in
place and MUST NOT run as part of ordinary verification — verification always
compares against the committed goldens, never regenerates them. There is no
`-update` path for `.minipy` goldens; a divergent case's `.minipy` golden is
generated by running the case through minipy directly (for example with
`go run ./cmd/minipy <path>` or an equivalent harness) and is committed like
any other reviewed test fixture.

`TestGoldensMatchCPython` (`conformance/cpython_test.go`) is skipped, with a
logged reason, when `-short` is set or no `python3.13` interpreter is on
`PATH`. When it runs, it first asserts the resolved interpreter reports
version `(3, 13)`, then runs every case with `PYTHONHASHSEED=0` so output
does not depend on per-process hash randomization.

## Recorded Deviations from the Coding Standard

Two deviations from `docs/coding-patterns.md` §11 are deliberate and
recorded here per §14:

- **§11.2 (keep source and expected output visible near the assertion).**
  The corpus is file-based instead. The invariant that requires it: every
  golden must be a byte-identical artifact produced by an external CPython
  3.13 process, so the expected output is external evidence rather than a
  restatement of minipy's own behavior — a Go string literal cannot satisfy
  that. Inline fixtures remain the default for all other minipy unit tests;
  this deviation is scoped to `conformance` alone.
- **§11.3 (prefer real in-memory registries, filesystems, and runtimes).**
  `TestGoldensMatchCPython` shells out to a real `python3.13` process rather
  than an in-memory double, because the entire point of the test is
  comparison against the real external interpreter; no in-memory CPython
  exists to substitute.

## Adding a Case

1. Write `<name>.py` under the right category directory, following the
   authoring rules above, with a provenance header comment.
2. Generate `<name>.expected` by running it through `python3.13` and
   capturing stdout.
3. Run it through minipy. If output matches `.expected`, the case is done.
4. If minipy's output genuinely and correctly diverges, add both header
   directives, point `minipy-divergence-doc` at the spec or compatibility
   section documenting the divergence, and commit `<name>.minipy` holding
   minipy's actual captured output.
5. Run `go test ./conformance` to confirm `Load` accepts the new case and
   `TestConformance` passes.
