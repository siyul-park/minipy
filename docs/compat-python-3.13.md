# minipy Python 3.13 Compatibility

This table compares CPython 3.13 syntax with the current minipy implementation.

Status legend:

| Status | Meaning |
|---|---|
| **Compiled** | Parses, type-checks, lowers to minivm, and has tests. |
| **Parse-only** | Parses into AST, then reports `UnsupportedFeature` before lowering. |
| **Unsupported** | Not yet represented enough to parse or execute correctly. |

## Statements

| Python 3.13 feature | minipy status | Notes |
|---|---:|---|
| Assignments, annotations, augmented assignment | Compiled | Starred unpack assignment supports one `*name` for list/tuple sources. Chained multi-target lowering remains limited. |
| `if` / `elif` / `else`, `while`, `for`, loop `else` | Compiled | `async for` is parse-only. |
| `def`, nested `def`, `return`, lambdas | Compiled | Defaults, positional-only, and keyword-only parameters compile. |
| `*args` / `**kwargs` parameters | Parse-only | Needs vararg representation and call dispatch. |
| `class`, single inheritance, dataclass fields, methods | Compiled | Multiple bases/class keywords parse-only. |
| Decorators | Compiled for bare names | Dotted/call-form decorators parse, then report `UnsupportedFeature`. |
| `try` / `except` / `else` / `finally`, `raise` | Compiled | `raise ... from ...` evaluates/checks cause but does not preserve chained traceback. |
| `except*` | Parse-only | Needs exception-group runtime. |
| `with` | Compiled | `async with` is parse-only. |
| `match` / `case` | Compiled | Current documented pattern restrictions still apply. |
| `del`, `assert`, `global`, `nonlocal`, `pass`, `break`, `continue` | Compiled |  |
| `import`, `from import` | Parse-only | Module loading/linking not implemented. |
| `async def`, `await`, async comprehensions | Parse-only | Scheduler/coroutine protocol not implemented. |
| `type` aliases | Compiled | Simple aliases to supported minipy types are available after declaration. |

## Expressions And Literals

| Python 3.13 feature | minipy status | Notes |
|---|---:|---|
| `int`, `float`, `bool`, `str`, `None` literals | Compiled | `int` is fixed int64, not arbitrary precision. |
| List/dict/set/tuple displays | Compiled | `*` list/set unpacking and `**` dict unpacking compile for compatible element/key/value types. |
| Calls | Compiled | Positional calls compile; keyword calls compile for known minipy functions, methods, and constructors. `*tuple` calls compile when arity is statically known. |
| Default argument filling | Compiled | Defaults lower at call sites. |
| Boolean, comparison, arithmetic, bitwise ops | Compiled | `@` parses but is unsupported; no matrix type/runtime. |
| Conditional expressions | Compiled |  |
| Walrus `:=` | Compiled | Name targets only. |
| Indexing/subscript | Compiled | Tuple index must be constant. |
| List and string slicing | Compiled | Optional start/stop/step supported. |
| Comprehensions | Compiled | List/dict/set comprehensions compile. Generator expressions compile as eager iterator construction on current VM. |
| `yield` statement | Compiled | `yield` expression and `yield from` parse-only/unsupported. |
| `is` / `is not`, `in` / `not in` | Compiled | Per current type restrictions. |
| f-strings | Compiled subset | Debug form and nested replacement limits remain as documented. |
| Bytes, complex, ellipsis | Unsupported | Need runtime types and lexer/parser completion. |
| Starred assignment/call/display unpacking | Compiled subset | `*tuple` calls and list/tuple assignment/display unpacking compile; dynamic `*list` calls and `**kwargs` calls remain unsupported. |

## Current Parse-only Queue

These forms are accepted by the parser so diagnostics are syntax-aware, but
still rejected before minivm lowering: `import`, `from import`, `async def`,
`async for`, `async with`, `await`, async comprehensions, `*args`, `**kwargs`,
dynamic starred calls, `**kwargs` calls, matrix multiply, decorator expressions,
multiple class bases/class keywords, `except*`, and dynamic keyword calls.
