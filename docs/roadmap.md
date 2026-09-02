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
- Scratch temporaries are frame locals with declared types, pooled per statement
  where safe (a frame containing a `try` or `with` keeps one slot per site), so a
  recursive call cannot clobber the scratch its caller still holds
  (`docs/spec/05-codegen.md`, Scratch slots).

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
  `repr`, `getattr`, `hasattr`, `isinstance`, `map`, `filter`, `compile`, `eval`,
  `exec`, and builtin exceptions.
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
- Keyword/starred calls remain restricted outside direct minipy function calls, except `sorted(..., key=None, reverse=bool)` and `sorted(..., key=Callable[[T], K], reverse=bool)` when K is statically orderable.
- Dynamic `**expr` call unpacking is not supported.
- Dynamic code uses explicit `dict[str, Any]` namespaces. Omitting both namespaces
  creates an empty mapping rather than exposing the caller frame, and code that
  needs a separately linked runtime type pool is rejected.
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

- Upstream: minivm's `transform.NewDCEPass` removes the `unreachable` that
  terminates an exhausted-iterator block, so a program using `next(iter(xs))`
  stops verifying at `-O2` and above (`verify: call: stack underflow`). This is
  why the default optimization level is still `O0`; see
  `docs/codegen-quality.md`, "The DCE defect", and the pinned reproducer in
  `codegen/testdata/control/iterator_next.py`.

- Audit chained assignment behavior and either implement true multi-target
  semantics or reject it explicitly before lowering.
- Keep docs, parser comments, and token comments aligned with the current grammar
  whenever syntax support moves between parse-only and lowered states.
- Add focused regression tests for every compatibility-matrix row that is marked
  ✅ or ◐.

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
