# Python PEG Grammar (reference)

> Upstream: <https://docs.python.org/3.13/reference/grammar.html> · captured 2026-06-23.
> This is **full Python** (CPython 3.13 PEG, identical to 3.14 for the subset
> minipy uses). minipy's actual grammar is the reduced subset in
> [`../spec/03-grammar.md`](../spec/03-grammar.md).

## Notation

- `~` ("cut"): commit to the current alternative; fail the rule if it fails
- `e1 e2`: match `e1`, then `e2`
- `e1 | e2`: ordered choice — match `e1` or `e2`
- `[e]` or `e?`: optional
- `e*`: zero or more · `e+`: one or more
- `s.e+`: one or more `e` separated by `s`
- `&e`: positive lookahead (no consume) · `!e`: negative lookahead
- `'kw'`: keyword · `"soft"`: soft keyword · UPPER: token (`NAME`, `NUMBER`, `NEWLINE`)

## Starting rules

```text
file:       [statements] ENDMARKER
interactive: statement_newline
eval:       expressions NEWLINE* ENDMARKER
func_type:  '(' [type_expressions] ')' '->' expression NEWLINE* ENDMARKER
```

## Statements

```text
statements: statement+
statement: compound_stmt | simple_stmts

simple_stmt:
    | assignment
    | type_alias
    | star_expressions
    | return_stmt
    | import_stmt
    | raise_stmt
    | pass_stmt
    | del_stmt
    | yield_stmt
    | assert_stmt
    | break_stmt
    | continue_stmt
    | global_stmt
    | nonlocal_stmt

compound_stmt:
    | function_def
    | if_stmt
    | class_def
    | with_stmt
    | for_stmt
    | try_stmt
    | while_stmt
    | match_stmt
```

### Assignment

```text
assignment:
    | NAME ':' expression ['=' annotated_rhs]
    | ('(' single_target ')' | single_subscript_attribute_target) ':' expression ['=' annotated_rhs]
    | (star_targets '=' )+ annotated_rhs !'=' [TYPE_COMMENT]
    | single_target augassign ~ annotated_rhs

augassign:
    | '+=' | '-=' | '*=' | '@=' | '/=' | '%=' | '&=' | '|=' | '^='
    | '<<=' | '>>=' | '**=' | '//='
```

### Control flow

```text
return_stmt:   'return' [star_expressions]
raise_stmt:    'raise' expression ['from' expression] | 'raise'
pass_stmt:     'pass'
break_stmt:    'break'
continue_stmt: 'continue'
del_stmt:      'del' del_targets &(';' | NEWLINE)
assert_stmt:   'assert' expression [',' expression]
global_stmt:   'global' ','.NAME+
nonlocal_stmt: 'nonlocal' ','.NAME+
```

### if / while / for

```text
if_stmt:
    | 'if' named_expression ':' block elif_stmt
    | 'if' named_expression ':' block [else_block]
elif_stmt:
    | 'elif' named_expression ':' block elif_stmt
    | 'elif' named_expression ':' block [else_block]
else_block: 'else' ':' block

while_stmt: 'while' named_expression ':' block [else_block]

for_stmt:
    | 'for' star_targets 'in' ~ star_expressions ':' [TYPE_COMMENT] block [else_block]
    | 'async' 'for' star_targets 'in' ~ star_expressions ':' [TYPE_COMMENT] block [else_block]
```

### Functions and classes

```text
function_def: decorators function_def_raw | function_def_raw
function_def_raw:
    | 'def' NAME [type_params] '(' [params] ')' ['->' expression] ':' [func_type_comment] block
    | 'async' 'def' NAME [type_params] '(' [params] ')' ['->' expression] ':' [func_type_comment] block

class_def: decorators class_def_raw | class_def_raw
class_def_raw: 'class' NAME [type_params] ['(' [arguments] ')'] ':' block

decorators: ('@' named_expression NEWLINE )+

params: parameters
parameters:
    | slash_no_default param_no_default* param_with_default* [star_etc]
    | slash_with_default param_with_default* [star_etc]
    | param_no_default+ param_with_default* [star_etc]
    | param_with_default+ [star_etc]
    | star_etc
param: NAME annotation?
annotation: ':' expression
default: '=' expression
```

### import / with / try

```text
import_stmt: import_name | import_from
import_name: 'import' dotted_as_names
import_from:
    | 'from' ('.' | '...')* dotted_name 'import' import_from_targets
    | 'from' ('.' | '...')+ 'import' import_from_targets
import_from_targets:
    | '(' import_from_as_names [','] ')'
    | import_from_as_names !','
    | '*'
dotted_as_name: dotted_name ['as' NAME]
dotted_name: dotted_name '.' NAME | NAME

with_stmt:
    | 'with' '(' ','.with_item+ ','? ')' ':' [TYPE_COMMENT] block
    | 'with' ','.with_item+ ':' [TYPE_COMMENT] block
    | 'async' 'with' '(' ','.with_item+ ','? ')' ':' block
    | 'async' 'with' ','.with_item+ ':' [TYPE_COMMENT] block
with_item: expression 'as' star_target &(',' | ')' | ':') | expression

try_stmt:
    | 'try' ':' block finally_block
    | 'try' ':' block except_block+ [else_block] [finally_block]
    | 'try' ':' block except_star_block+ [else_block] [finally_block]
except_block:
    | 'except' expression ':' block
    | 'except' expression 'as' NAME ':' block
    | 'except' ':' block
finally_block: 'finally' ':' block
```

## Expressions (lowest → highest precedence)

```text
expression:
    | disjunction 'if' disjunction 'else' expression
    | disjunction
    | lambdef

yield_expr: 'yield' 'from' expression | 'yield' [star_expressions]

disjunction:  conjunction ('or' conjunction )+ | conjunction
conjunction:  inversion ('and' inversion )+ | inversion
inversion:    'not' inversion | comparison
comparison:   bitwise_or compare_op_bitwise_or_pair+ | bitwise_or
compare_op_bitwise_or_pair:
    | '==' bitwise_or | '!=' bitwise_or | '<=' bitwise_or | '<' bitwise_or
    | '>=' bitwise_or | '>' bitwise_or | 'not' 'in' bitwise_or | 'in' bitwise_or
    | 'is' 'not' bitwise_or | 'is' bitwise_or
bitwise_or:   bitwise_or '|' bitwise_xor | bitwise_xor
bitwise_xor:  bitwise_xor '^' bitwise_and | bitwise_and
bitwise_and:  bitwise_and '&' shift_expr | shift_expr
shift_expr:   shift_expr '<<' sum | shift_expr '>>' sum | sum
sum:          sum '+' term | sum '-' term | term
term:         term '*' factor | term '/' factor | term '//' factor
            | term '%' factor | term '@' factor | factor
factor:       '+' factor | '-' factor | '~' factor | power
power:        await_primary '**' factor | await_primary
await_primary: 'await' primary | primary
primary:
    | primary '.' NAME
    | primary genexp
    | primary '(' [arguments] ')'
    | primary '[' slices ']'
    | atom
atom:
    | NAME
    | 'True' | 'False' | 'None'
    | strings
    | NUMBER
    | (tuple | group | genexp)
    | (list | listcomp)
    | (dict | set | dictcomp | setcomp)
    | '...'
slices: slice !',' | ','.(slice | starred_expression)+ [',']
slice: [expression] ':' [expression] [':' [expression]] | named_expression
```

## Literals & comprehensions

```text
list:  '[' [star_named_expressions] ']'
tuple: '(' [star_named_expression ',' [star_named_expressions]] ')'
set:   '{' star_named_expressions '}'
dict:  '{' [double_starred_kvpairs] '}'
kvpair: expression ':' expression

listcomp: '[' named_expression for_if_clauses ']'
setcomp:  '{' named_expression for_if_clauses '}'
dictcomp: '{' kvpair for_if_clauses '}'
genexp:   '(' (assignment_expression | expression !':=') for_if_clauses ')'
for_if_clauses: for_if_clause+
for_if_clause:
    | 'async' 'for' star_targets 'in' ~ disjunction ('if' disjunction )*
    | 'for' star_targets 'in' ~ disjunction ('if' disjunction )*

lambdef: 'lambda' [lambda_params] ':' expression
```

## Call arguments

```text
arguments: args [','] &')'
args:
    | ','.(starred_expression | (assignment_expression | expression !':=') !'=')+ [',' kwargs]
    | kwargs
kwargs:
    | ','.kwarg_or_starred+ ',' ','.kwarg_or_double_starred+
    | ','.kwarg_or_starred+
    | ','.kwarg_or_double_starred+
starred_expression: '*' expression
kwarg_or_starred: NAME '=' expression | starred_expression
kwarg_or_double_starred: NAME '=' expression | '**' expression
```

## Assignment targets

```text
star_targets: star_target !',' | star_target (',' star_target )* [',']
star_target: '*' (!'*' star_target) | target_with_star_atom
target_with_star_atom:
    | t_primary '.' NAME !t_lookahead
    | t_primary '[' slices ']' !t_lookahead
    | star_atom
star_atom:
    | NAME
    | '(' target_with_star_atom ')'
    | '(' [star_targets_tuple_seq] ')'
    | '[' [star_targets_list_seq] ']'
```
