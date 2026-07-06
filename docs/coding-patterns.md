# Coding Patterns

Default coding style for minipy contributors and agents.

Use the same convention shape as minivm's `docs/coding-patterns.md`. This document is the minipy-local owner for compiler-specific ownership, pipeline, native-module, and documentation rules.

## When to Read

Use this document before writing or changing minipy code, especially when local style is unclear or a new pattern is needed.

Match nearby code first. Use these rules to resolve ambiguity, not to override a clear local pattern.

## Source of Truth

| Concern | Source |
|---|---|
| formatting | `gofmt` |
| package-specific style | nearby code in the same package |
| shared coding patterns | minivm `docs/coding-patterns.md` |
| public API shape | existing public APIs and tests |
| language behavior | `docs/spec/` |
| Python compatibility status | `docs/compatibility.md` |
| remaining work | `docs/roadmap.md` |
| package ownership | this document and package comments |

## Fast Path

Read only the sections relevant to the change.

| Change | Read |
|---|---|
| function shape, helper extraction, naming | principles, functions |
| types, interfaces, fields, constructors | types |
| public APIs, options, builders, parsers | APIs |
| diagnostics, panic, recovery | errors |
| package boundaries or ownership | package ownership |
| lexer/parser/checker/lowerer behavior | compiler pipeline |
| builtins or operator behavior | native modules |
| tests | tests |
| documentation changes | docs |
| commits and PRs | git and PRs |

## Principles

### Simpler is Better

If two designs provide the same behavior, choose the simpler one.

Prefer fewer files, fewer types, fewer functions, fewer methods, fewer arguments, fewer names, less indirection, and more local code.

Do not add abstraction only because code can be split. Add abstraction when it clearly improves readability, removes real duplication, isolates real complexity, or names a meaningful domain step.

### Keep Public Surfaces Small

Push complexity down. Public APIs should stay simple even when implementation is difficult.

Prefer simple APIs over exposed mechanisms, explicit behavior over hidden behavior, local complexity over distributed state, and predictable structure over clever abstraction.

### Read Top-Down

Important behavior comes first. Details follow later.

Readers should understand the flow by reading downward:

```text
Compile
  parse
  load
  check
  emit
  verify
```

Avoid forcing readers to jump around to reconstruct the main path.

### Be Obvious

Prefer mechanically obvious code over clever code.

A slightly longer implementation is better when it avoids hidden state, improves debugging, preserves checker/lowerer symmetry, makes control flow explicit, or keeps behavior easy to verify.

### Preserve Symmetry

Checker and lowerer paths should stay structurally similar when possible.

Symmetry matters more than small local optimizations because it keeps behavior easier to compare, test, and maintain. This is especially important for type narrowing, static truth pruning, function specialization, closures, exceptions, patterns, and native calls.

### Keep Related Code Close

Keep state, validation, mutation, and cleanup for one behavior near each other.

Avoid splitting logic across files or helpers unless the split has clear ownership or readability benefit.

## Symbol and Algorithm Review

Review every changed file as a symbol set before and after the change. Check files, structs, fields, functions, methods, arguments, return values, variables, constants, interfaces, and errors.

For each symbol, ask:

1. Can it be removed?
2. Can it be private?
3. Can it be merged into an existing symbol?
4. Can its name be shorter without losing clarity?
5. Can its caller pass less data?
6. Can the callee return less data?
7. Can the behavior live closer to the state it reads or mutates?

Prefer deleting a symbol over renaming it, renaming over wrapping it, and local code over a new abstraction. Keep a public symbol only when a real caller or documented API boundary needs it.

Review algorithms the same way. Before keeping a complex path, look for a simpler or more efficient one:

- one pass instead of multiple passes
- map/set lookup instead of repeated linear scans
- early validation instead of late cleanup
- shared existing phase data instead of recomputing it
- direct control flow instead of callback or interface indirection
- clearer O(n) code over clever code with hidden constants

Do not optimize by obscuring behavior. Performance changes need benchmark or profile evidence; without evidence, choose the clearest correct algorithm.

Repeat the review loop until another pass finds no smaller public surface, no safe deletion, no clearer name, no simpler data flow, and no easier algorithm. Stop when the remaining complexity is required by behavior, compatibility, or a documented boundary.

### Keep the Language Subset Explicit

Every construct should be documented as lowered, parse-only, restricted, or out of scope.

Prefer static checks over runtime surprises. Unsupported constructs should fail in the checker before lowering.

## Functions

Each function should operate at one conceptual level.

Prefer this shape:

```go
func (c *Compiler) Compile() (*program.Program, error) {
    mod, err := c.parse()
    if err != nil {
        return nil, err
    }
    check, err := c.check(mod)
    if err != nil {
        return nil, err
    }
    return c.emit(check)
}
```

Avoid mixing high-level orchestration with low-level details in the same function unless the local code is clearer that way.

Extract a helper when it removes real duplication, gives a meaningful name to a domain step, or isolates complexity. Do not extract a helper only to shorten a function.

Keep helper names short and direct. Prefer names that describe the operation, not the implementation trick.

Name by caller-visible role, not implementation mechanics. Use the shortest standard name that stays clear in package, receiver, and local context. Prefer one word when context carries the rest: `state`, `frame`, `module`, `target`, `source`, `compiler`, `builder`, `cache`, `trace`, and `exit` are better than names that repeat the file or subsystem.

Avoid private names that repeat the package, one-letter names outside tight local scopes, non-standard abbreviations, and helpers named after a low-level step when a role name is enough. Common domain abbreviations such as `ID`, `IP`, `ABI`, `VM`, and `CPU` are fine.

Within a file and section, declare callers before callees so the file reads from policy to mechanics. A package-level helper should appear after the functions or methods that need it unless it is a constructor or shared public utility.

Let one function own a decision. Do not duplicate a helper's eligibility checks at every call site when the helper can return failure or fallback itself.

## Types

Add a type when it owns data with behavior, names a real domain concept, or prevents repeated error-prone structure.

Do not add a type only to group two values temporarily, hide a simple tuple, or create an abstraction before it is needed.

Interfaces should be small and consumer-owned. Prefer concrete types until there is a real boundary.

Constructors should establish invariants. If a value has no invariants, a struct literal may be clearer.

Use this order in every `.go` file:

1. public types
2. private types
3. public constants
4. private constants
5. public variables
6. private variables
7. constructors
8. public functions
9. public methods
10. private methods
11. private functions

Within each group, keep top-down flow: callers before callees.

Order struct fields by how readers understand the value:

1. lifecycle and policy objects
2. infrastructure
3. program data
4. runtime state
5. mutable counters
6. read-only config
7. sync primitives

Separate those layers with blank lines when it improves scanning. Keep `sync.Mutex` and related sync primitives last. Struct literals should follow the field declaration order.

Methods show ownership. A function used by one type belongs on that type, even if the receiver is not used directly. Keep methods with their owning type; split large types by concern only when the ownership remains obvious.

For minipy-specific types:

- AST nodes should stay data-only.
- Source types belong in `types`; checker-only bookkeeping belongs in `compiler`.
- Runtime helper shapes belong in `hostabi` only when host functions or minivm runtime boundaries need them.
- Native symbol behavior belongs in `builtins` or `operator`, not duplicated in the checker or lowerer.

## APIs

Public APIs should make the common path obvious and keep advanced behavior explicit.

Prefer options for rare configuration and direct arguments for required behavior. Functional options may be declared immediately before the constructor or function they configure.

Apply defaults first, then options. Public concrete constructors use `NewType`; private concrete constructors use `newType`. Constructors are standalone functions, never methods.

Keep builders focused. A builder should construct one thing, validate inputs near construction, and avoid becoming a general mutable configuration store.

Do not expose internal representation unless callers have a stable reason to depend on it.

For minipy:

- Keep `compiler.Compile` and `compiler.New(...).Compile()` as the obvious public entry points.
- Keep parser and lexer constructors simple and lazy where possible.
- Keep native module registration explicit through `module.Registry`.
- Do not make modules, classes, or native functions first-class runtime values without updating the public API, checker, lowerer, docs, and tests together.

## Errors

Return errors for expected failure. Panic only for internal invariants that indicate a compiler bug.

Keep error values stable when callers can reasonably branch on them.

Use structured error types only when callers need more than a message.

Do not recover broadly. Recovery should be local, documented, and tied to a specific boundary.

For user programs:

- Report lexical, syntactic, loading, and semantic failures through `token.Error` and accumulate them in `token.ErrorList`.
- Prefer precise diagnostic codes such as `SyntaxError`, `UnsupportedFeature`, `UnsupportedType`, `TypeMismatch`, `UndefinedName`, `UseBeforeDefinition`, `ArityMismatch`, and `PatternError`.
- Do not panic for malformed source input.
- Add or update `token/error.go`, tests, and docs when adding a diagnostic class.

## Package Ownership

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

Keep dependency direction simple:

- `token` must not depend on syntax, type, or compiler packages.
- `lexer` must not depend on `ast`, `parser`, `types`, or `compiler`.
- `ast` may depend on `token` for positions only.
- `parser` builds AST nodes and should not perform semantic checks that belong in `compiler/check.go`.
- `types` may map to minivm runtime types but must not depend on checker or lowerer state.
- `builtins` and `operator` depend on `module`, `types`, and syntax interfaces; they should not depend on each other.
- `compiler` is the integration layer and may depend on syntax, types, modules, native modules, host ABI helpers, and minivm.

## Compiler Pipeline

Keep phase ownership clear.

### Lexer

- Keep token spelling and token names in `token/token.go` synchronized with `docs/spec/01-lexical.md`.
- Emit recoverable diagnostics and continue scanning when possible.
- Keep soft keywords as `NAME`; the parser decides whether a soft keyword starts a special form.
- Do not split f-strings into multiple token kinds unless the parser and docs are updated together.

### Parser

- Accept parse-only syntax only when it improves diagnostics.
- Document every parse-only form.
- Keep precedence centralized in the expression parser and update `docs/spec/03-grammar.md` when adding an operator.
- Parse syntax shape only. Type checking, scope rules, module resolution, constructor legality, and runtime support checks belong in the checker.
- Preserve source positions from the first token of each node.

### Checker

- Prefer concrete types and closed unions over `Any`.
- Do not introduce implicit numeric promotion without documenting the rule and updating operator tests.
- Reject constructs that cannot be lowered before code generation.
- Keep flow narrowing and static-truth pruning mirrored between checker and lowerer when specializations make branches unreachable.
- When adding a type form, update `types`, annotation parsing, `resolveType`, assignability/printability as needed, lowering, and docs.

### Lowerer

- Lower only checked forms. The lowerer may assume the checker has rejected unsupported syntax and invalid types.
- Preserve minivm type pools and handler tables around optimizer passes as the current pipeline does.
- Verify every compiled program before returning it from `Compile`.
- For closure/capture changes, keep checker capture metadata and lowerer boxing behavior in sync.
- For specialization changes, keep per-specialization type tables isolated from the fallback function body.

## Native Modules

A native symbol should provide a coherent triple:

1. checker rule
2. emitter callback
3. optional runtime value / host function

Keep native operation semantics in `builtins` or `operator`; do not duplicate native type rules directly in the checker or lowerer.

Native symbols are callable names, not first-class values. If that changes, update `module`, checker name resolution, lowering, docs, and compatibility status.

## Tests

Tests should cover behavior, not internal shape, unless the internal shape is the contract being protected.

Prefer table tests for repeated behavior and focused tests for subtle control flow.

When a change touches multiple compiler phases, test the behavior through the highest meaningful public boundary and add lower-level tests only for phase-local contracts.

Keep fixtures small. A test should make the important source, diagnostic, or runtime behavior visible near the assertion.

Use `require` for assertions. Avoid direct `t.Fatal`, `t.Fatalf`, `t.Error`, and `t.Errorf` in new tests; use `require.NoError`, `require.ErrorIs`, `require.Equal`, `require.True`, or `require.Failf` instead.

Top-level tests target public symbols. Use names such as `TestFoo`, `TestNewFoo`, and `TestFoo_Bar`. Do not name top-level tests after private helpers unless that private shape is itself the contract being protected.

Match test files to production files when adding new test files:

```text
buffer.go      -> buffer_test.go
assembler.go   -> assembler_test.go
```

Keep nesting shallow. Aim for at most one `t.Run` level. Use table-driven tests when setup and assertions share one shape; use explicit subtests when cases need different setup or clearer labels. Do not mix table-driven and explicit subtest styles at the same nesting level.

Defer cleanup immediately after successful allocation. Avoid test helpers for fixtures, configured objects, or assertions by default. Duplicated setup is acceptable when it keeps the tested API visible.

Use package-local tests for lexer/token/parser/checker behavior, native module tests for builtin/operator rules, and integration tests for CLI/runtime paths where appropriate.

## Git and PRs

Keep commits focused. A commit should have one reason to exist.

Use commit messages that name the area and behavior, for example:

```text
compiler: reject dynamic kwargs unpacking
```

PR descriptions should include what changed, why it changed, how it was validated, and any intentionally deferred follow-up.

## Docs

Documentation should have one owner for each topic. Other documents should summarize and link to that owner instead of repeating the full explanation.

Use this standard document shape when it fits the document:

1. title and short purpose
2. `When to Read`
3. `Source of Truth` when relevant
4. main content
5. `Maintenance Notes`
6. `Related Docs`

The spec files are the owner for language behavior:

- `docs/spec/01-lexical.md` for tokens, literals, indentation, and f-strings.
- `docs/spec/02-types.md` for type forms, assignability, inference, narrowing, and specialization.
- `docs/spec/03-grammar.md` for syntax.
- `docs/spec/04-static-semantics.md` for checker behavior and diagnostics.
- `docs/spec/05-codegen.md` for lowering/runtime representation.
- `docs/spec/06-builtins.md` for builtin/operator/native-module behavior.

Status documents should link to the owner instead of repeating full rules:

- `docs/compatibility.md` summarizes user-facing Python compatibility status.
- `docs/roadmap.md` summarizes completed work and remaining gaps.
- `README.md` summarizes project purpose, package map, and run instructions.

Avoid stale milestone phrasing in spec files. If a feature is already shipped, describe the implementation and restrictions directly. If it is not shipped, state whether it is parse-only, rejected, planned, or out of scope.

Keep wording direct and standard. Prefer `minipy`, `minivm`, `lexer`, `parser`, `checker`, `lowerer`, `native module`, `source type`, `opcode`, `value`, and `diagnostic` consistently.

Agent instruction files are routing and enforcement surfaces. Keep `AGENTS.md` as the common Claude Code / Codex contract, keep `.claude/CLAUDE.md` as a short Claude overlay that imports `AGENTS.md`, and keep detailed coding rules in their owner docs.

## Maintenance Notes

When changing coding patterns:

- prefer rules that can be checked by reading nearby code
- avoid adding process that does not prevent real mistakes
- keep this document shorter than the code it governs
- keep minipy-specific rules aligned with minivm `docs/coding-patterns.md`
- update related docs if the documentation shape changes
- keep terminology aligned with the rest of `docs/`

## Related Docs

- minivm `docs/coding-patterns.md` - shared contributor coding patterns
- `README.md` - project overview and package map
- `docs/spec/00-overview.md` - compiler architecture and execution model
- `docs/spec/` - language and compiler behavior owners
- `docs/compatibility.md` - Python compatibility status
- `docs/roadmap.md` - remaining work and intentionally deferred features
