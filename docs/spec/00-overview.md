# Overview

Compiler architecture, execution model, and package ownership for minipy.

## When to Read

Read this first when you need a high-level map of the compiler pipeline, package
boundaries, or the difference between minipy and CPython.

For syntax details, go to `03-grammar.md`. For checker rules, go to
`04-static-semantics.md`. For bytecode lowering, go to `05-codegen.md`.

## Source of Truth

| Concern | Source |
|---|---|
| documentation map | `docs/README.md` |
| lexical rules | `docs/spec/01-lexical.md` |
| source types | `docs/spec/02-types.md` |
| grammar | `docs/spec/03-grammar.md` |
| static semantics | `docs/spec/04-static-semantics.md` |
| code generation | `docs/spec/05-codegen.md` |
| builtins and native modules | `docs/spec/06-builtins.md` |
| Python compatibility status | `docs/compatibility.md` |
| remaining work | `docs/roadmap.md` |

## Summary

minipy is a statically checked Python 3.13-inspired subset that compiles to
[minivm](https://github.com/siyul-park/minivm). It lexes and parses Python-like
source, checks the resulting AST with minipy's source type system, lowers directly
to minivm bytecode, optimizes the program, verifies it, and runs it through
minivm's interpreter.

minipy is intentionally a subset, not a drop-in CPython implementation. It
focuses on code that can be checked ahead of time and lowered without preserving
CPython's fully dynamic object model.

## Goals

- Keep the source language familiar to Python users while making every supported
  construct statically checkable.
- Emit compact minivm bytecode directly; do not build a Python object runtime or a
  CPython compatibility layer.
- Support safe plugin, DSL, and rules-style programs with predictable types,
  deterministic diagnostics, and explicit host-module boundaries.
- Let annotations be optional where whole-program inference can solve the type.

## Non-goals

- Full CPython semantics, C-extension compatibility, reflection, monkey patching,
  descriptor protocol compatibility, or dynamic object layout.
- Arbitrary precision integers, complex values, bytes runtime values, or a full
  Python standard library.
- Scheduler/coroutine semantics for `async`/`await` forms. They are parsed where
  useful but rejected before lowering.
- First-class module objects, class objects, or native function values. Imported
  modules and native symbols are compile-time names.

## Pipeline

`compiler.Compile` follows this top-down path:

```text
Compile
  read source
  parse tokens into an AST
  load reachable source and native modules
  check names, types, control flow, and calls
  lower checked forms to minivm bytecode
  optimize the program
  verify the final program
```

Phase ownership:

1. `lexer` reads runes from an `io.Reader`, emits tokens, indentation tokens, and
   lexical diagnostics.
2. `parser` builds an `ast.Module`, retaining parse-only forms so later phases can
   report precise unsupported-feature diagnostics.
3. `compiler` loads source modules through configured search roots and registers
   native modules from `module.Registry`.
4. The checker resolves names, types, class layouts, imports, pattern captures,
   control-flow rules, and call targets.
5. The lowerer emits minivm bytecode directly from the checked AST, using minivm
   primitives where possible and host functions only where runtime support is
   required.
6. The minivm optimizer runs at the configured level and the final program is
   verified before `Compile` returns.

## Package Ownership

| Package | Responsibility |
|---|---|
| `token` | Token kinds, positions, diagnostic codes, and Python-style error names. |
| `lexer` | `io.Reader` to token stream, including `INDENT`, `DEDENT`, `NEWLINE`, and `EOF`. |
| `ast` | Plain data nodes for statements, expressions, patterns, and f-strings. |
| `parser` | Token stream to `*ast.Module`. |
| `types` | minipy source types and their minivm runtime mappings. |
| `module` | Native/source module registry interfaces. |
| `builtins` | Native `builtins` module and builtin exception hierarchy. |
| `operator` | Native `operator` module and shared operator semantics. |
| `hostabi` | Host values used for iterators, strings, coroutines, and runtime helpers. |
| `compiler` | Module loading, checking, specialization, lowering, optimization, and verification. |
| `cmd/minipy` | CLI and REPL. |

The dependency direction is intentionally one-way: lower-level syntax packages do
not import the compiler; the compiler imports syntax/type/module packages and
minivm; native modules depend on `module` and `types`, not on each other.

## Execution Model

- Module-level code lowers into the minivm entry body and terminates by falling
  off the end of the bytecode.
- Functions lower to minivm function constants and can capture boxed locals from
  enclosing functions.
- Generators lower to minivm coroutine-style functions and are consumed through
  iterator helpers such as `next`.
- Imports load source modules at compile time and emit imported module bodies
  before use. Native modules expose typed symbols through the registry.
- Runtime errors use minivm structured errors; builtin exception classes are
  represented in the checker's class table so `raise` and `except` can be typed.

## Type Model Summary

minipy has source-level types even when minivm represents some of them with the
same low-level type. For example, `bool` is distinct from `int` in minipy, even
though both lower to integer-like VM values. The checker tracks:

- primitives: `int`, `float`, `bool`, `str`, `None`, `Any`
- containers: `list[T]`, `dict[K, V]`, `set[T]`, `tuple[...]`
- classes, iterators, callables, imported modules, closed unions, and inference
  variables

`Any` is a dynamic fallback, not the default behavior. The checker prefers
concrete types or closed unions and only uses `Any` when inference cannot stay
bounded.

## Error Model

Every user-facing diagnostic is a `token.Error` with a stable code, source
position, and Python-style rendered exception class (`SyntaxError`, `TypeError`,
`NameError`, `ValueError`, and related names). Phases accumulate diagnostics and
return a `token.ErrorList` instead of failing on the first error.

## Related Docs

- `docs/README.md` — documentation map and ownership guide.
- `docs/spec/01-lexical.md` — lexer and token rules.
- `docs/spec/02-types.md` — source type system.
- `docs/spec/03-grammar.md` — accepted grammar.
- `docs/spec/04-static-semantics.md` — checker rules.
- `docs/spec/05-codegen.md` — lowering and runtime representation.
- `docs/spec/06-builtins.md` — builtins and native modules.
