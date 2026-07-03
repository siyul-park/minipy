# minipy — Native Modules & Host ABI

minipy ships small typed native modules. Each native function is either
**lowered inline** to opcodes or **bound to a minivm host function**. Bare
builtin lookup is a fallback to the native `builtins` module, so
`import builtins; builtins.print(x)` and `from builtins import print as p` use the
same lowering as bare `print(x)`. The `operator` native module exposes Python's
operator-function names (`add`, `floordiv`, `eq`, `not_`, `contains`, ...).
Internally, a native module is a synthetic module entry plus a symbol table whose
exports map to `minivm/types.Value`; inline-only symbols use a native intrinsic
marker while host-backed symbols store the `interp.HostFunction` value.

## Binding strategies

1. **Inline** — the call is replaced by opcodes (no function call overhead).
   Example: `len(lst)` → `ARRAY_LEN`.
2. **Host function** — bound to a Go `interp.HostFunction` placed in the constant
   pool; emitted as `CONST_GET <i>; CALL`. Used for I/O and runtime helpers.

Native functions are statically typed; calling one with the wrong type/arity is a
compile error (`TypeMismatch`/`ArityMismatch`), not a runtime `TypeError`.

## Core builtins (M0–M3)

| builtin | signature | binding |
|---|---|---|
| `print` | `(*vals) -> None` (v1: `(str) -> None` + per-type overloads) | host |
| `len` | `(str) -> int`, `(list[T]) -> int`, `(dict[K,V]) -> int` | inline (`STRING_LEN`/`ARRAY_LEN`/`MAP_LEN`) |
| `range` | `(int) \| (int,int) \| (int,int,int) -> range` | inline (drives `for` desugar; no object) |
| `int` | `(float) -> int`, `(bool) -> int`, `(str) -> int` | inline conv (`F64_TO_I64_S`) / host (parse) |
| `float` | `(int) -> float`, `(str) -> float` | inline (`I64_TO_F64_S`) / host (parse) |
| `str` | `(int\|float\|bool) -> str` | host (format) |
| `bool` | `(int\|float\|str) -> bool` | inline (`!= 0` / nonempty) |
| `abs` | `(int) -> int`, `(float) -> float` | inline (branch / `F64_ABS`) |
| `min` / `max` | `(int,int)->int`, `(float,float)->float` | inline (compare + `SELECT` / `F64_MIN`/`MAX`) |

## `operator` native module (M8)

`operator` uses Python's standard function names for syntax operators:

| function | syntax |
|---|---|
| `add`, `sub`, `mul`, `truediv`, `floordiv`, `mod`, `pow` | `+`, `-`, `*`, `/`, `//`, `%`, `**` |
| `and_`, `or_`, `xor`, `lshift`, `rshift` | `&`, `|`, `^`, `<<`, `>>` |
| `neg`, `pos`, `invert`, `abs` | unary `-`, unary `+`, `~`, `abs` |
| `eq`, `ne`, `lt`, `le`, `gt`, `ge` | comparisons |
| `contains`, `truth`, `not_` | `b in a`, `bool(x)`, `not x` |

These functions are not first-class runtime values; they are compile-time native
symbols and must be called directly.

`print` is the canonical host function. Its Go shape (per minivm
[host-integration](https://github.com/siyul-park/minivm/blob/main/docs/host-integration.md)):

```go
print := interp.NewHostFunction(
    &types.FunctionType{Params: []types.Type{types.TypeString}, Returns: nil},
    func(vm *interp.Interpreter, args []types.Boxed) ([]types.Boxed, error) {
        s, _ := vm.Load(args[0].Ref()).(types.String)
        fmt.Println(string(s))   // host policy decides the sink
        return nil, nil
    },
)
```

`range` does not create a runtime object in v1; it only configures the `for`
loop's bounds at compile time ([`05-codegen.md`](05-codegen.md#for-range-and-iterables)).
A first-class lazy `range` object is deferred to M6 (it is naturally a generator).

## Later builtins (by milestone)

| milestone | adds |
|---|---|
| M3 | list methods `append/pop`, dict `get/keys/values/items`, str `upper/lower/split/join/find`, `enumerate`, `zip` |
| M5 | `isinstance` (limited, for class hierarchy), `@dataclass`, `@staticmethod` |
| M6 | `iter`, `next`, lazy `range` object |
| M7 | exception classes `Exception`, `ValueError`, `KeyError`, `IndexError`, … |
| M8 | native `builtins` and `operator` modules plus source module imports; curated typed stdlib native modules remain future library work |
| M10 | `isinstance(x, T)` narrows arbitrary union/`Any` members (lowered to `REF_TEST`) |

## Host-function ABI (for embedders)

minipy programs run inside a host Go program that calls minivm `interp.New`.
Native modules provide host functions for their own exported symbols; compiler
lowering helpers create any extra host functions needed for non-symbol runtime
operations. Contract:

- A host function has a `*types.FunctionType` (params/returns) that **must match**
  the type minipy assigns to the builtin; minipy emits calls assuming this ABI.
- Arguments arrive as typed `[]types.Boxed` (no reflection). Primitives are inline
  (`args[i].I64()`, `.F64()`, `.Bool()`); refs (`str`, `list`, …) are loaded with
  `vm.Load(args[i].Ref())`.
- Returns are `[]types.Boxed`; a `-> None` builtin returns `nil`.
- Resource and policy limits (heap, fuel, I/O sinks) are the host's to set via
  minivm options — minipy does not bypass them.

Native functions that need host support are inserted into the program constant
pool as `interp.HostFunction` values. Inline native functions emit bytecode
directly.

## Out of scope

`eval`, `exec`, `compile`, `globals`, `locals`, `vars`, `dir`, `getattr`,
`setattr`, `hasattr`, `__import__`, `open` (until M8 stdlib policy), `input`
(host-policy dependent), and any builtin returning an untyped/dynamic value in the
static core. Reflective/dynamic builtins, if ever added, belong to the opt-in M10
inference/union layer.
