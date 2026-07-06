# AGENTS.md

Repository instructions for Codex and Claude Code.

This file is the common agent contract. Codex reads `AGENTS.md` directly. Claude Code loads `.claude/CLAUDE.md`, which imports this file and adds Claude-specific reminders.

Keep this file terse and actionable. Put detailed coding rules in `docs/coding-patterns.md`, not here.

## Instruction Priority

1. Follow the user's latest explicit request first.
2. Follow the closest applicable repository instruction file.
3. Use this file as the root repository contract.
4. Use `docs/coding-patterns.md` as the coding-style authority.
5. Match nearby code when it is stricter than this guide.

If instructions conflict, choose the more specific instruction and mention the conflict in the final summary.

## Quick Commands

```bash
go test ./...
go test ./compiler ./parser ./lexer
go test -run TestCompile ./compiler
go run ./cmd/minipy --help
go run ./cmd/minipy run path/to/program.py
go run ./cmd/minipy repl
```

## Required Workflow

1. `git status --short`; never overwrite unrelated user changes.
2. Prefer structural exploration before edits; use direct file reads only when the target is known.
3. Read task-relevant docs from Task Router before writing code or tests.
4. Read `docs/coding-patterns.md` through its Fast Path: always apply §0, then the task-relevant sections from its When to Read table.
5. Mirror nearby tests; follow Test Conventions and `docs/coding-patterns.md` §6.
6. Update docs using `docs/coding-patterns.md` §8 when behavior, diagnostics, syntax support, compatibility, pitfalls, workflow, or conventions change.
7. Run narrow tests first, then `go test ./...` when the change warrants it.
8. For lexer/parser/checker/lowerer/native-module work, read the owning spec from Task Router and keep implementation, diagnostics, and docs synchronized.
9. Before reporting done, perform the Completion Gate below.

## Completion Gate

Do not call work complete, open/update a PR, or summarize a change as complete until every item below is true.

1. Every touched code/test file was re-read against `docs/coding-patterns.md` §0.7-§0.9 and the task-specific sections.
2. Every touched symbol has a current reason to exist.
3. Removable symbols were removed, inlined, merged, narrowed, made private, renamed by role, or replaced by direct local code.
4. A simpler algorithm or control flow was considered; the chosen shape is the simplest correct option found.
5. Another simplification pass found no safe improvement.
6. Declaration order follows `docs/coding-patterns.md` §1.3 and §2.4: callers before callees, except `With*` option functions may sit immediately above the constructor they configure.
7. Tests follow `docs/coding-patterns.md` §6 and assert behavior rather than private shape.
8. Public language behavior, diagnostics, compatibility status, and roadmap status are documented in the owning docs.
9. PR, commit, and documentation expectations follow `docs/coding-patterns.md` §7-§8.
10. Any intentionally skipped simplification is recorded in the final summary with the reason.

## Coding Pattern Map

`docs/coding-patterns.md` is the authority. This section routes agents to the right parts; it is not a replacement.

| Need | Read in `docs/coding-patterns.md` |
|---|---|
| Before any code/test edit | When to Read, §0 |
| Removing unnecessary structure | §0.1, §0.7-§0.9 |
| Naming, helper extraction, method ownership | §1.2, §1.4, §1.5 |
| File order, type/interface shape, struct fields | §2.1-§2.5 |
| Public API, options, builders, parsers | §3 |
| Errors, diagnostics, panic, recover | §4, §9 |
| Architecture build tags | §5 |
| Tests | §6 |
| Commits, PRs, final review | §7 |
| Documentation updates | §8 |
| Minipy compiler phases, native modules, subset status | §9 |

## Task Router

| Task | Read | Usually edit | Verify |
|---|---|---|---|
| Lexing / tokens | `docs/spec/01-lexical.md`, `docs/coding-patterns.md` §9.3 | `token/`, `lexer/` | `go test ./token ./lexer` |
| Parsing / grammar | `docs/spec/03-grammar.md`, `docs/coding-patterns.md` §9.3 | `ast/`, `parser/` | `go test ./ast ./parser` |
| Type checking / diagnostics | `docs/spec/02-types.md`, `docs/spec/04-static-semantics.md`, `docs/coding-patterns.md` §9.3 | `types/`, `compiler/check*.go`, `token/error.go` | `go test ./types ./compiler` |
| Lowering / runtime representation | `docs/spec/05-codegen.md`, `docs/coding-patterns.md` §9.3 | `compiler/lower*.go`, `hostabi/` | `go test ./compiler ./hostabi` |
| Builtins / operator semantics | `docs/spec/06-builtins.md`, `docs/coding-patterns.md` §9.4 | `builtins/`, `operator/`, `module/` | `go test ./builtins ./operator ./module ./compiler` |
| Module loading / imports | `docs/spec/00-overview.md`, `docs/spec/04-static-semantics.md` | `compiler/`, `module/` | `go test ./compiler ./module` |
| CLI / REPL | `README.md`, `docs/spec/00-overview.md` | `cmd/minipy/` | `go test ./cmd/minipy ./compiler` |
| Compatibility/status docs | `docs/README.md`, `docs/compatibility.md`, `docs/roadmap.md` | `docs/`, `README.md` | docs review + relevant package tests |
| Style-only change | `docs/coding-patterns.md` | touched package/docs | package tests or docs review |

## Documentation Index

Read only docs relevant to the task.

| Document | Covers |
|---|---|
| `README.md` | project purpose, package map, quick commands |
| `docs/README.md` | documentation map and ownership guide |
| `docs/spec/00-overview.md` | compiler architecture and execution model |
| `docs/spec/01-lexical.md` | tokens, literals, indentation, f-strings |
| `docs/spec/02-types.md` | source type system, assignability, inference, narrowing |
| `docs/spec/03-grammar.md` | accepted syntax and parse-only forms |
| `docs/spec/04-static-semantics.md` | checker behavior, scope, diagnostics |
| `docs/spec/05-codegen.md` | lowering and runtime representation |
| `docs/spec/06-builtins.md` | builtins, operator, native module behavior |
| `docs/compatibility.md` | user-facing Python 3.13 compatibility status |
| `docs/roadmap.md` | completed work and remaining gaps |
| `docs/coding-patterns.md` | style authority: shared principles, symbol review, naming, file layout, APIs, errors, tests, PR/docs rules, minipy compiler rules |

## Project Map

minipy is a statically checked Python 3.13-inspired subset compiler targeting minivm.

```text
source.py -> lexer -> parser -> checker -> lowerer -> minivm program -> verify/run
```

| Package | Responsibility |
|---|---|
| `token/` | token kinds, positions, diagnostic codes |
| `lexer/` | indentation lexer and literal scanner |
| `ast/` | data-only syntax tree nodes |
| `parser/` | recursive-descent parser for supported and parse-only syntax |
| `types/` | source-level type lattice and minivm type mapping |
| `module/` | native/source module registry contracts |
| `builtins/` | native `builtins` module and exception hierarchy |
| `operator/` | native `operator` module and shared operator semantics |
| `hostabi/` | runtime host ABI helpers and bridge types |
| `compiler/` | loader, checker, lowerer, optimizer/verification pipeline |
| `cmd/minipy/` | CLI and REPL |

## Key Invariants

Violations cause incorrect diagnostics, invalid lowering, or runtime mismatch.

- minipy is a subset, not a drop-in CPython implementation.
- Unsupported constructs should be rejected before lowering, not fail later at runtime.
- Parse-only syntax exists only to improve diagnostics and must be documented as parse-only.
- AST nodes stay data-only; semantic checks belong in compiler phases.
- Checker and lowerer assumptions must stay synchronized for narrowing, specialization, closures, exceptions, patterns, and native calls.
- Native symbol behavior belongs in `builtins` or `operator`; do not duplicate native type/lowering rules directly in the checker or lowerer.
- `compiler.Compile` and `compiler.New(...).Compile()` remain the obvious public entry points.
- Every compiled program must be verified before it is returned.
- Any user-facing language behavior change must update the owning spec file and compatibility/roadmap status when relevant.

## Tests

Before writing or modifying tests, read relevant docs from Task Router and apply `docs/coding-patterns.md` §6.

Core reminders:

- One top-level test per public symbol: `Test<Func>` or `Test<Type>_<Method>`.
- Put sub-cases under `t.Run`; do not split them into parallel top-level tests.
- Keep source snippets, diagnostics, and expected runtime behavior visible near assertions.
- Inline setup, run sequence, and assertions unless §6.8 allows a helper.
- Use `require`, not `assert` or direct `t.Fatal` / `t.Errorf`, in new tests.

## Documentation Maintenance

Update docs when behavior, diagnostics, syntax support, compatibility, commands, architecture, pitfalls, workflow, or conventions change. Use the owner matrix in `docs/coding-patterns.md` §8:

- workflow / convention rules -> update both `AGENTS.md` and `.claude/CLAUDE.md`
- coding style -> update `docs/coding-patterns.md`
- language syntax -> update `docs/spec/03-grammar.md`
- type/checker behavior -> update `docs/spec/02-types.md` or `docs/spec/04-static-semantics.md`
- lowering/runtime representation -> update `docs/spec/05-codegen.md`
- builtins/operator/native modules -> update `docs/spec/06-builtins.md`
- user-facing compatibility -> update `docs/compatibility.md`
- completed/deferred work -> update `docs/roadmap.md`

Keep edits terse and factual; document current behavior only; preserve formatting; verify Markdown.
