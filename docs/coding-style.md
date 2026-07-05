# Coding Style

This document captures conventions for changing minipy code and documentation.
The implementation is the source of truth; when behavior changes, update the
spec, compatibility matrix, and roadmap in the same change.

## Principles

- Keep the language subset explicit. A construct should be documented as lowered,
  parse-only, restricted, or out of scope.
- Prefer static checks over runtime surprises. Unsupported constructs should fail
  in the checker before lowering.
- Keep package dependencies directional and simple.
- Keep native behavior centralized: `builtins` owns builtin functions and
  exception classes; `operator` owns operator syntax and `operator.*` behavior.
- Avoid implicit coercions unless the type rules document them. minipy's source
  type system intentionally distinguishes `bool` from `int` and `int` from
  `float`.

## Package responsibilities

| Package | Responsibility |
|---|---|
| `token` | Token kinds, positions, diagnostic codes, and rendered Python-style error names. |
| `lexer` | Rune scanner, indentation handling, literal scanning, and lexical diagnostics. |
| `ast` | Data-only syntax tree nodes for modules, statements, expressions, patterns, and f-strings. |
| `parser` | Recursive-descent parser for supported and parse-only syntax forms. |
| `types` | Source-level type lattice and mapping to minivm runtime types. |
| `module` | Native/source module registry interfaces used by checker and lowerer. |
| `builtins` | Native `builtins` module, builtin type rules, emitters, host helpers, and exception hierarchy. |
| `operator` | Native `operator` module and the single source of operator type/lowering semantics. |
| `hostabi` | Runtime helper value shapes used by host functions and iterator/coroutine bridges. |
| `compiler` | Module loader, checker, specializer, lowerer, optimizer/verification pipeline, and import support. |
| `cmd/minipy` | CLI and REPL front ends. |
| `docs` | Implementation-facing specs and status documents. |

## Dependency direction

Follow the existing layering:

```text
token
  -> lexer
  -> ast
  -> parser
  -> types
  -> module
  -> builtins / operator
  -> compiler
  -> cmd/minipy
```

The actual graph is not a strict chain, but changes should preserve these rules:

- `token` must not depend on syntax, type, or compiler packages.
- `lexer` must not depend on `ast`, `parser`, `types`, or `compiler`.
- `ast` may depend on `token` for positions only.
- `parser` builds AST nodes and should not perform semantic checks that belong in
  `compiler/check.go`.
- `types` may map to minivm runtime types but must not depend on checker or
  lowerer state.
- `builtins` and `operator` depend on `module`, `types`, and syntax interfaces;
  they should not depend on each other.
- `compiler` is the integration layer and may depend on syntax, types, modules,
  native modules, host ABI helpers, and minivm.

## Diagnostics

- User-input errors should be reported through `token.Error` and accumulated in
  `token.ErrorList`.
- Prefer precise error codes such as `SyntaxError`, `UnsupportedFeature`,
  `UnsupportedType`, `TypeMismatch`, `UndefinedName`, `UseBeforeDefinition`,
  `ArityMismatch`, `PatternError`, and related codes already defined in `token`.
- Do not panic for malformed user programs. Panics are acceptable only for
  internal compiler invariants that should be unreachable after checking.
- When adding a new diagnostic class, update `token/error.go`, tests, and docs.

## Lexer conventions

- Keep token spelling and token names in `token/token.go` synchronized with
  `docs/spec/01-lexical.md`.
- The lexer should emit recoverable diagnostics and continue scanning when
  possible.
- Soft keywords stay as `NAME`; the parser decides whether a soft keyword starts a
  special form.
- Do not split f-strings into multiple token kinds unless the parser and docs are
  updated together.

## Parser conventions

- The parser may accept parse-only syntax so the checker can report a better
  unsupported-feature error. Document every parse-only form.
- Keep precedence centralized in the expression parser and update
  `docs/spec/03-grammar.md` when adding an operator.
- Parse syntax shape only. Type checking, scope rules, module resolution,
  constructor legality, and runtime support checks belong in the checker.
- Preserve source positions from the first token of each node.
- When adding a syntax node, update `ast`, parser tests, static semantics, codegen
  docs, and compatibility matrix.

## Type and checker conventions

- Prefer concrete types and closed unions over `Any`.
- Do not introduce implicit numeric promotion without documenting the rule and
  updating operator tests.
- A construct that cannot be lowered must produce a checker diagnostic before
  code generation.
- Keep flow narrowing and static-truth pruning mirrored between checker and
  lowerer when specializations make branches unreachable.
- When adding a type form, update `types`, annotation parsing, `resolveType`,
  assignability/printability as needed, lowering, and docs.

## Code generation conventions

- Lower only checked forms. The lowerer may assume the checker has rejected
  unsupported syntax and invalid types.
- Keep native operation semantics in `builtins` or `operator`; do not duplicate
  native type rules directly in the lowerer.
- Preserve minivm type pools and handler tables around optimizer passes as the
  current pipeline does.
- Verify every compiled program before returning it from `Compile`.
- For closure/capture changes, keep checker capture metadata and lowerer boxing
  behavior in sync.
- For specialization changes, keep per-specialization type tables isolated from
  the fallback function body.

## Native module conventions

A native symbol should provide a coherent triple:

1. checker rule
2. emitter callback
3. optional runtime value / host function

Native symbols are callable names, not first-class values. If that changes, update
`module`, checker name resolution, lowering, docs, and compatibility status.

## Documentation checklist

For any language or runtime behavior change, update the relevant files:

- `docs/spec/01-lexical.md` for tokens, literals, indentation, and f-strings.
- `docs/spec/02-types.md` for type forms, assignability, inference, narrowing,
  and specialization.
- `docs/spec/03-grammar.md` for syntax.
- `docs/spec/04-static-semantics.md` for checker behavior and diagnostics.
- `docs/spec/05-codegen.md` for lowering/runtime representation.
- `docs/spec/06-builtins.md` for builtin/operator/native-module behavior.
- `docs/compatibility.md` for user-facing Python compatibility status.
- `docs/roadmap.md` for completed work and remaining gaps.
- `README.md` when the public project summary, package map, or run instructions
  change.

Avoid stale milestone phrasing in spec files. If a feature is already shipped,
describe the implementation and restrictions directly. If it is not shipped,
state whether it is parse-only, rejected, planned, or out of scope.

## Test checklist

When changing behavior, add or update tests near the responsible package:

- lexer/token tests for tokenization and lexical diagnostics
- parser tests for AST shape and parse-only forms
- checker/compiler tests for semantic errors and generated behavior
- native module tests for builtin/operator type rules and emitters
- integration tests for CLI/runtime paths where appropriate

Docs-only changes do not require runtime test changes, but they should still be
reviewed against the current code before merging.
