# minipy

minipy is a statically checked Python 3.13-inspired subset compiler targeting
[minivm](https://github.com/siyul-park/minivm). It parses Python-like source,
checks it with minipy's own type system, lowers directly to minivm bytecode, and
runs through minivm's interpreter.

The project is intentionally a subset, not a drop-in CPython implementation. The
implemented subset focuses on code that can be checked ahead of time and lowered
without preserving CPython's fully dynamic object model.

## Highlights

- **Direct minivm target** — the compiler emits minivm programs and verifies the
  optimized bytecode before returning it.
- **Static source types** — `int`, `float`, `bool`, `str`, `None`, containers,
  tuples, classes, iterators, `Callable`, closed unions, `T | None`, and `Any` are
  modeled separately from minivm runtime types.
- **Whole-program inference** — annotations are optional where the checker can
  infer a concrete, union, or `Any` type from assignments, defaults, returns, and
  call sites.
- **Python-like syntax with explicit limits** — functions, classes, imports,
  control flow, exceptions, pattern matching, comprehensions, slicing, f-strings,
  generators, and common builtins are supported within the documented subset.
- **Native module model** — `builtins`, `operator`, and `typing` are registered
  native modules; applications can add modules with `compiler.WithNativeModules`,
  and source modules load through explicit `fs.FS` search roots.

## Current status

The shipped compiler, CLI, and REPL support the language described in
`docs/spec/`. `docs/compatibility.md` tracks that implementation against Python
3.13 syntax and expression forms. Forms that parse for diagnostics but still stop
before lowering are called out explicitly in the spec instead of being hidden in
old milestone text.

Notable limits include no arbitrary-precision integers, no complex/bytes runtime
values, no scheduler/coroutine semantics for `async`/`await`, no dynamic
`**kwargs` call unpacking, no first-class module/class/native-function values,
and no CPython standard-library compatibility beyond native modules supplied to
the compiler.

## Repository map

| Path | Purpose |
|---|---|
| `token/` | Token kinds, positions, and diagnostic codes. |
| `lexer/` | Python-like indentation lexer and literal scanner. |
| `ast/` | Plain AST nodes for statements, expressions, patterns, and f-strings. |
| `parser/` | Recursive-descent parser for the supported grammar and parse-only forms. |
| `types/` | Source-level type lattice and mapping to minivm types. |
| `module/` | Native/source module registry contracts. |
| `builtins/` | Native `builtins` module and exception hierarchy. |
| `operator/` | Native `operator` module and shared operator semantics. |
| `hostabi/` | Checked host ABI conversion, formatting, and iterator bridge helpers. |
| `compiler/` | Loader, checker, lowerer, optimizer/verification pipeline, and import support. |
| `cmd/minipy/` | CLI and REPL. |
| `docs/README.md` | Documentation map and ownership guide. |
| `docs/spec/` | Implementation-facing language specification. |
| `docs/compatibility.md` | Python 3.13 feature compatibility matrix. |
| `docs/roadmap.md` | Completed work and remaining gaps. |
| `docs/coding-patterns.md` | Project conventions for code and documentation changes. |

## Build and test

```sh
go test ./...
go run ./cmd/minipy --help
```

Run a file:

```sh
go run ./cmd/minipy run path/to/program.py
```

Start the REPL:

```sh
go run ./cmd/minipy repl
```

## Documentation

Start with [Documentation](docs/README.md) for the full documentation map.

- [Overview](docs/spec/00-overview.md)
- [Lexical structure](docs/spec/01-lexical.md)
- [Types](docs/spec/02-types.md)
- [Grammar](docs/spec/03-grammar.md)
- [Static semantics](docs/spec/04-static-semantics.md)
- [Code generation](docs/spec/05-codegen.md)
- [Builtins and native modules](docs/spec/06-builtins.md)
- [Python 3.13 compatibility matrix](docs/compatibility.md)
- [Roadmap](docs/roadmap.md)
- [Coding patterns](docs/coding-patterns.md)

## License

MIT
