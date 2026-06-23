# minipy — Code Generation (lowering to minivm)

The compiler walks the typed AST and produces a minivm `program.Program`. It emits
`instr.Instruction` sequences with **symbolic labels**; a final pass resolves
labels to the signed-16-bit relative offsets minivm branches expect
(`target = instruction_start + width + operand`). minivm's optimizer
(`optimize.NewOptimizer(optimize.O1)`) runs afterward.

Opcode names and semantics below are from minivm
[`docs/instruction-set.md`](https://github.com/siyul-park/minivm/blob/main/docs/instruction-set.md).
Stack effect notation: `a b → c` means pops `a` then `b`, pushes `c`.

## Program shape

- The module body becomes the **entry function** (`fp == 1`). Top-level `def`s and
  `class`es become `*Function`/struct-type constants in the program constant pool.
- Module globals → VM globals (`GLOBAL_*`, u16 index).
- Strings, function templates, and out-of-inline-range constants live in the
  constant pool (`CONST_GET`, u16 index).

## Values & literals

| minipy | emit |
|---|---|
| `int` literal `n` | `I64_CONST n` |
| `float` literal `x` | `F64_CONST x` |
| `True` / `False` | `I32_CONST 1` / `I32_CONST 0` |
| `None` | `REF_NULL` |
| `str` literal | `CONST_GET <pool index of String>` |

## Variables

| access | local | global | upvalue |
|---|---|---|---|
| read | `LOCAL_GET i` | `GLOBAL_GET i` | `UPVAL_GET i` |
| write | `LOCAL_SET i` | `GLOBAL_SET i` | `UPVAL_SET i` |
| write-and-keep | `LOCAL_TEE i` | `GLOBAL_TEE i` | — |
| delete (M9) | `LOCAL_DELETE i` | `GLOBAL_DELETE i` | — |

A mutable captured variable is boxed: `REF_NEW` at definition, `REF_GET`/`REF_SET`
through the upvalue cell (see [closures](#closures-m4)).

## Arithmetic & comparison

`int` (i64) and `float` (f64) pick the matching opcode family. Operands are
pushed left then right; the binary op pops both.

| op | `int` | `float` |
|---|---|---|
| `+ - *` | `I64_ADD/SUB/MUL` | `F64_ADD/SUB/MUL` |
| `/` | (int→float) `I64_TO_F64_S` both, then `F64_DIV` | `F64_DIV` |
| `//` | `I64_DIV_S` | `F64_DIV` then `F64_FLOOR` |
| `%` | `I64_REM_S` | `F64` floor-mod sequence |
| `**` | exponent-by-squaring loop / host `pow` | host `pow`→`F64` |
| `& \| ^ << >>` | `I64_AND/OR/XOR/SHL/SHR_S` | — (int only) |
| unary `-` | `I64_CONST 0` `SWAP` `I64_SUB` | `F64_NEG` |
| unary `~` | `I64_CONST -1` `I64_XOR` | — |
| `== != < <= > >=` | `I64_EQ/NE/LT_S/LE_S/GT_S/GE_S` → i32 | `F64_EQ/NE/LT/LE/GT/GE` → i32 |

`int / int` always yields `float` (matches Python true division): convert both
operands with `I64_TO_F64_S`, then `F64_DIV`. Overflow on `+ - * <<` is **not**
checked — i64 wraps, by design. `// %` by zero traps (`I64_DIV_S`/`REM_S`
→ `ErrDivideByZero`).

`str` `==`/`<`… use `STRING_EQ`/`STRING_LT`/… ; `str + str` is `STRING_CONCAT`.

## Boolean & short-circuit

`a and b` / `a or b` short-circuit via branches (operands are `bool`=i32):

```text
# a and b
<a>                 # i32 on stack
DUP
BR_IF  L_eval_b     # if a != 0, evaluate b
BR     L_end        # else result is a (false)
L_eval_b:
DROP
<b>
L_end:
```

`not a` → `I32_EQZ`. `a if c else b` lowers `<c>; BR_IF Lt; <b>; BR Le; Lt: <a>; Le:`
(or `SELECT` when both arms are side-effect-free single values).

## Control flow

### if / elif / else

```text
<cond>            # i32
I32_EQZ           # invert: jump when false
BR_IF L_else
<then-block>
BR L_end
L_else:
<else-block>      # or next elif chain
L_end:
```

### while

```text
L_top:
<cond>
I32_EQZ
BR_IF L_end
<body>            # break → BR L_end ; continue → BR L_top
BR L_top
L_end:
```

`break`/`continue` emit `BR` to the loop's `L_end`/`L_top` labels (a label stack
tracks the enclosing loop). `while…else` runs the `else` block at `L_end` only
when no `break` fired (separate `L_else_done` label).

### for (range and iterables)

`for i in range(a, b, s)` desugars to an integer `while` loop (no allocation):

```text
i = a
L_top:
<i < b>  (or i > b when s < 0)   # I64_LT_S
I32_EQZ ; BR_IF L_end
<body>
i = i + s
BR L_top
L_end:
```

`for x in <iterable>` (list/dict/generator, M3/M6) desugars to the iterator
protocol: obtain an iterator, then loop `RESUME`/`CORO_DONE`/`CORO_VALUE`
(see [generators](#generators-m6)). Over a `list[T]`, the simple form uses
`ARRAY_LEN` + an index loop with `ARRAY_GET`. Over a `dict`, `MAP_KEYS`→array then
index loop (or `MAP_ITER`).

## Functions & calls (M2)

A `def` builds a `*Function` via minivm's `FunctionBuilder` with the param/local
slot layout from [`04-static-semantics.md`](04-static-semantics.md), then is stored
as a constant. minivm's `CALL` convention: push args, push the funcref last, then
`CALL`.

```text
# r = f(x, y)
<x>
<y>
CONST_GET <f>
CALL
LOCAL_SET <r>
```

- `RETURN` returns the top of stack (or pushes `REF_NULL` for a `-> None` fall-off).
- **Tail calls:** a `return f(...)` in tail position emits `RETURN_CALL` (args + fn,
  reuses the frame) so self/mutual recursion runs in constant frame depth.
- Default arguments are materialized at the call site by the compiler (it knows
  which args are omitted) — no runtime default machinery.

## Strings & containers (M3)

| operation | emit |
|---|---|
| `list[T]` display `[a,b,c]` | push elems, `ARRAY_NEW <elemtype>` count, or `ARRAY_NEW_DEFAULT`+`ARRAY_SET` |
| `lst[i]` read / write | `ARRAY_GET` / `ARRAY_SET` (traps `ErrIndexOutOfRange`) |
| `len(lst)` | `ARRAY_LEN` |
| `dict` display `{k:v}` | push k/v pairs, `MAP_NEW <type>` count |
| `d[k]` read | `MAP_GET` (missing → zero value) or `MAP_LOOKUP` for membership |
| `d[k] = v` | `MAP_SET` |
| `k in d` | `MAP_LOOKUP` → use the `ok` flag |
| `len(d)` | `MAP_LEN` |
| `tuple` `(a,b)` | `STRUCT_NEW <type>` over fields; `t[const]` → `STRUCT_GET index` |
| `s1 + s2` | `STRING_CONCAT` ; `len(s)` → `STRING_LEN` |

Index/key types are statically known, so the compiler selects the right specialized
map/array type up front.

## Closures (M4)

For a nested function capturing `N` names: emit the captured upvalues, then the
function template, then `CLOSURE_NEW` (pops template + `N` upvals → closure).
Inside the closure, captured names use `UPVAL_GET/SET i`. A captured variable that
the inner function **writes** is boxed into a heap cell (`REF_NEW`) so both scopes
share mutations; read-only captures pass by value.

```text
# make_adder(n) returns lambda x: x + n
<n as upvalue>          # boxed cell if mutated, else value
CONST_GET <inner fn>
CLOSURE_NEW
```

## Classes (M5)

- A class type is a `StructType` (fields = annotated members, by index).
- Instantiation `C(args)`: `STRUCT_NEW_DEFAULT` (or build fields) then call
  `__init__` with the new struct as `self`.
- `obj.field` → `STRUCT_GET <index>`; `obj.field = v` → `STRUCT_SET <index>`.
- `obj.method(args)` → push `obj` + args, `CONST_GET <method fn>`, `CALL`.

## Generators (M6)

A `def` containing `yield` is a coroutine-function in minivm: `CALL` returns a
`Coroutine` handle. `yield e` emits `<e>; YIELD`. Iterating:

```text
<call gen()> → coro handle
L_top:
coro ; CORO_DONE ; BR_IF L_end
coro ; CORO_VALUE → current item (release handle path per minivm)
<loop body uses item>
coro ; <in> ; RESUME
BR L_top
L_end:
```

(Exact handle lifetime follows minivm coroutine semantics; `RESUME` delivers the
send-value, `CORO_DONE`/`CORO_VALUE` test/extract.)

## Exceptions & with (M7)

minipy uses minivm's built-in exception machinery: per-function handler tables
declared with `program.Builder.Try(start, end, catch, depth)`, `THROW` for guest
raises, and `ERROR_NEW` for error payload construction. Runtime traps and
host-function Go errors are catchable through the same handler path.

- `raise E(...)` constructs an exception value, wraps message payloads with
  `ERROR_NEW` when needed, then emits `THROW`.
- `try/except/finally` lowers to minivm handler-table entries around protected
  regions. Catch blocks receive the thrown value on the operand stack at the
  handler target. `finally` blocks are emitted on every exit edge (normal,
  exception, return) and rethrow with `THROW` when required.
- `with x as y:` desugars to `y = x.__enter__(); try: <body> finally: x.__exit__()`.

The compiler may still introduce a thin CFG/IR here, but only to compute protected
regions, stack depths, and `finally` edges; it must not replace minivm's native
handler-table and `THROW` path with a parallel sentinel-return unwinder.

## Statement completeness & pattern matching (M9)

### `del`

`del NAME` resolves the name to its storage class and emits the planned minivm
opcode `LOCAL_DELETE` or `GLOBAL_DELETE`. A later read follows normal
definite-assignment rules and becomes `UseBeforeDefinition` statically when
provable, or a runtime name error when a dynamic path deletes a still-declared
slot. `del obj.attr` and `del obj[key]` use the relevant class/container support
available by M9; maps use `MAP_DELETE`.

### `assert`

```text
<test>
BR_IF L_ok
<message or default "AssertionError">
ERROR_NEW
THROW
L_ok:
```

`assert test, msg` evaluates `msg` only on failure. The false path uses minivm's
exception path (`ERROR_NEW`; `THROW`) so it benefits from the same interpreter/JIT
handling as M7 exceptions.

### `match` / `case`

Pattern matching lowers to a decision tree:

- literal/value patterns use existing equality/comparison opcodes;
- sequence/mapping/class patterns emit shape tests followed by element/field tests;
- alternatives (`p1 | p2`) branch to shared success/failure labels;
- captures bind with `LOCAL_SET`/`GLOBAL_SET` in the selected case arm only;
- guards run after a pattern succeeds and must leave an i32 bool for `BR_IF`.

Dense scalar literal cases may use `BR_TABLE`; sparse or structured patterns use
ordered `BR_IF` chains matching Python case order.

## Unions, `Any` & specialization (M10)

The M10 layer lowers only the **residual** dynamic slots; anything the inference
pass resolved to a concrete type emits exactly as the static core does. This is
where "minimize types" pays off — runtime tag-checks appear only on slots that
stayed a union or `Any`.

- **Representation.** A concrete slot stays unboxed/native. An `A | B` / `Any`
  slot is a minivm `ref` holding a value boxed with its runtime tag (self-describing
  `Boxed` values; see
  [value-representation](https://github.com/siyul-park/minivm/blob/main/docs/value-representation.md#dynamic-any-values)).
- **Widen `T → A|B` / `T → Any`.** Box the value (if not already a `ref`); the tag
  is carried for free. No check emitted.
- **Narrow `A|B → T` / `Any → T`.** Emit `REF_CAST <T>` — a checked cast that traps
  `TypeError` at runtime — **unless** flow analysis already proved the type, in
  which case nothing is emitted. A slot inference resolved to concrete never reaches
  this path.
- **`isinstance(x, T)`** → `REF_TEST <T>` → i32. In the true branch the checker
  narrows `x` to `T`, so later uses need no further `REF_CAST`.
- **Union dispatch.** An operation on an un-narrowed union lowers to a tag switch: a
  `REF_TEST` chain (or jump table) selecting the per-member lowering, each arm
  operating on the unboxed member. `Optional[T]` is the two-arm case
  (`REF_IS_NULL` → null arm / `T` arm).
- **Specialization (monomorphization).** A polymorphic function is emitted once per
  concrete instantiation as a separate `*Function` constant (`f$int`, `f$str`, …).
  Each monomorphic call site links directly to its specialization with a normal
  `CALL`/`RETURN_CALL` — no dispatch. Only a call whose argument is itself a union
  goes through a tag switch that picks the matching specialization, or invokes a
  single union-typed body when instantiations were capped.

```text
# describe(x: int | str): isinstance dispatch
LOCAL_GET <x>
REF_TEST <int>                   # i32
I32_EQZ ; BR_IF L_str
LOCAL_GET <x> ; REF_CAST <int>   # narrowed (elided if flow already proved int)
<... int arm ...>
BR L_end
L_str:
LOCAL_GET <x> ; REF_CAST <str>
<... str arm ...>
L_end:
```

## Worked example (M0)

minipy:

```python
x: int = 6
y: int = 7
print(x * y)
```

emitted minivm (entry function; `print` bound as host fn constant 0):

```text
I64_CONST 6
GLOBAL_SET 0          # x
I64_CONST 7
GLOBAL_SET 1          # y
GLOBAL_GET 0
GLOBAL_GET 1
I64_MUL
CONST_GET 0           # print host function
CALL
```

After `optimize.O1`, `6 * 7` over constants folds to `I64_CONST 42` when the
globals are not observed elsewhere. See [`../roadmap.md`](../roadmap.md) for the
hand-checkable example per milestone.
