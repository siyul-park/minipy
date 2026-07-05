# Overview

minipy is a statically checked Python 3.13-inspired subset that compiles to
[minivm](https://github.com/siyul-park/minivm). The implementation is a real
compiler pipeline: lex, parse, type-check, lower to minivm, optimize, verify, and
run.

This specification describes the shipped compiler and CLI behavior. Roadmap
history lives in `docs/roadmap.md`; feature compatibility against CPython lives
in `docs/compatibility.md`.

## Goals

- Keep the source language familiar to Python users while making every supported
  construct statically checkable.
- Emit compact minivm bytecode directly; do not build a Python object runtime or
  a CPython compatibility layer.
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

1. `lexer` reads runes from an `io.Reader`, emits tokens, indentation tokens, and
   lexical diagnostics.
2. `parser` builds an `ast.Module`, retaining parse-only forms so later phases can
   report precise unsupported-feature diagnostics.
3. `compiler` loads source modules through configured search roots and registers
   native modules from `module.Registry`.
4. The checker resolves names, types, class layouts, imports, pattern captures,
   control-flow rules, and call targets.
5. Lowering emits minivm bytecode directly from the checked AST, using minivm
   primitives where possible and host functions only where runtime support is
   required.
6. The minivm optimizer runs at the configured level and the final program is
   verified before `Compile` returns.

## Package responsibilities

```text
token     token kinds, positions, diagnostic codes, and Python-style error names
lexer     io.Reader -> token stream, including INDENT/DEDENT/NEWLINE/EOF
ast       plain data nodes for statements, expressions, patterns, and f-strings
parser    token stream -> *ast.Module
types     minipy source types and their minivm runtime mappings
module    native/source module registry interfaces
builtins  native builtins module and builtin exception hierarchy
operator  native operator module and shared operator semantics
hostabi   host values used for iterators, strings, coroutines, and runtime helpers
compiler  module loading, checking, lowering, optimization, verification
cmd       CLI and REPL
```

The dependency direction is intentionally one-way: lower-level syntax packages do
not import the compiler; the compiler imports syntax/type/module packages and
minivm; native modules depend on `module` and `types`, not on each other.

## Execution model

- Module-level code lowers into the minivm entry body and terminates by falling
  off the end of the bytecode.
- Functions lower to minivm function constants and can capture boxed locals from
  enclosing functions.
- Generators lower to minivm coroutine-style functions and are consumed through
  iterator helpers such as `next`.
- Imports load source modules at compile time and emit imported module bodies
  before use. Native modules expose typed symbols through the registry.
- Runtime errors use minivm structured errors; builtin exception classes are
  represented in the checker's class table so `raise`/`except` can be typed.

## Type model summary

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

## Error model

Every user-facing diagnostic is a `token.Error` with a stable code, source
position, and Python-style rendered exception class (`SyntaxError`, `TypeError`,
`NameError`, `ValueError`, and related names). Phases accumulate diagnostics and
return a `token.ErrorList` instead of failing on the first error.
