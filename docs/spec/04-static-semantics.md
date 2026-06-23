# minipy — Static Semantics

How minipy type-checks and resolves names before codegen. Input: the AST from
[`03-grammar.md`](03-grammar.md). Output: a typed AST where every expression node
carries a resolved type ([`02-types.md`](02-types.md)) and every name carries a
resolved storage slot.

## Annotation requirements (the "type hints required" rule)

Static compilation happens **only where boundaries are annotated**:

| Site | Annotation | If missing |
|---|---|---|
| function parameter | required | `MissingAnnotation` |
| function return | required (`-> T`) | `MissingAnnotation` |
| class field | required | `MissingAnnotation` |
| module-level global | required (`NAME: T = …`) | `MissingAnnotation` |
| local variable | optional (inferred) | inferred from initializer |
| `lambda` params | optional (inferred from call site, M4) | inferred |

A module containing a function with any unannotated parameter or missing return
type **does not compile**. There is no implicit `Any` fallback in the static core
(that is the opt-in M9 dynamic mode).

## Local type inference

Locals use **assign-once declaration inference**:

1. The **first** assignment to a local in a scope **declares** it; its type is the
   inferred type of the RHS expression.
2. Later assignments must be **assignable** ([`02-types.md`](02-types.md)) to that
   declared type, else `TypeMismatch`. A local does not change type.
3. An explicit annotation (`x: T = e`) fixes the type to `T` and checks `e <: T`.
4. A name read before any assignment on all paths is `UseBeforeDefinition`.

```python
def f(n: int) -> int:
    total = 0          # total: int (inferred)
    i = 0              # i: int
    while i < n:
        total = total + i   # ok: int <: int
        i = i + 1
    total = "done"     # ERROR: TypeMismatch (str not <: int)
    return total
```

There is **no flow-sensitive narrowing** in v1 except the single special case of
`Optional[T]` (below). `if isinstance(...)` narrowing is deferred.

### Optional narrowing (the one flow rule)

A value of type `Optional[T]` is narrowed to `T` inside the true-branch of an
`x is not None` test (and to `None` in the false branch), and symmetrically for
`x is None`. Outside such a guard, member/operator use of an `Optional[T]`
(other than comparison to `None`) is `PossiblyNone`.

```python
def length(s: Optional[str]) -> int:
    if s is None:
        return 0
    return len(s)      # s narrowed to str here
```

(`is`/`is not` against `None` is enabled at M7; until then `Optional` use is
limited. See [`03-grammar.md`](03-grammar.md).)

## Expression typing (rules summary)

- **Literals:** `NUMBER` int→`int`, float→`float`; `True/False`→`bool`;
  `None`→`NoneType`; string→`str`.
- **Arithmetic** (`+ - * // % ** << >> & | ^ ~`): both operands same numeric type.
  `int op int → int`; `float` supports `+ - * / ** ` (and `// %` via float
  floor/mod) → `float`. **Mixed int/float is `TypeMismatch`.** No `bool`
  arithmetic. `+` also concatenates `str`/`list`. `*` repeats `str`/`list` by
  `int` (M3).
- **True division `/`:** `int / int → float` (always float, like Python). `//`
  keeps `int`.
- **Comparison** (`== != < <= > >=`): operands must be the same comparable type;
  result `bool`. `in`/`not in` require a container RHS (M3).
- **Boolean** (`and`/`or`/`not`): operands must be `bool` (no truthiness coercion
  of arbitrary types in v1); result `bool`. `and`/`or` short-circuit but, with
  `bool` operands, the result type is `bool`.
- **Conditional `a if c else b`:** `c: bool`; result is the common type of `a`,
  `b` (must be mutually assignable).
- **Call:** callee must be a function/closure/class type; arity and argument
  types checked positionally; result = return type. Builtins per
  [`06-builtins.md`](06-builtins.md).
- **Index `a[i]`:** `list[T][int]→T`, `dict[K,V][K]→V`, `str[int]→str`,
  `tuple` requires a constant `int` index → that element's type.
- **Attribute `a.x`:** `a` must be a class instance; `x` a declared field/method.

Division/modulo by zero is **not** a static error (value unknown); it traps at
runtime (`ZeroDivisionError`, from minivm `ErrDivideByZero`).

## Scopes and name resolution

minipy resolves names statically by LEGB, mapping each to a minivm slot:

| Scope | minivm storage | Opcode |
|---|---|---|
| local (function body, params) | frame local | `LOCAL_GET/SET/TEE` (u8 index) |
| enclosing (captured by nested fn) | closure upvalue | `UPVAL_GET/SET` (u8 index) |
| module global | VM global | `GLOBAL_GET/SET/TEE` (u16 index) |
| builtin | host function / inline | `CONST_GET` + `CALL`, or inline opcodes |

Rules:

- A name assigned in a function is **local** to it unless declared `global` or
  `nonlocal` (M4).
- `global x` binds `x` to the module global slot; `nonlocal x` binds to the nearest
  enclosing function local that defines `x` (else `NoBindingForNonlocal`).
- A nested function reading an enclosing local **captures** it: the enclosing
  variable is promoted to a closure upvalue and the nested function becomes a
  `*Closure` (`CLOSURE_NEW`). Capture is by reference cell when the inner function
  also writes it (`REF_NEW`/`UPVAL_*`), by value otherwise.
- Redefining a name in the same scope with an incompatible type is `TypeMismatch`;
  shadowing in a nested scope is allowed.
- Local slot indices are assigned densely per frame (params first); globals get
  u16 indices in module order; functions/strings/large constants go to the program
  constant pool referenced by `CONST_GET`.

## Class semantics (M5)

- Field order = declaration order = struct field index order; a subclass appends
  its fields after the base's.
- `self` is the first parameter of each method, typed as the class.
- Method resolution is static (no MRO): a call `obj.m(...)` resolves to the field's
  class method constant at compile time.
- `__init__` must assign every non-defaulted field on all paths, else
  `UninitializedField`.

## Error catalogue

| Error | When |
|---|---|
| `MissingAnnotation` | unannotated param/return/field/global |
| `TypeMismatch` | assignment/operator/argument type conflict |
| `UndefinedName` | name not resolvable in any scope |
| `UseBeforeDefinition` | local read before assignment on some path |
| `PossiblyNone` | use of `Optional[T]` without a `None` guard |
| `UnsupportedFeature` | syntactically valid Python outside the subset |
| `UnsupportedType` | annotation outside the type grammar |
| `IntOverflow` | integer literal exceeds int64 |
| `ArityMismatch` | wrong number of call arguments |
| `NoBindingForNonlocal` | `nonlocal x` with no enclosing binding |
| `UninitializedField` | `__init__` leaves a field unset |
| `NotComparable` / `NotIterable` / `NotIndexable` | operator applied to unsupported type |

Diagnostics carry source span (line/col), the offending construct, and — for
`UnsupportedFeature` — the milestone where support is planned (or "out of scope").
Compilation reports **all** errors it can before aborting (no fail-on-first).
