# Python Data Model (reference, summarized)

> Upstream: <https://docs.python.org/3.13/reference/datamodel.html> · captured 2026-06-23.
> The full data model is large; this is a summary of the parts relevant to the
> minipy subset. Read the upstream page for complete detail. minipy's own
> semantics live in [`../spec/04-static-semantics.md`](../spec/04-static-semantics.md).

## Objects, values, types

- Every Python object has an **identity** (`id`), a **type**, and a **value**.
  Identity never changes; type is (practically) fixed; value may change for
  mutable objects.
- `is` compares identity; `==` compares value (via `__eq__`).
- Mutable: `list`, `dict`, `set`, most user classes. Immutable: `int`, `float`,
  `bool`, `str`, `tuple`, `bytes`, `frozenset`, `None`.

## Standard type hierarchy (subset-relevant)

- **None** — the single `NoneType` value; falsy.
- **Numbers** — `int` (unbounded in CPython), `bool` (subtype of `int`), `float`,
  `complex`. minipy keeps `int` (→int64), `float`, `bool`; drops `complex`.
- **Sequences** — immutable: `str`, `tuple`, `bytes`; mutable: `list`, `bytearray`.
- **Mappings** — `dict`.
- **Sets** — `set`, `frozenset`.
- **Callables** — functions, methods, classes, generators.
- **Classes & instances** — attribute lookup via MRO; `__init__`, `__dict__`,
  descriptors, metaclasses.

## Truth value testing

A value is **falsy** if it is `None`, `False`, numeric zero (`0`, `0.0`), or an
empty container (`''`, `()`, `[]`, `{}`, `set()`, `0`-length); otherwise **truthy**.
Customizable via `__bool__` then `__len__`. minipy implements truthiness only for
its supported types and **does not** consult user `__bool__`/`__len__` in v1.

## Special ("dunder") methods (subset-relevant)

- Construction/repr: `__init__`, `__new__`, `__repr__`, `__str__`.
- Comparison: `__eq__`, `__ne__`, `__lt__`, `__le__`, `__gt__`, `__ge__`, `__hash__`.
- Numeric: `__add__`, `__sub__`, `__mul__`, `__truediv__`, `__floordiv__`,
  `__mod__`, `__pow__`, `__neg__`, and their reflected/in-place variants.
- Containers: `__len__`, `__getitem__`, `__setitem__`, `__contains__`, `__iter__`,
  `__next__`.
- Context managers: `__enter__`, `__exit__`.

minipy's static model maps a fixed, known set of these to minivm opcodes or host
functions per supported type; **arbitrary operator overloading on user classes is
deferred** (see roadmap M5+). General descriptor protocol, metaclasses,
`__slots__` introspection, and `__getattr__`/`__setattr__` interception are
**out of scope**.

## Execution & name resolution

- **Scopes:** local, enclosing (closures), global (module), builtins (LEGB).
- `global` / `nonlocal` rebind name resolution.
- Names must be bound before use; unbound → `NameError` (CPython) / compile error
  (minipy, since binding is statically known).

minipy resolves all names **statically** to minivm `LOCAL`/`GLOBAL`/`UPVAL` slots;
there is no runtime `__dict__`-based name lookup.
