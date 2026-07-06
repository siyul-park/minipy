# Builtins and Native Modules

Native module contract, builtin function behavior, operator module behavior, and
native call restrictions.

## When to Read

Read this when changing `builtins`, `operator`, builtin exception classes, native
symbol registration, native checker rules, or native emitters.

For general call restrictions, read `04-static-semantics.md`. For how native
symbols lower to bytecode or host helpers, read `05-codegen.md`.

## Source of Truth

| Concern | Source |
|---|---|
| native module interfaces | `module/` |
| builtin functions and exceptions | `builtins/` |
| operator functions and shared operator rules | `operator/` |
| host ABI helper values | `hostabi/` |
| checker integration | `compiler/check.go` |
| lowering integration | `compiler/compiler.go` |

## Summary

minipy exposes Python-like builtins through a native module named `builtins`.
Unqualified builtin names resolve through the module registry fallback, so source
programs can call `print`, `len`, `range`, and related functions directly.

The `operator` module is also native. Syntax operators and `operator.*` calls use
the same operator implementation, so the documented operator behavior has one
source of truth.

## Native Module Contract

A native module symbol carries:

- a type-check function
- a bytecode emit function
- an optional runtime value/host function

Native functions are callable by name, but they are not first-class values. A
program cannot store `print` in a variable and call it later.

## `builtins`

Implemented builtin functions:

| Function | Arity | Accepted argument types | Result |
|---|---:|---|---|
| `print(x)` | 1 | printable values | `None` |
| `str(x)` | 1 | printable values | `str` |
| `int(x)` | 1 | `int`, `float`, `bool`, `str` | `int` |
| `float(x)` | 1 | `int`, `float`, `bool`, `str` | `float` |
| `bool(x)` | 1 | convertible values and supported containers | `bool` |
| `abs(x)` | 1 | `int`, `float` | same as input |
| `len(x)` | 1 | `str`, list, dict, set, tuple | `int` |
| `enumerate(xs)` | 1 | `list[T]` | `list[tuple[int, T]]` |
| `zip(a, b)` | 2 | `list[A]`, `list[B]` | `list[tuple[A, B]]` |
| `range(stop)` | 1 | `int` | `Iterator[int]` |
| `range(start, stop)` | 2 | `int`, `int` | `Iterator[int]` |
| `range(start, stop, step)` | 3 | `int`, `int`, `int` | `Iterator[int]` |
| `iter(x)` | 1 | iterable values | `Iterator[T]` |
| `next(it)` | 1 | `Iterator[T]` | `T` |
| `isinstance(x, T)` | 2 | value plus supported type/class expression | `bool` |

`range(..., 0)` is diagnosed statically when the zero step is a constant integer
literal, including a unary sign.

## Printable and Convertible Values

Printable values are:

- `int`, `float`, `bool`, `str`, `None`
- printable lists, dicts, sets, and tuples
- printable closed unions
- `Any`

Convertible values for `int`, `float`, `str`, and numeric/truth operations are
limited by each builtin's checker rule. `int` and `float` parse strings through
host functions when needed; numeric/boolean conversions use VM opcodes where
possible.

`bool` and `operator.truth` accept scalar convertible values and these container
kinds: list, dict, set, tuple, and iterator. Tuple truthiness is based on arity.
Reference-like iterator/callable/class values use nullness.

## Iteration Builtins

`iter` accepts lists, dicts, sets, iterators, and strings. Dict iteration produces
keys; set iteration produces elements; string iteration produces strings.

`next` consumes `Iterator[T]`. End-of-iteration follows the runtime iterator /
coroutine protocol and traps through the VM when the iterator is exhausted.

`enumerate` and `zip` currently work on lists and eagerly produce lists of tuples.

## List Methods

Supported homogeneous `list[T]` methods:

| Method | Arity | Accepted argument types | Result |
|---|---:|---|---|
| `append(value)` | 1 | `T` | `None` |
| `pop()` | 0 | none | `T` |
| `pop(index)` | 1 | `int` | `T` |
| `index(value)` | 1 | `T` | `int` |
| `insert(index, value)` | 2 | `int`, `T` | `None` |
| `extend(values)` | 1 | `list[T]` | `None` |
| `reverse()` | 0 | none | `None` |

`index` returns the first equal element position and raises `ValueError` when no
element matches. `insert` normalizes negative indexes relative to the current
length, clamps indexes below zero to `0`, and clamps indexes above the current
length to `len(list)`. `extend` snapshots the source length before mutation, so
`xs.extend(xs)` appends the original contents once. `reverse` mutates in place.

## Exceptions

`builtins` also provides the builtin exception hierarchy used by the checker and
runtime error paths. The checker seeds these classes into the class table so
exception identity is shared with ordinary class/type checks.

Supported exception classes include:

```text
BaseException
Exception
ArithmeticError
LookupError
AssertionError
TypeError
NameError
UnboundLocalError
ValueError
IndexError
KeyError
RuntimeError
StopIteration
```

Exception instances carry a class id and message field in their runtime struct
shape. `raise` and `except` use that class identity; `except` targets must inherit
from `BaseException`.

## `operator`

The native `operator` module exports the functions used by syntax lowering.

Binary operator functions:

```text
add sub mul truediv floordiv mod pow and_ or_ xor lshift rshift
```

Comparison functions:

```text
eq ne lt le gt ge
```

Unary/logical helpers:

```text
neg pos invert contains not_ abs truth
```

The syntax forms `+`, `-`, `*`, `/`, `//`, `%`, `**`, bitwise operators, shifts,
comparisons, membership, unary operators, and logical truth helpers delegate to
these same type rules and emitters.

## Native Call Restrictions

Native calls do not support keyword arguments, starred arguments, or dynamic
`**kwargs` unpacking. Those forms are parsed, then rejected by the checker for
native symbols.

Native modules may be imported explicitly:

```python
import operator
from builtins import len
```

The imported module object is still compile-time-only; it may be used as an
attribute receiver (`operator.add(1, 2)`) but not stored or passed as a runtime
value.

## Related Docs

- `docs/README.md` — documentation map and ownership guide.
- `docs/spec/02-types.md` — source types accepted by builtin and operator rules.
- `docs/spec/04-static-semantics.md` — checker rules for calls and exceptions.
- `docs/spec/05-codegen.md` — lowering of native symbols and host helpers.
- `docs/compatibility.md` — user-facing builtin/operator support status.
