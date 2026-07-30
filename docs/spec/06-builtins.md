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
| checker integration | `compiler/check*.go` |
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

Applications extend the default registry with `compiler.WithNativeModules`.
Module and symbol names must be unique; duplicate registration is a configuration
error. Symbols that lower entirely to bytecode do not need a runtime value.

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
| `sorted(xs)` | 1 | `list[T]` where T is comparable (`int`, `float`, `str`, `bool`) | `list[T]` |
| `reversed(xs)` | 1 | `list[T]` | `list[T]` |
| `min(a, b, ...)` | 2+ | same comparable type (`int`, `float`, `str`, `bool`) | `T` |
| `min(xs)` | 1 | `list[T]` where T is comparable | `T` |
| `max(a, b, ...)` | 2+ | same comparable type (`int`, `float`, `str`, `bool`) | `T` |
| `max(xs)` | 1 | `list[T]` where T is comparable | `T` |
| `sum(xs)` | 1 | `list[int]` or `list[float]` | element type |
| `any(xs)` | 1 | `list[bool]` | `bool` |
| `all(xs)` | 1 | `list[bool]` | `bool` |
| `round(x)` | 1 | `float` | `int` |
| `round(x, n)` | 2 | `float`, `int` | `float` |
| `divmod(a, b)` | 2 | `int`, `int` or `float`, `float` | `tuple[T, T]` |
| `pow(base, exp)` | 2 | `int`/`float` combinations | `int` (both int) or `float` |
| `hex(n)` | 1 | `int` | `str` |
| `oct(n)` | 1 | `int` | `str` |
| `bin(n)` | 1 | `int` | `str` |
| `repr(x)` | 1 | printable values | `str` |
| `map(fn, xs)` | 2 | `Callable[[T], R]`, `list[T]` | `list[R]` |
| `filter(fn, xs)` | 2 | `Callable[[T], bool]`, `list[T]` | `list[T]` |

`print` and `str` render supported lists, tuples, dictionaries, and sets recursively using Python-style delimiters and quoted nested strings.

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
| `sort()` | 0 | none | `None` |
| `copy()` | 0 | none | `list[T]` |
| `count(value)` | 1 | `T` | `int` |
| `clear()` | 0 | none | `None` |
| `remove(value)` | 1 | `T` | `None` |

`index` returns the first equal element position and raises `ValueError` when no
element matches. `insert` normalizes negative indexes relative to the current
length, clamps indexes below zero to `0`, and clamps indexes above the current
length to `len(list)`. `extend` snapshots the source length before mutation, so
`xs.extend(xs)` appends the original contents once. `reverse` mutates in place.
`sort` sorts the list in place; element type must be comparable (`int`, `float`,
`str`, or `bool`). `copy` returns a shallow copy. `count` returns the number of
occurrences of a value. `clear` removes all elements. `remove` deletes the first
occurrence of a value and raises `ValueError` if not found.

## String Methods

Supported `str` methods:

| Method | Arity | Accepted argument types | Result |
|---|---:|---|---|
| `upper()` | 0 | none | `str` |
| `lower()` | 0 | none | `str` |
| `split()` | 0 | none | `list[str]` |
| `split(sep)` | 1 | `str` | `list[str]` |
| `join(parts)` | 1 | `list[str]` | `str` |
| `find(sub)` | 1 | `str` | `int` |
| `strip()` | 0 | none | `str` |
| `strip(chars)` | 1 | `str` | `str` |
| `lstrip()` | 0 | none | `str` |
| `lstrip(chars)` | 1 | `str` | `str` |
| `rstrip()` | 0 | none | `str` |
| `rstrip(chars)` | 1 | `str` | `str` |
| `startswith(prefix)` | 1 | `str` | `bool` |
| `endswith(suffix)` | 1 | `str` | `bool` |
| `replace(old, new)` | 2 | `str`, `str` | `str` |
| `replace(old, new, count)` | 3 | `str`, `str`, `int` | `str` |
| `count(sub)` | 1 | `str` | `int` |
| `isdigit()` | 0 | none | `bool` |
| `isalpha()` | 0 | none | `bool` |
| `isalnum()` | 0 | none | `bool` |
| `isspace()` | 0 | none | `bool` |
| `capitalize()` | 0 | none | `str` |
| `title()` | 0 | none | `str` |
| `swapcase()` | 0 | none | `str` |
| `center(width)` | 1 | `int` | `str` |
| `center(width, fill)` | 2 | `int`, `str` | `str` |
| `ljust(width)` | 1 | `int` | `str` |
| `ljust(width, fill)` | 2 | `int`, `str` | `str` |
| `rjust(width)` | 1 | `int` | `str` |
| `rjust(width, fill)` | 2 | `int`, `str` | `str` |
| `zfill(width)` | 1 | `int` | `str` |
| `encode()` | 0 | none | `bytes` |
| `format(*args)` | 0+ | printable | `str` |

`strip`/`lstrip`/`rstrip` without arguments strip whitespace; with a `chars`
argument they strip any character present in that string. `startswith`/`endswith`
test for a fixed prefix/suffix. `replace` replaces all occurrences by default;
with the optional `count` argument it replaces at most that many. `count` returns
the number of non-overlapping occurrences of the substring. The `is*` predicates
return `False` for empty strings. `capitalize` uppercases the first character and
lowercases the rest. `title` uppercases the first letter of each word. `swapcase`
swaps upper/lower case. `center`/`ljust`/`rjust` pad to the given width with a
fill character (default space). `zfill` pads with leading zeros, preserving a
leading sign character. `encode` returns the UTF-8 byte representation as
`bytes`. `format` substitutes positional arguments into `{}` or `{N}` placeholders
in the format string; `{{` and `}}` produce literal braces.

## Dict Methods

Supported homogeneous `dict[K, V]` methods:

| Method | Arity | Accepted argument types | Result |
|---|---:|---|---|
| `get(key)` | 1 | `K` | `V` |
| `get(key, default)` | 2 | `K`, `V` | `V` |
| `keys()` | 0 | none | `list[K]` |
| `values()` | 0 | none | `list[V]` |
| `items()` | 0 | none | `list[tuple[K, V]]` |
| `pop(key)` | 1 | `K` | `V` |
| `pop(key, default)` | 2 | `K`, `V` | `V` |
| `update(other)` | 1 | `dict[K, V]` | `None` |
| `setdefault(key, default)` | 2 | `K`, `V` | `V` |
| `clear()` | 0 | none | `None` |
| `copy()` | 0 | none | `dict[K, V]` |

`get` returns the value for key if present, otherwise the default (or the
zero value of `V` when no default is given). `pop` removes the key and returns
its value; raises `KeyError` if the key is not found and no default is given.
When a default argument is provided, `pop` returns the default instead of raising
`KeyError` for missing keys. `update` merges all entries
from the argument dict into the receiver, overwriting existing keys. `setdefault`
returns the value for key if present; otherwise inserts key with the default value
and returns it. `clear` removes all entries. `copy` returns a shallow copy.

## Set Methods

Supported homogeneous `set[T]` methods:

| Method | Arity | Accepted argument types | Result |
|---|---:|---|---|
| `add(elem)` | 1 | `T` | `None` |
| `remove(elem)` | 1 | `T` | `None` |
| `discard(elem)` | 1 | `T` | `None` |
| `pop()` | 0 | none | `T` |
| `clear()` | 0 | none | `None` |
| `union(other)` | 1 | `set[T]` | `set[T]` |
| `intersection(other)` | 1 | `set[T]` | `set[T]` |
| `difference(other)` | 1 | `set[T]` | `set[T]` |
| `issubset(other)` | 1 | `set[T]` | `bool` |
| `issuperset(other)` | 1 | `set[T]` | `bool` |
| `copy()` | 0 | none | `set[T]` |

`add` inserts an element; duplicates are silently ignored. `remove` deletes an
element and raises `KeyError` if not found. `discard` deletes an element silently
(no error if missing). `pop` removes and returns an arbitrary element; raises
`KeyError` on an empty set. `clear` removes all elements. `union` returns a new
set with elements from both sets. `intersection` returns a new set with elements
common to both. `difference` returns a new set with elements in the receiver but
not in the other. `issubset` returns `True` if all elements of the receiver are
in the other set. `issuperset` returns `True` if all elements of the other set
are in the receiver. `copy` returns a shallow copy.

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
import math
import string
from builtins import len
from typing import Literal
from math import pi, sqrt
from string import ascii_lowercase
```

The imported module object is still compile-time-only; it may be used as an
attribute receiver (`operator.add(1, 2)`, `typing.Literal[1]` in an annotation,
`math.sqrt(4.0)`, `string.digits`) but not stored or passed as a runtime value.

## `math`

The `math` module provides mathematical constants and functions. Constants are
`ConstantSymbol` instances that emit inline values; functions are callable symbols
backed by host functions.

### Constants

| Name | Value | Type |
|---|---|---|
| `pi` | 3.141592653589793 | `float` |
| `e` | 2.718281828459045 | `float` |
| `tau` | 6.283185307179586 | `float` |
| `inf` | positive infinity | `float` |
| `nan` | not-a-number | `float` |

Constants can be accessed as values (`x: float = math.pi`) or via
`from math import pi`. They are not callable.

### Functions

| Function | Arity | Accepted argument types | Result |
|---|---:|---|---|
| `ceil(x)` | 1 | `int`, `float` | `float` |
| `floor(x)` | 1 | `int`, `float` | `float` |
| `sqrt(x)` | 1 | `int`, `float` | `float` |
| `log(x)` | 1 | `int`, `float` | `float` |
| `log2(x)` | 1 | `int`, `float` | `float` |
| `log10(x)` | 1 | `int`, `float` | `float` |
| `exp(x)` | 1 | `int`, `float` | `float` |
| `sin(x)` | 1 | `int`, `float` | `float` |
| `cos(x)` | 1 | `int`, `float` | `float` |
| `tan(x)` | 1 | `int`, `float` | `float` |
| `asin(x)` | 1 | `int`, `float` | `float` |
| `acos(x)` | 1 | `int`, `float` | `float` |
| `atan(x)` | 1 | `int`, `float` | `float` |
| `fabs(x)` | 1 | `int`, `float` | `float` |
| `trunc(x)` | 1 | `int`, `float` | `float` |
| `degrees(x)` | 1 | `int`, `float` | `float` |
| `radians(x)` | 1 | `int`, `float` | `float` |
| `atan2(y, x)` | 2 | `int`/`float` | `float` |
| `fmod(x, y)` | 2 | `int`/`float` | `float` |
| `copysign(x, y)` | 2 | `int`/`float` | `float` |
| `pow(x, y)` | 2 | `int`/`float` | `float` |
| `isnan(x)` | 1 | `int`, `float` | `bool` |
| `isinf(x)` | 1 | `int`, `float` | `bool` |
| `isfinite(x)` | 1 | `int`, `float` | `bool` |
| `gcd(a, b)` | 2 | `int`, `int` | `int` |
| `factorial(n)` | 1 | `int` | `int` |

Integer arguments are promoted to float before computation (except `gcd` and
`factorial` which operate on integers directly). `factorial` raises `ValueError`
for negative inputs at runtime.

When any argument has type `Any`, the result type is `Any` and runtime dispatch
is used.

## `string`

The `string` module provides string constants matching Python's `string` module.
Constants are `ConstantSymbol` instances that emit their value inline via the
constant pool.

### Constants

| Name | Value | Type |
|---|---|---|
| `ascii_lowercase` | `"abcdefghijklmnopqrstuvwxyz"` | `str` |
| `ascii_uppercase` | `"ABCDEFGHIJKLMNOPQRSTUVWXYZ"` | `str` |
| `ascii_letters` | `ascii_lowercase + ascii_uppercase` | `str` |
| `digits` | `"0123456789"` | `str` |
| `hexdigits` | `"0123456789abcdefABCDEF"` | `str` |
| `octdigits` | `"01234567"` | `str` |
| `punctuation` | `"!\"#$%&'()*+,-./:;<=>?@[\\]^_` `` ` `` `{|}~"` | `str` |
| `whitespace` | `" \t\n\r\x0b\x0c"` | `str` |
| `printable` | `digits + ascii_letters + punctuation + whitespace` | `str` |

Constants can be accessed as values (`x: str = string.digits`) or via
`from string import ascii_lowercase`. They are not callable.

## Related Docs

- `docs/README.md` — documentation map and ownership guide.
- `docs/spec/02-types.md` — source types accepted by builtin and operator rules.
- `docs/spec/04-static-semantics.md` — checker rules for calls and exceptions.
- `docs/spec/05-codegen.md` — lowering of native symbols and host helpers.
- `docs/compatibility.md` — user-facing builtin/operator support status.
