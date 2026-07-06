# Coding Patterns

Default coding style for minipy contributors and agents.

Use this before writing or changing code. Match nearby code first; use this document when local style is unclear or a new pattern is needed.

The common sections intentionally mirror minivm's `docs/coding-patterns.md`. Minipy-specific compiler rules live in §9.

## When to Read

Read only what the change touches.

| Change | Read |
|---|---|
| function shape, helper extraction, naming | 0. Principles, 1. Functions |
| types, interfaces, fields, constructors | 2. Types |
| public APIs, options, builders, parsers | 3. APIs |
| errors, diagnostics, panic, recover | 4. Errors, 9. Minipy Compiler Rules |
| architecture-specific files | 5. Build Tags |
| tests | 6. Tests |
| commits and PRs | 7. Git and PRs |
| documentation changes | 8. Docs |
| lexer/parser/checker/lowerer/native modules | 9. Minipy Compiler Rules |

## Source of Truth

| Concern | Source |
|---|---|
| formatting | `gofmt` |
| package style | nearby code in the same package |
| shared coding-pattern shape | minivm `docs/coding-patterns.md` |
| public API shape | existing public APIs and tests |
| language behavior | `docs/spec/` |
| Python compatibility status | `docs/compatibility.md` |
| remaining work | `docs/roadmap.md` |
| documentation shape | `docs/README.md` |
| package ownership | this document and package comments |

## 0. Principles

### 0.1 Simpler is Better

If two designs provide the same behavior, choose the simpler one: fewer files, types, functions, methods, arguments, names, indirections, and more local code.

Add abstraction only when it improves readability, removes real duplication, isolates real complexity, or names a meaningful domain step.

### 0.2 Keep Public Surfaces Small

Push complexity down. Prefer simple APIs over exposed mechanisms, explicit behavior over hidden magic, local complexity over distributed state, and predictable structure over clever abstraction.

A difficult behavior should have one clear implementation, not many partially difficult call sites.

### 0.3 Read Top-Down

Put important behavior first and details later.

```text
Compile
  parse
  check
  emit
  verify
```

Readers should not jump around to reconstruct the main path.

### 0.4 Be Obvious

Prefer mechanically obvious code over clever code. Slightly longer code is better when it avoids hidden state, improves debugging, preserves checker/lowerer symmetry, makes control flow explicit, or keeps behavior easy to verify.

### 0.5 Preserve Symmetry

Keep checker and lowerer paths structurally similar when possible. Symmetry matters more than small local optimizations because it makes behavior easier to compare, test, and maintain.

### 0.6 Keep Related Code Close

Keep state, validation, mutation, and cleanup for one behavior near each other. Split only when ownership or readability clearly improves.

### 0.7 Review Every Symbol

Every file, type, interface, struct, field, function, method, parameter, result, constant, and variable needs a reason to exist.

For every touched symbol, ask whether it can be removed, inlined, merged with an existing owner, narrowed in scope, made private, renamed by role, or replaced by direct local code. Review nearby old symbols exposed by the change too.

A refactor is incomplete if it adds structure while leaving now-obvious dead fields, arguments, results, helpers, or wrapper files behind. If the only reason is future flexibility, symmetry without behavior, shorter code, or one-call-site convenience, remove it.

### 0.8 Prefer Simpler Algorithms

Before adding structure, look for a simpler or more efficient algorithm.

Prefer one direct pass over coordinated passes, local state over global maps, exact ownership over cleanup protocols, and data flow matching the compiler phase. Do not optimize by hiding behavior; keep correctness, checker/lowerer parity, and documented language restrictions obvious.

Performance claims need benchmark evidence.

### 0.9 Iterate Until Stable

Simplify in passes. Removing one symbol, helper, field, pass, or branch often exposes the next simplification.

Each pass checks: removable symbols, narrower ownership or visibility, simpler control flow, simpler algorithms, then tests/docs matching the final shape. Stop only when another pass finds no safe improvement.

Record intentionally non-viable simplifications in the final summary so future work does not re-derive them silently.

## 1. Functions

### 1.1 Use One Abstraction Level

Each function should operate at one conceptual level. Do not mix orchestration with parsing details, policy with arithmetic, or high-level flow with byte/index mutation.

Good functions read like behavior:

```go
func (c *Compiler) Compile() (*program.Program, error) {
    mod, err := c.parse()
    if err != nil {
        return nil, err
    }

    checked, err := c.check(mod)
    if err != nil {
        return nil, err
    }

    return c.emit(checked)
}
```

If comments explain transitions between unrelated steps, the function likely mixes levels.

### 1.2 Name by Role

Names describe caller-visible behavior, not implementation mechanics. Use the shortest standard name that is still clear; prefer one word when package, file, receiver, or context already provides meaning.

| Avoid | Prefer |
|---|---|
| `rewriteBranchAbsolute` | `normalize` |
| `appendInstrAndUpdateLen` | `commit` |
| `checkEmptyAndFormatProg` | `show` |
| `checkerContext` | `scope` |
| `loweringFrame` | `frame` |
| `sourceTypeValue` | `value` |
| `parseOperation` | `step` |

Receiver context counts: `c.check()` and `c.emit()` are clear on `*Compiler`.

Avoid names that repeat package/file/subsystem context, non-standard abbreviations, one-letter names outside tight scopes, and implementation-step names when a role name is enough.

Allowed abbreviations: common domain terms such as `ID`, `IP`, `ABI`, `VM`, and `CPU`.

Good role words: `value`, `step`, `state`, `frame`, `module`, `target`, `source`, `scope`, `compiler`, `builder`, `cache`, `trace`, `root`, `exit`.

Short and clear is the goal; cryptic is not.

### 1.3 Declare Callers Before Callees

Within a file and section, place high-level functions before helpers they call. Functional options may appear immediately before the constructor they configure so read order matches call sites like `New(root, WithFS(fsys))`.

### 1.4 Extract Only When Useful

Inline simple single-use logic.

Extract only when a helper removes real duplication, isolates real complexity, names meaningful behavior, or keeps the caller at one abstraction level. Do not extract tiny helpers that hide a short switch, predicate, or loop used once.

Let one helper own a branch-or-fallback decision. If it can decide whether it applies and return failure/fallback, callers should not duplicate its preconditions.

### 1.5 Methods Show Ownership

A function used by one type belongs on that type, even when the receiver is not used directly. Callbacks should also be methods when ownership belongs to a type.

```go
func (c *checker) checkName(n *ast.Name) types.Type { return c.scope.lookup(n.ID) }
```

Package functions are for constructors, functions used by multiple types, public general utilities, or helpers used only by other package-level functions.

Do not extract a tiny single-use method just to satisfy ownership; inline it instead.

### 1.6 Constructors Are Functions

Constructors are standalone functions, never methods.

```go
func newChecker(...) *checker
func NewCompiler(...) *Compiler
```

Public concrete types use `NewType`; private concrete types use `newType`.

### 1.7 Keep Methods with Their Type

A file should contain methods for one main type. Split a large type across files by concern if needed, but do not place methods for type A in type B's file just for locality.

For minipy code, checker behavior stays with checker files, lowering behavior stays with lowerer files, and public compiler entry points stay with `*Compiler`.

## 2. Types

Add a type only when it owns data with behavior, names a real domain concept, or prevents repeated error-prone structure. Do not add one to group temporary values, hide a simple tuple, or create future abstraction.

### 2.1 Define Interfaces Where Consumed

Interfaces belong in the package that consumes behavior, not the package that implements it. They describe what the caller needs.

### 2.2 Prefer Private Type, Public Instance

When there is one meaningful implementation, use an unexported concrete type with an exported value.

```go
type intType struct{}
var TypeInt = intType{}
```

### 2.3 Keep Interfaces Small

Do not create an interface until a consumer needs it. Do not add methods for later.

Assert compliance near the related type, in the private value section:

```go
var _ module.Native = (*nativeModule)(nil)
```

### 2.4 File Layout

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

Within each group, keep callers before callees. When a package function becomes a method, move it into method territory; constructors stay in the constructor section.

### 2.5 Order Struct Fields by Meaning

Order fields by how readers understand the type:

1. lifecycle and policy objects
2. infrastructure
3. program data
4. runtime state
5. mutable counters
6. read-only config
7. sync primitives

Separate layers with a blank line. Put rich behavioral objects near the top, mutable counters above read-only config, plain numeric config near the bottom, and `sync.Mutex` last. Struct literals follow field declaration order.

Field names should be short and clear; prefer one word when possible.

## 3. APIs

Public APIs should make the common path obvious, keep advanced behavior explicit, and avoid exposing internal representation without a stable caller need.

### 3.1 Constructors

Constructor names use `New<Type>` for public types and `new<Type>` for private types. Constructors establish invariants; if a value has no invariants, a struct literal may be clearer.

### 3.2 Parsers

Parser names:

| Function | Meaning |
|---|---|
| `Parse` | package primary type |
| `Parse<Type>` | secondary type |
| `ParseAll` | multiple values, usually from `io.Reader` |

```go
func Parse(s string) (*ast.Module, error)
func ParseExpr(s string) (ast.Expr, error)
func ParseAll(r io.Reader) ([]*ast.Module, error)
```

### 3.3 Options

Prefer functional options over config structs. Use direct arguments for required behavior and options for rare configuration. Apply defaults first, then options.

```go
func New(root string, opts ...func(*option)) *Compiler {
    opt := option{level: optimize.O2}
    for _, fn := range opts {
        fn(&opt)
    }
    ...
}
```

### 3.4 Builders

Builders are mutable; built values are treated as immutable. Discard builders after `Build()`.

A builder constructs one thing, validates near construction, and must not become a general mutable config store.

### 3.5 Avoid Premature API Surface

Do not add public methods, options, interfaces, or exported fields unless a real caller needs them. Smaller APIs are easier to maintain, test, and keep compatible.

## 4. Errors

Return errors for expected failure. Panic only for internal invariants.

### 4.1 Errors Are API

Sentinel errors are stable semantic categories, not implementation details.

Keep error values stable when callers can reasonably branch on them.

### 4.2 Wrap Errors with `%w`

Use `%w` whenever returning an error with context.

```go
return nil, fmt.Errorf("%w: %s", ErrModuleNotFound, name)
return fmt.Errorf("%w: line=%d", ErrInvalidSyntax, pos.Line)
```

### 4.3 Panic Only for Invariants

Panic is allowed only for violated internal invariants, usually in hot paths. Normal control flow returns errors.

Recover at the execution boundary, not throughout the codebase. Recovery must be local, documented, and tied to a specific boundary.

Use structured error types only when callers need more than a message.

For malformed user source, report diagnostics through `token.Error` / `token.ErrorList`; do not panic.

## 5. Build Tags

Keep architecture-specific code isolated behind build tags, with matching stubs and mirrored test tags.

```go
//go:build arm64
```

```go
//go:build !arm64
```

Portable behavior belongs in the default implementation. Architecture files provide only the narrow part that must differ.

When adding architecture-specific behavior, update the relevant compatibility and implementation docs.

## 6. Tests

Tests cover behavior, not private shape, unless the shape is the protected contract. If a change touches checker and lowerer paths, test both or explain why one is not applicable.

### 6.1 Tests Are Executable Documentation

Tests should show setup, execution, and expectation in one visible flow. Avoid fixture builders, test-only run wrappers, assertion helpers, and hidden setup helpers.

Duplicated setup is acceptable when it keeps the tested API visible.

```go
func TestCompile_RejectsUnsupported(t *testing.T) {
    _, err := Compile("async def f(): pass")
    require.Error(t, err)
    require.Contains(t, err.Error(), "unsupported")
}
```

### 6.2 Test Public Behavior

Top-level tests target public symbols.

| Symbol | Test |
|---|---|
| `Foo` | `TestFoo` |
| `NewFoo` | `TestNewFoo` |
| `(Foo).Bar` | `TestFoo_Bar` |

Do not name top-level tests after private helpers. Test private behavior through the public API that owns the observable behavior.

### 6.3 Match Test Files to Production Files

Use matching names: `lexer.go` -> `lexer_test.go`, `parser.go` -> `parser_test.go`, `check.go` -> `check_test.go`.

Tests for a public symbol belong in the test file matching the file that defines the owning type or constructor.

### 6.4 Keep Nesting Shallow

Aim for at most one `t.Run` level. Do not add wrapper subtests just to group cases.

Use table tests when setup and assertions share one shape. Use explicit subtests when cases need different setup or clearer labels. Do not mix styles at the same nesting level.

### 6.5 Use `require`

Always use `require`, not `assert`.

```go
require.NoError(t, err)
require.ErrorIs(t, err, ErrFoo)
require.Equal(t, want, got)
```

Avoid direct `t.Fatal`, `t.Fatalf`, `t.Error`, and `t.Errorf` in new tests.

### 6.6 Clean Up Immediately

Defer cleanup right after successful allocation.

```go
c := New(root)
defer c.Close()
```

### 6.7 Keep Fixtures Small

A test should make important source, diagnostics, or runtime behavior visible near the assertion. Prefer table tests for repeated behavior and focused tests for subtle control flow.

### 6.8 No Test Helpers by Default

Do not add test helpers for fixtures, programs, contexts, configured objects, assertions, or white-box introspection by default.

Before adding a helper, ask: can this be inlined, can this be a table, or does this belong in production code?

Only add a helper when it is clearly better than visible, local test flow.

### 6.9 Shared Compiler Tests

For compiler behavior, prefer package-local tests for lexer/token/parser/checker contracts and integration-style compile/run tests for behavior that crosses phases.

Behavior that does not fit a table row should be an explicit subtest after the table loop.

## 7. Git and PRs

Keep commits focused. A commit should have one reason to exist.

### 7.1 Branch and Commit Types

| Change | Branch | Commit |
|---|---|---|
| bug | `hotfix/<desc>` | `fix` |
| feature | `feature/<desc>` | `feat` |
| performance | `feature/<desc>` | `perf` |
| refactor | - | `refactor` |
| test | - | `test` |
| docs | - | `docs` |

Use lowercase, concise, hyphen-separated names.

### 7.2 Commit Format

Use `<type>(scope): <summary>`.

```text
feat(compiler): add trace lowering support
fix(parser): reject invalid match pattern
feat!: change source type format
```

Rules: imperative mood, at most 72 characters, one logical concern per commit. Breaking changes include `BREAKING CHANGE: ...`.

### 7.3 Performance Changes

Performance claims require benchmark evidence:

```text
before: ...
after:  ...
conclusion: ...
```

### 7.4 Self-Review Checklist

Before opening a PR, check:

- issue is fully resolved; no unrelated changes
- every touched symbol has a reason to exist
- removable symbols were removed, inlined, merged, narrowed, or made private
- the algorithm is the simplest correct option found
- repeated review passes find no safe simplification
- names are short, standard, and consistent
- public surface is minimal
- invariants are preserved
- tests cover behavior
- docs are updated when conventions change

### 7.5 Pull Requests

Follow the existing PR template. Explain what changed, why it changed, how it was tested, and benchmark impact if relevant. PR titles follow commit-summary style.

## 8. Docs

Documentation is part of the codebase. Each topic should have one owner; other docs summarize and link instead of repeating full explanations.

Use the standard shape from `docs/README.md`:

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

Keep wording direct and standard. Prefer `minipy`, `minivm`, `lexer`, `parser`, `checker`, `lowerer`, `native module`, `source type`, `opcode`, `value`, and `diagnostic` consistently.

Agent instruction files are routing and enforcement surfaces. Keep `AGENTS.md` as the common Claude Code / Codex contract, keep `.claude/CLAUDE.md` as a short Claude overlay that imports `AGENTS.md`, and keep detailed coding rules in their owner docs.

A convention-changing code change is incomplete without the matching documentation update.

| Change | Update |
|---|---|
| style, naming, structure | `docs/coding-patterns.md` |
| language syntax | `docs/spec/03-grammar.md` |
| type/checker behavior | `docs/spec/02-types.md` or `docs/spec/04-static-semantics.md` |
| lowering/runtime representation | `docs/spec/05-codegen.md` |
| builtins/operator/native modules | `docs/spec/06-builtins.md` |
| compatibility status | `docs/compatibility.md` |
| completed/deferred work | `docs/roadmap.md` |
| workflow / convention rules | `AGENTS.md` and `.claude/CLAUDE.md` |

## 9. Minipy Compiler Rules

### 9.1 Keep the Language Subset Explicit

Every construct should be documented as lowered, parse-only, restricted, rejected, planned, or out of scope.

Prefer static checks over runtime surprises. Unsupported constructs should fail in the checker before lowering.

Avoid stale milestone phrasing in spec files. If a feature is shipped, describe the implementation and restrictions directly. If it is not shipped, state whether it is parse-only, rejected, planned, or out of scope.

### 9.2 Package Ownership

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

### 9.3 Compiler Pipeline

Keep phase ownership clear.

#### Lexer

- Keep token spelling and token names in `token/token.go` synchronized with `docs/spec/01-lexical.md`.
- Emit recoverable diagnostics and continue scanning when possible.
- Keep soft keywords as `NAME`; the parser decides whether a soft keyword starts a special form.
- Do not split f-strings into multiple token kinds unless the parser and docs are updated together.

#### Parser

- Accept parse-only syntax only when it improves diagnostics.
- Document every parse-only form.
- Keep precedence centralized in the expression parser and update `docs/spec/03-grammar.md` when adding an operator.
- Parse syntax shape only. Type checking, scope rules, module resolution, constructor legality, and runtime support checks belong in the checker.
- Preserve source positions from the first token of each node.

#### Checker

- Prefer concrete types and closed unions over `Any`.
- Do not introduce implicit numeric promotion without documenting the rule and updating operator tests.
- Reject constructs that cannot be lowered before code generation.
- Keep flow narrowing and static-truth pruning mirrored between checker and lowerer when specializations make branches unreachable.
- When adding a type form, update `types`, annotation parsing, `resolveType`, assignability/printability as needed, lowering, and docs.

#### Lowerer

- Lower only checked forms. The lowerer may assume the checker has rejected unsupported syntax and invalid types.
- Preserve minivm type pools and handler tables around optimizer passes as the current pipeline does.
- Verify every compiled program before returning it from `Compile`.
- For closure/capture changes, keep checker capture metadata and lowerer boxing behavior in sync.
- For specialization changes, keep per-specialization type tables isolated from the fallback function body.

### 9.4 Native Modules

A native symbol should provide a coherent triple:

1. checker rule
2. emitter callback
3. optional runtime value / host function

Keep native operation semantics in `builtins` or `operator`; do not duplicate native type rules directly in the checker or lowerer.

Native symbols are callable names, not first-class values. If that changes, update `module`, checker name resolution, lowering, docs, and compatibility status.

## Agent Rule of Thumb

When unsure, choose the smallest correct change.

Prefer local code over a helper, one clear function over fragments, one short role name over mechanism names, one cohesive type over interfaces, explicit flow over clever indirection, one direct algorithm over coordinated state, and nearby style over a new pattern.

The best design keeps behavior obvious, names few things, and leaves the next reader with less to understand.

## Maintenance Notes

When changing coding patterns, keep rules readable from nearby code, avoid process that prevents no real mistakes, preserve useful historical rules unless they conflict with current code, keep terminology aligned with `docs/`, and update `docs/README.md` if the documentation shape changes.

## Related Docs

- minivm `docs/coding-patterns.md` - shared contributor coding patterns
- `README.md` - project overview and package map
- `docs/README.md` - documentation ownership and format
- `docs/spec/00-overview.md` - compiler architecture and execution model
- `docs/spec/` - language and compiler behavior owners
- `docs/compatibility.md` - Python compatibility status
- `docs/roadmap.md` - remaining work and intentionally deferred features
