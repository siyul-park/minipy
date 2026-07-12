# Types

Source-level type system, assignability, inference, narrowing, and specialization
rules for minipy.

## When to Read

Read this when changing source types, annotation parsing, assignability,
printability, inference, narrowing, or function specialization.

For syntax of annotations, read `03-grammar.md`. For checker behavior that uses
these types, read `04-static-semantics.md`. For minivm runtime representation,
read `05-codegen.md`.

## Source of Truth

| Concern | Source |
|---|---|
| source type definitions | `types/types.go` |
| annotation parsing | `parser/*.go` |
| type resolution and inference | `compiler/check*.go` |
| runtime mapping | `compiler/compiler.go`, `docs/spec/05-codegen.md` |
| builtin/operator type rules | `builtins/`, `operator/`, `docs/spec/06-builtins.md` |

## Summary

minipy uses a source-level static type system that is stricter than minivm's
runtime type model. The checker tracks distinctions such as `bool` versus `int`,
container element types, class layouts, callable signatures, and closed unions
before lowering to minivm types.

## Type Universe

| Source type | Meaning | Runtime mapping |
|---|---|---|
| `int` | signed 64-bit integer | `i64` |
| `float` | IEEE-754 `float64` | `f64` |
| `bool` | truth value, distinct from `int` | `i1`/integer boolean |
| `str` | immutable string value | minivm string |
| `bytes` | immutable sequence of bytes (0..255) | minivm `array[i8]` |
| `EllipsisType` | type of the `...` / `Ellipsis` singleton | zero-field minivm struct |
| `None` | absence of value | ref/null-like value |
| `Any` | dynamic fallback top type | dynamic ref |
| `list[T]` | homogeneous mutable sequence | minivm array of `T` |
| `dict[K, V]` | homogeneous map | minivm map `K -> V` |
| `set[T]` | homogeneous set | minivm map `T -> bool` |
| `tuple[T1, T2, ...]` | fixed-arity tuple, possibly heterogeneous | minivm struct |
| class type | declared fields and methods | minivm struct |
| `Iterator[T]` | source iterator type | ref |
| `Callable[[...], R]` | callable value type | minivm function/ref path |
| `A | B` | closed union | dynamic ref with narrowing |
| `Literal[...]` | scalar literal refinement | erased to scalar base type |
| module type | compile-time imported module receiver | compile-time only |

`Any` is not a general escape hatch for all code. It is used when inference
cannot keep a bounded concrete or union type. Operators, calls, and narrowing
still try to recover concrete information where possible.

## Annotation Syntax

The parser accepts these annotation forms:

```python
x: int
y: list[str]
z: dict[str, int]
p: set[int]
t: tuple[int, str]
f: Callable[[int, str], bool]
o: int | None
ref: "Node | None"
lit: Literal[1, "ready"]
ann: Annotated[int, "meta"]
ellipsis: EllipsisType = ...
```

`None` is accepted as an annotation atom. `EllipsisType` names the single
immutable Ellipsis value and cannot be called or directly constructed. `A | B`
is normalized into a closed union; duplicate members collapse, nested unions
flatten, and a single member collapses to that member.

String annotations are parsed as type expressions, not full modules, and then
resolved through the same annotation resolver. They support forward references
to declarations already collected for the module, including nested generic
positions such as `list["Node"]`.

`typing.Annotated[T, meta...]` resolves to `T`. Metadata must be literal syntax
and is ignored after validation.

`typing.Literal[...]` supports `int`, `bool`, `str`, and `None` values. Literal
types refine assignment and call checking when the source value is statically
known, then erase to their scalar base type for operations and code generation.

`type Name = expr` and `Name: TypeAlias = expr` create compile-time aliases once
`expr` resolves to a type. Aliases are scoped through the same module key system
as other compile-time symbols.

`Optional[T]` and `Union[...]` from `typing` normalize to the same forms as
`T | None` and `A | B`.

## Inference

Whole-program inference covers:

- unannotated globals and locals from their first assignment
- unannotated function parameters from default values or from `Any` when no
  stronger source exists
- unannotated returns by joining all value-return branches, with `None` for no
  value-return
- lambda parameter types from an expected `Callable` context
- comprehension targets from iterable element types
- pattern-capture variables from the matched subject type
- tuple/list unpacking targets from the value being unpacked

Inference is deterministic and must finish before code generation. Inference
variables must resolve to a concrete type, closed union, or `Any`; reaching code
generation with an unresolved type variable is a compiler bug.

## Assignability

`AssignableTo(src, dst)` is intentionally simple:

- exact structural equality is assignable
- a matching statically known literal value is assignable to its `Literal[...]`
  refinement
- a `Literal[...]` value is assignable to its erased base type
- any concrete value assignable to a union member may flow into that union
- a union may flow into a wider union that admits all of its members
- any value may flow into `Any`
- implicit numeric coercion is not allowed

For example, `int` is not assignable to `float`; write `float(x)` explicitly.
`bool` is not assignable to `int`.

## Numeric Types

`int` is signed 64-bit and `float` is `float64`. minipy does not implement
Python's arbitrary-precision integers, complex numbers, or implicit mixed numeric
arithmetic. Operators reject unsupported type combinations during checking.

## Bytes

`bytes` is a distinct primitive type, resolved via `types.Resolve("bytes")` and
backed by minivm `array[i8]` — it is not `list[int]` and does not share list's
lowering path. Indexing and direct iteration expose elements as `int` in
`0..255` (the signed `i8` storage is reinterpreted as unsigned). `bytes` is
immutable: item assignment, slice assignment, and deletion are rejected by the
checker (`token.NotIndexable`). `bytes` values do not support `bytes()`
construction, `bytearray`, string-style methods, ordering comparisons (`<`,
`>`, etc.), hashing/use as dict keys or set elements, or `print`/`str`/`repr`/
truthiness — only `len`, indexing, slicing, concatenation (`+`), `==`/`!=`,
`in`/`not in`, and iteration (including comprehensions) are supported.

## Containers and Keys

Lists, dictionaries, and sets are homogeneous. Empty list/dict/set displays need a
type hint from an annotation or expected context because there is no element to
infer from.

Dictionary keys and set elements are limited to hashable scalar source types:
`int`, `float`, `bool`, and `str`.

Tuples are fixed arity and may be heterogeneous. Tuple indexing must use a
constant integer index so the checker can select the exact field type.

## Classes

A class type contains a name and a fixed ordered field list. Class bodies support
annotated fields, methods, and `pass`. Supported inheritance is single-base
inheritance; multiple bases and class keywords are parsed but rejected by the
checker.

`@dataclass` and `@dataclass()` are the supported class decorator forms and behave
identically: both enable constructor arguments from fields and default-field
ordering checks. `@dataclass(...)` with any argument, other class decorators,
and complex decorator expressions are rejected.

A function decorator must evaluate to `Callable[[F], F]`, where `F` is the
decorated function's own signature. See
[04-static-semantics.md](04-static-semantics.md#decorators) for the accepted
decorator expression shapes and evaluation/application order.

Methods require a first `self` parameter. `self` may omit an annotation; if it is
annotated, it must match the containing class type. `__init__` must return `None`.

A restricted set of special methods has fixed signature constraints enforced by
the checker: `__len__(self) -> int`, `__getitem__(self, index) -> T`, and
`__setitem__(self, index, value) -> None`. They let a class participate in
`len(obj)`, `obj[i]`, and `obj[i] = v` through static dispatch; see
`docs/spec/04-static-semantics.md`.

Builtin exception classes are seeded into the class table so `raise`, `except`,
and `isinstance` can reason about their identities.

## Callable and Functions

Function values are represented as `Callable[[P...], R]`. Direct calls to known
minipy functions support positional arguments, keyword arguments, default values,
positional-only parameters, keyword-only parameters, `*args`, and `**kwargs` in
function definitions.

At call sites:

- `*tuple` calls into known minipy functions can expand when the tuple has a
  statically known arity.
- `**expr` dynamic call unpacking is parsed but rejected.
- Keyword/starred calls to native functions, builtin methods, or dynamic callable
  values are restricted as documented in the grammar/static semantics.

Polymorphic functions with union or `Any` parameters are specializable. The
checker creates monomorphic instantiations for concrete call-site argument tuples
when the specialized body type-checks, up to a fixed per-function cap. Calls fall
back to the union/`Any` body when specialization is not possible.

## Unions and Narrowing

Closed unions support flow-sensitive narrowing in two guard forms:

```python
if isinstance(x, T):
    ...

if x is None:
    ...
if x is not None:
    ...
```

The true and false branches receive narrowed overlay types. If a guard result is
statically known inside a specialization, the checker and code generator can skip
impossible branches.

`Optional[T]` from `typing` is represented as `T | None`; there is no separate
optional runtime type.

## Printable Types

`print`, `str`, and f-string replacement fields accept printable types. Printable
values include `int`, `float`, `bool`, `str`, `None`, homogeneous
containers/tuples, printable closed unions, and `Any`. `bytes` and
`EllipsisType` are not printable. User class instances are not generally
printable unless converted through an implemented path.

## Related Docs

- `docs/README.md` — documentation map and ownership guide.
- `docs/spec/03-grammar.md` — annotation syntax.
- `docs/spec/04-static-semantics.md` — checker behavior that applies these types.
- `docs/spec/05-codegen.md` — runtime representation.
- `docs/spec/06-builtins.md` — builtin and operator type rules.
