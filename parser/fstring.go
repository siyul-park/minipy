package parser

import (
	"strings"

	"github.com/siyul-park/minipy/ast"
	"github.com/siyul-park/minipy/token"
)

func (p *Parser) parseFStringParts(source string, pos token.Pos) []ast.FStringPart {
	var parts []ast.FStringPart
	var text strings.Builder
	flush := func() {
		if text.Len() > 0 {
			parts = append(parts, &ast.FStringText{Base: ast.Base{Position: pos}, Value: text.String()})
			text.Reset()
		}
	}
	for i := 0; i < len(source); i++ {
		switch source[i] {
		case '{':
			if i+1 < len(source) && source[i+1] == '{' {
				text.WriteByte('{')
				i++
				continue
			}
			flush()
			body, end, ok := fstringField(source, i+1)
			if !ok {
				p.errs.Add(pos, token.SyntaxError, "unterminated f-string replacement field")
				return parts
			}
			parts = append(parts, p.parseFStringField(body, pos))
			i = end
		case '}':
			if i+1 < len(source) && source[i+1] == '}' {
				text.WriteByte('}')
				i++
				continue
			}
			p.errs.Add(pos, token.SyntaxError, "single '}' is not allowed in f-string")
		default:
			text.WriteByte(source[i])
		}
	}
	flush()
	return parts
}

func fstringField(source string, start int) (string, int, bool) {
	depth := 0
	for i := start; i < len(source); i++ {
		switch source[i] {
		case '{':
			depth++
		case '}':
			if depth == 0 {
				return source[start:i], i, true
			}
			depth--
		}
	}
	return "", len(source), false
}

func (p *Parser) parseFStringField(body string, pos token.Pos) ast.FStringPart {
	expressionSource, debug, conversion, format := splitFStringField(body)
	expr, err := parseFStringExpr(expressionSource)
	if err != nil {
		p.errs.Add(pos, token.SyntaxError, "invalid f-string expression")
		expr = &ast.NoneLit{Base: ast.Base{Position: pos}}
	}
	var formatParts []ast.FStringPart
	if format != "" {
		formatParts = p.parseFStringParts(format, pos)
	}
	return &ast.FStringExpr{Base: ast.Base{Position: pos}, Expr: expr, Debug: debug, Conversion: conversion, Format: formatParts}
}

// splitFStringField splits a replacement field body into its expression source,
// optional debug prefix (`expr=`), conversion rune (s/r/a), and format spec.
// The scan tracks bracket depth so operators and colons inside the expression
// (subscripts, walrus, calls) are not mistaken for field separators, and it
// distinguishes the debug `=` from comparison operators (==, !=, <=, >=, :=).
func splitFStringField(body string) (expression, debug string, conversion rune, format string) {
	colon, bang, eq := -1, -1, -1
	depth := 0
	for i := 0; i < len(body); i++ {
		switch body[i] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case ':':
			if depth == 0 && colon == -1 && (i+1 >= len(body) || body[i+1] != '=') {
				colon = i
			}
		case '!':
			if depth == 0 && colon == -1 && i+1 < len(body) && body[i+1] != '=' {
				bang = i
			}
		case '=':
			if depth == 0 && colon == -1 && bang == -1 && !comparisonEq(body, i) {
				eq = i
			}
		}
	}

	// The expression ends at the first field separator that follows it.
	exprEnd := len(body)
	for _, p := range []int{eq, bang, colon} {
		if p >= 0 && p < exprEnd {
			exprEnd = p
		}
	}
	expression = strings.TrimSpace(body[:exprEnd])

	if eq >= 0 {
		// Debug text is the verbatim source up to the conversion or format
		// boundary, preserving whitespace around `=` exactly (f"{x = }").
		debugEnd := len(body)
		if bang >= 0 {
			debugEnd = bang
		} else if colon >= 0 {
			debugEnd = colon
		}
		debug = body[:debugEnd]
	}
	if bang >= 0 && bang+1 < len(body) {
		conversion = rune(body[bang+1])
	}
	if colon >= 0 {
		format = body[colon+1:]
	}
	return expression, debug, conversion, format
}

// comparisonEq reports whether the `=` at index i is part of a comparison or
// assignment operator (==, !=, <=, >=, :=) rather than a debug `=`.
func comparisonEq(body string, i int) bool {
	if i+1 < len(body) && body[i+1] == '=' {
		return true
	}
	if i > 0 {
		switch body[i-1] {
		case '=', '!', '<', '>', ':':
			return true
		}
	}
	return false
}

func parseFStringExpr(source string) (ast.Expr, error) {
	module, err := Parse(strings.NewReader(source + "\n"))
	if err != nil {
		return nil, err
	}
	if len(module.Body) != 1 {
		return nil, token.ErrorList{}
	}
	statement, ok := module.Body[0].(*ast.ExprStmt)
	if !ok {
		return nil, token.ErrorList{}
	}
	return statement.X, nil
}
