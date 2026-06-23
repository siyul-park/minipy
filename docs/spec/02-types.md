# minipy — Type System

minipy is statically typed. Every expression has a type known at compile time.
Types are written with Python type-hint syntax and map onto minivm runtime types.

## Primitive types

| minipy | minivm | Notes |
|---|---|---|
| `int` | `i64` | signed 64-bit, **wraps** on overflow; no bigint |
| `float` | `f64` | IEEE-754 double |
| `bool` | `i32` | `True`=1, `False`=0 (`BoxBool`) |
| `str` | `String` | immutable UTF-32 codepoint sequence, interned |
| `None` (`NoneType`) | `REF_NULL` | the single null value |

`bool` is represented as `i32` but is **a distinct type** from `int` in minipy's
checker (unlike CPython where `bool` ⊂ `int`). A `bool` is not assignable to an
`int` target without `int(b)`; this avoids accidental `True + 1` style code.
Arithmetic on `bool` is rejected — convert explicitly.

### Numeric coercion

There is **no implicit `int`→`float`** widening. Mixed arithmetic is a
`TypeMismatch`; convert with `float(x)` / `int(x)`.

```python
a: int = 3
b: float = 1.5
c = a + b          # ERROR: TypeMismatch (int + float)
c = float(a) + b   # OK -> float
```

Rationale: implicit widening hides the `I64_TO_F64_S` conversion and complicates
inference; explicit is simple and predictable. (May be relaxed later.)

## Container types

| minipy | minivm | Notes |
|---|---|---|
| `list[T]` | `*Array` (typed, elem = lowering of `T`) | mutable, homogeneous |
| `tuple[T1, T2, …]` | `*Struct` (one field per element) | immutable, fixed arity, heterogeneous |
| `dict[K, V]` | `*Map` (generic) / `*TypedMap[int32\|int64\|float32\|float64]` | native `MAP_*`; primitive-key specializations via `NewMapForType` |
| `set[T]` | `*Map` with `V = bool`/unit | (M4) modeled on map keys |
| `bytes` | `[]i8` array | (deferred) binary blob |

Containers are **homogeneous** where Python allows heterogeneous: `list[int]`
holds only `int`. A heterogeneous `list` requires `list[Any]` (dynamic, M9).
`tuple` is the heterogeneous fixed-shape container and lowers to a struct.

### dict key types

minivm maps key by **value identity** for `i32/i64/f32/f64` and by **heap ref
identity** otherwise. So `dict[int, V]` and `dict[float, V]` use the specialized
maps; `dict[str, V]` uses the generic `*Map` keyed by interned-string ref
identity (correct because equal strings share one ref). Keys must be a hashable
primitive or `str`; `dict[list, …]` is rejected.

## Callable types

| minipy | minivm |
|---|---|
| `def`/function value | `*Function` |
| nested `def` / `lambda` capturing names | `*Closure` (+ `UPVAL_*`) |
| `Callable[[A, B], R]` | `*FunctionType{Params:[A,B], Returns:[R]}` |

Function-type equality is **structural**, matching minivm `FunctionType` (params
and returns compared positionally; captures do not affect type identity, so a
closure and a plain function with the same signature are type-equal).

## Class types

A `class` (M5) lowers to a minivm `*Struct`:

- Annotated class fields become struct fields, in declaration order, addressed by
  index. Field type = lowering of the annotation.
- Methods are `*Function` constants taking the instance struct as first parameter
  (`self`).
- Instances are created via `STRUCT_NEW`; attribute access is `STRUCT_GET`/`SET`
  with the statically resolved field index.

Single inheritance only, with fields appended (base fields first). No MRO, no
multiple inheritance, no metaclasses.

## Special form types

| minipy annotation | meaning | minivm |
|---|---|---|
| `Optional[T]` (= `T \| None`) | `T` or `None` | `ref` (dynamic slot) + null check |
| `Any` | dynamic, any value | `ref` — **M9, low priority** |
| `Iterator[T]` / generator | lazy producer of `T` | coroutine / `Iterator` heap value (M6) |

`Optional[T]` uses a `ref` slot so it can hold either a `T` value or
`REF_NULL`; reads narrow with a null test (`REF_IS_NULL`) before use. `T | None`
union syntax (PEP 604) is the only union form accepted; general `Union[A, B]` of
two non-None types is **deferred** (would require tagged runtime dispatch).

### `Any` and the dynamic boundary (M9, low priority)

`Any` maps to minivm's `ref` ("the VM's dynamic any type"). A value typed `Any`
is stored verbatim as a self-describing `Boxed`; recovering a concrete type uses
`REF_TEST`/`REF_CAST`. Crossing `Any → T` inserts a checked cast (runtime
`TypeError` on failure); `T → Any` is always allowed. This is the seam for a
future gradual/dynamic mode and is **not** part of the static core — see
[`../roadmap.md`](../roadmap.md) M9.

## Type grammar (annotations)

Annotations are ordinary expressions in Python; minipy accepts only this subset:

```text
type:
    | NAME                         # int, float, bool, str, None, <class name>
    | NAME '[' type (',' type)* ']'  # list[int], dict[str,int], tuple[int,str], Callable[...], Optional[int]
    | type '|' 'None'              # Optional sugar (T | None)
```

Anything else in annotation position (arbitrary expressions, string forward refs
beyond names, `Literal`, `Annotated`, `TypeVar`, generics with bounds) is
`UnsupportedType`. Generic *user* classes are deferred.

## Assignability

`S` is assignable to `T` (`S <: T`) iff:

1. `S` and `T` are the same primitive; or
2. `T` is `Any`; or `S` is `Any` (with inserted cast, M9); or
3. `T = Optional[U]` and `S <: U` or `S = None`; or
4. both are `list[E]`/`dict[K,V]`/`tuple[…]` with **invariant** element types
   (`list[int]` is **not** assignable to `list[Any]`); or
5. both are callables, params contravariant / return covariant — **v1 uses
   invariance** for simplicity (exact signature match).

No implicit numeric widening (rule above). `bool`↮`int` is not assignability.
Failures are `TypeMismatch`. The full algorithm is in
[`04-static-semantics.md`](04-static-semantics.md); lowering of each type to
opcodes is in [`05-codegen.md`](05-codegen.md).
