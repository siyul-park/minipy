# Grammar

Accepted parser grammar, parse-only forms, and syntax notes for minipy source.

## When to Read

Read this when changing parser behavior, adding syntax, changing precedence, or
checking whether a source form is parsed before the checker rejects it.

For token spelling, read `01-lexical.md`. For checker restrictions on parsed
forms, read `04-static-semantics.md`.

## Source of Truth

| Concern | Source |
|---|---|
| parser implementation | `parser/*.go` |
| AST node shapes | `ast/ast.go` |
| token spelling | `token/token.go`, `docs/spec/01-lexical.md` |
| checker restrictions | `compiler/check*.go`, `docs/spec/04-static-semantics.md` |

## Notation

This file documents the grammar accepted by the parser and the forms that the
checker/lowerer currently support. Some constructs are intentionally parsed so the
compiler can issue precise unsupported-feature diagnostics later.

Notation is EBNF-like: `[]` optional, `{}` repeat, `|` alternatives, and quoted
strings are tokens or soft keywords.

## Module and Blocks

```text
module          ::= { NEWLINE | statement } EOF
statement       ::= compound_stmt | simple_stmt_list NEWLINE
simple_stmt_list::= simple_stmt { ';' simple_stmt } [';']
block           ::= ':' simple_stmt_list NEWLINE
                  | ':' NEWLINE INDENT { statement } DEDENT
```

Compound statements may use either an indented suite or an inline simple-statement
suite, except class bodies which must be indented blocks.

## Simple Statements

```text
simple_stmt     ::= pass_stmt
                  | break_stmt
                  | continue_stmt
                  | return_stmt
                  | yield_stmt
                  | raise_stmt
                  | global_stmt
                  | nonlocal_stmt
                  | del_stmt
                  | assert_stmt
                  | import_stmt
                  | import_from_stmt
                  | type_alias
                  | ann_assign
                  | assignment
                  | aug_assignment
                  | expr_stmt

pass_stmt       ::= 'pass'
break_stmt      ::= 'break'
continue_stmt   ::= 'continue'
return_stmt     ::= 'return' [expression]
yield_stmt      ::= 'yield' [expression]
raise_stmt      ::= 'raise' [expression ['from' expression]]
global_stmt     ::= 'global' NAME {',' NAME}
nonlocal_stmt   ::= 'nonlocal' NAME {',' NAME}
del_stmt        ::= 'del' del_target {',' del_target} [',']
assert_stmt     ::= 'assert' expression [',' expression]
type_alias      ::= 'type' NAME '=' expression
```

`type` is a soft keyword: it introduces a type alias only in `type Name = expr`
shape. Otherwise it remains a normal name.

Assignment targets are checked after parsing:

```text
ann_assign      ::= NAME ':' type_expr ['=' expression]
assignment      ::= target '=' expression
tuple_assignment::= tuple_target '=' expression
aug_assignment  ::= target augop expression

target          ::= NAME | primary '[' subscript ']' | primary '.' NAME | tuple_target
del_target      ::= NAME | primary '[' subscript ']' | primary '.' NAME
tuple_target    ::= NAME {',' (NAME | '*' NAME)} [',']
augop           ::= '+=' | '-=' | '*=' | '/=' | '//=' | '%=' | '&=' | '|=' | '^=' | '<<=' | '>>=' | '**='
```

Tuple/starred unpacking targets are supported for assignment and `for` targets.
List slice assignment and deletion are supported for contiguous slices with an
omitted step or a literal step of `1`. Augmented assignment is supported for
names and attributes; other augmented targets are rejected.

## Imports

```text
import_stmt     ::= 'import' import_alias {',' import_alias}
import_from_stmt::= 'from' {'.'} dotted_name? 'import' ('*' | import_names)
import_names    ::= import_alias {',' import_alias} | '(' import_alias {',' import_alias} [','] ')'
import_alias    ::= dotted_name ['as' NAME]
dotted_name     ::= NAME {'.' NAME}
```

Imports are supported only at module top level. `from __future__ import ...`
parses as a normal import-from statement and is validated by the checker. String
literals are accepted in annotation positions so postponed annotations can be
checked after future flags are known.

## Compound Statements

```text
compound_stmt   ::= if_stmt | while_stmt | for_stmt | try_stmt | with_stmt
                  | func_def | class_def | decorated | async_stmt | match_stmt

if_stmt         ::= 'if' expression block {'elif' expression block} ['else' block]
while_stmt      ::= 'while' expression block ['else' block]
for_stmt        ::= 'for' for_target 'in' expression block ['else' block]
for_target      ::= NAME | NAME {',' NAME} [',']
try_stmt        ::= 'try' block {except_clause} ['else' block] ['finally' block]
except_clause   ::= 'except' ['*'] [expression ['as' NAME]] block
with_stmt       ::= 'with' with_item {',' with_item} block
with_item       ::= expression ['as' NAME]
async_stmt      ::= 'async' ('def' ... | 'for' ... | 'with' ...)
```

`async def`, `async for`, `async with`, async comprehensions, and `await` are
parse-only until scheduler/runtime support exists. `except*` is parsed via the
star flag but is not lowered as ExceptionGroup semantics.

## Functions and Parameters

```text
func_def        ::= 'def' NAME '(' [params] ')' ['->' type_expr] block
params          ::= param {',' param} [',']
param           ::= NAME [':' type_expr] ['=' expression]
                  | '/' | '*'
                  | '*' NAME [':' type_expr] ['=' expression]
                  | '**' NAME [':' type_expr] ['=' expression]
```

The parser accepts positional-only separators (`/`), keyword-only separators
(`*`), `*args`, `**kwargs`, defaults, and optional annotations. The checker
validates duplicate parameters, defaults, vararg placement, call arity, and
assignability.

Function return annotations are optional; omitted returns are inferred. Generator
functions must return `Iterator[T]` and may contain `yield` statements.

## Classes and Decorators

```text
class_def       ::= 'class' NAME ['(' class_arg {',' class_arg} [','] ')'] class_block
class_arg       ::= expression | NAME '=' expression | '**' expression
class_block     ::= ':' NEWLINE INDENT { class_member | NEWLINE } DEDENT
class_member    ::= ann_assign NEWLINE | func_def | 'pass' NEWLINE
decorated       ::= decorator+ (func_def | class_def | async_stmt)
decorator       ::= '@' expression NEWLINE
```

The parser records all class bases, class keywords, and decorator expressions
without validating their semantics; validation is entirely the checker's
responsibility (see
[04-static-semantics.md](04-static-semantics.md#decorators)). The checker
supports one base class, `@dataclass`/`@dataclass()`, and method/field bodies.
Function decorators support a bare name, a module-qualified attribute, and a
call of either, provided the evaluated decorator is exactly `Callable[[F], F]`
for the decorated function's signature `F`. Class keywords, multiple bases,
other class decorators, and other decorator expression shapes produce
diagnostics.

## Match Statements and Patterns

```text
match_stmt      ::= 'match' match_subject ':' NEWLINE INDENT case_clause+ DEDENT
match_subject   ::= expression {',' expression} [',']
case_clause     ::= 'case' pattern ['if' expression] block
pattern         ::= or_pattern ['as' NAME]
or_pattern      ::= closed_pattern {'|' closed_pattern}
closed_pattern  ::= '_' | NAME | literal | dotted_name | class_pattern
                  | sequence_pattern | mapping_pattern | signed_number_pattern
sequence_pattern::= '[' [pattern_elems] ']' | '(' [pattern_elems] ')'
pattern_elems   ::= seq_elem {',' seq_elem} [',']
seq_elem        ::= pattern | '*' NAME | '*_'
mapping_pattern ::= '{' [mapping_item {',' mapping_item} [',' ['**' NAME]]] '}'
mapping_item    ::= expression ':' pattern
class_pattern   ::= dotted_name '(' [pattern {',' pattern} [',' keyword_patterns]] ')'
keyword_patterns::= keyword_pattern {',' keyword_pattern}
keyword_pattern ::= NAME '=' pattern
```

`match` and `case` are soft keywords. Capture patterns declare or update bindings
in the current scope. The checker validates pattern compatibility against the
subject type.

## Expressions

```text
expression      ::= lambda_expr | yield_expr | await_expr | conditional_or_named
lambda_expr     ::= 'lambda' [lambda_params] ':' expression
yield_expr      ::= 'yield' ['from'] [expression]
await_expr      ::= 'await' primary
conditional_or_named
                ::= disjunction ['if' disjunction 'else' expression]
                  | disjunction [':=' expression]
disjunction     ::= conjunction {'or' conjunction}
conjunction     ::= inversion {'and' inversion}
inversion       ::= 'not' inversion | comparison
comparison      ::= bitwise_or {comp_op bitwise_or}
comp_op         ::= '<' | '<=' | '>' | '>=' | '==' | '!=' | 'in' | 'not' 'in' | 'is' | 'is' 'not'
bitwise_or      ::= bitwise_xor {'|' bitwise_xor}
bitwise_xor     ::= bitwise_and {'^' bitwise_and}
bitwise_and     ::= shift {'&' shift}
shift           ::= sum {('<<' | '>>') sum}
sum             ::= term {('+' | '-') term}
term            ::= factor {('*' | '/' | '//' | '%' | '@') factor}
factor          ::= ('+' | '-' | '~') factor | power
power           ::= primary ['**' factor]
primary         ::= atom {call | attribute | subscript_expr}
```

`:=` is accepted only when the left side is a name. `yield` statements and
`yield`/`yield from` expressions are supported inside generator functions
returning `Iterator[T]`; `await` is parse-only.

## Calls and Primaries

```text
call            ::= '(' [argument {',' argument} [',']] ')'
argument        ::= expression | NAME '=' expression | '*' expression | '**' expression
attribute       ::= '.' NAME
subscript_expr  ::= '[' subscript ']'
subscript       ::= expression | [expression] ':' [expression] [':' [expression]]

atom            ::= NAME | literal | list_display | dict_or_set_display
                  | '(' [expression_or_tuple_or_generator] ')'

literal         ::= INT | FLOAT | STRING | FSTRING | 'True' | 'False' | 'None' | '...'
```

`...` is represented by `EllipsisLit`. Because subscript contents use the
ordinary expression grammar, `a[...]` preserves that node for the checker to
reject with the targeted unsupported-feature diagnostic.

Known minipy function calls support positional arguments, keyword arguments,
defaults, `*tuple` expansion, `*args`, and `**kwargs` parameters. Dynamic `**expr`
call unpacking is parsed but rejected. Keyword/starred calls to native functions,
builtin methods, and dynamic callable values remain limited.

## Displays and Comprehensions

```text
list_display    ::= '[' [star_expr {',' star_expr} [',']] ']'
                  | '[' star_expr comp_clauses ']'
dict_or_set_display
                ::= '{' '}'
                  | '{' dict_entries '}'
                  | '{' set_entries '}'
                  | '{' expression ':' expression comp_clauses '}'
                  | '{' star_expr comp_clauses '}'

dict_entries    ::= dict_entry {',' dict_entry} [',']
dict_entry      ::= expression ':' expression | '**' expression
set_entries     ::= star_expr {',' star_expr} [',']
star_expr       ::= expression | '*' expression
comp_clauses    ::= ('for' | 'async' 'for') NAME 'in' disjunction {'if' disjunction} {comp_clauses}
```

List, dict, set, and generator comprehensions are supported with name targets.
Async comprehensions are parse-only. Starred list/set elements and dict unpacking
are type-checked with the restrictions in `04-static-semantics.md`.

## Type Expressions

```text
type_expr       ::= type_atom {'|' type_atom}
type_atom       ::= NAME | 'None' | dotted_name | NAME '[' type_args ']'
type_args       ::= type_expr {',' type_expr}
                  | '[' [type_expr {',' type_expr}] ']' ',' type_expr   # Callable
```

Supported generics are `list[T]`, `dict[K, V]`, `set[T]`, `tuple[...]`,
`Iterator[T]`, `Callable[[...], R]`, and class/module-qualified class names.
Unknown generic names are rejected by the checker.

## Related Docs

- `docs/README.md` — documentation map and ownership guide.
- `docs/spec/01-lexical.md` — tokens used by this grammar.
- `docs/spec/02-types.md` — annotation and source type semantics.
- `docs/spec/04-static-semantics.md` — checker restrictions on parsed forms.
- `docs/compatibility.md` — user-facing syntax support matrix.
