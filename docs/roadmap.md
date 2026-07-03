# minipy — Implementation Roadmap

Each milestone is a **self-contained compilable subset**: a grammar slice, its
type rules, its builtins, and the minivm opcodes it targets. Each builds on the
previous. A milestone is "done" when its sample programs compile, type-check, and
run on minivm with the listed opcodes, and its worked example matches the emitted
bytecode.

Cross-references: grammar tags in [`spec/03-grammar.md`](spec/03-grammar.md),
type rules in [`spec/02-types.md`](spec/02-types.md), lowering in
[`spec/05-codegen.md`](spec/05-codegen.md).

Priority: **M0 → M9 are the static core, in order.** **M10 (inference, unions &
specialization) is low priority** and depends on none of the others shipping
first.

| Milestone | Theme | Priority |
|---|---|---|
| M0 | Expressions & literals | core |
| M1 | Control flow | core |
| M2 | Functions | core |
| M3 | Strings & containers | core |
| M4 | Closures & comprehensions | core |
| M5 | Classes | core |
| M6 | Generators & iterators | core |
| M7 | Exceptions & context managers | core |
| M8 | Modules & stdlib | core |
| M9 | Statement completeness & pattern matching | core |
| M10 | **Inference, unions & specialization** | **low** |

---

## M0 — Expressions & literals

The smallest runnable language: a module is a list of top-level statements over
scalars.

- **Grammar:** module statements; annotated globals (`x: int = …`); plain/aug
  assignment to a `NAME`; `expr_stmt`; full operator precedence chain over scalars;
  grouping `( )`.
- **Types:** `int`, `float`, `bool`, `str`, `None`. No implicit coercion;
  `int`=int64 wrap.
- **Builtins:** `print`, `int`, `float`, `str`, `bool`, `abs`.
- **Opcodes:** `I64_*`, `F64_*`, `I32_*` (bool/compare), `STRING_CONCAT`,
  `GLOBAL_*`, `CONST_GET`, `CALL` (host `print`).
- **Out:** control flow, functions, containers.
- **Sample:**
  ```python
  x: int = 6
  y: int = 7
  print(str(x * y))      # 42
  ```

## M1 — Control flow

- **Grammar:** `if/elif/else`, `while`, `for NAME in range(...)`, `break`,
  `continue`, `pass`, `else` on loops; conditional expression `a if c else b`.
- **Types:** conditions must be `bool`.
- **Opcodes:** `BR`, `BR_IF`, `I32_EQZ`, `SELECT`; `range` for-loop desugar.
- **Sample:**
  ```python
  total: int = 0
  for i in range(1, 101):
      if i % 2 == 0:
          total = total + i
  print(str(total))      # 2550
  ```

## M2 — Functions

- **Grammar:** `def NAME(params) -> type:`; positional args; `return`; recursion;
  bare-name decorators (`@staticmethod` placeholder). The original milestone
  required param/return annotations; the shipped M10 layer now infers missing
  annotations where possible. (M2.1: keyword args, default args.)
- **Types:** structural `Callable`; arity/positional type checks; local inference
  in bodies.
- **Opcodes:** `*Function` constants, `CALL`, `RETURN`, `RETURN_CALL` (tail),
  `LOCAL_*`.
- **Sample:**
  ```python
  def fib(n: int) -> int:
      if n < 2:
          return n
      return fib(n - 1) + fib(n - 2)
  print(str(fib(20)))    # 6765
  ```

## M3 — Strings & containers

- **Grammar:** list/dict/tuple displays; indexing/subscript; `in`/`not in`;
  f-strings; flat tuple-unpack assignment and `for k, v in …`; common
  list/dict/str methods.
- **Types:** `list[T]`, `dict[K,V]`, `tuple[…]`; homogeneous lists; tuple constant
  indexing.
- **Opcodes:** `ARRAY_*`, `MAP_*`, `STRUCT_*`, `STRING_*`.
- **Builtins:** `len`, `enumerate`, `zip`, container methods.
- **Sample:**
  ```python
  counts: dict[str, int] = {}
  for w in ["a", "b", "a"]:
      counts[w] = counts.get(w, 0) + 1
  print(str(counts["a"]))   # 2
  ```

## M4 — Closures & comprehensions

- **Grammar:** nested `def`, `lambda`, `global`/`nonlocal`, list/dict/set
  comprehensions.
- **Types:** capture analysis; `lambda` param inference from call site.
- **Opcodes:** `CLOSURE_NEW`, `UPVAL_GET/SET`, `REF_NEW/GET/SET` (mutable capture).
- **Sample:**
  ```python
  def adder(n: int) -> Callable[[int], int]:
      return lambda x: x + n
  add5 = adder(5)
  print(str(add5(10)))   # 15
  squares = [i * i for i in range(5)]
  ```

## M5 — Classes

- **Grammar:** `class NAME[(Base)]:` with annotated fields and methods; `__init__`;
  attribute access/assignment; `@dataclass`.
- **Types:** class type = struct; static method resolution; single inheritance
  (field append).
- **Opcodes:** `STRUCT_NEW(_DEFAULT)`, `STRUCT_GET/SET`, method `CALL`.
- **Sample:**
  ```python
  class Point:
      x: int
      y: int
      def __init__(self, x: int, y: int) -> None:
          self.x = x
          self.y = y
      def norm2(self) -> int:
          return self.x * self.x + self.y * self.y
  print(str(Point(3, 4).norm2()))   # 25
  ```

## M6 — Generators & iterators

- **Grammar:** `yield`; generator functions; `for` over generators/iterators;
  lazy `range`.
- **Types:** `Iterator[T]`/generator types; the iterator protocol.
- **Opcodes:** `YIELD`, `RESUME`, `CORO_DONE`, `CORO_VALUE`.
- **Sample:**
  ```python
  def upto(n: int) -> Iterator[int]:
      i = 0
      while i < n:
          yield i
          i = i + 1
  total: int = 0
  for v in upto(5):
      total = total + v       # 0+1+2+3+4 = 10
  ```

## M7 — Exceptions & context managers

Status: complete.

- **Grammar:** `try/except/finally`, `raise`, `with … as …`, `is`/`is not`
  (None identity), built-in exception classes.
- **Types:** exception type hierarchy (subset of classes); `with` target typing.
- **Lowering:** minivm handler tables via `program.Builder.Try`, `THROW`, and
  `ERROR_NEW`; `finally` on every exit edge; `with` -> `try/finally` desugar.
  VM traps are bridged to exception instances with one host function; guest
  exception matching stays in bytecode. See
  [`spec/05-codegen.md`](spec/05-codegen.md#exceptions--context-managers).
- **Sample:**
  ```python
  def safe_div(a: int, b: int) -> int:
      try:
          return a // b
      except ZeroDivisionError:
          return 0
  ```

## M8 — Modules & stdlib

- **Status:** module system complete; curated external stdlib modules remain
  future library work.
- **Grammar:** `import name [as alias]`, dotted imports, `from name import x as y`,
  relative imports, package `__init__.py`; `from ... import *` is explicitly
  unsupported.
- **Types:** cross-module name resolution over explicit `fs.FS` search roots;
  per-module qualified globals; imported classes/functions/globals usable through
  module attributes or from-import bindings; native `builtins` and `operator`
  modules.
- **Runtime:** imported module bodies inline once at first import point, parents
  before children, preserving observable import order. The CLI adds the script
  directory as `sys.path[0]` and supports repeatable `--path/-p`.
- **Sample:**
  ```python
  import helper
  from pkg.sub import value as v
  print(str(helper.double(v)))
  ```

## M9 — Statement completeness & pattern matching

The final static-core milestone absorbs forms previously listed as rejected but
still required for Python-subset completeness. It must ship **before** M10 so the
low-priority inference layer remains last.

- **Grammar:** `del`, `assert`, and `match`/`case` structural pattern matching.
  Supported patterns: wildcard `_`, capture, literal/value patterns over existing
  scalar/container/class values, sequence patterns, mapping patterns, class
  patterns, `|` alternatives, `as` patterns, and optional `if` guards. Starred
  sequence/mapping rest patterns are included here even though other unpacking
  forms stay milestone-specific.
- **Types:** `del NAME` makes the binding definitely-unassigned until assigned
  again; later reads use existing `UseBeforeDefinition` diagnostics. Pattern
  capture variables are declared/initialized on the matching case arm and must
  have a consistent type across alternatives in the same pattern. Guards must be
  `bool`. `assert` messages may be any printable scalar.
- **Lowering:** `assert test[, msg]` evaluates `test`; false path builds an
  `AssertionError` payload with minivm `ERROR_NEW` and raises it with `THROW`.
  `match` lowers to a decision tree with `BR_IF` (and `BR_TABLE` where profitable)
  reusing existing comparison/container/class opcodes. minivm has no slot-delete
  opcode, so `del NAME` stores the slot's uninitialized value (`types.Zero(kind)`:
  `REF_NULL` for refs, a typed zero const for scalars) and the checker marks the
  binding definitely-unassigned. Container key deletion uses `MAP_DELETE`; list
  item deletion reuses the `list.pop(i)` host; attribute deletion zeroes the field
  with `STRUCT_SET`.
- **Sample:**
  ```python
  status: int = 200
  match status:
      case 200:
          print("ok")
      case _:
          assert False, "unexpected status"
  ```

---

## M10 — Whole-program inference, unions & specialization

Status: complete (with one deferral, noted below).

A gradual layer on top of the static core. It adds three things the core
deliberately omits — **union types**, **whole-program type inference** (so
unannotated code still compiles), and **specialization** (compiling polymorphic
functions). minivm's `ref` ("any") type backs only the *residual* dynamic slots
that inference cannot pin down.

**As shipped (deviations from the original opt-in design, per project decision):**

- **Always on, no flag.** Whole-program inference is the default, not opt-in:
  unannotated globals are inferred from their first assignment, and unannotated
  parameters/returns are solved from the body. `MissingAnnotation` now fires only
  where inference cannot constrain a binding (e.g. a lambda with no Callable
  context), not merely because an annotation is absent.
- **Specialization is the union-typed-body fallback, not per-type
  monomorphization.** An unannotated parameter resolves to `Any` (the lattice
  top) and the function is compiled **once** with a dynamic `ref` body, reusing
  the union + `isinstance`/`REF_CAST` machinery. Emitting a separate `*Function`
  per concrete instantiation (`f$int`, `f$str`, …) — the spec's monomorphization
  optimization — is **deferred**; the single union-typed body is the documented
  cap fallback and produces identical results.
- **Inferred recursion** needs an explicit return annotation (a self-call sees
  the return type before the body finishes inferring it).

### Goals

- **Unions, not just `Any`.** Accept `A | B` / `Union[A, B]` of arbitrary types as
  a first-class **closed disjunction**, lowered to a tagged `ref`. `Optional[T]`
  becomes the special case `T | None`. `Any` is the **open top** of the lattice —
  the fallback used only when no bounded union fits.
- **Compile unannotated code by inference.** In *inference mode* the
  `MissingAnnotation` rule is relaxed: instead of erroring, the compiler solves for
  the types of unannotated bindings across the **whole program**, from call sites
  and bodies. Annotated boundaries stay fixed and seed the solver.
- **Minimize types.** Each binding gets the **narrowest** type consistent with all
  its uses (`concrete < closed-union < Any`). Because most slots resolve to a
  concrete type, runtime tag-checks/casts are emitted **only** where a slot is
  still a union or `Any` — unnecessary type checks are never generated.
- **Specialize like generics.** A function used at several concrete types is
  **monomorphized** — one `*Function` per instantiation, each call site linked to
  its specialization. Where instantiations would explode, fall back to a single
  union-typed body with internal tag dispatch.

### Mechanics

- **Type lattice:** `⊥ < concrete types < closed unions < Any (⊤)`. Inference picks
  the join/meet that keeps each binding minimal.
- **Narrowing:** `isinstance(x, T)` and `x is None` remove a member from a union
  inside the guarded branch (generalizing the core `Optional` rule); the dispatch
  is paid once and reused, not re-checked.
- **Representation:** concrete slots stay unboxed/native (the static fast path);
  union/`Any` slots are minivm `ref`, boxed with a runtime tag. `T → A|B` widens for
  free; `A|B → T` is a checked `REF_CAST` (runtime `TypeError`) unless flow already
  proved it. `isinstance` is `REF_TEST`. Boxing/unboxing follows minivm's
  dynamic-value rules ([value-representation](https://github.com/siyul-park/minivm/blob/main/docs/value-representation.md#dynamic-any-values)).
- **Linking:** monomorphic call sites emit a direct `CALL`/`RETURN_CALL` to the
  concrete specialization; only genuine union calls go through a tag switch. Full
  lowering: [`spec/05-codegen.md`](spec/05-codegen.md#unions-any--specialization-m10).

### Cost

Concrete (specialized) code is exactly as fast as the static core. Only the
residual union/`Any` slots are boxed, tag-guarded, and may miss the JIT — and the
minimization pass keeps those to the few places real dynamism survives.

### Open questions / remaining work

- Per-type monomorphization (one `*Function` per concrete instantiation) as an
  optimization over the current single union/`Any`-typed body, with an
  instantiation cap and union-body fallback past it.
- Inferred polymorphic recursion (a self-call currently sees the return type
  before the body finishes inferring it; annotate the return to recurse).
- Reassignment-driven widening of an inferred global to a union, and richer
  operator/attribute dispatch on bare unions.

### Samples

```python
# Union + narrowing
def describe(x: int | str) -> str:
    if isinstance(x, int):
        return "int:" + str(x)   # x narrowed to int
    return "str:" + x            # x narrowed to str

# Whole-program inference — no annotations, still compiles in inference mode
def identity(x):
    return x

a = identity(3)        # identity specialized at int -> a: int
b = identity("hi")     # identity specialized at str -> b: str
```

---

## Verification per milestone

For every milestone, before marking it done:

1. The sample program type-checks and emits a minivm program.
2. The hand-written worked example (in [`spec/05-codegen.md`](spec/05-codegen.md)
   for M0; per-milestone thereafter) matches the emitted bytecode; operand widths
   checked against minivm `instr/type.go`, semantics against minivm
   `docs/instruction-set.md`.
3. The program runs under `interp.New(prog).Run(ctx)` and produces the expected
   result/output.
4. Negative tests: each new error in the catalogue
   ([`spec/04-static-semantics.md`](spec/04-static-semantics.md#error-catalogue))
   has a program that triggers it.
