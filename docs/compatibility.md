# Python 3.13 Compatibility Matrix

User-facing support matrix for minipy compared with Python 3.13 syntax and
behavior.

## When to Read

Read this when you need a quick answer about whether a Python feature is
implemented, restricted, parse-only, or out of scope in minipy.

For normative details, follow the owning spec document instead of treating this
matrix as the complete language specification.

## Source of Truth

| Concern | Source |
|---|---|
| lexical behavior | `docs/spec/01-lexical.md` |
| type behavior | `docs/spec/02-types.md` |
| accepted syntax | `docs/spec/03-grammar.md` |
| checker restrictions | `docs/spec/04-static-semantics.md` |
| lowering/runtime behavior | `docs/spec/05-codegen.md` |
| builtins/native modules | `docs/spec/06-builtins.md` |
| planned/deferred work | `docs/roadmap.md` |

## Legend

This matrix compares minipy with Python 3.13 syntax and behavior. It describes
what the current compiler accepts, checks, and lowers; it is not a roadmap for
full CPython compatibility.

- ✅ implemented and lowered
- ◐ partially implemented or implemented with stricter static limits
- ⏳ parsed or planned, but rejected before lowering
- ❌ intentionally out of scope

## Source and Lexical Layer

| Feature | Status | Notes |
|---|---:|---|
| UTF-8 source input | ✅ | Leading UTF-8 BOM is skipped; encoding cookies are not implemented. |
| Indentation blocks | ✅ | Spaces only; tabs in indentation are rejected. |
| Comments | ✅ | `#` line comments outside strings. |
| Explicit line join | ✅ | Backslash-newline only. |
| Implicit line join | ✅ | Inside `()`, `[]`, `{}`. |
| Identifiers | ✅ | Unicode letters/digits plus `_`. |
| Reserved keywords | ✅ | Full reserved keyword token set. |
| Soft keywords `match`, `case`, `type` | ✅ | Lexed as `NAME` and interpreted by parser in context. |
| Integer literals | ◐ | Python spelling forms accepted, but bounded to signed 64-bit. |
| Float literals | ✅ | Parsed as `float64`. |
| Imaginary literals | ❌ | No complex type. |
| String literals | ✅ | Plain/raw/triple strings and adjacent string concatenation. |
| Bytes literals | ◐ | `b`/`B`/`br`/`rb` (case variants), single/double/triple quotes, adjacent-literal concatenation, ASCII-only direct characters, `\xNN` decoded, `\u`/`\U` not decoded, raw backslash preservation; no `bytes()` constructor or `bytearray`. |
| f-strings | ◐ | Single token plus parser-split parts; conversions `!s`, `!r`, `!a`; one nested format level. |
| Named Unicode escapes | ❌ | `\N{...}` is not implemented. |
| Ellipsis token `...` | ✅ | Longest-match delimiter token; `.`, `.5`, and attribute dots are unchanged. |

## Statements

| Feature | Status | Notes |
|---|---:|---|
| `pass` | ✅ | No-op. |
| Expression statements | ✅ | Value is dropped. |
| Annotated assignment | ✅ | Declares a typed binding; value optional. |
| Unannotated assignment | ✅ | First assignment infers binding type. |
| Tuple/starred unpack assignment | ✅ | Supports list/tuple sources and homogeneous starred rest. |
| Chained assignment | ◐ | Parsed only into the current assignment representation; avoid relying on CPython multi-target semantics. |
| Augmented assignment | ◐ | Names and attributes supported; other targets rejected. |
| `del` | ✅ | Names, list/dict items, and attributes; captured names rejected. |
| `assert` | ✅ | Throws structured assertion error on false test. |
| `if`/`elif`/`else` | ✅ | Includes narrowing and static truth pruning. |
| `while`/`else` | ✅ | `break` skips `else`. |
| `for`/`else` | ✅ | Iterates supported iterables; tuple target allowed. |
| `break`/`continue` | ✅ | Checked for loop scope. |
| `return` | ✅ | Checked for function scope and result type. |
| `yield` statement | ✅ | Supported in generator functions returning `Iterator[T]`. |
| `yield` expression | ✅ | Expression-position `x = yield v`; result type `None` in v1. |
| `yield from` | ✅ | Delegates to an iterable; result type `None` in v1. |
| `global` | ✅ | Function-scope declaration. |
| `nonlocal` | ✅ | Requires enclosing binding. |
| `type` alias statement | ✅ | Compile-time alias. |
| `import` | ✅ | Module top-level only. |
| `from ... import ...` | ✅ | Module top-level only; aliases supported. |
| `from __future__ import ...` | ◐ | Module-prefix only; accepts `annotations`; string annotations also resolve without it. |
| `from ... import *` | ◐ | Static expansion only; uses static `__all__` or public names. |
| `try`/`except`/`else`/`finally` | ✅ | Structured VM error path. |
| `except*` | ⏳ | Parsed; ExceptionGroup semantics not implemented. |
| `raise` | ✅ | Includes bare re-raise in `except`. |
| `with` | ✅ | Context-manager lowering for supported checked forms. |
| `async def` | ⏳ | Parsed, rejected until scheduler support. |
| `async for`/`async with` | ⏳ | Parsed, rejected. |
| `match`/`case` | ✅ | Structural patterns with static checks. |

## Definitions

| Feature | Status | Notes |
|---|---:|---|
| Function definitions | ✅ | Optional annotations, default values, closures. |
| Positional-only params `/` | ✅ | Checked at call sites. |
| Keyword-only params `*` | ✅ | Checked at call sites. |
| `*args` parameter | ✅ | Collected as `list[T]`. |
| `**kwargs` parameter | ✅ | Collected as `dict[str, T]`. |
| Function decorators | ◐ | `@decorator`, `@module.decorator`, `@factory(...)`, `@module.factory(...)`, and stacking are supported when the decorator evaluates to exactly `Callable[[F], F]` for the decorated function's own signature `F`. Evaluated top to bottom, applied bottom to top, at definition time. Other decorator expression shapes (subscripts, boolean expressions, instance attributes) are rejected by the checker. |
| Return type inference | ✅ | Joins value-return branches. |
| Nested functions and closures | ✅ | Captures boxed when needed. |
| Generator functions | ✅ | `Iterator[T]` result required. |
| Class definitions | ✅ | Fixed field/method layout. |
| Single inheritance | ✅ | One supported base class. |
| Multiple inheritance | ⏳ | Parsed, rejected. |
| Class keywords/metaclass syntax | ⏳ | Parsed, rejected: `metaclass=` and other class keywords are rejected (tracked by #22); `**kwargs` class keywords are rejected as dynamic. |
| `@dataclass` | ✅ | `@dataclass` and `@dataclass()` behave identically; field constructor/default checks. |
| `@dataclass(...)` with options | ⏳ | Parsed, rejected (tracked by #32). |
| Other class decorators | ⏳ | Rejected (tracked by #22). |
| Methods and `self` | ✅ | `self` required; `__init__` returns `None`. |
| Special methods (`__len__`, `__getitem__`, `__setitem__`) | ◐ | Static dispatch only; `len(obj)`, `obj[i]`, `obj[i] = v`. No other dunders, slicing, or `__iter__`. |

## Expressions

| Feature | Status | Notes |
|---|---:|---|
| Boolean operations | ✅ | `and`/`or` require bool operands. |
| Unary operations | ✅ | `+`, `-`, `~`, `not` where typed. |
| Arithmetic operations | ✅ | Supported numeric/operator combinations only. |
| Power `**` | ✅ | Through operator semantics. |
| Matrix multiply `@` | ⏳ | Tokenized/parsed, no semantics. |
| Comparisons | ✅ | Chained comparisons included. |
| `is` / `is not` | ✅ | Especially used for `None` narrowing. |
| `in` / `not in` | ✅ | Supported containers/strings/iterators. |
| Conditional expressions | ✅ | Arms must have same type. |
| Named expressions `:=` | ✅ | Name target only. |
| Lambdas | ◐ | Need expected `Callable` context. |
| Calls | ✅ | Direct minipy calls support args, kwargs, defaults, `*tuple`, `*args`, `**kwargs` parameters. |
| Dynamic `**kwargs` call unpack | ⏳ | Parsed, rejected. |
| Keyword/star native calls | ⏳ | Rejected for native/builtin method/dynamic callable paths. |
| Attribute access | ◐ | Classes/modules supported; arbitrary object attributes out of scope. Literal-only `getattr`/`hasattr` support declared class fields. |
| Indexing | ✅ | Lists, dicts, strings, constant tuple indexes. |
| Slicing | ✅ | Lists and strings. |
| Slice assignment/deletion | ◐ | `list[T]` contiguous slices only; omitted step or literal `1`; replacement length must match. |
| List literals | ✅ | Homogeneous; empty needs hint. |
| List methods | ◐ | `append`, `pop`, `index`, `insert`, `extend`, and `reverse`; statically typed homogeneous lists only. |
| Dict literals | ✅ | Homogeneous; empty needs hint; scalar hashable keys. |
| Set literals | ✅ | Homogeneous; empty needs hint; scalar hashable elements. |
| Tuple literals | ✅ | Fixed arity, heterogeneous. |
| Starred list/set elements | ◐ | Statically typed sources only. |
| Dict unpacking in displays | ◐ | Dict sources only; dynamic call unpack still unsupported. |
| List/dict/set comprehensions | ✅ | Eager construction; name targets. |
| Generator expressions | ✅ | Lazy; lowers to a synthesized generator function. |
| Async comprehensions | ⏳ | Parsed, rejected. |
| Await expressions | ⏳ | Parsed, rejected. |
| F-strings | ◐ | Printable replacement fields, limited conversions/format nesting. |
| Ellipsis literal/name | ◐ | `...` and unshadowed `Ellipsis` share one singleton; `is`, `is not`, `==`, and `!=` are supported, while ellipsis subscripts are rejected. |

## Types

| Feature | Status | Notes |
|---|---:|---|
| `int`, `float`, `bool`, `str`, `None` | ✅ | Source-level primitive types. |
| `EllipsisType` | ◐ | Annotation for the Ellipsis singleton; direct construction and `Literal[Ellipsis]` are unsupported. |
| `Any` | ✅ | Dynamic fallback top type. |
| `list[T]`, `dict[K, V]`, `set[T]` | ✅ | Homogeneous containers. |
| `tuple[...]` | ✅ | Fixed heterogeneous tuple. |
| `Iterator[T]` | ✅ | Iteration/generator result type. |
| `Callable[[...], R]` | ✅ | Used for lambdas/callable values. |
| `A | B` unions | ✅ | Closed unions, normalized. |
| String annotations | ✅ | Parsed as type expressions; supports forward references such as `"Node"`. |
| `typing.Optional[T]` and `typing.Union[...]` | ✅ | Normalize to closed union forms. |
| `typing.Annotated[T, ...]` | ✅ | Metadata literal-validated, then erased to `T`. |
| `typing.Literal[...]` | ◐ | Static-only refinement for `int`, `bool`, `str`, and `None` literal values. |
| Type aliases | ✅ | `type Name = expr` and `Name: TypeAlias = expr`; recursive cycles rejected (`CyclicAlias`). |
| Flow narrowing | ✅ | `isinstance(name, T)` and `name is/is not None`. |
| Monomorphic specialization | ✅ | For union/Any params with concrete direct call tuples, capped per function. |
| Arbitrary precision int | ❌ | Uses signed 64-bit. |
| Complex | ❌ | No runtime type. |
| Bytes | ◐ | `bytes` primitive type (`array[i8]`); `len`, indexing (unsigned `0..255`), slicing, `+`, `==`/`!=`, `in`/`not in`, direct iteration, comprehensions, `iter(bytes)`. No `bytes()`/`bytearray`, no methods, no ordering, no hashing/dict-set keys, no `print`/`str`/`repr`/truthiness. |
| General Python object model | ❌ | No descriptors, metaclasses, MRO, dynamic attributes, or runtime namespace dictionaries. |

## Builtins and Modules

| Feature | Status | Notes |
|---|---:|---|
| `print`, `str`, `int`, `float`, `bool`, `abs`, `len` | ✅ | Native builtins. |
| `ord`, `chr` | ✅ | Unicode codepoint conversion; static `str->int` / `int->str`; `ValueError` for invalid inputs. `chr` rejects surrogate codepoints (`0xD800..0xDFFF`), diverging from CPython. |
| `range`, `iter`, `next` | ✅ | Iterator paths. |
| `enumerate`, `zip` | ✅ | List-based eager helpers. |
| `getattr`, `hasattr` | ◐ | Concrete class instances and literal declared field names only; no methods, dynamic strings, defaults, or runtime lookup. |
| `isinstance` | ✅ | Type/class checks and narrowing support. |
| Builtin exceptions | ✅ | Seeded class hierarchy. |
| `operator` module | ✅ | Native functions for syntax operator semantics. |
| `typing` module | ◐ | Annotation-only native symbols; no runtime typing objects. |
| `Ellipsis` fallback | ◐ | Bare fallback name with normal shadowing; not exported as a native `builtins` symbol. |
| First-class native functions | ❌ | Native symbols are callable names only. |
| First-class modules/classes | ❌ | Compile-time receiver names only. |
| Standard library compatibility | ❌ | Only registered native/source modules are available. |

## Related Docs

- `docs/README.md` — documentation map and ownership guide.
- `docs/spec/` — normative language and compiler behavior.
- `docs/roadmap.md` — planned and deferred work.
