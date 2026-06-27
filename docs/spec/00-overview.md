# minipy — Overview

minipy is a **statically-typed subset of Python** that compiles ahead-of-time to
[minivm](https://github.com/siyul-park/minivm) bytecode. You write Python with
type hints; minipy type-checks it and emits a minivm program that runs on the
threaded interpreter and ARM64 trace JIT.

minipy is **not** a Python interpreter and is **not** dynamically typed. It is a
small, fast, embeddable language that *looks like* Python and is a strict subset
of CPython 3.13 syntax.

## Design goals

1. **Static, ahead-of-time.** Every type is known at compile time. No runtime
   type dispatch, no `__dict__` name lookup, no `eval`.
2. **A real subset.** Any minipy program is valid Python 3.13 source. minipy only
   *removes* and *constrains* syntax; it never invents new syntax CPython rejects.
3. **Fast on minivm.** Lower directly to minivm opcodes; reuse minivm's optimizer
   and JIT. The type system is designed so common code hits unboxed numeric and
   native container paths.
4. **Small and staged.** Ship a tiny core (M0) first, grow by milestones
   (see [`../roadmap.md`](../roadmap.md)).

## Non-goals

- **No arbitrary-precision `int`.** `int` is **int64** (see below).
- **Gradual typing via M10.** The M10 layer (always on) adds union types,
  whole-program type inference for unannotated code, and `isinstance`/`None`
  narrowing — with minivm's `ref` type backing only the residual dynamic (`Any`)
  slots inference cannot pin down. Fully-annotated code still compiles to the same
  concrete, unboxed fast path; only inferred-dynamic slots are boxed.
- No C extension API, `eval`/`exec`/`compile`, metaclasses, monkey-patching,
  `__getattr__` interception, `__slots__` games, descriptors beyond methods,
  multiple inheritance/MRO, or `complex` numbers.
- No threads/`async` in the core (minivm coroutines back generators, not asyncio).

## Typing model: optional boundary annotations + inference

minipy is statically typed, but you do **not** annotate every line. The rule:

- **Boundary annotations are optional where implemented:** function parameters
  and return types, module-level globals, and locals can be inferred by
  whole-program analysis when enough uses constrain them.
- **Annotations are still preferred at public boundaries** and required where the
  current implementation cannot infer a precise type, such as empty containers.
- **Local variable types are inferred** from their initializer and later checked
  against that inferred type.

```python
# OK — annotations plus inferred locals
def area(w: int, h: int) -> int:
    a = w * h          # a inferred as int
    return a

TOTAL: int = 0         # annotated module global

# OK — inferred from call sites and return body when resolvable
def add(x, y):
    return x + y

# ERROR — TypeMismatch: bool is not assignable to int target
n: int = True
```

Full rules: [`04-static-semantics.md`](04-static-semantics.md).

## `int` is int64, and overflow wraps

`int` maps to minivm `i64`. There is **no bigint**. Arithmetic that overflows
**wraps** with two's-complement semantics (matching minivm's `I64_*` opcodes,
which do not trap on overflow). This differs from CPython, where `int` is
unbounded.

```python
x: int = 9_223_372_036_854_775_807   # i64 max (2**63 - 1)
y: int = x + 1                        # wraps to -9223372036854775808
```

Implementation note: minivm stores integers in `[-2^48, 2^48-1]` inline and
*spills larger i64 values to a heap cell* transparently — still exactly 64-bit,
just a representation detail (see
[minivm value-representation](https://github.com/siyul-park/minivm/blob/main/docs/value-representation.md)).
`//` and `%` by zero raise at runtime (minivm `ErrDivideByZero`).

## Compilation pipeline

No intermediate representation: the typed AST lowers **directly** to a minivm
program with symbolic labels (jumps backpatched). Desugaring
(`for`→iterator loop, comprehensions→loops, `with`→`try/finally`) is an AST→AST
pass. minivm's own optimizer runs after emit.

```text
source (.py)
   │  lex            → tokens (+ INDENT/DEDENT)        01-lexical.md
   │  parse          → AST                             03-grammar.md
   │  typecheck      → typed AST (+ desugar)           02-types.md, 04-static-semantics.md
   │  emit           → minivm program (labels→offsets) 05-codegen.md
   │  optimize       → optimize.O1 (fold/dedup/DCE)    [minivm]
   ▼
minivm program  →  interp.New(...).Run()              [minivm interp + JIT]
```

Builtins (`print`, `len`, `range`, …) bind to inline lowerings or minivm host
functions: [`06-builtins.md`](06-builtins.md).

## Document map

| Doc | Contents |
|---|---|
| [`01-lexical.md`](01-lexical.md) | tokens, indentation, literal subset |
| [`02-types.md`](02-types.md) | type system + Python→minivm type mapping |
| [`03-grammar.md`](03-grammar.md) | the subset grammar, tagged by milestone |
| [`04-static-semantics.md`](04-static-semantics.md) | typing rules, inference, scoping, errors |
| [`05-codegen.md`](05-codegen.md) | lowering each construct to minivm opcodes |
| [`06-builtins.md`](06-builtins.md) | builtins + host-function ABI |
| [`../roadmap.md`](../roadmap.md) | milestones M0–M10 |
| [`../reference/`](../reference/) | upstream CPython 3.13 grammar/lexical/datamodel |
