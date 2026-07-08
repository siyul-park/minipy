// Package ast defines the minipy abstract syntax tree: a module of statements
// over scalar expressions, control flow, and functions
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

// For is `for Target in Iter: Body [else: Orelse]`. Orelse runs only when the
// loop exits without a break. Flat tuple targets are allowed.
type For struct {
	Base
	Target Expr
	Iter   Expr
	Body   []Stmt
	Orelse []Stmt
	Async  bool
}

// ParamKind distinguishes positional-only, normal, and keyword-only parameters.
type ParamKind int

const (
	ParamNormal ParamKind = iota
	ParamPosOnly
	ParamKwOnly
)

// Param is a function parameter with an optional type annotation and default.
type Param struct {
	Base
	Name    *Name
	Ann     Expr
	Default Expr
	Kind    ParamKind
	Vararg  bool
	Kwarg   bool
}

// Function is `def Name(Params) -> Returns: Body`.
type Function struct {
	Base
	Name           *Name
	Params         []*Param
	Returns        Expr
	Decorators     []*Name
	DecoratorExprs []Expr
	Body           []Stmt
	Async          bool
}

// Class is `class Name[(Base)]: Body`.
type Class struct {
	Base
	Name           *Name
	BaseClass      *Name
	Bases          []Expr
	Keywords       []*Keyword
	Decorators     []*Name
	DecoratorExprs []Expr
	Body           []Stmt
}

// Keyword is a keyword argument in a call or class header. Name is empty for
// `**expr`.
type Keyword struct {
	Base
	Name  string
	Value Expr
}

// ImportAlias is one imported name, optionally renamed with `as`.
type ImportAlias struct {
	Base
	Name string
	As   string
}

// Import is `import a [as b], ...`.
type Import struct {
	Base
	Names []*ImportAlias
}

// ImportFrom is `from module import name [as alias], ...`.
type ImportFrom struct {
	Base
	Module string
	Names  []*ImportAlias
	Level  int
}

// TypeAlias is Python 3.13's soft-keyword `type Name = expr`.
type TypeAlias struct {
	Base
	Name  *Name
	Value Expr
}

// Global is a `global x, y` declaration inside a function.
type Global struct {
	Base
	Names []string
}

// Nonlocal is a `nonlocal x, y` declaration inside a nested function.
type Nonlocal struct {
	Base
	Names []string
}

// Return is a `return` statement. Value is nil for bare `return`.
type Return struct {
	Base
	Value Expr
}

// Yield is a generator suspension statement. Value is nil for bare `yield`.
type Yield struct {
	Base
	Value Expr
}

// Delete is `del target1, target2, ...`. Each target is a Name, Subscript, or
// Attribute lvalue.
type Delete struct {
	Base
	Targets []Expr
}

// Assert is `assert Test[, Msg]`. Msg is nil when absent.
type Assert struct {
	Base
	Test Expr
	Msg  Expr
}

// Match is `match Subject: case ...` structural pattern matching.
type Match struct {
	Base
	Subject Expr
	Cases   []*Case
}

// Try is `try: Body [except ...] [else: Orelse] [finally: Finalbody]`.
type Try struct {
	Base
	Body      []Stmt
	Handlers  []*ExceptHandler
	Orelse    []Stmt
	Finalbody []Stmt
}

// ExceptHandler is one `except [Type] [as Name]: Body` clause.
type ExceptHandler struct {
	Base
	Type Expr
	Name string
	Body []Stmt
	Star bool
}

// Raise is `raise [Exc]`. Exc is nil for a bare re-raise.
type Raise struct {
	Base
	Exc   Expr
	Cause Expr
}

// With is `with Items: Body`.
type With struct {
	Base
	Items []*WithItem
	Body  []Stmt
	Async bool
}

// WithItem is one context manager item, optionally bound by `as`.
type WithItem struct {
	Base
	Context      Expr
	OptionalVars Expr
}

// Case is one `case Pattern [if Guard]: Body` arm of a Match. Guard is nil when
// absent.
type Case struct {
	Base
	Pattern Pattern
	Guard   Expr
	Body    []Stmt
}

// Break is the `break` statement.
type Break struct{ Base }

// Continue is the `continue` statement.
type Continue struct{ Base }

// Pass is the `pass` no-op statement.
type Pass struct{ Base }

// Pattern is a `match` case pattern node.
type Pattern interface {
	Node
	patternNode()
}

// WildcardPattern is `_`; it matches anything and binds nothing.
type WildcardPattern struct{ Base }

// CapturePattern is a bare name `x`; it matches anything and binds the subject.
type CapturePattern struct {
	Base
	Name string
}

// ValuePattern matches by equality against a literal or dotted value expression
// (e.g. `3`, `"s"`, `None`, `Color.RED`).
type ValuePattern struct {
	Base
	Value Expr
}

// SequencePattern is `[p, ...]` or `(p, ...)`. Star is the index of the starred
// element capturing the rest, or -1 when there is none.
type SequencePattern struct {
	Base
	Elems []Pattern
	Star  int
}

// StarPattern is `*rest` inside a SequencePattern; Name is "" for `*_`.
type StarPattern struct {
	Base
	Name string
}

// MappingPattern is `{key: p, ..., **rest}`. Rest is "" when no `**` capture is
// present.
type MappingPattern struct {
	Base
	Keys   []Expr
	Values []Pattern
	Rest   string
}

// ClassPattern is `Class(pos..., name=kw...)`. Class is a Name or dotted
// Attribute; KwNames pairs with Kw by index.
type ClassPattern struct {
	Base
	Class   Expr
	Args    []Pattern
	KwNames []string
	Kw      []Pattern
}

// OrPattern is `p1 | p2 | ...`; it matches when any alternative matches.
type OrPattern struct {
	Base
	Alts []Pattern
}

// AsPattern is `Pattern as Name`; it matches Pattern then binds the subject to
// Name.
type AsPattern struct {
	Base
	Pattern Pattern
	Name    string
}

// IfExp is the conditional expression `Body if Cond else Orelse`.
type IfExp struct {
	Base
	Body   Expr
	Cond   Expr
	Orelse Expr
}

// LambdaExpr is `lambda params: body`; Params carry inferred annotations when
// a Callable context is available.
type LambdaExpr struct {
	Base
	Params []*Param
	Body   Expr
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

// CallExpr is a function call `function(args...)`.
type CallExpr struct {
	Base
	Fn       Expr
	Args     []Expr
	Keywords []*Keyword
	StarArgs []Expr
	Kwargs   Expr
}

// Attribute is `x.name`.
type Attribute struct {
	Base
	X    Expr
	Name string
}

// Subscript is `x[index]`.
type Subscript struct {
	Base
	X     Expr
	Index Expr
}

// Slice is `lower:upper[:step]` inside a subscript.
type Slice struct {
	Base
	Lower Expr
	Upper Expr
	Step  Expr
}

// Starred is `*expr` in displays, calls, or assignment targets.
type Starred struct {
	Base
	X Expr
}

// NamedExpr is `(target := value)`.
type NamedExpr struct {
	Base
	Target *Name
	Value  Expr
}

// AwaitExpr is `await expr` (parse-only until scheduler/runtime support lands).
type AwaitExpr struct {
	Base
	X Expr
}

// YieldExpr is `yield expr` in expression position.
type YieldExpr struct {
	Base
	Value Expr
	From  bool
}

// GeneratorExp is `(elem for ...)`.
type GeneratorExp struct {
	Base
	Elem    Expr
	Clauses []*Comprehension
}

// UnionType is a union annotation `A | B | C`. It appears only in annotation
// position; the checker resolves its members to a types.Union.
type UnionType struct {
	Base
	Members []Expr
}

// ListLit is `[a, b, c]`.
type ListLit struct {
	Base
	Elems []Expr
}

// DictLit is `{k: v}`.
type DictLit struct {
	Base
	Keys   []Expr
	Values []Expr
}

// SetLit is `{a, b, c}`.
type SetLit struct {
	Base
	Elems []Expr
}

// Comprehension is one `for target in iter if ...` clause.
type Comprehension struct {
	Base
	Target *Name
	Iter   Expr
	Ifs    []Expr
	Async  bool
}

// ListComp is `[elem for ...]`.
type ListComp struct {
	Base
	Elem    Expr
	Clauses []*Comprehension
}

// DictComp is `{key: value for ...}`.
type DictComp struct {
	Base
	Key     Expr
	Value   Expr
	Clauses []*Comprehension
}

// SetComp is `{elem for ...}`.
type SetComp struct {
	Base
	Elem    Expr
	Clauses []*Comprehension
}

// TupleLit is `(a, b)` or a flat tuple target `a, b`.
type TupleLit struct {
	Base
	Elems []Expr
}

// FString is an f-string split into literal and formatted expression parts.
type FString struct {
	Base
	Parts []FStringPart
}

// FStringPart is either raw text or a formatted expression.
type FStringPart interface {
	Node
	fstringPartNode()
}

// FStringText is literal text inside an f-string.
type FStringText struct {
	Base
	Value string
}

// FStringExpr is a replacement field inside an f-string.
type FStringExpr struct {
	Base
	Expr       Expr
	Debug      string
	Conversion rune
	Format     []FStringPart
}

// Pos returns the position of the node's first token.
func (b Base) Pos() token.Pos { return b.Position }

func (*AnnAssign) stmtNode()  {}
func (*Assign) stmtNode()     {}
func (*AugAssign) stmtNode()  {}
func (*ExprStmt) stmtNode()   {}
func (*If) stmtNode()         {}
func (*While) stmtNode()      {}
func (*For) stmtNode()        {}
func (*Function) stmtNode()   {}
func (*Class) stmtNode()      {}
func (*Import) stmtNode()     {}
func (*ImportFrom) stmtNode() {}
func (*TypeAlias) stmtNode()  {}
func (*Global) stmtNode()     {}
func (*Nonlocal) stmtNode()   {}
func (*Return) stmtNode()     {}
func (*Yield) stmtNode()      {}
func (*Break) stmtNode()      {}
func (*Continue) stmtNode()   {}
func (*Pass) stmtNode()       {}
func (*Delete) stmtNode()     {}
func (*Assert) stmtNode()     {}
func (*Match) stmtNode()      {}
func (*Try) stmtNode()        {}
func (*Raise) stmtNode()      {}
func (*With) stmtNode()       {}

func (*WildcardPattern) patternNode() {}
func (*CapturePattern) patternNode()  {}
func (*ValuePattern) patternNode()    {}
func (*SequencePattern) patternNode() {}
func (*StarPattern) patternNode()     {}
func (*MappingPattern) patternNode()  {}
func (*ClassPattern) patternNode()    {}
func (*OrPattern) patternNode()       {}
func (*AsPattern) patternNode()       {}

func (*Name) exprNode()         {}
func (*LambdaExpr) exprNode()   {}
func (*IntLit) exprNode()       {}
func (*FloatLit) exprNode()     {}
func (*StrLit) exprNode()       {}
func (*BoolLit) exprNode()      {}
func (*NoneLit) exprNode()      {}
func (*UnaryExpr) exprNode()    {}
func (*BinaryExpr) exprNode()   {}
func (*BoolOp) exprNode()       {}
func (*Compare) exprNode()      {}
func (*CallExpr) exprNode()     {}
func (*IfExp) exprNode()        {}
func (*Attribute) exprNode()    {}
func (*Subscript) exprNode()    {}
func (*Slice) exprNode()        {}
func (*Starred) exprNode()      {}
func (*NamedExpr) exprNode()    {}
func (*AwaitExpr) exprNode()    {}
func (*YieldExpr) exprNode()    {}
func (*GeneratorExp) exprNode() {}
func (*UnionType) exprNode()    {}
func (*ListLit) exprNode()      {}
func (*DictLit) exprNode()      {}
func (*SetLit) exprNode()       {}
func (*ListComp) exprNode()     {}
func (*DictComp) exprNode()     {}
func (*SetComp) exprNode()      {}
func (*TupleLit) exprNode()     {}
func (*FString) exprNode()      {}

func (*FStringText) fstringPartNode() {}
func (*FStringExpr) fstringPartNode() {}
