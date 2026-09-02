# AGENTS.md

Repository contract for coding agents working on minipy.

`docs/coding-patterns.md` is the normative coding and compiler-architecture
standard. This file routes work to that document and defines the required
execution workflow. It MUST NOT duplicate detailed standard rules.

## Instruction priority

1. The user's latest explicit request.
2. The closest applicable repository instruction.
3. This repository contract.
4. The coding standard (`docs/coding-patterns.md`).
5. General Go convention.

Nearby code overrides the coding standard only when it is more specific,
internally consistent, and compliant. Mention unresolved instruction conflicts
in the final summary.

## Required workflow

1. Run `git status --short --branch`; never overwrite unrelated user changes.
2. Read the coding standard SS2 and the task sections from its SS1.3 reading index.
3. Read the owning specification from the Task Router before code or test edits.
4. Establish a runnable baseline before changing behavior.
5. For multi-package or uncertain work, map ownership and phase boundaries before
   editing.
6. Change the smallest complete ownership unit; keep checker, lowerer, runtime,
   diagnostics, tests, and docs synchronized where the contract crosses them.
7. Run narrow checks after each ownership unit, then the Completion Gate.
8. Perform one final simplification and semantic review before committing.

## Quick commands

```bash
go test ./...
go vet ./...
go test ./compiler ./parser ./lexer
go test -run TestCompile ./compiler
go test ./codegen           # emitted-program goldens; -update rewrites them
go run ./cmd/minipy --help
```

Development servers, watchers, and interactive commands MUST NOT be used in
automated verification.

## Task Router

| Task | Read | Usually edit | Narrow verification |
|---|---|---|---|
| tokens / lexing | SS3-SS4, SS6-SS11; `docs/spec/01-lexical.md` | `token/`, `lexer/` | `go test ./token ./lexer` |
| parsing / grammar | SS3-SS8, SS11; `docs/spec/03-grammar.md` | `ast/`, `parser/` | `go test ./ast ./parser` |
| types / checker / diagnostics | SS5-SS9, SS11; `docs/spec/02-types.md`, `docs/spec/04-static-semantics.md` | `types/`, `compiler/check*.go`, `token/error.go` | `go test ./types ./compiler` |
| lowering / program passes | SS6-SS11; `docs/spec/05-codegen.md` | `compiler/lower*.go`, `compiler/compiler.go`, `hostabi/` | `go test ./compiler ./hostabi` |
| builtins / operators / native modules | SS5-SS9, SS11; `docs/spec/06-builtins.md` | `builtins/`, `operator/`, `module/`, `typing/` | `go test ./builtins ./operator ./module ./typing ./compiler` |
| module graph / imports | SS5-SS10; `docs/spec/00-overview.md`, `docs/spec/04-static-semantics.md` | `compiler/`, `module/` | `go test ./compiler ./module` |
| CLI / REPL | SS3-SS5, SS9-SS11; `README.md`, `docs/spec/00-overview.md` | `cmd/minipy/` | `go test ./cmd/minipy ./compiler` |
| emitted bytecode quality | SS6-SS11; `docs/codegen-quality.md`, `docs/spec/05-codegen.md` | `compiler/lower*.go`, `codegen/testdata/` | `go test ./codegen ./compiler` |
| coding standard | all of `docs/coding-patterns.md` | the standard, this file, tool overlays | docs review + `go test ./...` when code changes |
| compatibility / status | SS12; `docs/README.md` | `docs/`, `README.md` | docs review + owning package tests |

## Project and ownership map

minipy is a statically checked Python 3.13-inspired subset compiler targeting
minivm.

```text
source -> tokens -> AST -> checked module graph -> lowered program
       -> ordered transforms -> verified minivm program
```

| Package | Responsibility |
|---|---|
| `token` | token kinds, positions, diagnostic codes and rendering |
| `lexer` | rune, indentation, and literal scanning |
| `ast` | data-only syntax nodes |
| `parser` | grammar, precedence, recovery, and AST construction |
| `types` | source type lattice and minivm mapping |
| `module` | native/source extension contracts and registry |
| `builtins` | builtin symbols and exception hierarchy |
| `operator` | operator typing and lowering semantics |
| `hostabi` | host/minivm value bridge and iterator protocol |
| `typing` | annotation-only native module |
| `compiler` | module graph, checker, specialization, lowerer, passes |
| `cmd/minipy` | CLI and REPL process boundaries |

## Non-negotiable compiler invariants

- minipy is a subset, not a CPython runtime or compatibility layer.
- AST nodes remain data-only; semantic facts belong to checker-owned metadata.
- Unsupported constructs fail before lowering.
- Parser recovery may preserve parse-only forms for precise diagnostics.
- Checker and lowerer remain synchronized for types, narrowing, specialization,
  closures, exceptions, patterns, native calls, and containers.
- Native operation rules live in `builtins` or `operator`, not duplicated in the
  checker or lowerer.
- Mutable compilation state belongs to one invocation, not reusable compiler
  configuration.
- Transform order remains visible; the optimizer owns every table it rewrites
  and its result is never overwritten with a pre-optimization copy.
- Every returned minivm program is verified after all transforms.
- User-facing behavior updates the owning spec and status documents.

## Documentation owners

| Concern | Owner |
|---|---|
| architecture and execution model | `docs/spec/00-overview.md` |
| lexical rules | `docs/spec/01-lexical.md` |
| source types | `docs/spec/02-types.md` |
| grammar | `docs/spec/03-grammar.md` |
| checker and diagnostics | `docs/spec/04-static-semantics.md` |
| lowering and runtime representation | `docs/spec/05-codegen.md` |
| builtins and native modules | `docs/spec/06-builtins.md` |
| compatibility | `docs/compatibility.md` |
| completed and deferred work | `docs/roadmap.md` |
| coding and architecture standard | `docs/coding-patterns.md` |

## Completion Gate

Do not report completion, commit, push, or open a PR until all applicable items
are true:

1. Re-read every touched file against the coding standard SS2 and its task sections.
2. Confirm every touched symbol has a current owner and reason to exist.
3. Remove obsolete wrappers, helpers, fields, parameters, results, and aliases.
4. Confirm functions hold one abstraction level and callers precede callees.
5. Confirm phase products and public APIs do not expose mutable implementation
   state.
6. Confirm checker/lowerer/native/runtime symmetry for cross-phase changes.
7. Confirm diagnostics and operational error identity are preserved.
8. Confirm tests use public behavior or an explicitly protected returned program
   representation, never a private-state back door.
9. Run narrow tests, `go vet ./...`, and `go test ./...`.
10. Synchronize specs, compatibility, roadmap, workflow, and standard documentation.
11. Review the complete diff for unrelated edits and breaking API impact.
12. Perform a final simplification pass; record any deliberate deviation from a
    SHOULD rule or rejected simplification in the final summary.

## Git and publication

Use focused Conventional Commits as defined by the coding standard SS12.2. Public API breaks
MUST use `!` and a `BREAKING CHANGE:` body. Push only a non-default branch and
include tests, docs, migration impact, and known limitations in the PR body.
