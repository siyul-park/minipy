// Package parser builds an ast.Module from minipy source. It implements the
// supported subset of the Python grammar (docs/spec/03-grammar.md): simple
// statements over the full operator-precedence expression chain, control
// flow, and function definitions/calls/returns. Constructs outside the
// subset are reported as UnsupportedFeature with the milestone that introduces
// them.
package parser

import (
	"io"
	"strconv"
	"strings"

	"github.com/siyul-park/minipy/ast"
	"github.com/siyul-park/minipy/lexer"
	"github.com/siyul-park/minipy/token"
)

// Parser turns a token stream into an AST, accumulating diagnostics. It holds
// the lexer and pulls tokens on demand during parsing, buffering only as much
// lookahead as the grammar needs.
type Parser struct {
	lex  *lexer.Lexer
	buf  []token.Token
	errs token.ErrorList
}

var augAssign = map[token.Type]token.Type{
	token.PLUSEQ:        token.PLUS,
	token.MINUSEQ:       token.MINUS,
	token.STAREQ:        token.STAR,
	token.SLASHEQ:       token.SLASH,
	token.DOUBLESLASHEQ: token.DOUBLESLASH,
	token.PERCENTEQ:     token.PERCENT,
	token.AMPEQ:         token.AMP,
	token.PIPEEQ:        token.PIPE,
	token.CARETEQ:       token.CARET,
	token.LSHIFTEQ:      token.LSHIFT,
	token.RSHIFTEQ:      token.RSHIFT,
	token.DOUBLESTAREQ:  token.DOUBLESTAR,
}

var compoundStmt = map[token.Type]string{
	token.EXCEPT:  "'except' exceptions",
	token.FINALLY: "'finally' exceptions",
}

var simpleKeywordStmt = map[token.Type]string{
	token.IMPORT: "'import' modules",
	token.FROM:   "'from' modules",
}

// New returns a Parser over source read from r. No input is read until Parse
// pulls tokens from the lexer.
func New(r io.Reader) *Parser {
	return &Parser{lex: lexer.New(r)}
}

// Parse reads minipy source from r and parses it into a Module. The returned
// error merges every lexical and syntactic diagnostic (token.ErrorList); the
// Module is always non-nil so callers can inspect a partial parse.
func Parse(r io.Reader) (*ast.Module, error) {
	return New(r).Parse()
}

// Parse parses the token stream into a Module, lexing on demand. Lexical
// diagnostics gathered while pulling tokens are merged ahead of syntactic ones.
func (p *Parser) Parse() (*ast.Module, error) {
	mod := p.parseModule()
	if le, ok := p.lex.Err().(token.ErrorList); ok {
		p.errs = append(le, p.errs...)
	}
	return mod, p.errs.Err()
}

// parseModule parses the whole token stream into a Module.
func (p *Parser) parseModule() *ast.Module {
	mod := &ast.Module{Base: ast.Base{Position: token.Pos{Line: 1, Column: 1}}}
	for !p.at(token.EOF) {
		switch {
		case p.at(token.NEWLINE):
			p.advance()
		case p.at(token.INDENT):
			p.errs.Add(p.cur().Pos, token.SyntaxError, "unexpected indent")
			p.skipBlock()
		default:
			mod.Body = append(mod.Body, p.parseStatement()...)
		}
	}
	return mod
}

// parseStatement parses one statement: a compound statement (if/while/for) as a
// single node, or a line of `;`-separated simple statements. Compound forms from
// later milestones and orphan elif/else are reported and skipped.
func (p *Parser) parseStatement() []ast.Stmt {
	switch p.cur().Type {
	case token.IF:
		return []ast.Stmt{p.parseIf()}
	case token.WHILE:
		return []ast.Stmt{p.parseWhile()}
	case token.FOR:
		return []ast.Stmt{p.parseFor()}
	case token.TRY:
		return []ast.Stmt{p.parseTry()}
	case token.WITH:
		return []ast.Stmt{p.parseWith()}
	case token.DEF:
		return []ast.Stmt{p.parseFunction(nil, false)}
	case token.CLASS:
		return []ast.Stmt{p.parseClass(nil)}
	case token.AT:
		return []ast.Stmt{p.parseDecorated()}
	case token.ELIF, token.ELSE:
		p.errs.Add(p.cur().Pos, token.SyntaxError, "'%s' without a matching 'if'", p.cur().Type)
		p.skipLine()
		p.skipBlock()
		return nil
	}
	if msg, ok := compoundStmt[p.cur().Type]; ok {
		p.errs.Add(p.cur().Pos, token.UnsupportedFeature, "%s is not supported yet", msg)
		p.skipLine()
		p.skipBlock()
		return nil
	}
	if p.cur().Type == token.NAME && p.cur().Literal == "match" && p.isMatchHeader() {
		return []ast.Stmt{p.parseMatch()}
	}
	return p.parseSimpleLine()
}

// parseBlock parses the suite after a compound header's ':' — either an inline
// simple-statement line or a NEWLINE-INDENT block of statements.
func (p *Parser) parseBlock() []ast.Stmt {
	p.expect(token.COLON)
	if !p.at(token.NEWLINE) {
		return p.parseSimpleLine()
	}
	p.advance() // NEWLINE
	if !p.at(token.INDENT) {
		p.errs.Add(p.cur().Pos, token.SyntaxError, "expected an indented block")
		return nil
	}
	p.advance() // INDENT
	var body []ast.Stmt
	for !p.at(token.DEDENT) && !p.at(token.EOF) {
		if p.at(token.NEWLINE) {
			p.advance()
			continue
		}
		body = append(body, p.parseStatement()...)
	}
	p.expect(token.DEDENT)
	return body
}

// parseIf parses `('if'|'elif') expression ':' block` with an optional `elif`
// chain or trailing `else`. elif/else are folded into the If's Orelse, so an
// `elif` is a nested If (the CPython AST shape).
func (p *Parser) parseIf() ast.Stmt {
	pos := p.cur().Pos
	p.advance() // 'if' or 'elif'
	cond := p.parseExpression()
	body := p.parseBlock()
	var orelse []ast.Stmt
	switch p.cur().Type {
	case token.ELIF:
		orelse = []ast.Stmt{p.parseIf()}
	case token.ELSE:
		p.advance()
		orelse = p.parseBlock()
	}
	return &ast.If{Base: ast.Base{Position: pos}, Cond: cond, Body: body, Orelse: orelse}
}

// parseWhile parses `'while' expression ':' block ['else' ':' block]`.
func (p *Parser) parseWhile() ast.Stmt {
	pos := p.cur().Pos
	p.advance()
	cond := p.parseExpression()
	body := p.parseBlock()
	var orelse []ast.Stmt
	if p.at(token.ELSE) {
		p.advance()
		orelse = p.parseBlock()
	}
	return &ast.While{Base: ast.Base{Position: pos}, Cond: cond, Body: body, Orelse: orelse}
}

// parseFor parses `'for' NAME 'in' expression ':' block ['else' ':' block]`.
func (p *Parser) parseFor() ast.Stmt {
	pos := p.cur().Pos
	p.advance() // 'for'
	target := p.parseForTarget()
	p.expect(token.IN)
	iter := p.parseExpression()
	body := p.parseBlock()
	var orelse []ast.Stmt
	if p.at(token.ELSE) {
		p.advance()
		orelse = p.parseBlock()
	}
	return &ast.For{Base: ast.Base{Position: pos}, Target: target, Iter: iter, Body: body, Orelse: orelse}
}

// parseTry parses `try: block` followed by except/else/finally clauses.
func (p *Parser) parseTry() ast.Stmt {
	pos := p.cur().Pos
	p.advance() // try
	body := p.parseBlock()
	var handlers []*ast.ExceptHandler
	for p.at(token.EXCEPT) {
		handlers = append(handlers, p.parseExceptHandler())
	}
	var orelse []ast.Stmt
	if p.at(token.ELSE) {
		p.advance()
		orelse = p.parseBlock()
	}
	var finalbody []ast.Stmt
	if p.at(token.FINALLY) {
		p.advance()
		finalbody = p.parseBlock()
	}
	if len(handlers) == 0 && len(finalbody) == 0 {
		p.errs.Add(pos, token.SyntaxError, "'try' must have at least one except or finally clause")
	}
	return &ast.Try{Base: ast.Base{Position: pos}, Body: body, Handlers: handlers, Orelse: orelse, Finalbody: finalbody}
}

func (p *Parser) parseExceptHandler() *ast.ExceptHandler {
	pos := p.cur().Pos
	p.advance() // except
	var typ ast.Expr
	var name string
	if !p.at(token.COLON) {
		typ = p.parseExpression()
		if p.at(token.AS) {
			p.advance()
			name = p.expect(token.NAME).Literal
		}
	}
	body := p.parseBlock()
	return &ast.ExceptHandler{Base: ast.Base{Position: pos}, Type: typ, Name: name, Body: body}
}

// parseWith parses `with item (, item)*: block`.
func (p *Parser) parseWith() ast.Stmt {
	pos := p.cur().Pos
	p.advance() // with
	var items []*ast.WithItem
	for {
		itemPos := p.cur().Pos
		ctx := p.parseExpression()
		var opt ast.Expr
		if p.at(token.AS) {
			p.advance()
			t := p.expect(token.NAME)
			opt = &ast.Name{Base: ast.Base{Position: t.Pos}, Name: t.Literal}
		}
		items = append(items, &ast.WithItem{Base: ast.Base{Position: itemPos}, Context: ctx, OptionalVars: opt})
		if !p.at(token.COMMA) {
			break
		}
		p.advance()
	}
	body := p.parseBlock()
	return &ast.With{Base: ast.Base{Position: pos}, Items: items, Body: body}
}

// parseDecorated parses one or more bare-name decorators followed by a function
// or class definition. Decorator expressions beyond bare names are deferred.
func (p *Parser) parseDecorated() ast.Stmt {
	var decorators []*ast.Name
	for p.at(token.AT) {
		pos := p.cur().Pos
		p.advance()
		if !p.at(token.NAME) {
			p.errs.Add(p.cur().Pos, token.UnsupportedFeature, "decorators must be bare names")
			p.skipLine()
			continue
		}
		name := &ast.Name{Base: ast.Base{Position: p.cur().Pos}, Name: p.cur().Literal}
		decorators = append(decorators, name)
		p.advance()
		if p.at(token.LPAREN) || p.at(token.DOT) {
			p.errs.Add(pos, token.UnsupportedFeature, "call-form and dotted decorators arrive later")
			p.skipLine()
			continue
		}
		p.expectLineEnd()
	}
	switch p.cur().Type {
	case token.DEF:
		return p.parseFunction(decorators, false)
	case token.CLASS:
		for _, dec := range decorators {
			if dec.Name != "dataclass" {
				p.errs.Add(dec.Pos(), token.UnsupportedFeature, "class decorator @%s is not supported", dec.Name)
			}
		}
		return p.parseClass(decorators)
	default:
		p.errs.Add(p.cur().Pos, token.SyntaxError, "expected def or class after decorator")
		p.skipLine()
		p.skipBlock()
		return nil
	}
}

// parseFunction parses `def NAME(params) -> type: block`.
func (p *Parser) parseFunction(decorators []*ast.Name, allowBareSelf bool) ast.Stmt {
	pos := p.cur().Pos
	p.advance() // def
	nameTok := p.expect(token.NAME)
	name := &ast.Name{Base: ast.Base{Position: nameTok.Pos}, Name: nameTok.Literal}
	p.expect(token.LPAREN)
	params := p.parseParams(allowBareSelf)
	p.expect(token.RPAREN)
	p.expect(token.ARROW)
	returns := p.parseType()
	body := p.parseBlock()
	return &ast.Function{
		Base:       ast.Base{Position: pos},
		Name:       name,
		Params:     params,
		Returns:    returns,
		Decorators: decorators,
		Body:       body,
	}
}

func (p *Parser) parseParams(allowBareSelf bool) []*ast.Param {
	var params []*ast.Param
	if p.at(token.RPAREN) || p.at(token.EOF) {
		return params
	}
	for {
		nameTok := p.expect(token.NAME)
		name := &ast.Name{Base: ast.Base{Position: nameTok.Pos}, Name: nameTok.Literal}
		index := len(params)
		var ann ast.Expr
		if !p.at(token.COLON) {
			if !(allowBareSelf && index == 0 && name.Name == "self") {
				p.errs.Add(p.cur().Pos, token.MissingAnnotation, "parameter %q needs a type annotation", name.Name)
				ann = p.parseType()
			}
		} else {
			p.advance()
			ann = p.parseType()
		}
		if p.at(token.ASSIGN) {
			p.errs.Add(p.cur().Pos, token.UnsupportedFeature, "default parameter values are not supported")
			p.advance()
			p.parseExpression()
		}
		params = append(params, &ast.Param{Base: ast.Base{Position: nameTok.Pos}, Name: name, Ann: ann})
		if !p.at(token.COMMA) {
			break
		}
		p.advance()
		if p.at(token.RPAREN) {
			break
		}
	}
	return params
}

// parseClass parses `class NAME[(Base)]: class_block`.
func (p *Parser) parseClass(decorators []*ast.Name) ast.Stmt {
	pos := p.cur().Pos
	p.advance() // class
	nameTok := p.expect(token.NAME)
	name := &ast.Name{Base: ast.Base{Position: nameTok.Pos}, Name: nameTok.Literal}
	var base *ast.Name
	if p.at(token.LPAREN) {
		p.advance()
		baseTok := p.expect(token.NAME)
		base = &ast.Name{Base: ast.Base{Position: baseTok.Pos}, Name: baseTok.Literal}
		if p.at(token.COMMA) {
			p.errs.Add(p.cur().Pos, token.UnsupportedFeature, "multiple inheritance is not supported")
			for !p.at(token.RPAREN) && !p.at(token.EOF) {
				p.advance()
			}
		}
		p.expect(token.RPAREN)
	}
	body := p.parseClassBlock()
	return &ast.Class{Base: ast.Base{Position: pos}, Name: name, BaseClass: base, Decorators: decorators, Body: body}
}

func (p *Parser) parseClassBlock() []ast.Stmt {
	p.expect(token.COLON)
	if !p.at(token.NEWLINE) {
		p.errs.Add(p.cur().Pos, token.SyntaxError, "class body must be an indented block")
		p.skipLine()
		return nil
	}
	p.advance()
	if !p.at(token.INDENT) {
		p.errs.Add(p.cur().Pos, token.SyntaxError, "expected an indented block")
		return nil
	}
	p.advance()
	var body []ast.Stmt
	for !p.at(token.DEDENT) && !p.at(token.EOF) {
		switch p.cur().Type {
		case token.NEWLINE:
			p.advance()
		case token.PASS:
			t := p.cur()
			p.advance()
			body = append(body, &ast.Pass{Base: ast.Base{Position: t.Pos}})
			p.expectLineEnd()
		case token.DEF:
			body = append(body, p.parseFunction(nil, true))
		case token.NAME:
			if p.peek(1).Type == token.COLON {
				body = append(body, p.parseAnnAssign())
				p.expectLineEnd()
			} else {
				p.errs.Add(p.cur().Pos, token.SyntaxError, "class body supports only fields and methods")
				p.skipLine()
			}
		default:
			p.errs.Add(p.cur().Pos, token.SyntaxError, "class body supports only fields and methods")
			p.skipLine()
			p.skipBlock()
		}
	}
	p.expect(token.DEDENT)
	return body
}

// parseForTarget parses the single NAME loop variable. Tuple-unpacking targets
// (`for k, v in ...`) are also accepted.
func (p *Parser) parseForTarget() ast.Expr {
	t := p.cur()
	if t.Type != token.NAME {
		p.errs.Add(t.Pos, token.SyntaxError, "expected a loop variable name, got %s", t.Type)
		return &ast.Name{Base: ast.Base{Position: t.Pos}, Name: ""}
	}
	p.advance()
	if p.at(token.COMMA) {
		elems := []ast.Expr{&ast.Name{Base: ast.Base{Position: t.Pos}, Name: t.Literal}}
		for p.at(token.COMMA) {
			p.advance()
			if p.at(token.NAME) {
				elem := &ast.Name{Base: ast.Base{Position: p.cur().Pos}, Name: p.cur().Literal}
				elems = append(elems, elem)
				p.advance()
			}
		}
		return &ast.TupleLit{Base: ast.Base{Position: t.Pos}, Elems: elems}
	}
	return &ast.Name{Base: ast.Base{Position: t.Pos}, Name: t.Literal}
}

// parseSimpleLine parses `;`-separated simple statements terminated by NEWLINE.
func (p *Parser) parseSimpleLine() []ast.Stmt {
	var stmts []ast.Stmt
	for {
		if s := p.parseSimpleStmt(); s != nil {
			stmts = append(stmts, s)
		}
		if p.at(token.SEMICOLON) {
			p.advance()
			if p.at(token.NEWLINE) || p.at(token.EOF) {
				break
			}
			continue
		}
		break
	}
	p.expectLineEnd()
	return stmts
}

// parseSimpleStmt parses a single assignment, annotated declaration, or
// expression statement.
func (p *Parser) parseSimpleStmt() ast.Stmt {
	switch p.cur().Type {
	case token.PASS:
		t := p.cur()
		p.advance()
		return &ast.Pass{Base: ast.Base{Position: t.Pos}}
	case token.BREAK:
		t := p.cur()
		p.advance()
		return &ast.Break{Base: ast.Base{Position: t.Pos}}
	case token.CONTINUE:
		t := p.cur()
		p.advance()
		return &ast.Continue{Base: ast.Base{Position: t.Pos}}
	case token.RETURN:
		t := p.cur()
		p.advance()
		var value ast.Expr
		if !p.at(token.SEMICOLON) && !p.at(token.NEWLINE) && !p.at(token.EOF) {
			value = p.parseExpression()
		}
		return &ast.Return{Base: ast.Base{Position: t.Pos}, Value: value}
	case token.YIELD:
		t := p.cur()
		p.advance()
		if p.at(token.FROM) {
			p.errs.Add(t.Pos, token.UnsupportedFeature, "'yield from' is not supported yet")
			p.skipToStmtEnd()
			return nil
		}
		var value ast.Expr
		if !p.at(token.SEMICOLON) && !p.at(token.NEWLINE) && !p.at(token.EOF) {
			value = p.parseExpression()
		}
		return &ast.Yield{Base: ast.Base{Position: t.Pos}, Value: value}
	case token.RAISE:
		return p.parseRaise()
	case token.GLOBAL:
		t := p.cur()
		p.advance()
		return &ast.Global{Base: ast.Base{Position: t.Pos}, Names: p.parseNameList()}
	case token.NONLOCAL:
		t := p.cur()
		p.advance()
		return &ast.Nonlocal{Base: ast.Base{Position: t.Pos}, Names: p.parseNameList()}
	case token.DEL:
		return p.parseDelete()
	case token.ASSERT:
		return p.parseAssert()
	}

	if msg, ok := simpleKeywordStmt[p.cur().Type]; ok {
		p.errs.Add(p.cur().Pos, token.UnsupportedFeature, "%s is not supported yet", msg)
		p.skipToStmtEnd()
		return nil
	}

	if p.at(token.NAME) && p.peek(1).Type == token.COLON {
		return p.parseAnnAssign()
	}

	pos := p.cur().Pos
	if p.at(token.NAME) && p.peek(1).Type == token.COMMA {
		target := p.parseFlatTupleTarget()
		if p.at(token.ASSIGN) {
			p.advance()
			return &ast.Assign{Base: ast.Base{Position: pos}, Target: target, Value: p.parseExpression()}
		}
		p.errs.Add(target.Pos(), token.SyntaxError, "tuple target requires assignment")
		return nil
	}
	target := p.parseExpression()

	switch {
	case p.at(token.ASSIGN):
		p.advance()
		value := p.parseExpression()
		p.requireTarget(target)
		return &ast.Assign{Base: ast.Base{Position: pos}, Target: target, Value: value}
	case augAssign[p.cur().Type] != token.ILLEGAL:
		op := augAssign[p.cur().Type]
		p.advance()
		value := p.parseExpression()
		p.requireTarget(target)
		return &ast.AugAssign{Base: ast.Base{Position: pos}, Target: target, Op: op, Value: value}
	default:
		return &ast.ExprStmt{Base: ast.Base{Position: pos}, X: target}
	}
}

// parseRaise parses `raise [expression]`.
func (p *Parser) parseRaise() ast.Stmt {
	t := p.cur()
	p.advance()
	var exc ast.Expr
	if !p.at(token.SEMICOLON) && !p.at(token.NEWLINE) && !p.at(token.EOF) {
		exc = p.parseExpression()
	}
	return &ast.Raise{Base: ast.Base{Position: t.Pos}, Exc: exc}
}

// parseAnnAssign parses `NAME ':' type ['=' expression]`.
func (p *Parser) parseAnnAssign() ast.Stmt {
	nameTok := p.cur()
	p.advance()
	name := &ast.Name{Base: ast.Base{Position: nameTok.Pos}, Name: nameTok.Literal}
	p.expect(token.COLON)
	ann := p.parseType()

	var value ast.Expr
	if p.at(token.ASSIGN) {
		p.advance()
		value = p.parseExpression()
	}
	return &ast.AnnAssign{Base: ast.Base{Position: nameTok.Pos}, Target: name, Ann: ann, Value: value}
}

// parseDelete parses `del target, ...` where each target is a Name, Subscript,
// or Attribute lvalue.
func (p *Parser) parseDelete() ast.Stmt {
	t := p.cur()
	p.advance() // 'del'
	var targets []ast.Expr
	for {
		target := p.parseExpression()
		switch target.(type) {
		case *ast.Name, *ast.Subscript, *ast.Attribute:
		default:
			p.errs.Add(target.Pos(), token.SyntaxError, "cannot delete this expression")
		}
		targets = append(targets, target)
		if !p.at(token.COMMA) {
			break
		}
		p.advance()
		if p.at(token.NEWLINE) || p.at(token.SEMICOLON) || p.at(token.EOF) {
			break
		}
	}
	return &ast.Delete{Base: ast.Base{Position: t.Pos}, Targets: targets}
}

// parseAssert parses `assert test[, msg]`.
func (p *Parser) parseAssert() ast.Stmt {
	t := p.cur()
	p.advance() // 'assert'
	test := p.parseExpression()
	var msg ast.Expr
	if p.at(token.COMMA) {
		p.advance()
		msg = p.parseExpression()
	}
	return &ast.Assert{Base: ast.Base{Position: t.Pos}, Test: test, Msg: msg}
}

// isMatchHeader decides whether a line beginning with the soft keyword `match`
// is a match statement rather than an ordinary use of `match` as a name. It is a
// statement only when the logical line ends in a bracket-depth-0 ':' immediately
// followed by a NEWLINE (an indented case block). `match = x`, `match: T`, and
// `match(x)` therefore stay expressions/assignments.
func (p *Parser) isMatchHeader() bool {
	switch p.peek(1).Type {
	case token.ASSIGN, token.COLON, token.NEWLINE, token.SEMICOLON, token.EOF:
		return false
	}
	if augAssign[p.peek(1).Type] != token.ILLEGAL {
		return false
	}
	depth := 0
	for i := 1; ; i++ {
		switch p.peek(i).Type {
		case token.LPAREN, token.LBRACKET, token.LBRACE:
			depth++
		case token.RPAREN, token.RBRACKET, token.RBRACE:
			depth--
		case token.NEWLINE, token.EOF:
			return false
		case token.COLON:
			if depth == 0 {
				return p.peek(i+1).Type == token.NEWLINE
			}
		}
	}
}

// parseMatch parses `match subject: NEWLINE INDENT (case ...)+ DEDENT`.
func (p *Parser) parseMatch() ast.Stmt {
	pos := p.cur().Pos
	p.advance() // 'match'
	subject := p.parseMatchSubject()
	p.expect(token.COLON)
	if !p.at(token.NEWLINE) {
		p.errs.Add(p.cur().Pos, token.SyntaxError, "expected an indented block of 'case' clauses")
		p.skipLine()
		return &ast.Match{Base: ast.Base{Position: pos}, Subject: subject}
	}
	p.advance() // NEWLINE
	if !p.at(token.INDENT) {
		p.errs.Add(p.cur().Pos, token.SyntaxError, "expected an indented block of 'case' clauses")
		return &ast.Match{Base: ast.Base{Position: pos}, Subject: subject}
	}
	p.advance() // INDENT
	var cases []*ast.Case
	for !p.at(token.DEDENT) && !p.at(token.EOF) {
		if p.at(token.NEWLINE) {
			p.advance()
			continue
		}
		if !(p.cur().Type == token.NAME && p.cur().Literal == "case") {
			p.errs.Add(p.cur().Pos, token.SyntaxError, "expected 'case'")
			p.skipLine()
			p.skipBlock()
			continue
		}
		cases = append(cases, p.parseCase())
	}
	p.expect(token.DEDENT)
	return &ast.Match{Base: ast.Base{Position: pos}, Subject: subject, Cases: cases}
}

// parseMatchSubject parses a match subject; a top-level comma makes it a tuple.
func (p *Parser) parseMatchSubject() ast.Expr {
	pos := p.cur().Pos
	first := p.parseExpression()
	if !p.at(token.COMMA) {
		return first
	}
	elems := []ast.Expr{first}
	for p.at(token.COMMA) {
		p.advance()
		if p.at(token.COLON) || p.at(token.EOF) {
			break
		}
		elems = append(elems, p.parseExpression())
	}
	return &ast.TupleLit{Base: ast.Base{Position: pos}, Elems: elems}
}

// parseCase parses `case patterns [if guard]: block`.
func (p *Parser) parseCase() *ast.Case {
	pos := p.cur().Pos
	p.advance() // 'case'
	pattern := p.parsePatterns()
	var guard ast.Expr
	if p.at(token.IF) {
		p.advance()
		guard = p.parseExpression()
	}
	body := p.parseBlock()
	return &ast.Case{Base: ast.Base{Position: pos}, Pattern: pattern, Guard: guard, Body: body}
}

// parsePatterns parses one case pattern; a top-level comma makes it an open
// sequence pattern (`case a, b:`).
func (p *Parser) parsePatterns() ast.Pattern {
	pos := p.cur().Pos
	first := p.parseSeqElem()
	if !p.at(token.COMMA) {
		return first
	}
	elems := []ast.Pattern{first}
	star := -1
	if _, ok := first.(*ast.StarPattern); ok {
		star = 0
	}
	for p.at(token.COMMA) {
		p.advance()
		if p.at(token.COLON) || p.at(token.IF) || p.at(token.EOF) {
			break
		}
		e := p.parseSeqElem()
		if _, ok := e.(*ast.StarPattern); ok && star < 0 {
			star = len(elems)
		}
		elems = append(elems, e)
	}
	return &ast.SequencePattern{Base: ast.Base{Position: pos}, Elems: elems, Star: star}
}

// parseSeqElem parses a sequence element: a starred rest `*name` or an
// or-pattern.
func (p *Parser) parseSeqElem() ast.Pattern {
	if p.at(token.STAR) {
		pos := p.cur().Pos
		p.advance()
		name := ""
		if p.at(token.NAME) {
			name = p.cur().Literal
			p.advance()
		}
		return &ast.StarPattern{Base: ast.Base{Position: pos}, Name: name}
	}
	return p.parseOrPattern()
}

// parseOrPattern parses `closed ('|' closed)* ['as' NAME]`; `as` binds looser
// than `|`.
func (p *Parser) parseOrPattern() ast.Pattern {
	pos := p.cur().Pos
	alts := []ast.Pattern{p.parseClosedPattern()}
	for p.at(token.PIPE) {
		p.advance()
		alts = append(alts, p.parseClosedPattern())
	}
	var pat ast.Pattern
	if len(alts) == 1 {
		pat = alts[0]
	} else {
		pat = &ast.OrPattern{Base: ast.Base{Position: pos}, Alts: alts}
	}
	if p.at(token.AS) {
		p.advance()
		name := p.expect(token.NAME).Literal
		pat = &ast.AsPattern{Base: ast.Base{Position: pos}, Pattern: pat, Name: name}
	}
	return pat
}

// parseClosedPattern parses a single non-or pattern.
func (p *Parser) parseClosedPattern() ast.Pattern {
	t := p.cur()
	switch t.Type {
	case token.LBRACKET:
		return p.parseSequencePattern(token.LBRACKET, token.RBRACKET)
	case token.LPAREN:
		return p.parseSequencePattern(token.LPAREN, token.RPAREN)
	case token.LBRACE:
		return p.parseMappingPattern()
	case token.NONE, token.TRUE, token.FALSE, token.INT, token.FLOAT, token.STRING, token.FSTRING:
		return &ast.ValuePattern{Base: ast.Base{Position: t.Pos}, Value: p.parseAtom()}
	case token.MINUS, token.PLUS:
		p.advance()
		num := p.parseAtom()
		return &ast.ValuePattern{Base: ast.Base{Position: t.Pos}, Value: &ast.UnaryExpr{Base: ast.Base{Position: t.Pos}, Op: t.Type, X: num}}
	case token.NAME:
		return p.parseNamePattern()
	default:
		p.errs.Add(t.Pos, token.SyntaxError, "invalid pattern: unexpected %s", t.Type)
		if !p.at(token.EOF) {
			p.advance()
		}
		return &ast.WildcardPattern{Base: ast.Base{Position: t.Pos}}
	}
}

// parseNamePattern parses `_` (wildcard), a bare name (capture), a dotted value,
// or a class pattern when followed by `(`.
func (p *Parser) parseNamePattern() ast.Pattern {
	t := p.cur()
	pos := t.Pos
	if t.Literal == "_" {
		p.advance()
		return &ast.WildcardPattern{Base: ast.Base{Position: pos}}
	}
	var expr ast.Expr = &ast.Name{Base: ast.Base{Position: pos}, Name: t.Literal}
	p.advance()
	dotted := false
	for p.at(token.DOT) {
		p.advance()
		nameTok := p.expect(token.NAME)
		expr = &ast.Attribute{Base: ast.Base{Position: pos}, X: expr, Name: nameTok.Literal}
		dotted = true
	}
	if p.at(token.LPAREN) {
		return p.parseClassPattern(expr, pos)
	}
	if dotted {
		return &ast.ValuePattern{Base: ast.Base{Position: pos}, Value: expr}
	}
	return &ast.CapturePattern{Base: ast.Base{Position: pos}, Name: t.Literal}
}

// parseClassPattern parses `Class(pos..., name=kw...)` after the class name.
func (p *Parser) parseClassPattern(class ast.Expr, pos token.Pos) ast.Pattern {
	p.expect(token.LPAREN)
	cp := &ast.ClassPattern{Base: ast.Base{Position: pos}, Class: class}
	for !p.at(token.RPAREN) && !p.at(token.EOF) {
		if p.cur().Type == token.NAME && p.peek(1).Type == token.ASSIGN {
			name := p.cur().Literal
			p.advance() // NAME
			p.advance() // '='
			cp.KwNames = append(cp.KwNames, name)
			cp.Kw = append(cp.Kw, p.parseOrPattern())
		} else {
			cp.Args = append(cp.Args, p.parseOrPattern())
		}
		if !p.at(token.COMMA) {
			break
		}
		p.advance()
	}
	p.expect(token.RPAREN)
	return cp
}

// parseSequencePattern parses a bracketed/parenthesized sequence pattern. A
// single parenthesized pattern with no comma is a group, returned directly.
func (p *Parser) parseSequencePattern(open, closing token.Type) ast.Pattern {
	pos := p.cur().Pos
	p.advance() // open
	var elems []ast.Pattern
	star := -1
	sawComma := false
	for !p.at(closing) && !p.at(token.EOF) {
		e := p.parseSeqElem()
		if _, ok := e.(*ast.StarPattern); ok && star < 0 {
			star = len(elems)
		}
		elems = append(elems, e)
		if !p.at(token.COMMA) {
			break
		}
		sawComma = true
		p.advance()
	}
	p.expect(closing)
	if open == token.LPAREN && len(elems) == 1 && !sawComma {
		return elems[0]
	}
	return &ast.SequencePattern{Base: ast.Base{Position: pos}, Elems: elems, Star: star}
}

// parseMappingPattern parses `{key: pattern, ..., **rest}`.
func (p *Parser) parseMappingPattern() ast.Pattern {
	pos := p.cur().Pos
	p.advance() // '{'
	mp := &ast.MappingPattern{Base: ast.Base{Position: pos}}
	for !p.at(token.RBRACE) && !p.at(token.EOF) {
		if p.at(token.DOUBLESTAR) {
			p.advance()
			mp.Rest = p.expect(token.NAME).Literal
			if p.at(token.COMMA) {
				p.advance()
			}
			break
		}
		key := p.parseExpression()
		p.expect(token.COLON)
		mp.Keys = append(mp.Keys, key)
		mp.Values = append(mp.Values, p.parseOrPattern())
		if !p.at(token.COMMA) {
			break
		}
		p.advance()
	}
	p.expect(token.RBRACE)
	return mp
}

func (p *Parser) parseFlatTupleTarget() ast.Expr {
	pos := p.cur().Pos
	var elems []ast.Expr
	for {
		t := p.expect(token.NAME)
		elems = append(elems, &ast.Name{Base: ast.Base{Position: t.Pos}, Name: t.Literal})
		if !p.at(token.COMMA) {
			break
		}
		p.advance()
	}
	return &ast.TupleLit{Base: ast.Base{Position: pos}, Elems: elems}
}

func (p *Parser) parseNameList() []string {
	var names []string
	for {
		t := p.expect(token.NAME)
		if t.Literal != "" {
			names = append(names, t.Literal)
		}
		if !p.at(token.COMMA) {
			break
		}
		p.advance()
	}
	return names
}

// parseType parses an annotation, including union forms `A | B | C`.
func (p *Parser) parseType() ast.Expr {
	pos := p.cur().Pos
	first := p.parseTypeAtom()
	if !p.at(token.PIPE) {
		return first
	}
	members := []ast.Expr{first}
	for p.at(token.PIPE) {
		p.advance()
		members = append(members, p.parseTypeAtom())
	}
	return &ast.UnionType{Base: ast.Base{Position: pos}, Members: members}
}

// parseTypeAtom parses a single non-union annotation atom: a name, a None, or a
// subscripted/generic type such as list[T], dict[K, V], or Callable[[P], R].
func (p *Parser) parseTypeAtom() ast.Expr {
	t := p.cur()
	if t.Type == token.NAME || t.Type == token.NONE {
		p.advance()
		name := t.Literal
		if t.Type == token.NONE {
			name = "None"
		}
		node := &ast.Name{Base: ast.Base{Position: t.Pos}, Name: name}
		if p.at(token.LBRACKET) {
			if name == "Callable" {
				return p.parseCallableType(node)
			}
			p.advance()
			var args []ast.Expr
			for !p.at(token.RBRACKET) && !p.at(token.EOF) {
				args = append(args, p.parseType())
				if !p.at(token.COMMA) {
					break
				}
				p.advance()
			}
			p.expect(token.RBRACKET)
			var index ast.Expr
			if len(args) == 1 {
				index = args[0]
			} else {
				index = &ast.TupleLit{Base: ast.Base{Position: t.Pos}, Elems: args}
			}
			return &ast.Subscript{Base: ast.Base{Position: t.Pos}, X: node, Index: index}
		}
		return node
	}
	p.errs.Add(t.Pos, token.UnsupportedType, "expected a type name, got %s", t.Type)
	return &ast.Name{Base: ast.Base{Position: t.Pos}, Name: ""}
}

func (p *Parser) parseCallableType(base *ast.Name) ast.Expr {
	pos := base.Pos()
	p.expect(token.LBRACKET)
	p.expect(token.LBRACKET)
	var params []ast.Expr
	for !p.at(token.RBRACKET) && !p.at(token.EOF) {
		params = append(params, p.parseType())
		if !p.at(token.COMMA) {
			break
		}
		p.advance()
	}
	p.expect(token.RBRACKET)
	p.expect(token.COMMA)
	ret := p.parseType()
	p.expect(token.RBRACKET)
	return &ast.Subscript{
		Base: ast.Base{Position: pos},
		X:    base,
		Index: &ast.TupleLit{Base: ast.Base{Position: pos}, Elems: []ast.Expr{
			&ast.TupleLit{Base: ast.Base{Position: pos}, Elems: params},
			ret,
		}},
	}
}

// parseExpression is the expression entry: a disjunction, optionally followed by
// a conditional `if cond else orelse` or a lambda expression.
func (p *Parser) parseExpression() ast.Expr {
	if p.at(token.LAMBDA) {
		return p.parseLambda()
	}
	x := p.parseDisjunction()
	if p.at(token.IF) {
		p.advance()
		cond := p.parseDisjunction()
		p.expect(token.ELSE)
		orelse := p.parseExpression()
		return &ast.IfExp{Base: ast.Base{Position: x.Pos()}, Body: x, Cond: cond, Orelse: orelse}
	}
	return x
}

func (p *Parser) parseLambda() ast.Expr {
	pos := p.cur().Pos
	p.advance()
	var params []*ast.Param
	if !p.at(token.COLON) {
		for {
			t := p.expect(token.NAME)
			name := &ast.Name{Base: ast.Base{Position: t.Pos}, Name: t.Literal}
			params = append(params, &ast.Param{Base: ast.Base{Position: t.Pos}, Name: name})
			if !p.at(token.COMMA) {
				break
			}
			p.advance()
		}
	}
	p.expect(token.COLON)
	return &ast.LambdaExpr{Base: ast.Base{Position: pos}, Params: params, Body: p.parseExpression()}
}

func (p *Parser) parseDisjunction() ast.Expr {
	x := p.parseConjunction()
	for p.at(token.OR) {
		pos := p.cur().Pos
		p.advance()
		y := p.parseConjunction()
		x = &ast.BoolOp{Base: ast.Base{Position: pos}, Op: token.OR, X: x, Y: y}
	}
	return x
}

func (p *Parser) parseConjunction() ast.Expr {
	x := p.parseInversion()
	for p.at(token.AND) {
		pos := p.cur().Pos
		p.advance()
		y := p.parseInversion()
		x = &ast.BoolOp{Base: ast.Base{Position: pos}, Op: token.AND, X: x, Y: y}
	}
	return x
}

func (p *Parser) parseInversion() ast.Expr {
	if p.at(token.NOT) {
		pos := p.cur().Pos
		p.advance()
		x := p.parseInversion()
		return &ast.UnaryExpr{Base: ast.Base{Position: pos}, Op: token.NOT, X: x}
	}
	return p.parseComparison()
}

func (p *Parser) parseComparison() ast.Expr {
	x := p.parseBinary(1)
	var ops []token.Type
	var rest []ast.Expr
	for {
		op := p.cur().Type
		switch op {
		case token.EQ, token.NE, token.LT, token.LE, token.GT, token.GE:
			p.advance()
			ops = append(ops, op)
			rest = append(rest, p.parseBinary(1))
		case token.IN:
			p.advance()
			ops = append(ops, op)
			rest = append(rest, p.parseBinary(1))
		case token.IS:
			p.advance()
			if p.at(token.NOT) {
				p.advance()
				ops = append(ops, token.ISNOT)
			} else {
				ops = append(ops, token.IS)
			}
			rest = append(rest, p.parseBinary(1))
		case token.NOT:
			if p.peek(1).Type != token.IN {
				if len(ops) == 0 {
					return x
				}
				return &ast.Compare{Base: ast.Base{Position: x.Pos()}, X: x, Ops: ops, Comparators: rest}
			}
			p.advance()
			p.advance()
			ops = append(ops, token.NOTIN)
			rest = append(rest, p.parseBinary(1))
		default:
			if len(ops) == 0 {
				return x
			}
			return &ast.Compare{Base: ast.Base{Position: x.Pos()}, X: x, Ops: ops, Comparators: rest}
		}
	}
}

// binPrec maps each binary operator to its precedence; higher binds tighter.
// It covers bitwise_or (1) down to term (6); comparison sits just above and
// unary/power just below (parseFactor) per docs/spec/03-grammar.md.
var binPrec = map[token.Type]int{
	token.PIPE:        1,
	token.CARET:       2,
	token.AMP:         3,
	token.LSHIFT:      4,
	token.RSHIFT:      4,
	token.PLUS:        5,
	token.MINUS:       5,
	token.STAR:        6,
	token.SLASH:       6,
	token.DOUBLESLASH: 6,
	token.PERCENT:     6,
}

// parseBinary parses left-associative binary operators by precedence climbing.
func (p *Parser) parseBinary(minPrec int) ast.Expr {
	x := p.parseFactor()
	for {
		prec, ok := binPrec[p.cur().Type]
		if !ok || prec < minPrec {
			return x
		}
		op := p.cur().Type
		pos := p.cur().Pos
		p.advance()
		y := p.parseBinary(prec + 1)
		x = &ast.BinaryExpr{Base: ast.Base{Position: pos}, Op: op, X: x, Y: y}
	}
}

func (p *Parser) parseFactor() ast.Expr {
	if p.at(token.PLUS) || p.at(token.MINUS) || p.at(token.TILDE) {
		op := p.cur().Type
		pos := p.cur().Pos
		p.advance()
		x := p.parseFactor()
		return &ast.UnaryExpr{Base: ast.Base{Position: pos}, Op: op, X: x}
	}
	return p.parsePower()
}

func (p *Parser) parsePower() ast.Expr {
	x := p.parsePrimary()
	if p.at(token.DOUBLESTAR) {
		pos := p.cur().Pos
		p.advance()
		y := p.parseFactor() // right-associative
		return &ast.BinaryExpr{Base: ast.Base{Position: pos}, Op: token.DOUBLESTAR, X: x, Y: y}
	}
	return x
}

func (p *Parser) parsePrimary() ast.Expr {
	x := p.parseAtom()
	for {
		switch p.cur().Type {
		case token.LPAREN:
			x = p.parseCall(x)
		case token.DOT:
			pos := p.cur().Pos
			p.advance()
			name := p.expect(token.NAME)
			x = &ast.Attribute{Base: ast.Base{Position: pos}, X: x, Name: name.Literal}
		case token.LBRACKET:
			pos := p.cur().Pos
			p.advance()
			idx := p.parseExpression()
			p.expect(token.RBRACKET)
			x = &ast.Subscript{Base: ast.Base{Position: pos}, X: x, Index: idx}
		default:
			return x
		}
	}
}

// parseCall parses `fn(arg, arg, ...)` (positional args only).
func (p *Parser) parseCall(fn ast.Expr) ast.Expr {
	pos := p.cur().Pos
	p.advance() // (
	var args []ast.Expr
	for !p.at(token.RPAREN) && !p.at(token.EOF) {
		args = append(args, p.parseExpression())
		if p.at(token.COMMA) {
			p.advance()
			continue
		}
		break
	}
	p.expect(token.RPAREN)
	return &ast.CallExpr{Base: ast.Base{Position: pos}, Fn: fn, Args: args}
}

func (p *Parser) parseAtom() ast.Expr {
	t := p.cur()
	switch t.Type {
	case token.NAME:
		p.advance()
		return &ast.Name{Base: ast.Base{Position: t.Pos}, Name: t.Literal}
	case token.TRUE:
		p.advance()
		return &ast.BoolLit{Base: ast.Base{Position: t.Pos}, Value: true}
	case token.FALSE:
		p.advance()
		return &ast.BoolLit{Base: ast.Base{Position: t.Pos}, Value: false}
	case token.NONE:
		p.advance()
		return &ast.NoneLit{Base: ast.Base{Position: t.Pos}}
	case token.INT:
		p.advance()
		v, _ := strconv.ParseInt(t.Literal, 0, 64)
		return &ast.IntLit{Base: ast.Base{Position: t.Pos}, Value: v}
	case token.FLOAT:
		p.advance()
		v, _ := strconv.ParseFloat(t.Literal, 64)
		return &ast.FloatLit{Base: ast.Base{Position: t.Pos}, Value: v}
	case token.STRING, token.FSTRING:
		return p.parseString()
	case token.LPAREN:
		return p.parseGroup()
	case token.LBRACKET:
		return p.parseList()
	case token.LBRACE:
		return p.parseDict()
	case token.LAMBDA:
		return p.parseLambda()
	default:
		p.errs.Add(t.Pos, token.SyntaxError, "unexpected token %s", t.Type)
		if !p.at(token.EOF) {
			p.advance()
		}
		return &ast.NoneLit{Base: ast.Base{Position: t.Pos}}
	}
}

// parseString folds adjacent string literals into one StrLit (compile-time
// concatenation, docs/spec/01-lexical.md).
func (p *Parser) parseString() ast.Expr {
	t := p.cur()
	if t.Type == token.FSTRING {
		p.advance()
		return p.parseFStringToken(t)
	}
	value := t.Literal
	p.advance()
	for p.at(token.STRING) {
		value += p.cur().Literal
		p.advance()
	}
	return &ast.StrLit{Base: ast.Base{Position: t.Pos}, Value: value}
}

// parseGroup parses a parenthesized expression or tuple display.
func (p *Parser) parseGroup() ast.Expr {
	pos := p.cur().Pos
	p.advance() // (
	if p.at(token.RPAREN) {
		p.advance()
		return &ast.TupleLit{Base: ast.Base{Position: pos}}
	}
	inner := p.parseExpression()
	if !p.at(token.COMMA) {
		p.expect(token.RPAREN)
		return inner
	}
	elems := []ast.Expr{inner}
	for p.at(token.COMMA) {
		p.advance()
		if p.at(token.RPAREN) || p.at(token.EOF) {
			break
		}
		elems = append(elems, p.parseExpression())
	}
	p.expect(token.RPAREN)
	return &ast.TupleLit{Base: ast.Base{Position: pos}, Elems: elems}
}

func (p *Parser) parseList() ast.Expr {
	pos := p.cur().Pos
	p.advance()
	var elems []ast.Expr
	for !p.at(token.RBRACKET) && !p.at(token.EOF) {
		elem := p.parseExpression()
		if p.at(token.FOR) {
			clauses := p.parseComprehensionClauses()
			p.expect(token.RBRACKET)
			return &ast.ListComp{Base: ast.Base{Position: pos}, Elem: elem, Clauses: clauses}
		}
		elems = append(elems, elem)
		if !p.at(token.COMMA) {
			break
		}
		p.advance()
	}
	p.expect(token.RBRACKET)
	return &ast.ListLit{Base: ast.Base{Position: pos}, Elems: elems}
}

func (p *Parser) parseDict() ast.Expr {
	pos := p.cur().Pos
	p.advance()
	var keys, values []ast.Expr
	for !p.at(token.RBRACE) && !p.at(token.EOF) {
		key := p.parseExpression()
		if !p.at(token.COLON) {
			if p.at(token.FOR) {
				clauses := p.parseComprehensionClauses()
				p.expect(token.RBRACE)
				return &ast.SetComp{Base: ast.Base{Position: pos}, Elem: key, Clauses: clauses}
			}
			elems := []ast.Expr{key}
			for p.at(token.COMMA) {
				p.advance()
				if p.at(token.RBRACE) || p.at(token.EOF) {
					break
				}
				elem := p.parseExpression()
				if p.at(token.FOR) {
					clauses := p.parseComprehensionClauses()
					p.expect(token.RBRACE)
					return &ast.SetComp{Base: ast.Base{Position: pos}, Elem: elem, Clauses: clauses}
				}
				elems = append(elems, elem)
			}
			p.expect(token.RBRACE)
			return &ast.SetLit{Base: ast.Base{Position: pos}, Elems: elems}
		}
		p.advance()
		keys = append(keys, key)
		value := p.parseExpression()
		if p.at(token.FOR) {
			clauses := p.parseComprehensionClauses()
			p.expect(token.RBRACE)
			return &ast.DictComp{Base: ast.Base{Position: pos}, Key: key, Value: value, Clauses: clauses}
		}
		values = append(values, value)
		if !p.at(token.COMMA) {
			break
		}
		p.advance()
	}
	p.expect(token.RBRACE)
	return &ast.DictLit{Base: ast.Base{Position: pos}, Keys: keys, Values: values}
}

func (p *Parser) parseComprehensionClauses() []*ast.Comprehension {
	var clauses []*ast.Comprehension
	for p.at(token.FOR) {
		pos := p.cur().Pos
		p.advance()
		t := p.expect(token.NAME)
		target := &ast.Name{Base: ast.Base{Position: t.Pos}, Name: t.Literal}
		p.expect(token.IN)
		iter := p.parseDisjunction()
		var ifs []ast.Expr
		for p.at(token.IF) {
			p.advance()
			ifs = append(ifs, p.parseDisjunction())
		}
		clauses = append(clauses, &ast.Comprehension{Base: ast.Base{Position: pos}, Target: target, Iter: iter, Ifs: ifs})
	}
	return clauses
}

func (p *Parser) parseFStringToken(t token.Token) ast.Expr {
	return &ast.FString{Base: ast.Base{Position: t.Pos}, Parts: p.parseFStringParts(t.Literal, t.Pos)}
}

func (p *Parser) parseFStringParts(s string, pos token.Pos) []ast.FStringPart {
	var parts []ast.FStringPart
	var text strings.Builder
	flush := func() {
		if text.Len() > 0 {
			parts = append(parts, &ast.FStringText{Base: ast.Base{Position: pos}, Value: text.String()})
			text.Reset()
		}
	}
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '{':
			if i+1 < len(s) && s[i+1] == '{' {
				text.WriteByte('{')
				i++
				continue
			}
			flush()
			body, end, ok := fstringField(s, i+1)
			if !ok {
				p.errs.Add(pos, token.SyntaxError, "unterminated f-string replacement field")
				return parts
			}
			parts = append(parts, p.parseFStringField(body, pos))
			i = end
		case '}':
			if i+1 < len(s) && s[i+1] == '}' {
				text.WriteByte('}')
				i++
				continue
			}
			p.errs.Add(pos, token.SyntaxError, "single '}' is not allowed in f-string")
		default:
			text.WriteByte(s[i])
		}
	}
	flush()
	return parts
}

func fstringField(s string, start int) (string, int, bool) {
	depth := 0
	for i := start; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			if depth == 0 {
				return s[start:i], i, true
			}
			depth--
		}
	}
	return "", len(s), false
}

func (p *Parser) parseFStringField(body string, pos token.Pos) ast.FStringPart {
	exprSrc, debug, conv, format := splitFStringField(body)
	expr, err := parseFStringExpr(exprSrc)
	if err != nil {
		p.errs.Add(pos, token.SyntaxError, "invalid f-string expression")
		expr = &ast.NoneLit{Base: ast.Base{Position: pos}}
	}
	var formatParts []ast.FStringPart
	if format != "" {
		formatParts = p.parseFStringParts(format, pos)
	}
	return &ast.FStringExpr{Base: ast.Base{Position: pos}, Expr: expr, Debug: debug, Conversion: conv, Format: formatParts}
}

func splitFStringField(body string) (expr, debug string, conv rune, format string) {
	cut := len(body)
	for i, r := range body {
		if r == '!' || r == ':' || r == '=' {
			cut = i
			break
		}
	}
	expr = strings.TrimSpace(body[:cut])
	rest := body[cut:]
	if strings.HasPrefix(rest, "=") {
		debug = body[:cut] + "="
		rest = rest[1:]
	}
	if strings.HasPrefix(rest, "!") && len(rest) >= 2 {
		conv = rune(rest[1])
		rest = rest[2:]
	}
	if strings.HasPrefix(rest, ":") {
		format = rest[1:]
	}
	return expr, debug, conv, format
}

func parseFStringExpr(src string) (ast.Expr, error) {
	mod, err := Parse(strings.NewReader(src + "\n"))
	if err != nil {
		return nil, err
	}
	if len(mod.Body) != 1 {
		return nil, token.ErrorList{}
	}
	stmt, ok := mod.Body[0].(*ast.ExprStmt)
	if !ok {
		return nil, token.ErrorList{}
	}
	return stmt.X, nil
}

func (p *Parser) requireTarget(target ast.Expr) {
	switch target.(type) {
	case *ast.Name, *ast.Subscript, *ast.TupleLit, *ast.Attribute:
	default:
		p.errs.Add(target.Pos(), token.SyntaxError, "cannot assign to this expression")
	}
}

func (p *Parser) expect(tt token.Type) token.Token {
	if p.at(tt) {
		t := p.cur()
		p.advance()
		return t
	}
	p.errs.Add(p.cur().Pos, token.SyntaxError, "expected %s, got %s", tt, p.cur().Type)
	return p.cur()
}

func (p *Parser) expectLineEnd() {
	switch p.cur().Type {
	case token.NEWLINE:
		p.advance()
	case token.EOF:
		// fine
	default:
		p.errs.Add(p.cur().Pos, token.SyntaxError, "expected end of line, got %s", p.cur().Type)
		p.skipLine()
	}
}

// skipToStmtEnd advances to the next `;`, NEWLINE, or EOF without consuming it.
func (p *Parser) skipToStmtEnd() {
	for !p.at(token.SEMICOLON) && !p.at(token.NEWLINE) && !p.at(token.EOF) {
		p.advance()
	}
}

// skipLine consumes through the next NEWLINE (inclusive).
func (p *Parser) skipLine() {
	for !p.at(token.NEWLINE) && !p.at(token.EOF) {
		p.advance()
	}
	if p.at(token.NEWLINE) {
		p.advance()
	}
}

// skipBlock consumes an INDENT..DEDENT region if one starts here.
func (p *Parser) skipBlock() {
	if !p.at(token.INDENT) {
		return
	}
	depth := 0
	for !p.at(token.EOF) {
		switch p.cur().Type {
		case token.INDENT:
			depth++
		case token.DEDENT:
			depth--
		}
		p.advance()
		if depth == 0 {
			return
		}
	}
}

func (p *Parser) cur() token.Token {
	return p.peek(0)
}

func (p *Parser) peek(n int) token.Token {
	for len(p.buf) <= n {
		p.buf = append(p.buf, p.lex.Next())
	}
	return p.buf[n]
}

func (p *Parser) at(tt token.Type) bool {
	return p.cur().Type == tt
}

// advance consumes the current token. EOF is never consumed, so cur stays at
// EOF once the stream is exhausted.
func (p *Parser) advance() {
	if p.cur().Type == token.EOF {
		return
	}
	p.buf = p.buf[1:]
}
