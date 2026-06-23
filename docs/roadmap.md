# minipy — Implementation Roadmap

Each milestone is a **self-contained compilable subset**: a grammar slice, its
type rules, its builtins, and the minivm opcodes it targets. Each builds on the
previous. A milestone is "done" when its sample programs compile, type-check, and
run on minivm with the listed opcodes, and its worked example matches the emitted
bytecode.

Cross-references: grammar tags in [`spec/03-grammar.md`](spec/03-grammar.md),
type rules in [`spec/02-types.md`](spec/02-types.md), lowering in
[`spec/05-codegen.md`](spec/05-codegen.md).

Priority: **M0 → M8 are the static core, in order.** **M9 (dynamic typing) is
low priority** and depends on none of the others shipping first.

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
| M9 | **Dynamic typing (`Any`)** | **low** |

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
  bare-name decorators (`@staticmethod` placeholder). Param/return annotations
  **required**. (M2.1: keyword args, default args.)
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

- **Grammar:** `try/except/finally`, `raise`, `with … as …`, `is`/`is not`
  (None identity), built-in exception classes.
- **Types:** exception type hierarchy (subset of classes); `with` target typing.
- **Lowering:** compiler-managed handler-label stack + `finally` on every exit
  edge; `with` → `try/finally` desugar. (A thin CFG/IR may be introduced here if
  the label-chain approach gets unwieldy — see
  [`spec/05-codegen.md`](spec/05-codegen.md#exceptions--with-m7).)
- **Sample:**
  ```python
  def safe_div(a: int, b: int) -> int:
      try:
          return a // b
      except ZeroDivisionError:
          return 0
  ```

## M8 — Modules & stdlib

- **Grammar:** `import name`, `from name import x`; multi-file compilation.
- **Types:** cross-module name resolution; typed module interfaces.
- **Runtime:** a curated typed stdlib subset (`math`, `random`, string helpers)
  exposed as host functions/modules; per-module global namespaces.
- **Sample:**
  ```python
  from math import sqrt
  print(str(sqrt(2.0)))
  ```

---

## M9 — Dynamic typing (`Any`) — LOW PRIORITY

An **opt-in** gradual/dynamic layer on top of the static core, built on minivm's
`ref` ("any") type. It does **not** change the static default and is not a
dependency of M0–M8; it is scheduled last and may be deferred indefinitely.

- **Goal:** allow `Any`-typed values and (optionally) unannotated code to compile
  by treating unknown types as `ref`, recovering concrete types at runtime.
- **Grammar/types:** `Any` annotation accepted everywhere; an optional
  "dynamic module" mode where missing annotations infer `Any` instead of erroring
  (the static `MissingAnnotation` rule is relaxed only in this mode).
- **Lowering:** `Any` slots are minivm `ref`; operations on `Any` insert runtime
  type checks/dispatch via `REF_TEST`/`REF_CAST`; `Any → T` is a checked cast
  (runtime `TypeError`), `T → Any` is free. Boxing/unboxing follows minivm's
  dynamic-value rules ([value-representation](https://github.com/siyul-park/minivm/blob/main/docs/value-representation.md#dynamic-any-values)).
- **Cost:** dynamic operations are slower (boxed, guarded, can miss the JIT). The
  static core stays the fast path; `Any` is the escape hatch.
- **Open questions:** how much operator/attribute dynamism to support; whether to
  allow fully untyped functions; interaction with containers (`list[Any]`).
- **Sample (dynamic mode):**
  ```python
  def describe(x: Any) -> str:
      if isinstance(x, int):
          return "int:" + str(x)
      return "other"
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
