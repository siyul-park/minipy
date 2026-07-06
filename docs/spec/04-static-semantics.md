# Static Semantics

Checker behavior for names, modules, types, class layouts, call targets, control
flow, unsupported features, and diagnostics.

## When to Read

Read this when changing checker behavior, diagnostics, type resolution,
specialization, narrowing, imports, pattern validation, or supported/rejected
statement forms.

For syntax accepted by the parser before these checks, read `03-grammar.md`. For
how checked forms lower to bytecode, read `05-codegen.md`.

## Source of Truth

| Concern | Source |
|---|---|
| checker implementation | `compiler/check.go` |
| type lattice | `types/types.go`, `docs/spec/02-types.md` |
| AST shapes | `ast/ast.go` |
| parser shape | `parser/parser.go`, `docs/spec/03-grammar.md` |
| native call rules | `builtins/`, `operator/`, `docs/spec/06-builtins.md` |
| diagnostics | `token/error.go` |

## Summary

The checker resolves names, modules, types, class layouts, call targets, control
flow, and unsupported-feature diagnostics before bytecode generation. Any lexical,
syntactic, loading, or semantic error stops `Compile` and returns a
`token.ErrorList`.

## Scope and Bindings

### Modules

Each source file is checked as a module. The entry module uses `__main__`; loaded
source modules use canonical dotted module names derived from the configured
module search roots.

Top-level declarations and definitions are stored by module-qualified key except
for `__main__`, whose names remain unqualified. Imports create compile-time
bindings to modules, symbols, or native symbols.

Imports are supported only at module top level. Relative imports resolve from the
current module/package context.

`from __future__ import ...` is recognized before normal declarations. Future
imports may appear only at the start of a module, after an optional module
docstring and before any other statement. Supported future flags:

- `annotations`: accepted for Python compatibility. String annotations resolve
  through the normal annotation resolver regardless of this flag.

Unknown future flags and future imports after ordinary statements are syntax
errors.

`from module import *` is supported only at module top level when the target
module's export set is statically known. Source modules export a static
`__all__` when it is a top-level list or tuple of string literals; otherwise
they export public top-level names that do not start with `_`. Native modules
export their registered symbol names. Star import expansion creates compile-time
bindings only, detects conflicts with existing local names, and performs no
runtime namespace reflection.

### Globals and Locals

- A top-level annotated assignment declares a global slot.
- An unannotated first assignment declares a global or local with the value type.
- Reassignments must be assignable to the declared type.
- Reads before initialization are reported.
- `global` and `nonlocal` are valid only inside functions.
- `nonlocal` must refer to an enclosing local.
- Captured locals are boxed when needed so closures and nonlocal writes share the
  same storage.

`del name` marks a binding definitely uninitialized. Later reads reuse the same
use-before-definition diagnostic as any other uninitialized binding.

## Control Flow

- `break` and `continue` require an enclosing loop.
- `return` requires an enclosing function.
- A non-generator function with a non-`None` result must return on every path.
- `while` and `for` `else` blocks are checked after the loop body.
- `try` must have at least one `except` or `finally` clause.
- Bare `raise` is valid only while checking an `except` handler.

`if` guards participate in flow-sensitive narrowing. When an `if` body always
returns or raises and has no `else`, the negative narrowing of the guard applies
to the rest of the enclosing block.

## Type Resolution

Annotations resolve through primitive names, aliases, classes, imported module
attributes, and generic forms:

```text
int float bool str None Any
list[T] dict[K, V] set[T] tuple[...] Iterator[T] Callable[[...], R]
A | B
typing.Optional[T] typing.Union[...] typing.Annotated[T, ...]
typing.Literal[...] typing.TypeAlias
```

Unknown annotation names, unresolved string forward references, unsupported
generic names, malformed `Callable`, invalid `Annotated` metadata, unsupported
`Literal` arguments, and unsupported attribute annotations are diagnostics.

String annotations are parsed as type expressions and resolved after top-level
classes and functions are declared, so forward references such as `"Node"` and
`list["Node"]` work without runtime annotation evaluation.

`type Name = expr` records a compile-time type alias once `expr` resolves to a
valid type. `Name: TypeAlias = expr` from `typing` is the legacy-compatible form
and records the same kind of alias without creating a runtime binding.

## Inference Rules

The checker uses whole-program inference, but it does not infer across arbitrary
runtime reflection. Important cases:

- unannotated globals/locals bind to the first assigned value type
- unannotated function parameters with defaults use the default value type
- unannotated function parameters without stronger information use `Any`
- unannotated return types join all value-return branch types; no value-return
  means `None`
- lambda parameters are inferred only from an expected `Callable` context
- comprehension targets take the iterable element type
- tuple/list unpacking targets take their corresponding source element types
- pattern captures take the matched subject or sub-pattern type

Different assigned types do not implicitly widen a previously declared binding.
Use an explicit union annotation when a binding may hold multiple types.

## Specialization

A function is specializable when it is not a generator and at least one parameter
has a union or `Any` type. At a direct minipy call site, if all polymorphic
parameters receive concrete argument types, the checker attempts to create a
monomorphic clone for that concrete signature.

Specialization succeeds only when the cloned body type-checks under the concrete
parameter types. It is skipped when:

- the function is not specializable
- any polymorphic argument is non-concrete, a union, `Any`, or invalid
- the per-function specialization cap has been reached
- the instantiation would recursively re-enter itself
- checking the clone produces diagnostics

Skipped calls fall back to the original union/`Any` body. The cap is fixed by the
implementation (`maxSpecializations`).

## Narrowing and Static Truth

The checker recognizes:

```python
isinstance(name, T)
name is None
name is not None
```

for bindings whose current type is a union or `Any`. It overlays narrowed types in
true and false branches. In a specialized function body, when the narrowed value is
already concrete, the checker can statically determine a guard result and the
lowerer prunes the impossible branch.

## Expressions

### Literals and Displays

- Empty list/dict/set displays require an expected type from an annotation or
  context.
- Non-empty lists and sets must be homogeneous.
- Dict keys and values must be homogeneous.
- Dict keys and set elements are limited to `int`, `float`, `bool`, and `str`.
- Tuple displays keep fixed arity and element-specific types.
- Starred list elements accept lists or homogeneous tuples.
- Starred set elements accept sets.
- Dict unpacking accepts dicts; dynamic call `**kwargs` unpacking is rejected.

### Indexing and Slicing

- Lists require `int` indexes and return their element type.
- Dicts require assignable key types and return the value type.
- Strings require `int` indexes and return `str`.
- Tuples require a constant integer index and return the corresponding field type.
- Slicing is supported for lists and strings; bounds must be `int` when present.
- Slice assignment is not supported.

### Operators

Operator type rules are centralized in the `operator` package. Syntax operators
and `operator.*` native calls share the same rules. `and`/`or` require `bool`
operands and return `bool`; `not` is unary bool negation.

`@` is syntactically accepted but unsupported because no matrix type/semantics are
implemented.

### Calls

Direct calls to known minipy functions support:

- positional arguments
- keyword arguments
- defaults
- positional-only and keyword-only parameters
- `*args` and `**kwargs` parameters
- `*tuple` expansion when the tuple has statically known arity

Unsupported call forms include dynamic `**expr`, keyword/starred calls to native
functions, keyword/starred calls through dynamic callable values, and starred
arguments to builtin methods. Constructor calls support keywords/starred arguments
only through dataclass or `__init__`-derived constructor parameter information.

Native functions cannot be first-class values. Class and module objects are also
compile-time-only as values; they may appear as call or attribute receivers in the
supported positions.

List methods are resolved statically on `list[T]` receivers. Supported methods
are `append`, `pop`, `index`, `insert`, `extend`, and `reverse`; unknown list
methods are rejected. Method arity is checked at compile time, element arguments
must be assignable to `T`, `insert` indexes must be `int`, and `extend` accepts
only `list[T]`.

List slice assignment and deletion are resolved statically on `list[T]`
receivers. Bounds must be `int` when present. Assignment requires the right-hand
side to be `list[T]`; tuple, string, bytes, and other receivers are rejected.
Only contiguous mutation is supported: the step must be omitted or a literal
`1`. Dynamic steps and extended slices are rejected with `UnsupportedFeature`.

### Lambdas

A lambda needs an expected `Callable[[...], R]` type. Its parameter count must
match the callable, and its body must be assignable to the callable result.

### F-strings

Replacement fields must be printable. Conversions are limited to `!s`, `!r`, and
`!a`. Format specs may contain one level of nested replacement fields.

### Async and Yield Expressions

`async def`, `async for`, `async with`, async comprehensions, `await`, and yield
expressions parse but are rejected before lowering. Yield statements are supported
inside generator functions returning `Iterator[T]`; a generator cannot return a
value.

## Statements

### Functions

Function definitions are predeclared before bodies are checked, enabling forward
calls within a module. Nested functions are captured as locals. Omitted return
annotations are inferred; annotated returns are enforced. Generators must return
`Iterator[T]` and yield values assignable to `T`.

### Classes

Classes are predeclared before class bodies are checked. Supported class bodies
contain annotated fields, methods, and `pass`.

Constraints:

- at most one supported base class
- no class keywords
- `@dataclass` is the supported class decorator
- non-name decorator expressions are rejected
- methods require `self`
- `__init__` must return `None`
- dataclass fields with defaults must not precede non-default fields

### Pattern Matching

The checker validates each pattern against the subject type and declares capture
bindings. Sequence patterns require list or tuple subjects; mapping patterns
require dict subjects; class patterns require known class subjects and known
fields.

### Exceptions

Builtin exception classes are seeded into the class table. `except` targets must
be classes that inherit from `BaseException`. `raise` accepts exception instances
or compatible exception construction paths according to the checker/lowerer.

`except*` syntax is parsed but ExceptionGroup semantics are not implemented.

### With Statements

`with` statements are checked and lowered through context-manager-style attribute
lookups. `async with` is parse-only.

## Diagnostics

Semantic errors use `token.Error` codes such as `TypeMismatch`, `UndefinedName`,
`UseBeforeDefinition`, `ArityMismatch`, `UnsupportedType`, `UnsupportedFeature`,
`PatternError`, and related codes. The rendered error name follows the associated
Python exception class declared in `token/error.go`.

## Related Docs

- `docs/README.md` — documentation map and ownership guide.
- `docs/spec/02-types.md` — source type system used by the checker.
- `docs/spec/03-grammar.md` — syntax accepted before checker validation.
- `docs/spec/05-codegen.md` — lowering of checked forms.
- `docs/spec/06-builtins.md` — native builtin and operator checker rules.
- `docs/compatibility.md` — user-facing support matrix.
