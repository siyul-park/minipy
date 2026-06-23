// Package parser builds an ast.Module from minipy source. It implements the
// M0–M1 subset of the Python grammar (docs/spec/03-grammar.md): simple
// statements over the full operator-precedence expression chain, plus M1
// control flow (if/elif/else, while, for-in-range, break/continue/pass and the
// conditional expression). Constructs outside the subset are reported as
// UnsupportedFeature with the milestone that introduces them.
package parser

import (
	"io"
	"strconv"

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
	token.DEF:     "'def' (M2 functions)",
	token.CLASS:   "'class' (M5 classes)",
	token.TRY:     "'try' (M7 exceptions)",
	token.EXCEPT:  "'except' (M7 exceptions)",
	token.FINALLY: "'finally' (M7 exceptions)",
	token.WITH:    "'with' (M7 context managers)",
	token.AT:      "decorators (M2)",
}

var simpleKeywordStmt = map[token.Type]string{
	token.RETURN:   "'return' (M2 functions)",
	token.GLOBAL:   "'global' (M4 closures)",
	token.NONLOCAL: "'nonlocal' (M4 closures)",
	token.YIELD:    "'yield' (M6 generators)",
	token.RAISE:    "'raise' (M7 exceptions)",
	token.IMPORT:   "'import' (M8 modules)",
	token.FROM:     "'from' (M8 modules)",
	token.DEL:      "'del' (out of scope)",
	token.ASSERT:   "'assert' (out of scope)",
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

// parseForTarget parses the single NAME loop variable. Tuple-unpacking targets
// (`for k, v in ...`) are an M3 extension.
func (p *Parser) parseForTarget() *ast.Name {
	t := p.cur()
	if t.Type != token.NAME {
		p.errs.Add(t.Pos, token.SyntaxError, "expected a loop variable name, got %s", t.Type)
		return &ast.Name{Base: ast.Base{Position: t.Pos}, Name: ""}
	}
	p.advance()
	if p.at(token.COMMA) {
		p.errs.Add(p.cur().Pos, token.UnsupportedFeature, "tuple-unpacking 'for' targets are M3")
		for p.at(token.COMMA) {
			p.advance()
			if p.at(token.NAME) {
				p.advance()
			}
		}
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

// parseType parses an annotation. M0 accepts only a scalar type name; container,
// optional, and union forms are reported as UnsupportedType.
func (p *Parser) parseType() ast.Expr {
	t := p.cur()
	if t.Type == token.NAME || t.Type == token.NONE {
		p.advance()
		name := t.Literal
		if t.Type == token.NONE {
			name = "None"
		}
		node := &ast.Name{Base: ast.Base{Position: t.Pos}, Name: name}
		if p.at(token.LBRACKET) || p.at(token.PIPE) {
			p.errs.Add(t.Pos, token.UnsupportedType, "container/optional/union annotations arrive later")
			p.skipTypeTail()
		}
		return node
	}
	p.errs.Add(t.Pos, token.UnsupportedType, "expected a type name, got %s", t.Type)
	return &ast.Name{Base: ast.Base{Position: t.Pos}, Name: ""}
}

// parseExpression is the expression entry: a disjunction, optionally followed by
// a conditional `if cond else orelse` (M1). lambda arrives in M4.
func (p *Parser) parseExpression() ast.Expr {
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
		case token.IN, token.IS:
			p.errs.Add(p.cur().Pos, token.UnsupportedFeature, "'%s' comparison is not supported yet", op)
			p.advance()
			ops = append(ops, op)
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
			p.errs.Add(p.cur().Pos, token.UnsupportedFeature, "attribute access is M5")
			p.advance()
			if p.at(token.NAME) {
				p.advance()
			}
		case token.LBRACKET:
			p.errs.Add(p.cur().Pos, token.UnsupportedFeature, "subscripting is M3")
			p.skipBracketed(token.LBRACKET, token.RBRACKET)
		default:
			return x
		}
	}
}

// parseCall parses `fn(arg, arg, ...)` (positional args only in M0).
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
	case token.STRING:
		return p.parseString()
	case token.LPAREN:
		return p.parseGroup()
	case token.LBRACKET:
		p.errs.Add(t.Pos, token.UnsupportedFeature, "list displays are M3")
		p.skipBracketed(token.LBRACKET, token.RBRACKET)
		return &ast.NoneLit{Base: ast.Base{Position: t.Pos}}
	case token.LBRACE:
		p.errs.Add(t.Pos, token.UnsupportedFeature, "dict/set displays are M3")
		p.skipBracketed(token.LBRACE, token.RBRACE)
		return &ast.NoneLit{Base: ast.Base{Position: t.Pos}}
	case token.LAMBDA:
		p.errs.Add(t.Pos, token.UnsupportedFeature, "lambda is M4")
		p.skipToStmtEnd()
		return &ast.NoneLit{Base: ast.Base{Position: t.Pos}}
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
	value := t.Literal
	p.advance()
	for p.at(token.STRING) {
		value += p.cur().Literal
		p.advance()
	}
	return &ast.StrLit{Base: ast.Base{Position: t.Pos}, Value: value}
}

// parseGroup parses a parenthesized expression; tuple displays are M3.
func (p *Parser) parseGroup() ast.Expr {
	p.advance() // (
	inner := p.parseExpression()
	if p.at(token.COMMA) {
		p.errs.Add(p.cur().Pos, token.UnsupportedFeature, "tuple displays are M3")
		for p.at(token.COMMA) {
			p.advance()
			if p.at(token.RPAREN) || p.at(token.EOF) {
				break
			}
			p.parseExpression()
		}
	}
	p.expect(token.RPAREN)
	return inner
}

func (p *Parser) requireTarget(target ast.Expr) {
	if _, ok := target.(*ast.Name); !ok {
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

// skipBracketed consumes a balanced open..close bracket region starting here.
func (p *Parser) skipBracketed(open, closing token.Type) {
	if !p.at(open) {
		return
	}
	depth := 0
	for !p.at(token.EOF) {
		switch p.cur().Type {
		case open:
			depth++
		case closing:
			depth--
		}
		p.advance()
		if depth == 0 {
			return
		}
	}
}

// skipTypeTail consumes a `[...]` or `| ...` annotation tail for recovery.
func (p *Parser) skipTypeTail() {
	if p.at(token.LBRACKET) {
		p.skipBracketed(token.LBRACKET, token.RBRACKET)
		return
	}
	for p.at(token.PIPE) {
		p.advance()
		p.parseType()
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
