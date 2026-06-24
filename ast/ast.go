// Package ast defines the minipy abstract syntax tree: a module of statements
// over scalar expressions, M1 control flow, and M2 functions
// (docs/spec/03-grammar.md). Every node carries the source position of its
// first token.
package ast

import "github.com/siyul-park/minipy/token"

// Node is any AST node; it reports the position of its first token.
type Node interface {
	Pos() token.Pos
}

// Stmt is a statement node.
type Stmt interface {
	Node
	stmtNode()
}

// Expr is an expression node.
type Expr interface {
	Node
	exprNode()
}

// Base carries a node's source position; embed it in every node.
type Base struct {
	Position token.Pos
}

// Pos returns the position of the node's first token.
func (b Base) Pos() token.Pos { return b.Position }

// Module is a whole compilation unit: an ordered list of top-level statements.
type Module struct {
	Base
	Body []Stmt
}

// AnnAssign is an annotated declaration `target: ann [= value]`. Value is nil
// for a bare declaration.
type AnnAssign struct {
	Base
	Target *Name
	Ann    Expr
	Value  Expr
}

// Assign is a plain assignment `target = value`.
type Assign struct {
	Base
	Target Expr
	Value  Expr
}

// AugAssign is an augmented assignment `target <op>= value`; Op is the base
// binary operator (e.g. token.PLUS for `+=`).
type AugAssign struct {
	Base
	Target Expr
	Op     token.Type
	Value  Expr
}

// ExprStmt is an expression evaluated for effect; its value is discarded.
type ExprStmt struct {
	Base
	X Expr
}

// If is `if Cond: Body [else: Orelse]`. An `elif` chain is represented as a
// nested If in Orelse, matching the CPython AST shape.
type If struct {
	Base
	Cond   Expr
	Body   []Stmt
	Orelse []Stmt
}

// While is `while Cond: Body [else: Orelse]`. Orelse runs only when the loop
// exits without a break.
type While struct {
	Base
	Cond   Expr
	Body   []Stmt
	Orelse []Stmt
}

// For is `for Target in Iter: Body [else: Orelse]`. In M1 Iter must be a
// range(...) call; Orelse runs only when the loop exits without a break.
type For struct {
	Base
	Target *Name
	Iter   Expr
	Body   []Stmt
	Orelse []Stmt
}

// Param is a function parameter with a required type annotation.
type Param struct {
	Base
	Name *Name
	Ann  Expr
}

// Function is `def Name(Params) -> Returns: Body`.
type Function struct {
	Base
	Name       *Name
	Params     []*Param
	Returns    Expr
	Decorators []*Name
	Body       []Stmt
}

// Return is a `return` statement. Value is nil for bare `return`.
type Return struct {
	Base
	Value Expr
}

// Break is the `break` statement.
type Break struct{ Base }

// Continue is the `continue` statement.
type Continue struct{ Base }

// Pass is the `pass` no-op statement.
type Pass struct{ Base }

// IfExp is the conditional expression `Body if Cond else Orelse`.
type IfExp struct {
	Base
	Body   Expr
	Cond   Expr
	Orelse Expr
}

// Name is an identifier reference.
type Name struct {
	Base
	Name string
}

// IntLit is an integer literal (int64).
type IntLit struct {
	Base
	Value int64
}

// FloatLit is a floating-point literal (float64).
type FloatLit struct {
	Base
	Value float64
}

// StrLit is a decoded string literal.
type StrLit struct {
	Base
	Value string
}

// BoolLit is `True` or `False`.
type BoolLit struct {
	Base
	Value bool
}

// NoneLit is `None`.
type NoneLit struct {
	Base
}

// UnaryExpr is a prefix operation: `+x`, `-x`, `~x`, or `not x`.
type UnaryExpr struct {
	Base
	Op token.Type
	X  Expr
}

// BinaryExpr is an arithmetic, bitwise, or shift operation.
type BinaryExpr struct {
	Base
	Op   token.Type
	X, Y Expr
}

// BoolOp is a short-circuiting `and`/`or`.
type BoolOp struct {
	Base
	Op   token.Type
	X, Y Expr
}

// Compare is a (possibly chained) comparison `x op y op z ...`.
type Compare struct {
	Base
	X           Expr
	Ops         []token.Type
	Comparators []Expr
}

// CallExpr is a function call `fn(args...)`.
type CallExpr struct {
	Base
	Fn   Expr
	Args []Expr
}

func (*AnnAssign) stmtNode() {}
func (*Assign) stmtNode()    {}
func (*AugAssign) stmtNode() {}
func (*ExprStmt) stmtNode()  {}
func (*If) stmtNode()        {}
func (*While) stmtNode()     {}
func (*For) stmtNode()       {}
func (*Function) stmtNode()  {}
func (*Return) stmtNode()    {}
func (*Break) stmtNode()     {}
func (*Continue) stmtNode()  {}
func (*Pass) stmtNode()      {}

func (*Name) exprNode()       {}
func (*IntLit) exprNode()     {}
func (*FloatLit) exprNode()   {}
func (*StrLit) exprNode()     {}
func (*BoolLit) exprNode()    {}
func (*NoneLit) exprNode()    {}
func (*UnaryExpr) exprNode()  {}
func (*BinaryExpr) exprNode() {}
func (*BoolOp) exprNode()     {}
func (*Compare) exprNode()    {}
func (*CallExpr) exprNode()   {}
func (*IfExp) exprNode()      {}
