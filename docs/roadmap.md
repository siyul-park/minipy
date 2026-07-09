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

- Primitive source types: `int`, `float`, `bool`, `str`, `None`, `Any`.
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
- Classes with fields/methods, single inheritance, `@dataclass`.
- Imports and source-module loading from configured roots.
- Exceptions with `try`/`except`/`else`/`finally`, `raise`, and bare re-raise.
- `with` statements for supported checked context-manager shapes.
- Pattern matching with sequence, mapping, class, value, wildcard, capture, or/as
  patterns and guards.
- List/dict/set/tuple displays, comprehensions, generator expressions, slicing,
  f-strings, named expressions, and common operators.

### Native modules ✅

- `builtins`: `print`, `str`, `int`, `float`, `bool`, `abs`, `len`, `enumerate`,
  `zip`, `range`, `iter`, `next`, `isinstance`, and builtin exceptions.
- `operator`: syntax operator semantics and native `operator.*` functions share
  one implementation.

## Current Explicit Restrictions

These are implemented with deliberate limits, not undocumented bugs.

- Integers are signed 64-bit, not arbitrary precision.
- Floats are `float64`; complex numbers are unsupported.
- Bytes runtime values are unsupported.
- Empty list/dict/set displays need an annotation or expected context.
- Dict keys and set elements are limited to scalar hashable source types.
- Tuple indexing needs a constant integer index.
- Lambdas need an expected `Callable` type.
- Native functions, module objects, and class objects are not first-class runtime
  values.
- Keyword/starred calls are restricted outside direct minipy function calls.
- Dynamic `**expr` call unpacking is not supported.
- Multiple class bases, class keywords, and non-`@dataclass` class decorators are
  not supported.
- `except*` is parsed but ExceptionGroup semantics are not implemented.
- Async forms parse for diagnostics but are rejected.

## Remaining Work

### P0 correctness/consistency

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
- Slice assignment for lists and strings where semantics are clear.
- Generator `send`/`throw`/`close` and return-value propagation; v1 supports
  `yield`/`yield from` with a `None` resume value only.
- Richer class decorators or decorator-call lowering.
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
