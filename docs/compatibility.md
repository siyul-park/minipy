# minipy Python 3.13 Compatibility

This matrix tracks minipy against the official Python 3.13 language syntax and
core expression forms. It does not cover the Python standard library.

Sources:

- Python 3.13 Language Reference: Simple statements
- Python 3.13 Language Reference: Compound statements
- Python 3.13 Language Reference: Expressions

Status key:

| Mark | Meaning |
|---|---|
| ✅ | Implemented: parses, type-checks, lowers to minivm, and has coverage. |
| ◐ | Partial: parsed, compiled only for a subset, or rejected with `UnsupportedFeature`. |
| — | Not implemented: not represented enough to parse or execute correctly. |

## Simple Statements

| Official Python 3.13 feature | Implemented | Remarks |
|---|---:|---|
| Expression statement | ✅ | Supported for compiled expression forms. |
| Assignment statement | ✅ | Starred unpack assignment supports one `*name` for list/tuple sources. Chained multi-target lowering remains limited. |
| Augmented assignment statement | ✅ | Matrix augmented assignment `@=` is parsed but unsupported. |
| Annotated assignment statement | ✅ | Supported for minipy types. |
| `assert` statement | ✅ |  |
| `pass` statement | ✅ |  |
| `del` statement | ✅ |  |
| `return` statement | ✅ |  |
| `yield` statement | ✅ | Basic generator yield compiles. |
| `yield from` statement | ◐ | Parsed, then rejected before lowering. |
| `raise` statement | ✅ | `raise ... from ...` evaluates/checks cause but does not preserve chained traceback. |
| `break` statement | ✅ |  |
| `continue` statement | ✅ |  |
| `import` statement | ◐ | Parsed, then rejected; module loading/linking is not implemented. |
| `from ... import ...` statement | ◐ | Parsed, then rejected; module loading/linking is not implemented. |
| `from __future__ import ...` statement | ◐ | Covered by import parsing; no future-feature handling. |
| `global` statement | ✅ |  |
| `nonlocal` statement | ✅ |  |
| `type` alias statement | ✅ | Simple aliases to supported minipy types are available after declaration. |
| Generic `type` alias parameters | — | PEP 695-style type parameters are not documented as supported. |

## Compound Statements

| Official Python 3.13 feature | Implemented | Remarks |
|---|---:|---|
| `if` statement | ✅ | Includes `elif` and `else`. |
| `while` statement | ✅ |  |
| `while ... else` clause | ✅ |  |
| `for` statement | ✅ | Tuple/list destructuring supported under current type restrictions. |
| `for ... else` clause | ✅ |  |
| `async for` statement | ◐ | Parsed, then rejected until scheduler/coroutine support exists. |
| `try ... except` statement | ✅ |  |
| `try ... else` clause | ✅ |  |
| `try ... finally` clause | ✅ |  |
| `except*` clause | ◐ | Parsed, then rejected; exception-group runtime is missing. |
| `with` statement | ✅ |  |
| Multiple context managers in `with` | ✅ | Supported when each context manager type is supported. |
| `async with` statement | ◐ | Parsed, then rejected until scheduler/coroutine support exists. |
| `match` statement | ✅ | Pattern restrictions below still apply. |
| Literal pattern | ✅ |  |
| Capture pattern | ✅ |  |
| Wildcard pattern | ✅ |  |
| Value pattern | ✅ |  |
| Group pattern | ✅ |  |
| Sequence pattern | ◐ | Starred tuple patterns are rejected. |
| Mapping pattern | ✅ | Supported under current mapping type restrictions. |
| Class pattern | ✅ | Dataclass-style destructuring supported under current class restrictions. |
| OR pattern | ✅ |  |
| AS pattern | ✅ |  |
| Match guard | ✅ |  |
| Function definition | ✅ | Includes nested `def`. |
| Function return annotation | ✅ | Supported for minipy types. |
| Function parameter annotation | ✅ | Supported for minipy types. |
| Default parameter value | ✅ | Defaults lower at call sites. |
| Positional-only parameter | ✅ | Keyword calls to positional-only parameters are rejected. |
| Keyword-only parameter | ✅ |  |
| `*args` parameter | ◐ | Parsed, then rejected; vararg representation and dispatch are missing. |
| `**kwargs` parameter | ◐ | Parsed, then rejected; kwarg representation and dispatch are missing. |
| Function decorator: bare name | ✅ |  |
| Function decorator: dotted/call expression | ◐ | Parsed, then rejected. |
| Generic function type parameters | — | PEP 695-style type parameters are not documented as supported. |
| Lambda expression | ✅ |  |
| Class definition | ✅ |  |
| Single inheritance | ✅ |  |
| Multiple inheritance | ◐ | Parsed, then rejected. |
| Class keyword arguments | ◐ | Parsed, then rejected. |
| Class decorator: bare name | ✅ |  |
| Class decorator: dotted/call expression | ◐ | Parsed, then rejected. |
| Generic class type parameters | — | PEP 695-style type parameters are not documented as supported. |
| `async def` function | ◐ | Parsed, then rejected until scheduler/coroutine support exists. |

## Expressions And Literals

| Official Python 3.13 feature | Implemented | Remarks |
|---|---:|---|
| Identifier/name expression | ✅ |  |
| Integer literal | ✅ | Fixed int64, not arbitrary precision. |
| Floating-point literal | ✅ |  |
| Boolean literal | ✅ |  |
| String literal | ✅ |  |
| Raw string literal | ✅ |  |
| f-string literal | ◐ | Compiled subset; debug form and nested replacement limits remain. |
| Bytes literal | — | Runtime type and lexer/parser completion are missing. |
| Imaginary/complex literal | — | Runtime type is missing. |
| `None` literal | ✅ |  |
| Ellipsis literal | — | Runtime value/type is missing. |
| Parenthesized form | ✅ |  |
| Tuple display | ✅ | Includes tuple packing. |
| List display | ✅ | `*` unpacking compiles for compatible list/tuple elements. |
| Set display | ✅ | `*` unpacking compiles for compatible set elements. |
| Dictionary display | ✅ | `**` unpacking compiles for compatible key/value types. |
| List comprehension | ✅ |  |
| Set comprehension | ✅ |  |
| Dictionary comprehension | ✅ |  |
| Generator expression | ◐ | Compiles as eager iterator construction on the current VM. |
| Async comprehension | ◐ | Parsed, then rejected until scheduler/coroutine support exists. |
| Attribute reference | ✅ | Supported for known minipy objects/classes. |
| Subscription/indexing | ✅ | Tuple index must be constant. |
| Slicing | ✅ | List and string slicing support optional start/stop/step. |
| Call expression | ✅ | Positional calls compile. |
| Keyword call argument | ✅ | Compiles for known minipy functions, methods, and constructors. Dynamic keyword calls remain unsupported. |
| Starred call argument `*args` | ◐ | `*tuple` calls compile when arity is statically known; dynamic `*list` calls remain unsupported. |
| Double-star call argument `**kwargs` | ◐ | Parsed, then rejected. |
| Await expression | ◐ | Parsed, then rejected until scheduler/coroutine support exists. |
| Unary arithmetic operators | ✅ | `+`, `-`, and `~` supported under current type restrictions. |
| Arithmetic binary operators | ✅ | `+`, `-`, `*`, `/`, `//`, `%`, and `**` supported under current type restrictions. |
| Matrix multiplication operator `@` | ◐ | Parsed, then rejected; no matrix type/runtime. |
| Bitwise binary operators | ✅ | `&`, `|`, `^`, `<<`, and `>>` supported under current type restrictions. |
| Comparison operators | ✅ | Chained comparisons supported under current type restrictions. |
| Identity operators `is` / `is not` | ✅ | Supported under current type restrictions. |
| Membership operators `in` / `not in` | ✅ | Supported under current type restrictions. |
| Boolean operators `and` / `or` / `not` | ✅ |  |
| Conditional expression | ✅ |  |
| Assignment expression `:=` | ✅ | Name targets only. |
| Lambda expression | ✅ | Also listed under compound-adjacent callable forms. |
| Expression list | ✅ | Tuple packing and unpacking follow current tuple/list restrictions. |
| Starred expression | ◐ | Supported in assignment, displays, and statically known tuple calls; rejected elsewhere. |
| Yield expression | ◐ | Basic `yield` works in generator bodies; expression-position limits remain. |
| `yield from` expression | ◐ | Parsed, then rejected. |

## Current Parse-only Queue

Forms marked partial above generally parse for syntax-aware diagnostics, then
stop before minivm lowering until the missing runtime, dispatch, or type-system
support lands.
