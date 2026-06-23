# minipy — Coding Style

Adapted from minivm's
[coding-patterns](https://github.com/siyul-park/minivm/blob/main/docs/coding-patterns.md)
for this project. Read code like a behavior specification: a reader should grasp
*what* a package does and *where* the complexity lives without simulating the VM
in their head.

## Core philosophy

- **Readability over cleverness.** Explicit behavior beats hidden magic.
- **Push complexity down.** Keep public APIs (`lexer.Lex`, `parser.Parse`,
  `compiler.Compile`) small even when the implementation is involved.
- **Top-down.** Declare callers above callees; reading a file downward follows
  execution. Policy first, mechanics later.
- **Small surface.** Fewer files, types, functions, and arguments — each doing
  one conceptual thing (orchestrate, transform, validate, emit, or normalize).

## Project shape

The pipeline is one package per phase, mirroring the Go standard library
(`go/token`, `go/ast`, `go/parser`) and minivm's package split:

```text
token    lexical tokens, positions, the shared diagnostic vocabulary
ast      syntax tree nodes (plain data; every node carries a token.Pos)
lexer    io.Reader -> []token.Token
parser   io.Reader -> *ast.Module
types    minipy source types (int/float/bool/str/None) + mapping to minivm types
compiler type checker (`checker`, check.go) + lowering (`compiler`, compiler.go);
         Compile(io.Reader) -> *program.Program
cmd      the CLI and REPL
```

Dependency direction is one-way and acyclic: `token <- {lexer, ast, parser,
types, compiler}`, `compiler -> {ast, parser, types, minivm}`,
`cmd -> {compiler, minivm}`.

### minivm is used directly

minipy compiles to minivm and runs on its interpreter. minivm packages
(`program`, `instr`, `interp`, `types`, `optimize`) are imported where needed —
no wrapper layer. Import minivm's `types` as `vmtypes` to keep it distinct from
minipy's own `types`.

### Inputs are io.Reader

Source-consuming entry points (`lexer.Lex`, `parser.Parse`, `compiler.Compile`)
take an `io.Reader`, not a string. Callers wrap with `strings.NewReader` or pass
an `*os.File` directly.

### Errors are Python-consistent

Every diagnostic is a `*token.Error` with a catalogue `Code` and a source
position. `Code.Python()` maps it to the CPython exception name a user would see
for the same mistake (`TypeError`, `NameError`, `SyntaxError`, `ValueError`), and
that is what `Error()` renders. A phase collects a `token.ErrorList` and reports
**every** error it can before aborting — never fail-on-first.

## File layout (fixed order per file)

1. Public type
2. Private type
3. Public const
4. Private const
5. Public var
6. Private var (incl. interface-compliance assertions)
7. Constructors (`New<Type>`)
8. Public functions
9. Public methods
10. Private methods
11. Private functions

## Naming

- Intent-based: names describe the caller-visible outcome, not the mechanism.
- Shortest clear name; avoid one-letter names except domain standards
  (`VM`, `i` for the interpreter receiver in host functions).
- Constructors are `New<Type>`. The primary parse-like entry point is `Parse`/
  `Lex`/`Compile`; secondary targets get a `Parse<Type>` form.
- Prefer functional options (`WithOutput`) over config structs; apply defaults
  before options.

## Error handling

- Wrap propagated errors with `%w` and context: `fmt.Errorf("assemble: %w", err)`.
- Diagnostics from a compile phase are values (`token.ErrorList`), not panics.
- Panic only for violated internal invariants; recover at execution boundaries.

## Lowering to minivm

- The `compiler` (the lowering half, compiler.go) assumes a validated AST: it
  relies on the type table and never re-reports errors.
- The entry function has no module-level locals (`bp == sp`) and no entry-frame
  `RETURN` — it halts by running off the end of its code. Branch targets must
  stay within the code (the block analysis rejects a jump to `len(code)`), so
  `module` emits a trailing `NOP` as a landing pad for any merge label bound at
  the very end.
- Prefer inline opcode sequences over host functions. Use a host function only
  when an operation cannot be lowered inline today (e.g. `**` and float `%`,
  which need a loop/temporaries the module-entry frame has no locals for). Such
  cases are documented at their definition as future inline/extension-op work.

## Testing

- Use `go test` with `testify/require` (never `assert`).
- **One test function per public symbol**; sub-cases are `t.Run` subtests.
  Name them `Test<Func>` or `Test<Type>_<Method>`. Diagnostic/error-path cases
  for an entry symbol live in a single companion `Test<Func>Errors` table test
  (e.g. `TestCompile` + `TestCompileErrors`); do not add further per-feature test
  functions — fold new behavior in as subtests of the existing pair.
- Tests are **self-contained**: inline setup, execution, and assertions. The
  only shared helpers are thin adapters (e.g. wrapping a string in an
  `io.Reader`, or asserting a diagnostic `Code` is present).
- Assert diagnostics on the `Code` (via the typed `token.ErrorList`), not on the
  rendered string, so message wording can evolve.
- Table-driven where every case shares the same shape; explicit `t.Run`
  otherwise. Do not mix the two at one nesting level.
- Target ≥80% statement coverage per package.

## Git

- Commit subject: `<type>(scope): <summary>`, imperative mood, ≤72 chars, one
  logical concern per commit. Types: `feat fix refactor docs test chore perf ci`.
- Update this file when the style changes; update the specs in `docs/spec/` when
  the language changes.
