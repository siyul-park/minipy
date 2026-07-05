# Types

minipy uses a source-level static type system that is stricter than minivm's
runtime type model. The checker tracks distinctions such as `bool` versus `int`,
container element types, class layouts, and closed unions before lowering to
minivm types.

## Type universe

| Source type | Meaning | Runtime mapping |
|---|---|---|
| `int` | signed 64-bit integer | `i64` |
| `float` | IEEE-754 `float64` | `f64` |
| `bool` | truth value, distinct from `int` | `i1`/integer boolean |
| `str` | immutable string value | minivm string |
| `None` | absence of value | ref/null-like value |
| `Any` | dynamic fallback top type | dynamic ref |
| `list[T]` | homogeneous mutable sequence | minivm array of `T` |
| `dict[K, V]` | homogeneous map | minivm map `K -> V` |
| `set[T]` | homogeneous set | minivm map `T -> bool` |
| `tuple[T, ...]` | fixed arity heterogeneous tuple | minivm struct |
| class type | declared fields and methods | minivm struct |
| `Iterator[T]` | iterator/coroutine-like producer | ref |
| `Callable[[...], R]` | callable value type | minivm function/ref path |
| `A | B` | closed union | dynamic ref with narrowing |
| module type | compile-time imported module receiver | compile-time only |

`Any` is not a general escape hatch for all code. It is used when inference
cannot keep a bounded concrete or union type. Operators, calls, and narrowing
still try to recover concrete information where possible.

## Annotation syntax

The parser accepts these annotation forms:

```python
x: int
y: list[str]
z: dict[str, int]
p: set[int]
t: tuple[int, str]
f: Callable[[int, str], bool]
o: int | None
```

`None` is accepted as an annotation atom. `A | B` is normalized into a closed
union; duplicate members collapse, nested unions flatten, and a single member
collapses to that member.

`type Name = expr` creates a compile-time alias once `expr` resolves to a type.
Aliases are scoped through the same module key system as other compile-time
symbols.

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
- any concrete value assignable to a union member may flow into that union
- a union may flow into a wider union that admits all of its members
- any value may flow into `Any`
- implicit numeric coercion is not allowed

For example, `int` is not assignable to `float`; write `float(x)` explicitly.
`bool` is not assignable to `int`.

## Numeric types

`int` is signed 64-bit and `float` is `float64`. minipy does not implement
Python's arbitrary-precision integers, complex numbers, or implicit mixed numeric
arithmetic. Operators reject unsupported type combinations during checking.

## Containers and keys

Lists, dictionaries, and sets are homogeneous. Empty list/dict/set displays need
a type hint from an annotation or expected context because there is no element to
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

`@dataclass` is the supported class decorator. It enables constructor arguments
from fields and default-field ordering checks. Other class decorators and complex
decorator expressions are rejected.

Methods require a first `self` parameter. `self` may omit an annotation; if it is
annotated, it must match the containing class type. `__init__` must return
`None`.

Builtin exception classes are seeded into the class table so `raise`, `except`,
and `isinstance` can reason about their identities.

## Callable and functions

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

## Unions and narrowing

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

`Optional[T]` is represented as `T | None`; there is no separate optional type in
the implementation.

## Printable types

`print`, `str`, and f-string replacement fields accept printable types. Printable
values include primitives, `None`, homogeneous containers/tuples, printable
closed unions, and `Any`. User class instances are not generally printable unless
converted through an implemented path.
