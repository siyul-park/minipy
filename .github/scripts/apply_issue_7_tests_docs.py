from pathlib import Path


def write(path: str, content: str) -> None:
    file = Path(path)
    if file.exists():
        if file.read_text() != content:
            raise RuntimeError(f"refusing to overwrite {path}")
        return
    file.parent.mkdir(parents=True, exist_ok=True)
    file.write_text(content)


def append(path: str, marker: str, content: str) -> None:
    file = Path(path)
    text = file.read_text()
    if marker in text:
        return
    file.write_text(text.rstrip() + "\n\n" + content.strip() + "\n")


write(
    "lexer/ellipsis_test.go",
    '''package lexer

import (
    "testing"

    "github.com/siyul-park/minipy/token"
    "github.com/stretchr/testify/require"
)

func TestLexEllipsis(t *testing.T) {
    tests := []struct {
        name string
        src  string
        want []token.Type
    }{
        {"literal", "...", []token.Type{token.ELLIPSIS, token.NEWLINE, token.EOF}},
        {"longest match", "....", []token.Type{token.ELLIPSIS, token.DOT, token.NEWLINE, token.EOF}},
        {"dot", ".", []token.Type{token.DOT, token.NEWLINE, token.EOF}},
        {"leading-dot float", ".5", []token.Type{token.FLOAT, token.NEWLINE, token.EOF}},
        {"attribute", "a.b", []token.Type{token.NAME, token.DOT, token.NAME, token.NEWLINE, token.EOF}},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            tokens, err := lex(tt.src)
            require.NoError(t, err)
            got := make([]token.Type, len(tokens))
            for i, tok := range tokens {
                got[i] = tok.Type
            }
            require.Equal(t, tt.want, got)
        })
    }
}
''',
)

write(
    "parser/ellipsis_test.go",
    '''package parser

import (
    "testing"

    "github.com/siyul-park/minipy/ast"
    "github.com/stretchr/testify/require"
)

func TestParseEllipsis(t *testing.T) {
    t.Run("standalone", func(t *testing.T) {
        mod, err := parse("...\n")
        require.NoError(t, err)
        require.IsType(t, &ast.EllipsisLit{}, mod.Body[0].(*ast.ExprStmt).X)
    })

    t.Run("assignment", func(t *testing.T) {
        mod, err := parse("x = ...\n")
        require.NoError(t, err)
        require.IsType(t, &ast.EllipsisLit{}, mod.Body[0].(*ast.Assign).Value)
    })

    t.Run("subscript", func(t *testing.T) {
        mod, err := parse("a[...]\n")
        require.NoError(t, err)
        sub := mod.Body[0].(*ast.ExprStmt).X.(*ast.Subscript)
        require.IsType(t, &ast.EllipsisLit{}, sub.Index)
    })
}
''',
)

write(
    "types/ellipsis_test.go",
    '''package types

import (
    "testing"

    vmtypes "github.com/siyul-park/minivm/types"
    "github.com/stretchr/testify/require"
)

func TestEllipsisType(t *testing.T) {
    require.Equal(t, "EllipsisType", Ellipsis.String())
    require.Equal(t, vmtypes.KindRef, Ellipsis.VM().Kind())
    require.False(t, Equal(Ellipsis, None))
    require.False(t, Equal(Ellipsis, Int))

    resolved, ok := Resolve("EllipsisType")
    require.True(t, ok)
    require.True(t, Equal(Ellipsis, resolved))
}
''',
)

write(
    "compiler/ellipsis_test.go",
    '''package compiler

import (
    "io"
    "strings"
    "testing"

    "github.com/siyul-park/minipy/token"
    "github.com/stretchr/testify/require"
)

func TestCompileEllipsis(t *testing.T) {
    t.Run("singleton annotation and comparisons", func(t *testing.T) {
        src := "x: EllipsisType = ...\n" +
            "assert x is Ellipsis\n" +
            "assert ... is Ellipsis\n" +
            "assert ... == Ellipsis\n" +
            "assert not (... != Ellipsis)\n"
        require.Empty(t, run(t, src))
    })

    t.Run("function round trip", func(t *testing.T) {
        src := "def identity(x: EllipsisType) -> EllipsisType:\n" +
            "    return x\n" +
            "assert identity(...) is Ellipsis\n"
        require.Empty(t, run(t, src))
    })

    t.Run("global shadowing", func(t *testing.T) {
        require.Equal(t, "1\n", run(t, "Ellipsis = 1\nprint(str(Ellipsis))\n"))
    })

    t.Run("local shadowing", func(t *testing.T) {
        src := "def value() -> int:\n" +
            "    Ellipsis = 2\n" +
            "    return Ellipsis\n" +
            "print(str(value()))\n"
        require.Equal(t, "2\n", run(t, src))
    })

    t.Run("ordering rejected", func(t *testing.T) {
        _, err := Compile(strings.NewReader("assert ... < ...\n"), WithOutput(io.Discard))
        require.Error(t, err)
        code(t, err, token.NotComparable)
    })

    t.Run("literal subscript rejected", func(t *testing.T) {
        errs := checkOnly(t, "a: list[int] = [1]\nprint(str(a[...]))\n")
        require.NotEmpty(t, errs)
        code(t, errs, token.UnsupportedFeature)
        require.Contains(t, errs.Error(), "ellipsis subscript is not supported")
    })

    t.Run("builtin subscript rejected", func(t *testing.T) {
        errs := checkOnly(t, "a: list[int] = [1]\nprint(str(a[Ellipsis]))\n")
        require.NotEmpty(t, errs)
        code(t, errs, token.UnsupportedFeature)
        require.Contains(t, errs.Error(), "ellipsis subscript is not supported")
    })

    t.Run("type is not constructible", func(t *testing.T) {
        _, err := Compile(strings.NewReader("x = EllipsisType()\n"), WithOutput(io.Discard))
        require.Error(t, err)
        code(t, err, token.UndefinedName)
    })
}
''',
)

append(
    "docs/spec/01-lexical.md",
    "<!-- ellipsis-token -->",
    '''<!-- ellipsis-token -->
## Ellipsis token

The delimiter `...` is one `ELLIPSIS` token under longest-match scanning. A
single `.` remains `DOT`, while a leading-dot number such as `.5` remains a
`FLOAT` token.
''',
)
append(
    "docs/spec/02-types.md",
    "<!-- ellipsis-type -->",
    '''<!-- ellipsis-type -->
## `EllipsisType`

`EllipsisType` is the source type of the single immutable Ellipsis value. It has
a reference-kind VM representation and cannot be directly constructed.
''',
)
append(
    "docs/spec/03-grammar.md",
    "<!-- ellipsis-atom -->",
    '''<!-- ellipsis-atom -->
## Ellipsis atom

`...` is an atom expression represented by `EllipsisLit`. Subscripts use the
ordinary expression grammar, so `a[...]` retains the Ellipsis node as its index.
''',
)
append(
    "docs/spec/04-static-semantics.md",
    "<!-- ellipsis-semantics -->",
    '''<!-- ellipsis-semantics -->
## Ellipsis

`...` and the unshadowed fallback name `Ellipsis` have type `EllipsisType`.
Normal bindings shadow the fallback. Ellipsis supports `is`, `is not`, `==`, and
`!=`; ordering and ellipsis subscripts are rejected statically.
''',
)
append(
    "docs/spec/05-codegen.md",
    "<!-- ellipsis-codegen -->",
    '''<!-- ellipsis-codegen -->
## Ellipsis lowering

The compiler reuses one immutable runtime constant for the literal and builtin
fallback name. Identity and equality use `REF_EQ`; negative forms append
`I32_EQZ`.
''',
)
append(
    "docs/spec/06-builtins.md",
    "<!-- ellipsis-builtin -->",
    '''<!-- ellipsis-builtin -->
## `Ellipsis`

`Ellipsis` is a fallback constant resolved only when no temporary, local,
capture, module, global, function, class, or imported binding shadows it.
''',
)
append(
    "docs/compatibility.md",
    "<!-- ellipsis-compatibility -->",
    '''<!-- ellipsis-compatibility -->
## Ellipsis

minipy supports `...`, the `Ellipsis` singleton, `EllipsisType`, and
identity/equality comparisons. Ellipsis subscript expansion remains unsupported.
''',
)
