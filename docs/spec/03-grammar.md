# minipy — Grammar (the subset)

This is minipy's grammar: a reduction of the full Python 3.13 PEG
([reference](../reference/python-grammar.md)). Every rule here is a restriction of
an upstream rule — minipy adds **no** new syntax.

Each construct is tagged with the milestone that introduces it
(see [`../roadmap.md`](../roadmap.md)): `[M0]`…`[M10]`. Forms not yet assigned to
the active milestone are rejected with `UnsupportedFeature` until their planned
milestone lands; this document treats rejected forms as implementation targets
unless a separate non-goal says otherwise.

## Notation

Same PEG notation as the reference. `…` marks where the upstream rule has more
alternatives that minipy drops.

## Module & blocks

```text
file:     statement* ENDMARKER
block:    simple_stmts | NEWLINE INDENT statement+ DEDENT
statement: compound_stmt | simple_stmts
simple_stmts: ';'.simple_stmt+ [';'] NEWLINE
```

## Simple statements

```text
simple_stmt:                                    # [M0] unless noted
    | assignment
    | expr_stmt
    | return_stmt        # [M2]
    | pass_stmt          # [M1]
    | break_stmt         # [M1]
    | continue_stmt      # [M1]
    | global_stmt        # [M4]
    | nonlocal_stmt      # [M4]
    | raise_stmt         # [M7]
    | import_stmt        # [M8]
    | del_stmt           # [M9]
    | assert_stmt        # [M9]

assignment:                                     # [M0]
    | NAME ':' type ['=' expression]            # annotated (declaration)
    | target '=' expression                     # plain assign (target must be pre-declared/inferable)
    | target augassign expression               # [M0] +=, -=, *=, //=, %=, &=, |=, ^=, <<=, >>=, **=
    | (target '.' NAME | target '[' expression ']') '=' expression   # attr/index store [M3/M5]

augassign: '+=' | '-=' | '*=' | '/=' | '//=' | '%=' | '&=' | '|=' | '^=' | '<<=' | '>>=' | '**='
target:    NAME | NAME '[' expression ']' | NAME '.' NAME
expr_stmt: expression                            # value discarded (e.g. a call)

return_stmt:   'return' [expression]
pass_stmt:     'pass'
break_stmt:    'break'
continue_stmt: 'continue'
global_stmt:   'global' ','.NAME+
nonlocal_stmt: 'nonlocal' ','.NAME+
raise_stmt:    'raise' [expression]              # [M7] (no 'from')
del_stmt:      'del' del_targets                 # [M9]
assert_stmt:   'assert' expression [',' expression] # [M9]
del_targets:   del_target (',' del_target)* [',']
del_target:    NAME | primary '.' NAME | primary '[' expression ']'
```

**Dropped from upstream `assignment`:** chained `a = b = c`, tuple/list
destructuring targets in v1 (`a, b = …` deferred to M3), `type_alias`,
`TYPE_COMMENT`, walrus.

## Compound statements

```text
compound_stmt:
    | if_stmt        # [M1]
    | while_stmt     # [M1]
    | for_stmt       # [M1]
    | function_def   # [M2]
    | class_def      # [M5]
    | try_stmt       # [M7]
    | with_stmt      # [M7]
    | match_stmt     # [M9]

if_stmt:    'if' expression ':' block ('elif' expression ':' block)* ['else' ':' block]
while_stmt: 'while' expression ':' block ['else' ':' block]
for_stmt:   'for' NAME 'in' expression ':' block ['else' ':' block]     # single target

function_def:
    | ['@' NAME NEWLINE]* 'def' NAME '(' [params] ')' '->' type ':' block
params: param (',' param)* [',']
param:  NAME ':' type ['=' expression]           # annotation REQUIRED; default optional
                                                 # *args/**kwargs/'/'/'*' separators deferred

class_def:
    | 'class' NAME ['(' NAME ')'] ':' class_block          # single optional base [M5]
class_block: NEWLINE INDENT class_member+ DEDENT
class_member: NAME ':' type [ '=' expression ] NEWLINE     # field
            | function_def                                  # method

try_stmt:
    | 'try' ':' block except_block+ ['else' ':' block] ['finally' ':' block]
    | 'try' ':' block 'finally' ':' block
except_block: 'except' [type ['as' NAME]] ':' block

with_stmt: 'with' with_item (',' with_item)* ':' block
with_item: expression ['as' NAME]

match_stmt: 'match' expression ':' NEWLINE INDENT case_block+ DEDENT
case_block: 'case' patterns [guard] ':' block
guard:      'if' expression
```

**Note:** `for_stmt` takes a **single** `NAME` target in v1; tuple-unpacking
targets (`for k, v in d.items()`) are an M3 extension. Decorators are restricted
to a bare `NAME` (e.g. `@staticmethod`, `@dataclass`) — call-form and dotted
decorators deferred. The function `'->' type` is **required** (no inference of
return type at the boundary).

## Expressions

The full Python precedence chain is kept, **minus** `lambdef` (→M4), walrus,
`await`, `@` (matmul), and `yield` (→M6 in expression position).

```text
expression:
    | disjunction 'if' disjunction 'else' expression     # [M1] conditional expr
    | disjunction
    | lambdef                                            # [M4]

disjunction: conjunction ('or' conjunction)*             # [M0]
conjunction: inversion ('and' inversion)*                # [M0]
inversion:   'not' inversion | comparison                # [M0]
comparison:  bitwise_or (compare_op bitwise_or)*         # [M0]
compare_op:  '==' | '!=' | '<' | '<=' | '>' | '>='
           | 'in' | 'not' 'in'                           # [M3] (containers)
           | 'is' | 'is' 'not'                            # [M7] (None identity)

bitwise_or:  bitwise_xor ('|' bitwise_xor)*              # [M0]
bitwise_xor: bitwise_and ('^' bitwise_and)*              # [M0]
bitwise_and: shift_expr ('&' shift_expr)*                # [M0]
shift_expr:  sum (('<<' | '>>') sum)*                    # [M0]
sum:         term (('+' | '-') term)*                    # [M0]
term:        factor (('*' | '/' | '//' | '%') factor)*   # [M0]  (no '@')
factor:      ('+' | '-' | '~') factor | power            # [M0]
power:       primary ['**' factor]                       # [M0]

primary:
    | primary '.' NAME                  # attribute      [M5]
    | primary '(' [arguments] ')'       # call           [M2]
    | primary '[' expression ']'        # index/subscript [M3]
    | atom

atom:
    | NAME
    | 'True' | 'False' | 'None'
    | NUMBER
    | strings                            # incl. f-strings [M3]
    | '(' expression ')'                 # grouping
    | '(' [expression (',' expression)+ [',']] ')'   # tuple        [M3]
    | '[' [expression (',' expression)* [',']] ']'   # list display [M3]
    | '{' [kvpair (',' kvpair)* [',']] '}'           # dict display [M3]
    | list_comp | dict_comp | set_comp               # comprehensions [M4]

kvpair:    expression ':' expression
arguments: expression (',' expression)* [',']        # positional only in v1; kwargs [M2.1]
```

**Comprehensions [M4]:**

```text
list_comp: '[' expression for_if_clause+ ']'
dict_comp: '{' kvpair for_if_clause+ '}'
set_comp:  '{' expression for_if_clause+ '}'
for_if_clause: 'for' NAME 'in' disjunction ('if' disjunction)*
lambdef:   'lambda' [NAME (',' NAME)*] ':' expression    # untyped params inferred from call site
```

## Pattern matching [M9]

`match`/`case` uses Python's soft keywords and remains an upstream-compatible
subset of structural pattern matching. The first matching `case` runs; no fallthrough.

```text
patterns:       pattern ('|' pattern)* ['as' NAME]
pattern:
    | '_'                                  # wildcard
    | NAME                                 # capture
    | literal_pattern
    | value_pattern
    | sequence_pattern
    | mapping_pattern
    | class_pattern
    | '(' pattern ')'

literal_pattern: 'None' | 'True' | 'False' | NUMBER | STRING
value_pattern:   NAME ('.' NAME)+
sequence_pattern:'[' [pattern (',' pattern)* [',']] ']'
               | '(' [pattern (',' pattern)* [',']] ')'
mapping_pattern: '{' [kv_pattern (',' kv_pattern)* [',']] '}'
kv_pattern:      (literal_pattern | value_pattern) ':' pattern
class_pattern:   NAME '(' [pattern (',' pattern)* [',']] ')'
```

Capture names bind only inside the selected `case` block. Alternatives in a
single `|` pattern must bind the same names with compatible types. Guards must
type-check as `bool`.

## Deferred forms (rejected until milestone)

`async`/`await`, `yield from`, `*`/`**` unpacking in calls and displays outside
M9 pattern rest positions, starred assignment targets, slicing with step
(`a[i:j:k]`), generator expressions as bare args, `Union[A,B]` (non-None, M10),
keyword-only / positional-only markers, multiple inheritance, decorators with
arguments, nested-tuple `for` targets (beyond M3 flat unpack), set/frozenset
literals before M4, `complex`/`bytes` literals (per [`01-lexical.md`](01-lexical.md)).

Each rejected form reports `UnsupportedFeature` with the construct name and a
pointer to the planned milestone. A form with no milestone yet remains queued for
roadmap triage rather than permanently excluded.
