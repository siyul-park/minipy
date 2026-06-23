package compiler

import (
	"github.com/siyul-park/minipy/ast"
	"github.com/siyul-park/minipy/token"
	"github.com/siyul-park/minipy/types"
)

// global is a module-level binding: its declared type, VM global slot, and
// whether it has been assigned a value yet.
type global struct {
	typ   types.Type
	index int
	init  bool
}

// checker resolves names and types for a module, producing a per-expression
// type table and a global symbol table consumed by the compiler.
type checker struct {
	errs     token.ErrorList
	exprType map[ast.Expr]types.Type
	globals  map[string]*global
	order    []string
	loops    int // enclosing-loop depth, for break/continue validation
}

func newChecker() *checker {
	return &checker{
		exprType: map[ast.Expr]types.Type{},
		globals:  map[string]*global{},
	}
}

// check walks every top-level statement, accumulating diagnostics.
func (c *checker) check(mod *ast.Module) {
	c.checkBlock(mod.Body)
}

// checkBlock checks a statement sequence (a module body or a compound block).
func (c *checker) checkBlock(body []ast.Stmt) {
	for _, s := range body {
		c.stmt(s)
	}
}

func (c *checker) stmt(s ast.Stmt) {
	switch n := s.(type) {
	case *ast.AnnAssign:
		c.annAssign(n)
	case *ast.Assign:
		c.assign(n)
	case *ast.AugAssign:
		c.augAssign(n)
	case *ast.ExprStmt:
		c.expr(n.X)
	case *ast.If:
		c.ifStmt(n)
	case *ast.While:
		c.whileStmt(n)
	case *ast.For:
		c.forStmt(n)
	case *ast.Break:
		if c.loops == 0 {
			c.errs.Add(n.Pos(), token.SyntaxError, "'break' outside loop")
		}
	case *ast.Continue:
		if c.loops == 0 {
			c.errs.Add(n.Pos(), token.SyntaxError, "'continue' outside loop")
		}
	case *ast.Pass:
		// no-op
	}
}

func (c *checker) ifStmt(n *ast.If) {
	c.condition(n.Cond)
	c.checkBlock(n.Body)
	c.checkBlock(n.Orelse)
}

func (c *checker) whileStmt(n *ast.While) {
	c.condition(n.Cond)
	c.loops++
	c.checkBlock(n.Body)
	c.loops--
	c.checkBlock(n.Orelse)
}

// forStmt checks `for NAME in range(...)`. The target is auto-declared int (the
// range element type); its body runs inside a loop for break/continue.
func (c *checker) forStmt(n *ast.For) {
	c.forRange(n)
	g := c.declare(n.Target.Name, types.Int, n.Target.Pos())
	g.init = true
	c.exprType[n.Target] = g.typ
	c.loops++
	c.checkBlock(n.Body)
	c.loops--
	c.checkBlock(n.Orelse)
}

// forRange validates that a for-loop iterable is a range(...) call with 1–3 int
// arguments and, when present, a constant int literal step. Other iterables are
// deferred to M3/M6.
func (c *checker) forRange(n *ast.For) {
	call, ok := n.Iter.(*ast.CallExpr)
	if !ok {
		c.errs.Add(n.Iter.Pos(), token.UnsupportedFeature, "'for' iterates only over range(...) in M1 (other iterables arrive in M3/M6)")
		return
	}
	name, ok := call.Fn.(*ast.Name)
	if !ok || name.Name != "range" {
		c.errs.Add(n.Iter.Pos(), token.UnsupportedFeature, "'for' iterates only over range(...) in M1 (other iterables arrive in M3/M6)")
		for _, a := range call.Args {
			c.expr(a)
		}
		return
	}
	if len(call.Args) < 1 || len(call.Args) > 3 {
		c.errs.Add(call.Pos(), token.ArityMismatch, "range() takes 1 to 3 arguments (%d given)", len(call.Args))
	}
	for i, a := range call.Args {
		at := c.expr(a)
		if at != types.Int && at != types.Invalid {
			c.errs.Add(a.Pos(), token.TypeMismatch, "range() argument must be int, got %s", at)
		}
		if i == 2 {
			switch {
			case !isConstIntLiteral(a):
				c.errs.Add(a.Pos(), token.UnsupportedFeature, "range() step must be a constant int literal in M1")
			case constIntValue(a) == 0:
				c.errs.Add(a.Pos(), token.SyntaxError, "range() step must not be zero")
			}
		}
	}
}

// condition checks that a control-flow test types as bool (no truthiness
// coercion, docs/spec/02-types.md).
func (c *checker) condition(e ast.Expr) {
	t := c.expr(e)
	if t != types.Bool && t != types.Invalid {
		c.errs.Add(e.Pos(), token.TypeMismatch, "condition must be bool, got %s", t)
	}
}

func (c *checker) annAssign(n *ast.AnnAssign) {
	t := types.Invalid
	if name, ok := n.Ann.(*ast.Name); ok {
		if resolved, known := types.Resolve(name.Name); known {
			t = resolved
		} else {
			c.errs.Add(n.Ann.Pos(), token.UnsupportedType, "unknown type %q", name.Name)
		}
	}
	g := c.declare(n.Target.Name, t, n.Pos())
	if n.Value != nil {
		vt := c.expr(n.Value)
		if t != types.Invalid && vt != types.Invalid && !types.AssignableTo(vt, t) {
			c.errs.Add(n.Value.Pos(), token.TypeMismatch, "cannot assign %s to %s %q", vt, t, n.Target.Name)
		}
		g.init = true
	}
}

func (c *checker) assign(n *ast.Assign) {
	name, ok := n.Target.(*ast.Name)
	if !ok {
		return
	}
	vt := c.expr(n.Value)
	g, declared := c.globals[name.Name]
	if !declared {
		c.errs.Add(n.Pos(), token.MissingAnnotation, "global %q needs a type annotation on its first assignment", name.Name)
		g = c.declare(name.Name, vt, n.Pos())
		g.init = true
		c.exprType[name] = vt
		return
	}
	if g.typ != types.Invalid && vt != types.Invalid && !types.AssignableTo(vt, g.typ) {
		c.errs.Add(n.Value.Pos(), token.TypeMismatch, "cannot assign %s to %s %q", vt, g.typ, name.Name)
	}
	g.init = true
	c.exprType[name] = g.typ
}

func (c *checker) augAssign(n *ast.AugAssign) {
	name, ok := n.Target.(*ast.Name)
	if !ok {
		return
	}
	g, declared := c.globals[name.Name]
	if !declared {
		c.errs.Add(n.Pos(), token.UndefinedName, "name %q is not defined", name.Name)
		c.expr(n.Value)
		return
	}
	if !g.init {
		c.errs.Add(n.Pos(), token.UseBeforeDefinition, "name %q used before assignment", name.Name)
	}
	c.exprType[name] = g.typ
	vt := c.expr(n.Value)
	rt := c.binaryType(g.typ, n.Op, vt, n.Pos())
	if rt != types.Invalid && g.typ != types.Invalid && !types.AssignableTo(rt, g.typ) {
		c.errs.Add(n.Pos(), token.TypeMismatch, "result %s is not assignable to %s %q", rt, g.typ, name.Name)
	}
	g.init = true
}

// declare registers a new global or returns the existing one, reporting a type
// change on redeclaration.
func (c *checker) declare(name string, t types.Type, pos token.Pos) *global {
	if g, ok := c.globals[name]; ok {
		if t != types.Invalid && g.typ != types.Invalid && g.typ != t {
			c.errs.Add(pos, token.TypeMismatch, "cannot redeclare %q from %s to %s", name, g.typ, t)
		}
		return g
	}
	g := &global{typ: t, index: len(c.order)}
	c.globals[name] = g
	c.order = append(c.order, name)
	return g
}

// expr types an expression, records the result, and returns it.
func (c *checker) expr(e ast.Expr) types.Type {
	t := c.exprTypeOf(e)
	c.exprType[e] = t
	return t
}

func (c *checker) exprTypeOf(e ast.Expr) types.Type {
	switch n := e.(type) {
	case *ast.IntLit:
		return types.Int
	case *ast.FloatLit:
		return types.Float
	case *ast.BoolLit:
		return types.Bool
	case *ast.StrLit:
		return types.Str
	case *ast.NoneLit:
		return types.None
	case *ast.Name:
		return c.nameType(n)
	case *ast.UnaryExpr:
		return c.unaryType(n)
	case *ast.BinaryExpr:
		return c.binary(n)
	case *ast.BoolOp:
		return c.boolOpType(n)
	case *ast.Compare:
		return c.compareType(n)
	case *ast.CallExpr:
		return c.callType(n)
	case *ast.IfExp:
		return c.ifExpType(n)
	default:
		return types.Invalid
	}
}

// ifExpType types the conditional expression `body if cond else orelse`: cond
// must be bool and the two arms must share a type (docs/spec/04-static-semantics.md).
func (c *checker) ifExpType(n *ast.IfExp) types.Type {
	c.condition(n.Cond)
	bt := c.expr(n.Body)
	et := c.expr(n.Orelse)
	if bt == types.Invalid || et == types.Invalid {
		return types.Invalid
	}
	if bt != et {
		c.errs.Add(n.Pos(), token.TypeMismatch, "conditional expression arms have different types: %s and %s", bt, et)
		return types.Invalid
	}
	return bt
}

func (c *checker) nameType(n *ast.Name) types.Type {
	g, ok := c.globals[n.Name]
	if !ok {
		c.errs.Add(n.Pos(), token.UndefinedName, "name %q is not defined", n.Name)
		return types.Invalid
	}
	if !g.init {
		c.errs.Add(n.Pos(), token.UseBeforeDefinition, "name %q used before assignment", n.Name)
	}
	return g.typ
}

func (c *checker) unaryType(n *ast.UnaryExpr) types.Type {
	t := c.expr(n.X)
	switch n.Op {
	case token.NOT:
		if t != types.Bool && t != types.Invalid {
			c.errs.Add(n.Pos(), token.TypeMismatch, "'not' requires bool, got %s", t)
		}
		return types.Bool
	case token.MINUS, token.PLUS:
		if t.IsNumeric() {
			return t
		}
		if t != types.Invalid {
			c.errs.Add(n.Pos(), token.TypeMismatch, "bad operand type for unary %s: %s", n.Op, t)
		}
		return types.Invalid
	case token.TILDE:
		if t == types.Int {
			return types.Int
		}
		if t != types.Invalid {
			c.errs.Add(n.Pos(), token.TypeMismatch, "bad operand type for unary ~: %s", t)
		}
		return types.Invalid
	default:
		return types.Invalid
	}
}

func (c *checker) binary(n *ast.BinaryExpr) types.Type {
	lt := c.expr(n.X)
	rt := c.expr(n.Y)
	return c.binaryType(lt, n.Op, rt, n.Pos())
}

// binaryType applies the arithmetic/bitwise/shift typing rules
// (docs/spec/04-static-semantics.md). Mixed int/float and bool arithmetic are
// rejected; `str + str` is the only non-numeric case.
func (c *checker) binaryType(lt types.Type, op token.Type, rt types.Type, pos token.Pos) types.Type {
	if lt == types.Invalid || rt == types.Invalid {
		return types.Invalid
	}
	switch op {
	case token.PLUS:
		if lt == types.Str && rt == types.Str {
			return types.Str
		}
		return c.arith(lt, op, rt, pos)
	case token.MINUS, token.STAR, token.DOUBLESLASH, token.PERCENT, token.DOUBLESTAR:
		return c.arith(lt, op, rt, pos)
	case token.SLASH:
		if lt == types.Int && rt == types.Int {
			return types.Float
		}
		if lt == types.Float && rt == types.Float {
			return types.Float
		}
		return c.mismatch(op, lt, rt, pos)
	case token.AMP, token.PIPE, token.CARET, token.LSHIFT, token.RSHIFT:
		if lt == types.Int && rt == types.Int {
			return types.Int
		}
		return c.mismatch(op, lt, rt, pos)
	default:
		return types.Invalid
	}
}

func (c *checker) arith(lt types.Type, op token.Type, rt types.Type, pos token.Pos) types.Type {
	if lt == types.Int && rt == types.Int {
		return types.Int
	}
	if lt == types.Float && rt == types.Float {
		return types.Float
	}
	return c.mismatch(op, lt, rt, pos)
}

func (c *checker) mismatch(op token.Type, lt, rt types.Type, pos token.Pos) types.Type {
	c.errs.Add(pos, token.TypeMismatch, "unsupported operand type(s) for %s: %s and %s", op, lt, rt)
	return types.Invalid
}

func (c *checker) boolOpType(n *ast.BoolOp) types.Type {
	lt := c.expr(n.X)
	rt := c.expr(n.Y)
	if (lt != types.Bool && lt != types.Invalid) || (rt != types.Bool && rt != types.Invalid) {
		c.errs.Add(n.Pos(), token.TypeMismatch, "'%s' requires bool operands, got %s and %s", n.Op, lt, rt)
	}
	return types.Bool
}

func (c *checker) compareType(n *ast.Compare) types.Type {
	prev := c.expr(n.X)
	for i, op := range n.Ops {
		rt := c.expr(n.Comparators[i])
		c.checkComparable(op, prev, rt, n.Pos())
		prev = rt
	}
	return types.Bool
}

func (c *checker) checkComparable(op token.Type, lt, rt types.Type, pos token.Pos) {
	if lt == types.Invalid || rt == types.Invalid {
		return
	}
	if op == token.IN || op == token.IS {
		return // already reported as UnsupportedFeature by the parser
	}
	if lt == types.None || rt == types.None {
		c.errs.Add(pos, token.UnsupportedFeature, "comparing to None uses 'is' (M7)")
		return
	}
	if lt != rt {
		c.errs.Add(pos, token.NotComparable, "'%s' not supported between instances of %s and %s", op, lt, rt)
	}
}

func (c *checker) callType(n *ast.CallExpr) types.Type {
	name, ok := n.Fn.(*ast.Name)
	if !ok {
		c.errs.Add(n.Pos(), token.UnsupportedFeature, "only direct builtin calls are supported in M0")
		for _, a := range n.Args {
			c.expr(a)
		}
		return types.Invalid
	}

	argTypes := make([]types.Type, len(n.Args))
	for i, a := range n.Args {
		argTypes[i] = c.expr(a)
	}

	if name.Name == "range" {
		c.errs.Add(name.Pos(), token.UnsupportedFeature, "range as a value is M6; in M1 range(...) appears only in a 'for' loop")
		return types.Invalid
	}
	if !isBuiltin(name.Name) {
		c.errs.Add(name.Pos(), token.UndefinedName, "name %q is not defined (user functions arrive in M2)", name.Name)
		return types.Invalid
	}
	if len(argTypes) != 1 {
		c.errs.Add(n.Pos(), token.ArityMismatch, "%s() takes exactly one argument (%d given)", name.Name, len(argTypes))
		return types.Invalid
	}
	if argTypes[0] == types.Invalid {
		return types.Invalid // the argument's own error is already reported
	}
	rt, ok := builtinReturn(name.Name, argTypes[0])
	if !ok {
		c.errs.Add(n.Pos(), token.TypeMismatch, "%s() does not accept %s", name.Name, argTypes[0])
		return types.Invalid
	}
	return rt
}

// isConstIntLiteral reports whether e is an int literal, optionally with a unary
// +/- sign — the only form M1 accepts as a range step.
func isConstIntLiteral(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.IntLit:
		return true
	case *ast.UnaryExpr:
		if x.Op == token.MINUS || x.Op == token.PLUS {
			_, ok := x.X.(*ast.IntLit)
			return ok
		}
	}
	return false
}

// constIntValue evaluates a constant int literal (the form isConstIntLiteral
// accepts) to its int64 value; used by the compiler to fix the range step.
func constIntValue(e ast.Expr) int64 {
	switch x := e.(type) {
	case *ast.IntLit:
		return x.Value
	case *ast.UnaryExpr:
		if x.Op == token.MINUS {
			return -constIntValue(x.X)
		}
		return constIntValue(x.X)
	}
	return 0
}
