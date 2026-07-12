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

The `typing` module is native and annotation-only. It exposes static symbols for
type resolution and alias compatibility, but no first-class runtime typing
objects.

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
| `len(x)` | 1 | `str`, `bytes`, list, dict, set, tuple, or class instance with `__len__` | `int` |
| `enumerate(xs)` | 1 | `list[T]` | `list[tuple[int, T]]` |
| `zip(a, b)` | 2 | `list[A]`, `list[B]` | `list[tuple[A, B]]` |
| `range(stop)` | 1 | `int` | `Iterator[int]` |
| `range(start, stop)` | 2 | `int`, `int` | `Iterator[int]` |
| `range(start, stop, step)` | 3 | `int`, `int`, `int` | `Iterator[int]` |
| `iter(x)` | 1 | iterable values | `Iterator[T]` |
| `next(it)` | 1 | `Iterator[T]` | `T` |
| `getattr(obj, "field")` | 2 | concrete class instance plus string literal field name | declared field type |
| `hasattr(obj, "field")` | 2 | concrete class instance plus string literal field name | `bool` |
| `isinstance(x, T)` | 2 | value plus supported type/class expression | `bool` |
| `ord(s)` | 1 | `str` (exactly one codepoint) | `int` |
| `chr(n)` | 1 | `int` (`0 <= n <= 0x10FFFF`) | `str` |

`len(obj)` on a class instance that defines `__len__(self) -> int` rewrites to a
direct `obj.__len__()` call and raises `ValueError` at runtime when the returned
length is negative. Built-in containers keep their inline lowering.

`range(..., 0)` is diagnosed statically when the zero step is a constant integer
literal, including a unary sign.

### `Ellipsis` fallback

The bare name `Ellipsis` resolves to the immutable singleton only after ordinary
temporary, local, capture, module, global, function, class, and imported bindings
have failed to resolve, so normal shadowing is preserved. It is a compiler
fallback rather than a registered callable native symbol; `EllipsisType()` and
`from builtins import Ellipsis` are not supported.

## Static Attribute Builtins

`getattr` and `hasattr` expose only the part of attribute introspection that can
be resolved entirely by the checker:

- the receiver must have one concrete class type
- the attribute name must be a string literal
- only declared or inherited fields participate
- methods, modules, containers, unions, `Any`, and dynamically computed names are
  unsupported
- compiler-internal fields such as `__classid` are not exposed

`getattr(obj, "field")` has the field's declared source type and lowers exactly
like direct `obj.field` access: evaluate the receiver once and emit `STRUCT_GET`
with the statically resolved field index. A missing field is an `UndefinedName`
diagnostic.

`hasattr(obj, "field")` evaluates the receiver once for normal expression side
effects, discards the value, and returns a compile-time-resolved boolean. A
missing field is therefore `False`, not a runtime lookup or exception.

There is no third default argument for `getattr`, no bound-method result, and no
runtime metadata table or dynamic `__dict__` fallback.

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

## Unicode Codepoint Builtins

`ord(s)` returns the Unicode codepoint of a single-character string and `chr(n)`
returns the one-codepoint string for a codepoint. Both are registered in the
`builtins` native module, so they work as bare builtins, through
`import builtins` / `from builtins import …`, and as the unqualified-name
fallback. Type errors are compile-time diagnostics from each builtin's static
result rule (`str -> int` for `ord`, `int -> str` for `chr`).

Both lower through narrow host helpers (`ordHost`, `chrHost`) rather than inline
VM opcodes, because minivm exposes no codepoint get/create string opcodes. At
runtime, `ord` checks that the string has exactly one codepoint (via rune
iteration) and `chr` checks `0 <= n <= 0x10FFFF`; out-of-range or wrong-arity
inputs raise `ValueError` through the shared exception machinery.

`bool` is not accepted for `ord`/`chr` (a `bool` argument is a compile-time
`TypeMismatch`, like `abs`/`len`); CPython would raise a runtime `TypeError`.

Surrogate codepoints (`0xD800..0xDFFF`) are rejected by `chr` with `ValueError`,
so `chr` only accepts Unicode scalar values in `0..0x10FFFF` excluding the
surrogate range. This diverges from CPython, which accepts the full
`0..0x10FFFF` range including surrogates.

## Iteration Builtins

`iter` accepts lists, dicts, sets, iterators, strings, and bytes. Dict iteration
produces keys; set iteration produces elements; string iteration produces
strings; bytes iteration produces `int` elements in `0..255` (`bytesIter`
reinterprets the underlying signed `i8` storage as unsigned).

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

`bytes` participates in a narrow slice of these: `add` (`+`) concatenates two
`bytes` into a new `bytes`; `eq`/`ne` compare by length and content; `contains`
(`in`/`not in`) accepts an `int` needle in `0..255`. `lt`/`le`/`gt`/`ge` and the
other numeric/bitwise operators reject `bytes` (`NotComparable` for ordering,
type mismatch otherwise) — bytes has no ordering, hashing, or truthiness/
conversion support.

## `typing`

The native `typing` module exports annotation-only names:

```text
Any Annotated Callable Iterator Literal Optional TypeAlias Union
```

These names may be imported with `import typing` or `from typing import ...` and
used in annotations. `Annotated[T, ...]` erases to `T`; `Literal[...]` validates
statically known scalar values and erases to the scalar base type; `TypeAlias`
marks legacy annotated alias declarations. Using these names as runtime values or
calling them is rejected before lowering.

## Native Call Restrictions

Native calls do not support keyword arguments, starred arguments, or dynamic
`**kwargs` unpacking. Those forms are parsed, then rejected by the checker for
native symbols.

Native modules may be imported explicitly:

```python
import operator
import typing
from builtins import len
from typing import Literal
```

The imported module object is still compile-time-only; it may be used as an
attribute receiver (`operator.add(1, 2)`, `typing.Literal[1]` in an annotation)
but not stored or passed as a runtime value.

## Related Docs

- `docs/README.md` — documentation map and ownership guide.
- `docs/spec/02-types.md` — source types accepted by builtin and operator rules.
- `docs/spec/04-static-semantics.md` — checker rules for calls and exceptions.
- `docs/spec/05-codegen.md` — lowering of native symbols and host helpers.
- `docs/compatibility.md` — user-facing builtin/operator support status.
