# Roadmap

Current implementation state, explicit restrictions, and remaining known work for
minipy.

## When to Read

Read this when deciding what is shipped, deliberately restricted, planned, or out
of scope.

Do not use this file as the normative language specification. Shipped behavior is
owned by `docs/spec/`; compatibility status is summarized in
`docs/compatibility.md`.

## Source of Truth

| Concern | Source |
|---|---|
| shipped language behavior | `docs/spec/` |
| user-facing support matrix | `docs/compatibility.md` |
| compiler architecture | `docs/spec/00-overview.md` |
| contributor patterns | `docs/coding-patterns.md` |

## Legend

- ✅ shipped
- ◐ shipped with explicit restrictions
- ⏳ parsed/planned but rejected before lowering
- ❌ out of scope unless the project direction changes

## Shipped Core

### Compiler pipeline ✅

- Lexer, parser, checker, direct minivm lowering, optimization, and verification.
- CLI `run` and REPL entry points.
- Compile options for output sink, optimization level, and module search roots.
- Accumulated diagnostics through `token.ErrorList`.

### Static type system ✅

- Primitive source types: `int`, `float`, `bool`, `str`, `bytes`,
  `EllipsisType`, `None`, `Any`.
- Containers: `list[T]`, `dict[K, V]`, `set[T]`, fixed tuples.
- Classes, single inheritance, methods, `@dataclass` construction, builtin
  exception classes.
- `Iterator[T]`, `Callable[[...], R]`, closed unions, `T | None` optional style.
- Whole-program inference for unannotated locals/globals/params/returns where
  supported.
- Flow narrowing for `isinstance(name, T)` and `name is/is not None`.
- Monomorphic specialization for direct calls to union/`Any` parameter functions,
  capped per function with fallback to the original body.

### Statements and expressions ✅ / ◐

- Assignments, annotations, tuple/starred unpacking, augmented assignment for
  names/attributes, `del`, `assert`.
- `if`, `while`, `for`, loop `else`, `break`, `continue`.
- Functions, nested functions, closures, lambdas with `Callable` context.
- Classes with fields/methods, single inheritance, `@dataclass`/`@dataclass()`.
- Function decorators (`@decorator`, `@module.decorator`, `@factory(...)`,
  `@module.factory(...)`, stacking) that evaluate to `Callable[[F], F]` for the
  decorated function's own signature.
- Imports and source-module loading from configured roots.
- Exceptions with `try`/`except`/`else`/`finally`, `raise`, and bare re-raise.
- `with` statements for supported checked context-manager shapes.
- Pattern matching with sequence, mapping, class, value, wildcard, capture, or/as
  patterns and guards.
- List/dict/set/tuple displays, comprehensions, generator expressions, slicing,
  f-strings, named expressions, Ellipsis singleton expressions, and common
  operators.

### Native modules ✅

- `builtins`: `print`, `str`, `int`, `float`, `bool`, `abs`, `len`, `enumerate`,
  `zip`, `range`, `iter`, `next`, `ord`, `chr`, `sorted`, `reversed`, `min`,
  `max`, `sum`, `any`, `all`, `round`, `divmod`, `pow`, `hex`, `oct`, `bin`,
  `repr`, `getattr`, `hasattr`, `isinstance`, `map`, `filter`, and builtin
  exceptions.
- `operator`: syntax operator semantics and native `operator.*` functions share
  one implementation.
- `math`: constants (`pi`, `e`, `tau`, `inf`, `nan`) and functions (`ceil`,
  `floor`, `sqrt`, `log`, `log2`, `log10`, `exp`, `sin`, `cos`, `tan`, `asin`,
  `acos`, `atan`, `atan2`, `fabs`, `fmod`, `copysign`, `pow`, `trunc`,
  `degrees`, `radians`, `isnan`, `isinf`, `isfinite`, `gcd`, `factorial`).
  Constants are `ConstantSymbol` values that emit inline; integer args are
  promoted to float where applicable.
- `string`: constants (`ascii_lowercase`, `ascii_uppercase`, `ascii_letters`,
  `digits`, `hexdigits`, `octdigits`, `punctuation`, `whitespace`, `printable`).
  Constants are `ConstantSymbol` values that emit inline via the constant pool.
- `functools`: higher-order function utilities (`reduce`). Inline-emitted
  iteration loop with lambda type inference for the accumulator function.
  Supports static types and dynamic/Any fallback.
- `random`: pseudo-random number generation (`random`, `randint`, `randrange`,
  `uniform`, `choice`, `shuffle`, `seed`). All functions backed by host
  functions using Go's `math/rand/v2`. Supports static types and dynamic/Any
  fallback.
- `sys`: system constants (`maxsize`, `platform`, `version`, `byteorder`) and
  functions (`getrecursionlimit`, `exit`). Constants are `ConstantSymbol` values
  that emit inline; `exit` halts the VM with UNREACHABLE. Supports static types
  and dynamic/Any fallback.

## Current Explicit Restrictions

These are implemented with deliberate limits, not undocumented bugs.

- Integers are signed 64-bit, not arbitrary precision.
- Floats are `float64`; complex numbers are unsupported.
- Bytes literals and values are supported (`len`, indexing, slicing,
  concatenation, `==`/`!=`, `in`/`not in`, iteration/comprehensions), but there
  is no `bytes()` constructor, no `bytearray`, no bytes methods, no ordering
  comparisons, and no hashing/dict-set key use.
- Empty list/dict/set displays need an annotation or expected context.
- Dict keys and set elements are limited to scalar hashable source types.
- Tuple indexing needs a constant integer index.
- Ellipsis is a literal/bare-name singleton with identity/equality support;
  ellipsis subscripts, `Literal[Ellipsis]`, construction, and builtins-module
  import are not supported.
- Lambdas need an expected `Callable` type.
- Native functions, module objects, and class objects are not first-class runtime
  values.
- `getattr` and `hasattr` accept only concrete class instances and literal names
  of declared/inherited fields. They do not expose methods, defaults, dynamic
  strings, runtime namespace lookup, or compiler-internal fields.
- Keyword/starred calls are restricted outside direct minipy function calls.
- Dynamic `**expr` call unpacking is not supported.
- Multiple class bases, class keywords, and non-`@dataclass`/`@dataclass()` class
  decorators are not supported.
- Function decorators are restricted to a bare name, a module-qualified
  attribute, or a call of either, and must evaluate to `Callable[[F], F]` for
  the decorated function's own signature; other decorator expression shapes
  (arbitrary PEP 614 expressions) are not supported.
- `except*` is parsed but ExceptionGroup semantics are not implemented.
- Async forms parse for diagnostics but are rejected.
- `dict` and `set` do not preserve insertion order (unlike CPython's
  guaranteed dict order since 3.7); iteration order is unspecified and may
  vary between runs. This follows from mapping `dict`/`set` onto minivm's map
  types, which are backed by Go maps with randomized iteration and no
  insertion-order tracking (`minivm/types/map.go`). A fix requires adding
  insertion-order tracking to minivm's map types — upstream work, outside
  this repository. See `docs/spec/02-types.md#iteration-order`.

## Remaining Work

### P0 correctness/consistency

- Audit chained assignment behavior and either implement true multi-target
  semantics or reject it explicitly before lowering.
- Keep docs, parser comments, and token comments aligned with the current grammar
  whenever syntax support moves between parse-only and lowered states.
- Add focused regression tests for every compatibility-matrix row that is marked
  ✅ or ◐.

### P0 defects found by the conformance corpus

Each was reproduced against CPython 3.13 while porting `conformance/testdata/`.
The corpus deliberately avoids these rather than pinning minipy's answer as a
divergence, so fixing one means adding its case.

- Negative list indexing traps the VM for reads and writes (`xs[-1]`), while
  negative slicing, `.pop(-1)`, `.insert(-1, ...)`, and negative string indexing
  all work.
- Reading a field of a user-defined `Exception` subclass segfaults; the same
  class shape on a non-exception base works.
- `assert` inside `try`/`except` always traps with a type mismatch, whatever the
  caught class is, while `raise AssertionError(...)` works.
- Single-argument `dict.get` returns the value type's zero value instead of
  `None` for a missing key, and the checker types the result as `V`, so
  `is None` is rejected.
- `int ** negative_int` and `pow(int, negative_int)` trap instead of producing a
  float.
- `str.split()` with no separator neither collapses whitespace runs nor treats
  tabs and newlines as separators.
- `len(str)` counts UTF-8 bytes rather than codepoints, disagreeing with string
  iteration, which is codepoint-correct.
- `str.format()` ignores every embedded format spec. F-strings honor width,
  precision, and alignment, but not `,` grouping or `#` alternate form.
- `in`/`not in` over `list[tuple[...]]` compares by identity while `==` on the
  same values compares structurally.

### P1 language/runtime improvements

- Dynamic `**kwargs` call unpacking and broader starred-call support.
- First-class callable/module/class value model, if the project decides it is
  worth the extra runtime complexity.
- A broader reflection model (`type`, `issubclass`, metadata attributes, dynamic
  names, module/function/class values) only if first-class runtime metadata is
  introduced deliberately.
- Slice assignment for lists and strings where semantics are clear.
- Generator `send`/`throw`/`close` and return-value propagation; v1 supports
  `yield`/`yield from` with a `None` resume value only.
- Richer class decorators (beyond `@dataclass`/`@dataclass()`), metaclasses, and
  arbitrary PEP 614 decorator expressions.
- Signature-changing decorators and callable parameter packs (`ParamSpec`,
  `Concatenate`-like typing).
- More complete context-manager protocol coverage for `with`.
- ExceptionGroup / `except*` support.
- Scheduler and coroutine runtime support for `async`/`await`.

### P2 Python compatibility expansion

- More standard-library-like native modules.
- More string/list/dict/set methods where they can be statically typed.
- Broader `typing` compatibility where it remains static-only and erasable.
- Better f-string format-spec fidelity.
- More CPython-compatible diagnostics where doing so does not complicate the
  compiler pipeline.

## Out of Scope by Default

- Full CPython object model, descriptors, metaclasses, monkey patching, and MRO
  compatibility.
- C-extension ABI compatibility.
- Arbitrary precision integer semantics.
- Complex numbers unless minivm/runtime support is added deliberately.
- A full standard library clone.

## Related Docs

- `docs/README.md` — documentation map and ownership guide.
- `docs/spec/` — source of truth for shipped language/compiler behavior.
- `docs/compatibility.md` — user-facing support matrix.
- `docs/coding-patterns.md` — contributor patterns for keeping docs/code aligned.
