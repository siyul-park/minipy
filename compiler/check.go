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

type local struct {
	typ   types.Type
	index int
	init  bool
	boxed bool
}

type param struct {
	name string
	typ  types.Type
}

type fn struct {
	name      string
	params    []param
	ret       types.Type
	inferRet  bool         // return type is inferred from the body (no annotation)
	returns   []types.Type // return expression types collected while inferring
	generator bool
	slot      *global
	local     *local
	locals    map[string]*local
	order     []string
	parent    *fn
	children  map[string]*fn
	captures  map[string]*capture
	capOrder  []string
	globals   map[string]bool
	nonlocal  map[string]bool
}

type classField struct {
	name  string
	typ   types.Type
	index int
	value ast.Expr
	pos   token.Pos
}

type classInfo struct {
	name       string
	typ        *types.Class
	fields     []classField
	fieldIndex map[string]int
	methods    map[string]*fn
	methodBody map[string][]ast.Stmt
	base       *classInfo
	classID    int
	low        int
	high       int
	dataclass  bool
}

type capture struct {
	name  string
	typ   types.Type
	index int
	src   *local
	boxed bool
}

// checker resolves names and types for a module, producing a per-expression
// type table and a global symbol table consumed by the compiler.
type checker struct {
	errs       token.ErrorList
	types      map[ast.Expr]types.Type
	globals    map[string]*global
	funcs      map[string]*fn
	classes    map[string]*classInfo
	lambdas    map[*ast.LambdaExpr]*fn
	order      []string
	classOrder []string
	loops      int // enclosing-loop depth, for break/continue validation
	excepts    int // enclosing-except depth, for bare raise validation
	fn         *fn
	// narrowed overlays flow-sensitive types onto bindings inside a guarded
	// region (isinstance / is-None). nameType consults it first so a use sees
	// the narrowed member type, not the declared union.
	narrowed map[string]types.Type
}

func newChecker() *checker {
	c := &checker{
		types:    map[ast.Expr]types.Type{},
		globals:  map[string]*global{},
		funcs:    map[string]*fn{},
		classes:  map[string]*classInfo{},
		lambdas:  map[*ast.LambdaExpr]*fn{},
		narrowed: map[string]types.Type{},
	}
	c.declareBuiltinExceptions()
	return c
}

// check walks every top-level statement, accumulating diagnostics.
func (c *checker) check(mod *ast.Module) {
	c.declareClasses(mod.Body)
	c.declareFuncs(mod.Body)
	c.checkBlock(mod.Body)
	c.computeClassIntervals()
}

// checkBlock checks a statement sequence (a module body or a compound block).
// When an `if` whose body always returns/raises and has no else precedes other
// statements, the negative narrowing of its condition applies to the rest of
// the block (e.g. `if isinstance(x, int): return ...` narrows x for what
// follows).
func (c *checker) checkBlock(body []ast.Stmt) {
	for i, s := range body {
		c.stmt(s)
		iff, ok := s.(*ast.If)
		if !ok || len(iff.Orelse) != 0 || !blockReturns(iff.Body) {
			continue
		}
		if _, neg := c.narrowings(iff.Cond); len(neg) > 0 {
			rest := body[i+1:]
			c.withNarrow(neg, func() { c.checkBlock(rest) })
			return
		}
	}
}

// withNarrow runs fn with the given bindings narrowed, restoring the previous
// overlay afterward.
func (c *checker) withNarrow(m map[string]types.Type, fn func()) {
	if len(m) == 0 {
		fn()
		return
	}
	type saved struct {
		t  types.Type
		ok bool
	}
	old := make(map[string]saved, len(m))
	for k, v := range m {
		prev, ok := c.narrowed[k]
		old[k] = saved{prev, ok}
		c.narrowed[k] = v
	}
	fn()
	for k, s := range old {
		if s.ok {
			c.narrowed[k] = s.t
		} else {
			delete(c.narrowed, k)
		}
	}
}

// narrowings extracts the type refinements a condition implies for its true
// (pos) and false (neg) branches. It recognizes isinstance(NAME, T) and
// NAME is/is not None, and only narrows bindings whose type is a union or Any.
func (c *checker) narrowings(cond ast.Expr) (pos, neg map[string]types.Type) {
	pos = map[string]types.Type{}
	neg = map[string]types.Type{}
	switch e := cond.(type) {
	case *ast.CallExpr:
		name, ok := e.Fn.(*ast.Name)
		if !ok || name.Name != "isinstance" || len(e.Args) != 2 {
			return
		}
		target, ok := e.Args[0].(*ast.Name)
		if !ok {
			return
		}
		cur := c.currentType(target.Name)
		if !narrowable(cur) {
			return
		}
		t := c.resolveType(e.Args[1])
		if t == types.Invalid {
			return
		}
		pos[target.Name] = t
		if w := types.Without(cur, t); w != types.Invalid {
			neg[target.Name] = w
		}
	case *ast.Compare:
		if len(e.Ops) != 1 {
			return
		}
		target, ok := e.X.(*ast.Name)
		if !ok {
			return
		}
		if _, ok := e.Comparators[0].(*ast.NoneLit); !ok {
			return
		}
		cur := c.currentType(target.Name)
		if !narrowable(cur) {
			return
		}
		switch e.Ops[0] {
		case token.IS:
			pos[target.Name] = types.None
			if w := types.Without(cur, types.None); w != types.Invalid {
				neg[target.Name] = w
			}
		case token.ISNOT:
			if w := types.Without(cur, types.None); w != types.Invalid {
				pos[target.Name] = w
			}
			neg[target.Name] = types.None
		}
	}
	return
}

// currentType returns the binding's current (possibly narrowed) type without
// emitting diagnostics, or Invalid if the name is unknown here.
func (c *checker) currentType(name string) types.Type {
	if t, ok := c.narrowed[name]; ok {
		return t
	}
	if c.fn != nil && !c.fn.globals[name] {
		if l, ok := c.fn.locals[name]; ok {
			return l.typ
		}
		if cap, ok := c.fn.captures[name]; ok {
			return cap.typ
		}
	}
	if g, ok := c.globals[name]; ok {
		return g.typ
	}
	return types.Invalid
}

// narrowable reports whether a binding of type t benefits from flow narrowing.
func narrowable(t types.Type) bool {
	if _, ok := t.(*types.Union); ok {
		return true
	}
	return types.IsAny(t)
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
	case *ast.Function:
		c.function(n)
	case *ast.Class:
		c.classStmt(n)
	case *ast.Global:
		if c.fn == nil {
			c.errs.Add(n.Pos(), token.SyntaxError, "'global' outside function")
			return
		}
		for _, name := range n.Names {
			c.fn.globals[name] = true
		}
	case *ast.Nonlocal:
		if c.fn == nil {
			c.errs.Add(n.Pos(), token.SyntaxError, "'nonlocal' outside function")
			return
		}
		for _, name := range n.Names {
			if c.findEnclosingLocal(name) == nil {
				c.errs.Add(n.Pos(), token.NoBindingForNonlocal, "no binding for nonlocal %q found", name)
				continue
			}
			c.fn.nonlocal[name] = true
		}
	case *ast.Return:
		c.ret(n)
	case *ast.Yield:
		c.yieldStmt(n)
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
	case *ast.Delete:
		c.deleteStmt(n)
	case *ast.Assert:
		c.assertStmt(n)
	case *ast.Match:
		c.matchStmt(n)
	case *ast.Try:
		c.tryStmt(n)
	case *ast.Raise:
		c.raiseStmt(n)
	case *ast.With:
		c.withStmt(n)
	}
}

func (c *checker) tryStmt(n *ast.Try) {
	locals, globals := c.snapshotInits()
	c.checkBlock(n.Body)
	for _, h := range n.Handlers {
		ht := c.handlerType(h)
		if h.Name != "" {
			c.bindCapture(h.Name, ht, h.Pos())
		}
		c.excepts++
		c.checkBlock(h.Body)
		c.excepts--
	}
	c.checkBlock(n.Orelse)
	c.restoreInits(locals, globals)
	c.checkBlock(n.Finalbody)
}

func (c *checker) handlerType(h *ast.ExceptHandler) types.Type {
	if h.Type == nil {
		return c.classes["BaseException"].typ
	}
	info := c.exceptionClass(h.Type)
	if info == nil {
		return types.Invalid
	}
	return info.typ
}

func (c *checker) raiseStmt(n *ast.Raise) {
	if n.Exc == nil {
		if c.excepts == 0 {
			c.errs.Add(n.Pos(), token.SyntaxError, "bare raise outside except")
		}
		return
	}
	t := c.expr(n.Exc)
	if t == types.Invalid {
		return
	}
	cls, ok := t.(*types.Class)
	if !ok || !c.isException(cls.Name) {
		c.errs.Add(n.Pos(), token.TypeMismatch, "raise requires an Exception instance, got %s", t)
	}
}

func (c *checker) withStmt(n *ast.With) {
	for _, item := range n.Items {
		ct := c.expr(item.Context)
		cls, ok := ct.(*types.Class)
		if !ok {
			if ct != types.Invalid {
				c.errs.Add(item.Pos(), token.UnsupportedFeature, "with requires a context manager, got %s", ct)
			}
			continue
		}
		info := c.classes[cls.Name]
		enter := methodInfo(info, "__enter__")
		exit := methodInfo(info, "__exit__")
		if enter == nil || exit == nil {
			c.errs.Add(item.Pos(), token.UnsupportedFeature, "%s is not a context manager", cls.Name)
			continue
		}
		if len(enter.params) != 1 {
			c.errs.Add(item.Pos(), token.ArityMismatch, "%s.__enter__() takes no arguments", cls.Name)
		}
		if len(exit.params) != 1 {
			c.errs.Add(item.Pos(), token.ArityMismatch, "%s.__exit__() takes no arguments", cls.Name)
		}
		if item.OptionalVars != nil {
			name, ok := item.OptionalVars.(*ast.Name)
			if !ok {
				c.errs.Add(item.OptionalVars.Pos(), token.SyntaxError, "with target must be a name")
			} else if c.fn != nil {
				l := c.declareLocal(name.Name, enter.ret, name.Pos())
				l.init = true
				c.types[name] = l.typ
			} else {
				g := c.declare(name.Name, enter.ret, name.Pos())
				g.init = true
				c.types[name] = g.typ
			}
		}
	}
	c.checkBlock(n.Body)
}

func (c *checker) snapshotInits() (map[string]bool, map[string]bool) {
	locals := map[string]bool{}
	if c.fn != nil {
		for name, l := range c.fn.locals {
			locals[name] = l.init
		}
	}
	globals := map[string]bool{}
	for name, g := range c.globals {
		globals[name] = g.init
	}
	return locals, globals
}

func (c *checker) restoreInits(locals, globals map[string]bool) {
	if c.fn != nil {
		for name, l := range c.fn.locals {
			l.init = locals[name]
		}
	}
	for name, g := range c.globals {
		g.init = globals[name]
	}
}

// deleteStmt checks each `del` target. A deleted Name becomes
// definitely-unassigned so a later read reuses UseBeforeDefinition; subscript and
// attribute targets are checked as deletable lvalues.
func (c *checker) deleteStmt(n *ast.Delete) {
	for _, target := range n.Targets {
		switch t := target.(type) {
		case *ast.Name:
			c.deleteName(t)
		case *ast.Subscript:
			ct := c.expr(t.X)
			it := c.expr(t.Index)
			switch ct.(type) {
			case *types.List, *types.Dict:
				c.indexResultType(t, ct, it)
			default:
				if ct != types.Invalid {
					c.errs.Add(t.Pos(), token.UnsupportedFeature, "cannot delete an item from %s", ct)
				}
			}
		case *ast.Attribute:
			rt := c.expr(t.X)
			c.fieldType(t, rt)
		default:
			c.errs.Add(target.Pos(), token.SyntaxError, "cannot delete this expression")
		}
	}
}

func (c *checker) deleteName(n *ast.Name) {
	if c.fn != nil {
		if c.fn.globals[n.Name] {
			c.deleteGlobalName(n)
			return
		}
		if l, ok := c.fn.locals[n.Name]; ok {
			if !l.init {
				c.errs.Add(n.Pos(), token.UseBeforeDefinition, "name %q used before assignment", n.Name)
			}
			l.init = false
			c.types[n] = l.typ
			return
		}
		if cap := c.capture(n); cap != nil {
			c.errs.Add(n.Pos(), token.UnsupportedFeature, "cannot delete captured name %q", n.Name)
			return
		}
	}
	c.deleteGlobalName(n)
}

func (c *checker) deleteGlobalName(n *ast.Name) {
	g, ok := c.globals[n.Name]
	if !ok {
		c.errs.Add(n.Pos(), token.UndefinedName, "name %q is not defined", n.Name)
		return
	}
	if !g.init {
		c.errs.Add(n.Pos(), token.UseBeforeDefinition, "name %q used before assignment", n.Name)
	}
	g.init = false
	c.types[n] = g.typ
}

// assertStmt checks the test types as bool and the optional message is printable.
func (c *checker) assertStmt(n *ast.Assert) {
	c.condition(n.Test)
	if n.Msg != nil {
		mt := c.expr(n.Msg)
		if mt != types.Invalid && !types.Printable(mt) {
			c.errs.Add(n.Msg.Pos(), token.TypeMismatch, "assert message must be printable, got %s", mt)
		}
	}
}

// matchStmt checks the subject, then every case pattern (declaring captures),
// guard, and body.
func (c *checker) matchStmt(n *ast.Match) {
	subjT := c.expr(n.Subject)
	for _, cs := range n.Cases {
		c.checkPattern(cs.Pattern, subjT)
		if cs.Guard != nil {
			c.condition(cs.Guard)
		}
		c.checkBlock(cs.Body)
	}
}

// bindCapture declares a pattern capture variable in the current scope and marks
// it initialized; re-binding the same name must keep a consistent type.
func (c *checker) bindCapture(name string, t types.Type, pos token.Pos) {
	if name == "" || name == "_" {
		return
	}
	if c.fn != nil {
		if l, ok := c.fn.locals[name]; ok {
			if t != types.Invalid && l.typ != types.Invalid && !types.Equal(l.typ, t) {
				c.errs.Add(pos, token.PatternError, "capture %q binds inconsistent types %s and %s", name, l.typ, t)
			}
			l.init = true
			return
		}
		l := c.declareLocal(name, t, pos)
		l.init = true
		return
	}
	if g, ok := c.globals[name]; ok {
		if t != types.Invalid && g.typ != types.Invalid && !types.Equal(g.typ, t) {
			c.errs.Add(pos, token.PatternError, "capture %q binds inconsistent types %s and %s", name, g.typ, t)
		}
		g.init = true
		return
	}
	g := c.declare(name, t, pos)
	g.init = true
}

// checkPattern validates a pattern against the subject type and declares any
// capture variables it introduces.
func (c *checker) checkPattern(p ast.Pattern, subjT types.Type) {
	switch pat := p.(type) {
	case *ast.WildcardPattern:
		// matches anything, binds nothing
	case *ast.CapturePattern:
		c.bindCapture(pat.Name, subjT, pat.Pos())
	case *ast.StarPattern:
		c.bindCapture(pat.Name, types.NewList(subjT), pat.Pos())
	case *ast.AsPattern:
		c.checkPattern(pat.Pattern, subjT)
		c.bindCapture(pat.Name, subjT, pat.Pos())
	case *ast.OrPattern:
		for _, alt := range pat.Alts {
			c.checkPattern(alt, subjT)
		}
	case *ast.ValuePattern:
		vt := c.expr(pat.Value)
		if vt != types.Invalid && subjT != types.Invalid && !types.Equal(vt, subjT) {
			c.errs.Add(pat.Pos(), token.PatternError, "pattern value %s is not comparable to subject %s", vt, subjT)
		}
	case *ast.SequencePattern:
		c.checkSequencePattern(pat, subjT)
	case *ast.MappingPattern:
		c.checkMappingPattern(pat, subjT)
	case *ast.ClassPattern:
		c.checkClassPattern(pat, subjT)
	}
}

func (c *checker) checkSequencePattern(pat *ast.SequencePattern, subjT types.Type) {
	switch s := subjT.(type) {
	case *types.List:
		for _, e := range pat.Elems {
			if star, ok := e.(*ast.StarPattern); ok {
				c.bindCapture(star.Name, types.NewList(s.Elem), star.Pos())
				continue
			}
			c.checkPattern(e, s.Elem)
		}
	case *types.Tuple:
		if pat.Star >= 0 {
			c.errs.Add(pat.Pos(), token.UnsupportedFeature, "starred pattern on a tuple is not supported")
			return
		}
		if len(pat.Elems) != len(s.Elems) {
			c.errs.Add(pat.Pos(), token.PatternError, "sequence pattern expects %d elements, %s has %d", len(pat.Elems), subjT, len(s.Elems))
			return
		}
		for i, e := range pat.Elems {
			c.checkPattern(e, s.Elems[i])
		}
	default:
		if subjT != types.Invalid {
			c.errs.Add(pat.Pos(), token.PatternError, "sequence pattern requires a list or tuple subject, got %s", subjT)
		}
	}
}

func (c *checker) checkMappingPattern(pat *ast.MappingPattern, subjT types.Type) {
	d, ok := subjT.(*types.Dict)
	if !ok {
		if subjT != types.Invalid {
			c.errs.Add(pat.Pos(), token.PatternError, "mapping pattern requires a dict subject, got %s", subjT)
		}
		return
	}
	for i, key := range pat.Keys {
		kt := c.expr(key)
		if kt != types.Invalid && !types.AssignableTo(kt, d.Key) {
			c.errs.Add(key.Pos(), token.PatternError, "mapping key %s does not match dict key %s", kt, d.Key)
		}
		c.checkPattern(pat.Values[i], d.Value)
	}
	if pat.Rest != "" {
		c.bindCapture(pat.Rest, types.NewDict(d.Key, d.Value), pat.Pos())
	}
}

func (c *checker) checkClassPattern(pat *ast.ClassPattern, subjT types.Type) {
	name, ok := pat.Class.(*ast.Name)
	if !ok {
		c.errs.Add(pat.Pos(), token.UnsupportedFeature, "dotted class pattern is not supported")
		return
	}
	info := c.classes[name.Name]
	if info == nil {
		c.errs.Add(pat.Pos(), token.UndefinedName, "class %q is not defined", name.Name)
		return
	}
	if subjT != types.Invalid && !types.Equal(subjT, info.typ) {
		c.errs.Add(pat.Pos(), token.PatternError, "class pattern %s does not match subject %s", name.Name, subjT)
	}
	for i, sub := range pat.Args {
		if i < len(info.fields) {
			c.checkPattern(sub, info.fields[i].typ)
		} else {
			c.errs.Add(sub.Pos(), token.PatternError, "class %s has no positional field %d", name.Name, i)
		}
	}
	for i, kw := range pat.KwNames {
		idx, ok := info.fieldIndex[kw]
		if !ok {
			c.errs.Add(pat.Kw[i].Pos(), token.UndefinedName, "field %q is not defined on %s", kw, name.Name)
			continue
		}
		c.checkPattern(pat.Kw[i], info.fields[idx].typ)
	}
}

func (c *checker) ifStmt(n *ast.If) {
	c.condition(n.Cond)
	pos, neg := c.narrowings(n.Cond)
	c.withNarrow(pos, func() { c.checkBlock(n.Body) })
	c.withNarrow(neg, func() { c.checkBlock(n.Orelse) })
}

func (c *checker) whileStmt(n *ast.While) {
	c.condition(n.Cond)
	c.loops++
	c.checkBlock(n.Body)
	c.loops--
	c.checkBlock(n.Orelse)
}

// forStmt checks `for TARGET in ITERABLE`. The target is auto-declared to the
// iterable element type; its body runs inside a loop for break/continue.
func (c *checker) forStmt(n *ast.For) {
	target := forTargetName(n.Target)
	iter := c.expr(n.Iter)
	elem := iterableElem(iter)
	if elem == types.Invalid {
		c.errs.Add(n.Iter.Pos(), token.NotIterable, "%s is not iterable", iter)
	}
	if tupleTarget, ok := n.Target.(*ast.TupleLit); ok {
		c.bindForTupleTarget(tupleTarget, elem)
	} else if c.fn != nil {
		l := c.declareLocal(target.Name, elem, target.Pos())
		l.init = true
		c.types[target] = l.typ
	} else {
		g := c.declare(target.Name, elem, target.Pos())
		g.init = true
		c.types[target] = g.typ
	}
	c.loops++
	c.checkBlock(n.Body)
	c.loops--
	c.checkBlock(n.Orelse)
}

func iterableElem(t types.Type) types.Type {
	switch x := t.(type) {
	case *types.List:
		return x.Elem
	case *types.Dict:
		return x.Key
	case *types.Set:
		return x.Elem
	case *types.Iterator:
		return x.Elem
	default:
		if types.Equal(t, types.Str) {
			return types.Str
		}
		return types.Invalid
	}
}

func (c *checker) bindForTupleTarget(target *ast.TupleLit, elem types.Type) {
	tuple, ok := elem.(*types.Tuple)
	if !ok {
		if elem != types.Invalid {
			c.errs.Add(target.Pos(), token.TypeMismatch, "tuple target cannot unpack %s", elem)
		}
		return
	}
	if len(tuple.Elems) != len(target.Elems) {
		c.errs.Add(target.Pos(), token.ArityMismatch, "for target needs %d values, got %d", len(target.Elems), len(tuple.Elems))
		return
	}
	for i, e := range target.Elems {
		name, ok := e.(*ast.Name)
		if !ok {
			c.errs.Add(e.Pos(), token.SyntaxError, "for tuple target must be a name")
			continue
		}
		if c.fn != nil {
			l := c.declareLocal(name.Name, tuple.Elems[i], name.Pos())
			l.init = true
			c.types[name] = l.typ
			continue
		}
		g := c.declare(name.Name, tuple.Elems[i], name.Pos())
		g.init = true
		c.types[name] = g.typ
	}
}

func forTargetName(e ast.Expr) *ast.Name {
	if name, ok := e.(*ast.Name); ok {
		return name
	}
	return &ast.Name{Base: ast.Base{Position: e.Pos()}, Name: ""}
}

// condition checks that a control-flow test types as bool (no truthiness
// coercion, docs/spec/02-types.md).
func (c *checker) condition(e ast.Expr) {
	t := c.expr(e)
	if !types.Equal(t, types.Bool) && t != types.Invalid {
		c.errs.Add(e.Pos(), token.TypeMismatch, "condition must be bool, got %s", t)
	}
}

func (c *checker) annAssign(n *ast.AnnAssign) {
	t := c.resolveType(n.Ann)
	var g *global
	var l *local
	if c.fn != nil {
		l = c.declareLocal(n.Target.Name, t, n.Pos())
	} else {
		g = c.declare(n.Target.Name, t, n.Pos())
	}
	if n.Value != nil {
		vt := c.exprWithHint(n.Value, t)
		if t != types.Invalid && vt != types.Invalid && !types.AssignableTo(vt, t) {
			c.errs.Add(n.Value.Pos(), token.TypeMismatch, "cannot assign %s to %s %q", vt, t, n.Target.Name)
		}
		if l != nil {
			l.init = true
		} else {
			g.init = true
		}
	}
}

func (c *checker) assign(n *ast.Assign) {
	name, ok := n.Target.(*ast.Name)
	if !ok {
		c.assignTarget(n.Target, n.Value, n.Pos())
		return
	}
	if c.fn == nil {
		if _, isFunc := c.funcs[name.Name]; isFunc {
			c.errs.Add(n.Pos(), token.TypeMismatch, "cannot assign to function %q", name.Name)
			c.expr(n.Value)
			return
		}
	}
	vt := c.expr(n.Value)
	if c.fn != nil {
		if c.fn.globals[name.Name] {
			goto global
		}
		if c.fn.nonlocal[name.Name] {
			cap := c.capture(name)
			if cap == nil {
				c.expr(n.Value)
				return
			}
			vt := c.expr(n.Value)
			if cap.typ != types.Invalid && vt != types.Invalid && !types.AssignableTo(vt, cap.typ) {
				c.errs.Add(n.Value.Pos(), token.TypeMismatch, "cannot assign %s to %s %q", vt, cap.typ, name.Name)
			}
			cap.boxed = true
			cap.src.boxed = true
			c.types[name] = cap.typ
			return
		}
		l, declared := c.fn.locals[name.Name]
		if !declared {
			l = c.declareLocal(name.Name, vt, n.Pos())
		}
		if l.typ != types.Invalid && vt != types.Invalid && !types.AssignableTo(vt, l.typ) {
			c.errs.Add(n.Value.Pos(), token.TypeMismatch, "cannot assign %s to %s %q", vt, l.typ, name.Name)
		}
		l.init = true
		c.types[name] = l.typ
		return
	}
global:
	g, declared := c.globals[name.Name]
	if !declared {
		// Whole-program inference: an unannotated global takes the type of its
		// first assignment instead of requiring an annotation.
		g = c.declare(name.Name, vt, n.Pos())
		g.init = true
		c.types[name] = vt
		return
	}
	if g.typ != types.Invalid && vt != types.Invalid && !types.AssignableTo(vt, g.typ) {
		c.errs.Add(n.Value.Pos(), token.TypeMismatch, "cannot assign %s to %s %q", vt, g.typ, name.Name)
	}
	g.init = true
	c.types[name] = g.typ
}

func (c *checker) assignTarget(target ast.Expr, value ast.Expr, pos token.Pos) {
	switch t := target.(type) {
	case *ast.Subscript:
		ct := c.expr(t.X)
		it := c.expr(t.Index)
		vt := c.expr(value)
		elem := c.indexResultType(t, ct, it)
		if elem != types.Invalid && vt != types.Invalid && !types.AssignableTo(vt, elem) {
			c.errs.Add(value.Pos(), token.TypeMismatch, "cannot assign %s to indexed %s", vt, elem)
		}
	case *ast.Attribute:
		rt := c.expr(t.X)
		ft := c.fieldType(t, rt)
		vt := c.expr(value)
		if ft != types.Invalid && vt != types.Invalid && !types.AssignableTo(vt, ft) {
			c.errs.Add(value.Pos(), token.TypeMismatch, "cannot assign %s to field %s", vt, ft)
		}
	case *ast.TupleLit:
		vt := c.expr(value)
		c.unpackAssign(t, vt, value.Pos())
	default:
		c.errs.Add(pos, token.SyntaxError, "cannot assign to this expression")
		c.expr(value)
	}
}

func (c *checker) unpackAssign(target *ast.TupleLit, vt types.Type, pos token.Pos) {
	var elems []types.Type
	switch t := vt.(type) {
	case *types.Tuple:
		elems = t.Elems
	case *types.List:
		elems = make([]types.Type, len(target.Elems))
		for i := range elems {
			elems[i] = t.Elem
		}
	default:
		if vt != types.Invalid {
			c.errs.Add(pos, token.TypeMismatch, "cannot unpack %s", vt)
		}
		return
	}
	if len(elems) != len(target.Elems) {
		c.errs.Add(pos, token.ArityMismatch, "unpack needs %d values, got %d", len(target.Elems), len(elems))
		return
	}
	for i, elem := range target.Elems {
		name, ok := elem.(*ast.Name)
		if !ok {
			c.errs.Add(elem.Pos(), token.SyntaxError, "tuple unpack target must be a name")
			continue
		}
		if c.fn != nil {
			l, declared := c.fn.locals[name.Name]
			if !declared {
				l = c.declareLocal(name.Name, elems[i], name.Pos())
			}
			if !types.AssignableTo(elems[i], l.typ) {
				c.errs.Add(name.Pos(), token.TypeMismatch, "cannot assign %s to %s %q", elems[i], l.typ, name.Name)
			}
			l.init = true
			c.types[name] = l.typ
			continue
		}
		g, declared := c.globals[name.Name]
		if !declared {
			// Whole-program inference: infer the unannotated global from the
			// unpacked element type instead of requiring an annotation.
			g = c.declare(name.Name, elems[i], name.Pos())
			g.init = true
			c.types[name] = g.typ
			continue
		}
		if !types.AssignableTo(elems[i], g.typ) {
			c.errs.Add(name.Pos(), token.TypeMismatch, "cannot assign %s to %s %q", elems[i], g.typ, name.Name)
		}
		g.init = true
		c.types[name] = g.typ
	}
}

func (c *checker) augAssign(n *ast.AugAssign) {
	name, ok := n.Target.(*ast.Name)
	if !ok {
		attr, ok := n.Target.(*ast.Attribute)
		if !ok {
			c.errs.Add(n.Pos(), token.UnsupportedFeature, "augmented assignment target is not supported")
			c.expr(n.Value)
			return
		}
		rt := c.expr(attr.X)
		ft := c.fieldType(attr, rt)
		vt := c.expr(n.Value)
		rt2 := c.binaryType(ft, n.Op, vt, n.Pos())
		if rt2 != types.Invalid && ft != types.Invalid && !types.AssignableTo(rt2, ft) {
			c.errs.Add(n.Pos(), token.TypeMismatch, "result %s is not assignable to field %s", rt2, ft)
		}
		return
	}
	if c.fn != nil {
		if c.fn.globals[name.Name] {
			goto global
		}
		if c.fn.nonlocal[name.Name] {
			cap := c.capture(name)
			if cap == nil {
				c.expr(n.Value)
				return
			}
			c.types[name] = cap.typ
			vt := c.expr(n.Value)
			rt := c.binaryType(cap.typ, n.Op, vt, n.Pos())
			if rt != types.Invalid && cap.typ != types.Invalid && !types.AssignableTo(rt, cap.typ) {
				c.errs.Add(n.Pos(), token.TypeMismatch, "result %s is not assignable to %s %q", rt, cap.typ, name.Name)
			}
			cap.boxed = true
			cap.src.boxed = true
			return
		}
		l, declared := c.fn.locals[name.Name]
		if !declared {
			c.errs.Add(n.Pos(), token.UndefinedName, "name %q is not defined", name.Name)
			c.expr(n.Value)
			return
		}
		if !l.init {
			c.errs.Add(n.Pos(), token.UseBeforeDefinition, "name %q used before assignment", name.Name)
		}
		c.types[name] = l.typ
		vt := c.expr(n.Value)
		rt := c.binaryType(l.typ, n.Op, vt, n.Pos())
		if rt != types.Invalid && l.typ != types.Invalid && !types.AssignableTo(rt, l.typ) {
			c.errs.Add(n.Pos(), token.TypeMismatch, "result %s is not assignable to %s %q", rt, l.typ, name.Name)
		}
		l.init = true
		return
	}
global:
	g, declared := c.globals[name.Name]
	if !declared {
		c.errs.Add(n.Pos(), token.UndefinedName, "name %q is not defined", name.Name)
		c.expr(n.Value)
		return
	}
	if !g.init {
		c.errs.Add(n.Pos(), token.UseBeforeDefinition, "name %q used before assignment", name.Name)
	}
	c.types[name] = g.typ
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
		if _, isFunc := c.funcs[name]; isFunc && t != types.Invalid {
			c.errs.Add(pos, token.TypeMismatch, "cannot redeclare function %q", name)
			return g
		}
		if _, isClass := c.classes[name]; isClass && t != types.Invalid {
			c.errs.Add(pos, token.TypeMismatch, "cannot redeclare class %q", name)
			return g
		}
		if t != types.Invalid && g.typ != types.Invalid && !types.Equal(g.typ, t) {
			c.errs.Add(pos, token.TypeMismatch, "cannot redeclare %q from %s to %s", name, g.typ, t)
		}
		return g
	}
	if _, isClass := c.classes[name]; isClass && t != types.Invalid {
		c.errs.Add(pos, token.TypeMismatch, "cannot redeclare class %q", name)
	}
	g := &global{typ: t, index: len(c.order)}
	c.globals[name] = g
	c.order = append(c.order, name)
	return g
}

func (c *checker) declareLocal(name string, t types.Type, pos token.Pos) *local {
	if l, ok := c.fn.locals[name]; ok {
		if t != types.Invalid && l.typ != types.Invalid && !types.Equal(l.typ, t) {
			c.errs.Add(pos, token.TypeMismatch, "cannot redeclare %q from %s to %s", name, l.typ, t)
		}
		return l
	}
	l := &local{typ: t, index: len(c.fn.params) + len(c.fn.order)}
	c.fn.locals[name] = l
	c.fn.order = append(c.fn.order, name)
	return l
}

func (c *checker) declareFuncLocal(info *fn, pos token.Pos) {
	if info.local != nil {
		return
	}
	info.local = c.declareLocal(info.name, types.NewCallable(srcTypes(info.params), info.ret), pos)
}

func (c *checker) declareFuncs(body []ast.Stmt) {
	for _, s := range body {
		f, ok := s.(*ast.Function)
		if !ok {
			continue
		}
		if _, exists := c.funcs[f.Name.Name]; exists {
			c.errs.Add(f.Name.Pos(), token.TypeMismatch, "cannot redeclare function %q", f.Name.Name)
			continue
		}
		if _, exists := c.classes[f.Name.Name]; exists {
			c.errs.Add(f.Name.Pos(), token.TypeMismatch, "cannot redeclare class %q as a function", f.Name.Name)
			continue
		}
		if _, exists := c.globals[f.Name.Name]; exists {
			c.errs.Add(f.Name.Pos(), token.TypeMismatch, "cannot redeclare %q as a function", f.Name.Name)
			continue
		}
		info := &fn{
			name:      f.Name.Name,
			generator: containsYield(f.Body),
			locals:    map[string]*local{},
			children:  map[string]*fn{},
			captures:  map[string]*capture{},
			globals:   map[string]bool{},
			nonlocal:  map[string]bool{},
		}
		if f.Returns == nil {
			info.inferRet = true
			info.ret = types.None // refined from collected returns after the body
		} else {
			info.ret = c.resolveType(f.Returns)
		}
		for _, p := range f.Params {
			info.params = append(info.params, param{name: p.Name.Name, typ: c.paramType(p)})
		}
		info.slot = c.declare(f.Name.Name, types.Invalid, f.Pos())
		c.funcs[f.Name.Name] = info
	}
}

func (c *checker) declareClasses(body []ast.Stmt) {
	for _, s := range body {
		cls, ok := s.(*ast.Class)
		if !ok {
			continue
		}
		name := cls.Name.Name
		if _, exists := c.classes[name]; exists {
			c.errs.Add(cls.Name.Pos(), token.TypeMismatch, "cannot redeclare class %q", name)
			continue
		}
		if _, exists := c.globals[name]; exists {
			c.errs.Add(cls.Name.Pos(), token.TypeMismatch, "cannot redeclare %q as a class", name)
			continue
		}
		if _, exists := c.funcs[name]; exists {
			c.errs.Add(cls.Name.Pos(), token.TypeMismatch, "cannot redeclare function %q as a class", name)
			continue
		}
		c.classes[name] = &classInfo{
			name:       name,
			typ:        types.NewClass(name, nil),
			fieldIndex: map[string]int{},
			methods:    map[string]*fn{},
			methodBody: map[string][]ast.Stmt{},
		}
		c.classOrder = append(c.classOrder, name)
	}
}

var builtinExceptionNames = []string{
	"BaseException",
	"Exception",
	"ZeroDivisionError",
	"ValueError",
	"TypeError",
	"IndexError",
	"KeyError",
	"RuntimeError",
	"AssertionError",
	"StopIteration",
}

var builtinExceptionBase = map[string]string{
	"Exception":         "BaseException",
	"ZeroDivisionError": "Exception",
	"ValueError":        "Exception",
	"TypeError":         "Exception",
	"IndexError":        "Exception",
	"KeyError":          "Exception",
	"RuntimeError":      "Exception",
	"AssertionError":    "Exception",
	"StopIteration":     "Exception",
}

func (c *checker) declareBuiltinExceptions() {
	fields := []classField{
		{name: "__classid", typ: types.Int, index: 0},
		{name: "message", typ: types.Str, index: 1},
	}
	for _, name := range builtinExceptionNames {
		info := &classInfo{
			name:       name,
			typ:        types.NewClass(name, nil),
			fields:     append([]classField(nil), fields...),
			fieldIndex: map[string]int{"__classid": 0, "message": 1},
			methods:    map[string]*fn{},
			methodBody: map[string][]ast.Stmt{},
		}
		c.classes[name] = info
		c.classOrder = append(c.classOrder, name)
	}
	for name, base := range builtinExceptionBase {
		c.classes[name].base = c.classes[base]
	}
	for _, name := range builtinExceptionNames {
		c.classes[name].typ.Fields = classTypeFields(c.classes[name].fields)
	}
}

func (c *checker) computeClassIntervals() {
	children := map[string][]*classInfo{}
	for _, name := range c.classOrder {
		info := c.classes[name]
		if info == nil || info.base == nil {
			continue
		}
		children[info.base.name] = append(children[info.base.name], info)
	}
	next := 1
	var dfs func(*classInfo)
	dfs = func(info *classInfo) {
		info.classID = next
		info.low = next
		next++
		for _, child := range children[info.name] {
			dfs(child)
		}
		info.high = next - 1
	}
	if root := c.classes["BaseException"]; root != nil {
		dfs(root)
	}
}

func (c *checker) classStmt(n *ast.Class) {
	info := c.classes[n.Name.Name]
	if info == nil {
		return
	}
	for _, dec := range n.Decorators {
		if dec.Name == "dataclass" {
			info.dataclass = true
			continue
		}
		c.errs.Add(dec.Pos(), token.UnsupportedFeature, "class decorator @%s is not supported", dec.Name)
	}
	if n.BaseClass != nil {
		base := c.classes[n.BaseClass.Name]
		if base == nil {
			c.errs.Add(n.BaseClass.Pos(), token.UnsupportedType, "unknown base class %q", n.BaseClass.Name)
		} else if base == info {
			c.errs.Add(n.BaseClass.Pos(), token.TypeMismatch, "class %q cannot inherit from itself", info.name)
		} else {
			info.base = base
			info.fields = append(info.fields, base.fields...)
			for name, idx := range base.fieldIndex {
				info.fieldIndex[name] = idx
			}
		}
	}
	for _, s := range n.Body {
		switch member := s.(type) {
		case *ast.AnnAssign:
			c.classField(info, member)
		case *ast.Function:
			c.classMethod(info, member)
		case *ast.Pass:
			// no-op
		default:
			c.errs.Add(member.Pos(), token.SyntaxError, "class body supports only fields and methods")
		}
	}
	c.checkDataclassDefaults(info)
	info.typ.Fields = classTypeFields(info.fields)
	for _, s := range n.Body {
		if f, ok := s.(*ast.Function); ok {
			if method := info.methods[f.Name.Name]; method != nil {
				c.checkFunctionBody(f.Body, f.Params, method, f.Pos())
			}
		}
	}
}

func (c *checker) exceptionClass(e ast.Expr) *classInfo {
	name, ok := e.(*ast.Name)
	if !ok {
		c.errs.Add(e.Pos(), token.UnsupportedFeature, "exception type must be a class name")
		return nil
	}
	info := c.classes[name.Name]
	if info == nil {
		if _, ok := types.Resolve(name.Name); ok {
			c.errs.Add(e.Pos(), token.TypeMismatch, "except type must inherit from Exception, got %s", name.Name)
			return nil
		}
		c.errs.Add(e.Pos(), token.UndefinedName, "class %q is not defined", name.Name)
		return nil
	}
	if !c.isException(info.name) {
		c.errs.Add(e.Pos(), token.TypeMismatch, "except type must inherit from Exception, got %s", info.name)
		return nil
	}
	return info
}

func (c *checker) isException(name string) bool {
	info := c.classes[name]
	for info != nil {
		if info.name == "BaseException" {
			return true
		}
		info = info.base
	}
	return false
}

func methodInfo(info *classInfo, name string) *fn {
	for info != nil {
		if method := info.methods[name]; method != nil {
			return method
		}
		info = info.base
	}
	return nil
}

func (c *checker) classField(info *classInfo, n *ast.AnnAssign) {
	name := n.Target.Name
	if _, exists := info.fieldIndex[name]; exists {
		c.errs.Add(n.Target.Pos(), token.TypeMismatch, "cannot redeclare field %q", name)
	}
	t := c.resolveType(n.Ann)
	field := classField{name: name, typ: t, index: len(info.fields), value: n.Value, pos: n.Target.Pos()}
	info.fieldIndex[name] = field.index
	info.fields = append(info.fields, field)
	if n.Value != nil {
		vt := c.exprWithHint(n.Value, t)
		if t != types.Invalid && vt != types.Invalid && !types.AssignableTo(vt, t) {
			c.errs.Add(n.Value.Pos(), token.TypeMismatch, "cannot assign %s to field %s %q", vt, t, name)
		}
	}
}

func (c *checker) classMethod(info *classInfo, n *ast.Function) {
	if _, exists := info.methods[n.Name.Name]; exists {
		c.errs.Add(n.Name.Pos(), token.TypeMismatch, "cannot redeclare method %q", n.Name.Name)
		return
	}
	if len(n.Params) == 0 || n.Params[0].Name.Name != "self" {
		c.errs.Add(n.Pos(), token.MissingAnnotation, "method %q needs self parameter", n.Name.Name)
		return
	}
	params := make([]param, 0, len(n.Params))
	for i, p := range n.Params {
		pt := types.Type(info.typ)
		if i > 0 {
			pt = c.paramType(p)
		} else if p.Ann != nil {
			pt = c.resolveType(p.Ann)
			if pt != types.Invalid && !types.Equal(pt, info.typ) {
				c.errs.Add(p.Ann.Pos(), token.TypeMismatch, "self must be %s, got %s", info.typ, pt)
			}
		}
		params = append(params, param{name: p.Name.Name, typ: pt})
	}
	inferRet := n.Returns == nil && n.Name.Name != "__init__"
	ret := types.None
	if n.Returns != nil {
		ret = c.resolveType(n.Returns)
		if n.Name.Name == "__init__" && ret != types.Invalid && !types.Equal(ret, types.None) {
			c.errs.Add(n.Returns.Pos(), token.TypeMismatch, "__init__ must return None, got %s", ret)
		}
	}
	info.methods[n.Name.Name] = &fn{
		name:      info.name + "." + n.Name.Name,
		params:    params,
		ret:       ret,
		inferRet:  inferRet,
		generator: containsYield(n.Body),
		locals:    map[string]*local{},
		children:  map[string]*fn{},
		captures:  map[string]*capture{},
		globals:   map[string]bool{},
		nonlocal:  map[string]bool{},
	}
	info.methodBody[n.Name.Name] = n.Body
}

func (c *checker) checkDataclassDefaults(info *classInfo) {
	if !info.dataclass {
		return
	}
	seenDefault := false
	for _, field := range info.fields {
		if field.value != nil {
			seenDefault = true
			continue
		}
		if seenDefault {
			c.errs.Add(field.pos, token.TypeMismatch, "non-default field %q follows default field", field.name)
		}
	}
}

func classTypeFields(fields []classField) []types.Field {
	out := make([]types.Field, len(fields))
	for i, f := range fields {
		out[i] = types.Field{Name: f.name, Type: f.typ}
	}
	return out
}

func (c *checker) nestedFuncs(info *fn, body []ast.Stmt) {
	for _, s := range body {
		f, ok := s.(*ast.Function)
		if !ok {
			continue
		}
		if _, exists := info.children[f.Name.Name]; exists {
			c.errs.Add(f.Name.Pos(), token.TypeMismatch, "cannot redeclare function %q", f.Name.Name)
			continue
		}
		child := &fn{
			name:      f.Name.Name,
			generator: containsYield(f.Body),
			locals:    map[string]*local{},
			parent:    info,
			children:  map[string]*fn{},
			captures:  map[string]*capture{},
			globals:   map[string]bool{},
			nonlocal:  map[string]bool{},
		}
		if f.Returns == nil {
			child.inferRet = true
			child.ret = types.None
		} else {
			child.ret = c.resolveType(f.Returns)
		}
		for _, p := range f.Params {
			child.params = append(child.params, param{name: p.Name.Name, typ: c.paramType(p)})
		}
		info.children[f.Name.Name] = child
		c.declareFuncLocal(child, f.Pos())
	}
}

func srcTypes(params []param) []types.Type {
	out := make([]types.Type, len(params))
	for i, p := range params {
		out[i] = p.typ
	}
	return out
}

// paramType resolves a parameter's declared type, defaulting an unannotated
// parameter to Any so whole-program inference can compile it as a dynamic slot.
func (c *checker) paramType(p *ast.Param) types.Type {
	if p.Ann == nil {
		return types.Any
	}
	return c.resolveType(p.Ann)
}

func (c *checker) resolveType(e ast.Expr) types.Type {
	if name, ok := e.(*ast.Name); ok {
		if resolved, known := types.Resolve(name.Name); known {
			return resolved
		}
		if cls, known := c.classes[name.Name]; known {
			return cls.typ
		}
		c.errs.Add(e.Pos(), token.UnsupportedType, "unknown type %q", name.Name)
		return types.Invalid
	}
	if u, ok := e.(*ast.UnionType); ok {
		members := make([]types.Type, len(u.Members))
		for i, m := range u.Members {
			mt := c.resolveType(m)
			if mt == types.Invalid {
				return types.Invalid
			}
			members[i] = mt
		}
		return types.NewUnion(members...)
	}
	if sub, ok := e.(*ast.Subscript); ok {
		base, ok := sub.X.(*ast.Name)
		if !ok {
			c.errs.Add(e.Pos(), token.UnsupportedType, "unsupported type annotation")
			return types.Invalid
		}
		switch base.Name {
		case "list":
			return types.NewList(c.resolveType(sub.Index))
		case "set":
			elem := c.resolveType(sub.Index)
			if elem != types.Invalid && !hashableKey(elem) {
				c.errs.Add(sub.Index.Pos(), token.UnsupportedType, "set element type %s is not supported", elem)
				return types.Invalid
			}
			return types.NewSet(elem)
		case "dict":
			args, ok := sub.Index.(*ast.TupleLit)
			if !ok || len(args.Elems) != 2 {
				c.errs.Add(e.Pos(), token.UnsupportedType, "dict annotation needs key and value types")
				return types.Invalid
			}
			key := c.resolveType(args.Elems[0])
			if key != types.Invalid && !hashableKey(key) {
				c.errs.Add(args.Elems[0].Pos(), token.UnsupportedType, "dict key type %s is not supported", key)
				return types.Invalid
			}
			return types.NewDict(key, c.resolveType(args.Elems[1]))
		case "tuple":
			if args, ok := sub.Index.(*ast.TupleLit); ok {
				elems := make([]types.Type, len(args.Elems))
				for i, elem := range args.Elems {
					elems[i] = c.resolveType(elem)
				}
				return types.NewTuple(elems...)
			}
			return types.NewTuple(c.resolveType(sub.Index))
		case "Iterator":
			return types.NewIterator(c.resolveType(sub.Index))
		case "Optional":
			elem := c.resolveType(sub.Index)
			if elem == types.Invalid {
				return types.Invalid
			}
			return types.NewUnion(elem, types.None)
		case "Union":
			var members []types.Type
			if args, ok := sub.Index.(*ast.TupleLit); ok {
				members = make([]types.Type, len(args.Elems))
				for i, elem := range args.Elems {
					members[i] = c.resolveType(elem)
				}
			} else {
				members = []types.Type{c.resolveType(sub.Index)}
			}
			for _, m := range members {
				if m == types.Invalid {
					return types.Invalid
				}
			}
			return types.NewUnion(members...)
		case "Callable":
			args, ok := sub.Index.(*ast.TupleLit)
			if !ok || len(args.Elems) != 2 {
				c.errs.Add(e.Pos(), token.UnsupportedType, "Callable annotation needs parameter list and return type")
				return types.Invalid
			}
			paramTuple, ok := args.Elems[0].(*ast.TupleLit)
			if !ok {
				c.errs.Add(args.Elems[0].Pos(), token.UnsupportedType, "Callable parameter list must be bracketed")
				return types.Invalid
			}
			params := make([]types.Type, len(paramTuple.Elems))
			for i, elem := range paramTuple.Elems {
				params[i] = c.resolveType(elem)
			}
			return types.NewCallable(params, c.resolveType(args.Elems[1]))
		default:
			c.errs.Add(e.Pos(), token.UnsupportedType, "unknown generic type %q", base.Name)
			return types.Invalid
		}
	}
	return types.Invalid
}

func (c *checker) function(n *ast.Function) {
	if c.fn != nil {
		info := c.fn.children[n.Name.Name]
		if info == nil {
			return
		}
		info.local.init = true
		c.checkFunctionBody(n.Body, n.Params, info, n.Pos())
		return
	}
	info := c.funcs[n.Name.Name]
	if info == nil {
		return
	}
	info.slot.init = true
	c.checkFunctionBody(n.Body, n.Params, info, n.Pos())
}

func (c *checker) checkFunctionBody(body []ast.Stmt, params []*ast.Param, info *fn, pos token.Pos) {
	prev := c.fn
	c.fn = info
	c.nestedFuncs(info, body)
	for i, p := range info.params {
		if _, exists := info.locals[p.name]; exists {
			c.errs.Add(params[i].Name.Pos(), token.TypeMismatch, "duplicate parameter %q", p.name)
			continue
		}
		info.locals[p.name] = &local{typ: p.typ, index: i, init: true}
	}
	if info.generator {
		if _, ok := info.ret.(*types.Iterator); !ok && info.ret != types.Invalid {
			c.errs.Add(pos, token.TypeMismatch, "generator function %q must return Iterator[T], got %s", info.name, info.ret)
		}
	}
	c.checkBlock(body)
	if info.inferRet {
		// Infer the return type as the join of every return expression's type;
		// a body with no value-returns is None.
		if len(info.returns) == 0 {
			info.ret = types.None
		} else {
			ret := info.returns[0]
			for _, rt := range info.returns[1:] {
				ret = types.Join(ret, rt)
			}
			info.ret = ret
		}
	}
	if !info.generator && !types.Equal(info.ret, types.None) && !blockReturns(body) {
		c.errs.Add(pos, token.TypeMismatch, "function %q may fall through without returning %s", info.name, info.ret)
	}
	c.fn = prev
}

func (c *checker) ret(n *ast.Return) {
	if c.fn == nil {
		c.errs.Add(n.Pos(), token.SyntaxError, "'return' outside function")
		if n.Value != nil {
			c.expr(n.Value)
		}
		return
	}
	if c.fn.generator {
		if n.Value != nil {
			c.errs.Add(n.Pos(), token.TypeMismatch, "generator function cannot return a value")
			c.expr(n.Value)
		}
		return
	}
	rt := types.Type(types.None)
	if n.Value != nil {
		if c.fn.inferRet {
			rt = c.expr(n.Value)
		} else {
			rt = c.exprWithHint(n.Value, c.fn.ret)
		}
	}
	if c.fn.inferRet {
		// Return type is being inferred: collect this branch's type instead of
		// checking against a fixed annotation.
		c.fn.returns = append(c.fn.returns, rt)
		return
	}
	if c.fn.ret != types.Invalid && rt != types.Invalid && !types.AssignableTo(rt, c.fn.ret) {
		c.errs.Add(n.Pos(), token.TypeMismatch, "cannot return %s from function returning %s", rt, c.fn.ret)
	}
}

func (c *checker) yieldStmt(n *ast.Yield) {
	if c.fn == nil {
		c.errs.Add(n.Pos(), token.SyntaxError, "'yield' outside function")
		if n.Value != nil {
			c.expr(n.Value)
		}
		return
	}
	iter, ok := c.fn.ret.(*types.Iterator)
	if !ok {
		if c.fn.ret != types.Invalid {
			c.errs.Add(n.Pos(), token.TypeMismatch, "yield in function returning %s; expected Iterator[T]", c.fn.ret)
		}
		if n.Value != nil {
			c.expr(n.Value)
		}
		return
	}
	yt := types.None
	if n.Value != nil {
		yt = c.exprWithHint(n.Value, iter.Elem)
	}
	if yt != types.Invalid && iter.Elem != types.Invalid && !types.AssignableTo(yt, iter.Elem) {
		c.errs.Add(n.Pos(), token.TypeMismatch, "cannot yield %s from generator yielding %s", yt, iter.Elem)
	}
}

func blockReturns(body []ast.Stmt) bool {
	for _, s := range body {
		switch n := s.(type) {
		case *ast.Return:
			return true
		case *ast.If:
			if len(n.Orelse) > 0 && blockReturns(n.Body) && blockReturns(n.Orelse) {
				return true
			}
		case *ast.Try:
			if blockReturns(n.Finalbody) {
				return true
			}
			if blockReturns(n.Body) {
				return true
			}
			if len(n.Handlers) > 0 && blockTerminates(n.Body) {
				all := true
				for _, h := range n.Handlers {
					all = all && blockReturns(h.Body)
				}
				if all {
					return true
				}
			}
		case *ast.With:
			if blockReturns(n.Body) {
				return true
			}
		}
	}
	return false
}

func blockTerminates(body []ast.Stmt) bool {
	for _, s := range body {
		switch n := s.(type) {
		case *ast.Return, *ast.Raise:
			return true
		case *ast.If:
			if len(n.Orelse) > 0 && blockTerminates(n.Body) && blockTerminates(n.Orelse) {
				return true
			}
		case *ast.Try:
			if blockReturns(n.Finalbody) || blockReturns(n.Body) {
				return true
			}
		case *ast.With:
			if blockTerminates(n.Body) {
				return true
			}
		}
	}
	return false
}

func containsYield(body []ast.Stmt) bool {
	for _, s := range body {
		switch n := s.(type) {
		case *ast.Yield:
			return true
		case *ast.If:
			if containsYield(n.Body) || containsYield(n.Orelse) {
				return true
			}
		case *ast.While:
			if containsYield(n.Body) || containsYield(n.Orelse) {
				return true
			}
		case *ast.For:
			if containsYield(n.Body) || containsYield(n.Orelse) {
				return true
			}
		case *ast.Try:
			if containsYield(n.Body) || containsYield(n.Orelse) || containsYield(n.Finalbody) {
				return true
			}
			for _, h := range n.Handlers {
				if containsYield(h.Body) {
					return true
				}
			}
		case *ast.With:
			if containsYield(n.Body) {
				return true
			}
		case *ast.Function:
			continue
		}
	}
	return false
}

// expr types an expression, records the result, and returns it.
func (c *checker) expr(e ast.Expr) types.Type {
	return c.exprWithHint(e, nil)
}

func (c *checker) exprWithHint(e ast.Expr, hint types.Type) types.Type {
	t := c.typeOf(e, hint)
	c.types[e] = t
	return t
}

func (c *checker) typeOf(e ast.Expr, hint types.Type) types.Type {
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
	case *ast.ListLit:
		return c.listType(n, hint)
	case *ast.DictLit:
		return c.dictType(n, hint)
	case *ast.TupleLit:
		elems := make([]types.Type, len(n.Elems))
		for i, elem := range n.Elems {
			elems[i] = c.expr(elem)
		}
		return types.NewTuple(elems...)
	case *ast.LambdaExpr:
		return c.lambda(n, hint)
	case *ast.SetLit:
		return c.setType(n, hint)
	case *ast.ListComp:
		elem := c.compElem(n.Clauses, n.Elem)
		return types.NewList(elem)
	case *ast.DictComp:
		cleanup := c.compClauses(n.Clauses)
		defer cleanup()
		kt := c.expr(n.Key)
		vt := c.expr(n.Value)
		if !hashableKey(kt) {
			c.errs.Add(n.Key.Pos(), token.UnsupportedType, "dict key type %s is not supported", kt)
			return types.Invalid
		}
		return types.NewDict(kt, vt)
	case *ast.SetComp:
		elem := c.compElem(n.Clauses, n.Elem)
		if !hashableKey(elem) {
			c.errs.Add(n.Elem.Pos(), token.UnsupportedType, "set element type %s is not supported", elem)
			return types.Invalid
		}
		return types.NewSet(elem)
	case *ast.Subscript:
		ct := c.expr(n.X)
		it := c.expr(n.Index)
		return c.indexResultType(n, ct, it)
	case *ast.Attribute:
		return c.fieldType(n, c.expr(n.X))
	case *ast.FString:
		c.fstring(n)
		return types.Str
	default:
		return types.Invalid
	}
}

func (c *checker) listType(n *ast.ListLit, hint types.Type) types.Type {
	if len(n.Elems) == 0 {
		if lt, ok := hint.(*types.List); ok {
			return lt
		}
		c.errs.Add(n.Pos(), token.UnsupportedType, "empty list needs list[T] annotation")
		return types.Invalid
	}
	elem := c.expr(n.Elems[0])
	for _, e := range n.Elems[1:] {
		et := c.expr(e)
		if elem != types.Invalid && et != types.Invalid && !types.Equal(elem, et) {
			c.errs.Add(e.Pos(), token.TypeMismatch, "list elements must have same type: %s and %s", elem, et)
			return types.Invalid
		}
	}
	return types.NewList(elem)
}

func (c *checker) dictType(n *ast.DictLit, hint types.Type) types.Type {
	if len(n.Keys) == 0 {
		if dt, ok := hint.(*types.Dict); ok {
			return dt
		}
		c.errs.Add(n.Pos(), token.UnsupportedType, "empty dict needs dict[K, V] annotation")
		return types.Invalid
	}
	kt := c.expr(n.Keys[0])
	vt := c.expr(n.Values[0])
	for i := 1; i < len(n.Keys); i++ {
		k := c.expr(n.Keys[i])
		v := c.expr(n.Values[i])
		if kt != types.Invalid && k != types.Invalid && !types.Equal(kt, k) {
			c.errs.Add(n.Keys[i].Pos(), token.TypeMismatch, "dict keys must have same type: %s and %s", kt, k)
			return types.Invalid
		}
		if vt != types.Invalid && v != types.Invalid && !types.Equal(vt, v) {
			c.errs.Add(n.Values[i].Pos(), token.TypeMismatch, "dict values must have same type: %s and %s", vt, v)
			return types.Invalid
		}
	}
	if !hashableKey(kt) {
		c.errs.Add(n.Keys[0].Pos(), token.UnsupportedType, "dict key type %s is not supported", kt)
		return types.Invalid
	}
	return types.NewDict(kt, vt)
}

func hashableKey(t types.Type) bool {
	return types.Equal(t, types.Int) || types.Equal(t, types.Float) || types.Equal(t, types.Bool) || types.Equal(t, types.Str)
}

func (c *checker) indexResultType(n *ast.Subscript, ct, it types.Type) types.Type {
	switch t := ct.(type) {
	case *types.List:
		if it != types.Invalid && !types.Equal(it, types.Int) {
			c.errs.Add(n.Index.Pos(), token.TypeMismatch, "list index must be int, got %s", it)
		}
		return t.Elem
	case *types.Dict:
		if it != types.Invalid && !types.AssignableTo(it, t.Key) {
			c.errs.Add(n.Index.Pos(), token.TypeMismatch, "dict key must be %s, got %s", t.Key, it)
		}
		return t.Value
	case *types.Tuple:
		if idx, ok := constTupleIndex(n.Index); ok {
			if idx < 0 || idx >= len(t.Elems) {
				c.errs.Add(n.Index.Pos(), token.IntOverflow, "tuple index %d out of range", idx)
				return types.Invalid
			}
			return t.Elems[idx]
		}
		c.errs.Add(n.Index.Pos(), token.UnsupportedFeature, "tuple index must be a constant int")
		return types.Invalid
	default:
		if types.Equal(ct, types.Str) {
			if it != types.Invalid && !types.Equal(it, types.Int) {
				c.errs.Add(n.Index.Pos(), token.TypeMismatch, "str index must be int, got %s", it)
			}
			return types.Str
		}
		if ct != types.Invalid {
			c.errs.Add(n.Pos(), token.NotIndexable, "%s is not indexable", ct)
		}
		return types.Invalid
	}
}

func (c *checker) fieldType(n *ast.Attribute, recv types.Type) types.Type {
	cls, ok := recv.(*types.Class)
	if !ok {
		if recv != types.Invalid {
			c.errs.Add(n.Pos(), token.UnsupportedFeature, "attribute %s on %s is not supported", n.Name, recv)
		}
		return types.Invalid
	}
	info := c.classes[cls.Name]
	if info == nil {
		return types.Invalid
	}
	idx, ok := info.fieldIndex[n.Name]
	if !ok {
		c.errs.Add(n.Pos(), token.UndefinedName, "field %q is not defined on %s", n.Name, cls.Name)
		return types.Invalid
	}
	return info.fields[idx].typ
}

func constTupleIndex(e ast.Expr) (int, bool) {
	if lit, ok := e.(*ast.IntLit); ok {
		return int(lit.Value), true
	}
	return 0, false
}

func (c *checker) fstring(n *ast.FString) {
	for _, part := range n.Parts {
		c.fstringPart(part)
	}
}

func (c *checker) fstringPart(part ast.FStringPart) {
	if expr, ok := part.(*ast.FStringExpr); ok {
		c.expr(expr.Expr)
		if expr.Conversion != 0 && expr.Conversion != 's' && expr.Conversion != 'r' && expr.Conversion != 'a' {
			c.errs.Add(expr.Pos(), token.UnsupportedFeature, "unsupported f-string conversion !%c", expr.Conversion)
		}
		for _, fp := range expr.Format {
			c.fstringPart(fp)
		}
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
	if !types.Equal(bt, et) {
		c.errs.Add(n.Pos(), token.TypeMismatch, "conditional expression arms have different types: %s and %s", bt, et)
		return types.Invalid
	}
	return bt
}

func (c *checker) nameType(n *ast.Name) types.Type {
	if t, ok := c.narrowed[n.Name]; ok {
		return t
	}
	if c.fn != nil {
		if c.fn.globals[n.Name] {
			goto global
		}
		if l, ok := c.fn.locals[n.Name]; ok {
			if !l.init {
				c.errs.Add(n.Pos(), token.UseBeforeDefinition, "name %q used before assignment", n.Name)
			}
			return l.typ
		}
		if cap := c.capture(n); cap != nil {
			return cap.typ
		}
	}
	if fn, ok := c.funcs[n.Name]; ok {
		if !fn.slot.init {
			c.errs.Add(n.Pos(), token.UseBeforeDefinition, "function %q used before definition", n.Name)
		}
		return types.NewCallable(srcTypes(fn.params), fn.ret)
	}
	if _, ok := c.classes[n.Name]; ok {
		c.errs.Add(n.Pos(), token.UnsupportedFeature, "class value %q is not supported", n.Name)
		return types.Invalid
	}
global:
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

func (c *checker) findEnclosingLocal(name string) *local {
	for fn := c.fn.parent; fn != nil; fn = fn.parent {
		if l, ok := fn.locals[name]; ok {
			return l
		}
	}
	return nil
}

func (c *checker) capture(n *ast.Name) *capture {
	if c.fn == nil {
		return nil
	}
	if cap, ok := c.fn.captures[n.Name]; ok {
		return cap
	}
	for fn := c.fn.parent; fn != nil; fn = fn.parent {
		if l, ok := fn.locals[n.Name]; ok {
			if c.fn.parent != fn {
				ensureCapture(c.fn.parent, n.Name, l)
			}
			cap := &capture{name: n.Name, typ: l.typ, index: len(c.fn.capOrder), src: l, boxed: l.boxed}
			c.fn.captures[n.Name] = cap
			c.fn.capOrder = append(c.fn.capOrder, n.Name)
			return cap
		}
	}
	if c.fn.nonlocal[n.Name] {
		c.errs.Add(n.Pos(), token.NoBindingForNonlocal, "no binding for nonlocal %q found", n.Name)
		return nil
	}
	return nil
}

func ensureCapture(fn *fn, name string, src *local) {
	if fn == nil {
		return
	}
	if _, ok := fn.locals[name]; ok {
		return
	}
	if _, ok := fn.captures[name]; ok {
		return
	}
	if fn.parent != nil {
		ensureCapture(fn.parent, name, src)
	}
	fn.captures[name] = &capture{name: name, typ: src.typ, index: len(fn.capOrder), src: src, boxed: src.boxed}
	fn.capOrder = append(fn.capOrder, name)
}

func (c *checker) lambda(n *ast.LambdaExpr, hint types.Type) types.Type {
	callable, ok := hint.(*types.Callable)
	if !ok {
		c.errs.Add(n.Pos(), token.MissingAnnotation, "lambda parameter types need a Callable context")
		return types.Invalid
	}
	if len(n.Params) != len(callable.Params) {
		c.errs.Add(n.Pos(), token.ArityMismatch, "lambda expects %d parameter types, got %d", len(n.Params), len(callable.Params))
		return types.Invalid
	}
	info := &fn{
		name:     "<lambda>",
		ret:      callable.Return,
		locals:   map[string]*local{},
		parent:   c.fn,
		children: map[string]*fn{},
		captures: map[string]*capture{},
		globals:  map[string]bool{},
		nonlocal: map[string]bool{},
	}
	for i, p := range n.Params {
		info.params = append(info.params, param{name: p.Name.Name, typ: callable.Params[i]})
		p.Ann = typeExpr(p.Pos(), callable.Params[i])
	}
	prev := c.fn
	c.fn = info
	for i, p := range info.params {
		info.locals[p.name] = &local{typ: p.typ, index: i, init: true}
	}
	bt := c.exprWithHint(n.Body, callable.Return)
	if bt != types.Invalid && callable.Return != types.Invalid && !types.AssignableTo(bt, callable.Return) {
		c.errs.Add(n.Body.Pos(), token.TypeMismatch, "cannot return %s from lambda returning %s", bt, callable.Return)
	}
	c.fn = prev
	c.lambdas[n] = info
	return callable
}

func typeExpr(pos token.Pos, t types.Type) ast.Expr {
	return &ast.Name{Base: ast.Base{Position: pos}, Name: t.String()}
}

func (c *checker) setType(n *ast.SetLit, hint types.Type) types.Type {
	if len(n.Elems) == 0 {
		if st, ok := hint.(*types.Set); ok {
			return st
		}
		c.errs.Add(n.Pos(), token.UnsupportedType, "empty set needs set[T] annotation")
		return types.Invalid
	}
	elem := c.expr(n.Elems[0])
	for _, e := range n.Elems[1:] {
		et := c.expr(e)
		if elem != types.Invalid && et != types.Invalid && !types.Equal(elem, et) {
			c.errs.Add(e.Pos(), token.TypeMismatch, "set elements must have same type: %s and %s", elem, et)
			return types.Invalid
		}
	}
	if !hashableKey(elem) {
		c.errs.Add(n.Elems[0].Pos(), token.UnsupportedType, "set element type %s is not supported", elem)
		return types.Invalid
	}
	return types.NewSet(elem)
}

func (c *checker) compElem(clauses []*ast.Comprehension, elem ast.Expr) types.Type {
	cleanup := c.compClauses(clauses)
	defer cleanup()
	return c.expr(elem)
}

func (c *checker) compClauses(clauses []*ast.Comprehension) func() {
	var tempGlobals []string
	for _, clause := range clauses {
		iter := c.expr(clause.Iter)
		elem := iterableElem(iter)
		if elem == types.Invalid {
			c.errs.Add(clause.Iter.Pos(), token.NotIterable, "%s is not iterable", iter)
		}
		if c.fn != nil {
			l := c.declareLocal(clause.Target.Name, elem, clause.Target.Pos())
			l.init = true
			c.types[clause.Target] = l.typ
		} else {
			g := c.declare(clause.Target.Name, elem, clause.Target.Pos())
			g.init = true
			c.types[clause.Target] = g.typ
			tempGlobals = append(tempGlobals, clause.Target.Name)
		}
		for _, ifExpr := range clause.Ifs {
			c.condition(ifExpr)
		}
	}
	return func() {
		for _, name := range tempGlobals {
			delete(c.globals, name)
		}
	}
}

func (c *checker) unaryType(n *ast.UnaryExpr) types.Type {
	t := c.expr(n.X)
	switch n.Op {
	case token.NOT:
		if !types.Equal(t, types.Bool) && t != types.Invalid {
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
		if types.Equal(t, types.Int) {
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
		if types.Equal(lt, types.Str) && types.Equal(rt, types.Str) {
			return types.Str
		}
		if ll, ok := lt.(*types.List); ok && types.AssignableTo(rt, lt) {
			return types.NewList(ll.Elem)
		}
		return c.arith(lt, op, rt, pos)
	case token.STAR:
		if types.Equal(lt, types.Str) && types.Equal(rt, types.Int) {
			return types.Str
		}
		if ll, ok := lt.(*types.List); ok && types.Equal(rt, types.Int) {
			return types.NewList(ll.Elem)
		}
		return c.arith(lt, op, rt, pos)
	case token.MINUS, token.DOUBLESLASH, token.PERCENT, token.DOUBLESTAR:
		return c.arith(lt, op, rt, pos)
	case token.SLASH:
		if types.Equal(lt, types.Int) && types.Equal(rt, types.Int) {
			return types.Float
		}
		if types.Equal(lt, types.Float) && types.Equal(rt, types.Float) {
			return types.Float
		}
		return c.mismatch(op, lt, rt, pos)
	case token.AMP, token.PIPE, token.CARET, token.LSHIFT, token.RSHIFT:
		if types.Equal(lt, types.Int) && types.Equal(rt, types.Int) {
			return types.Int
		}
		return c.mismatch(op, lt, rt, pos)
	default:
		return types.Invalid
	}
}

func (c *checker) arith(lt types.Type, op token.Type, rt types.Type, pos token.Pos) types.Type {
	if types.Equal(lt, types.Int) && types.Equal(rt, types.Int) {
		return types.Int
	}
	if types.Equal(lt, types.Float) && types.Equal(rt, types.Float) {
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
	if (!types.Equal(lt, types.Bool) && lt != types.Invalid) || (!types.Equal(rt, types.Bool) && rt != types.Invalid) {
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
	if op == token.IS || op == token.ISNOT {
		if !identityComparable(lt) || !identityComparable(rt) {
			c.errs.Add(pos, token.TypeMismatch, "'%s' requires reference operands, got %s and %s", op, lt, rt)
		}
		return
	}
	if op == token.IN || op == token.NOTIN {
		if !containsType(lt, rt) {
			c.errs.Add(pos, token.NotIterable, "'%s' requires container RHS, got %s in %s", op, lt, rt)
		}
		return
	}
	if types.Equal(lt, types.None) || types.Equal(rt, types.None) {
		c.errs.Add(pos, token.UnsupportedFeature, "comparing to None uses 'is'")
		return
	}
	if !types.Equal(lt, rt) {
		c.errs.Add(pos, token.NotComparable, "'%s' not supported between instances of %s and %s", op, lt, rt)
	}
}

func identityComparable(t types.Type) bool {
	if types.Equal(t, types.None) || types.Equal(t, types.Str) || types.IsAny(t) {
		return true
	}
	switch t.(type) {
	case *types.List, *types.Dict, *types.Set, *types.Tuple, *types.Class, *types.Iterator, *types.Callable, *types.Union:
		return true
	default:
		return false
	}
}

func containsType(needle, haystack types.Type) bool {
	switch t := haystack.(type) {
	case *types.List:
		return types.AssignableTo(needle, t.Elem)
	case *types.Dict:
		return types.AssignableTo(needle, t.Key)
	case *types.Set:
		return types.AssignableTo(needle, t.Elem)
	default:
		return types.Equal(haystack, types.Str) && types.Equal(needle, types.Str)
	}
}

// isinstanceType checks isinstance(value, T). The first argument is any value;
// the second is a type operand (resolved as an annotation, not a value) whose
// resolved type is recorded for the compiler to lower as a REF_TEST.
func (c *checker) isinstanceType(n *ast.CallExpr) types.Type {
	if len(n.Args) != 2 {
		c.errs.Add(n.Pos(), token.ArityMismatch, "isinstance() takes exactly 2 arguments (%d given)", len(n.Args))
		for _, a := range n.Args {
			c.expr(a)
		}
		return types.Bool
	}
	c.expr(n.Args[0])
	t := c.resolveType(n.Args[1])
	c.types[n.Args[1]] = t
	return types.Bool
}

func (c *checker) callType(n *ast.CallExpr) types.Type {
	name, ok := n.Fn.(*ast.Name)
	if !ok {
		if attr, ok := n.Fn.(*ast.Attribute); ok {
			return c.methodCallType(n, attr)
		}
		fnType := c.expr(n.Fn)
		return c.callableCallType(n, fnType)
	}

	if name.Name == "isinstance" {
		return c.isinstanceType(n)
	}

	argTypes := make([]types.Type, len(n.Args))
	for i, a := range n.Args {
		argTypes[i] = c.expr(a)
	}

	if cls, ok := c.classes[name.Name]; ok {
		return c.constructorCallType(n, cls, argTypes)
	}
	if fn, ok := c.funcs[name.Name]; ok {
		if c.fn == nil && !fn.slot.init {
			c.errs.Add(name.Pos(), token.UseBeforeDefinition, "function %q used before definition", name.Name)
			return types.Invalid
		}
		if len(argTypes) != len(fn.params) {
			c.errs.Add(n.Pos(), token.ArityMismatch, "%s() takes exactly %d arguments (%d given)", name.Name, len(fn.params), len(argTypes))
			return types.Invalid
		}
		for i, at := range argTypes {
			pt := fn.params[i].typ
			if at != types.Invalid && pt != types.Invalid && !types.AssignableTo(at, pt) {
				c.errs.Add(n.Args[i].Pos(), token.TypeMismatch, "%s() argument %d must be %s, got %s", name.Name, i+1, pt, at)
			}
		}
		return fn.ret
	}
	if c.fn != nil {
		if l, ok := c.fn.locals[name.Name]; ok {
			return c.callableCallType(n, l.typ)
		}
		if cap := c.capture(name); cap != nil {
			return c.callableCallType(n, cap.typ)
		}
	}
	if g, ok := c.globals[name.Name]; ok && !isBuiltin(name.Name) {
		return c.callableCallType(n, g.typ)
	}
	if !isBuiltin(name.Name) {
		c.errs.Add(name.Pos(), token.UndefinedName, "name %q is not defined", name.Name)
		return types.Invalid
	}
	rt, ok := builtinReturn(name.Name, argTypes)
	if !ok {
		if min, max, arity := builtinArity(name.Name); arity && (len(argTypes) < min || len(argTypes) > max) {
			if min == max {
				c.errs.Add(n.Pos(), token.ArityMismatch, "%s() takes exactly %d argument(s) (%d given)", name.Name, min, len(argTypes))
			} else {
				c.errs.Add(n.Pos(), token.ArityMismatch, "%s() takes %d to %d arguments (%d given)", name.Name, min, max, len(argTypes))
			}
			return types.Invalid
		}
		c.errs.Add(n.Pos(), token.TypeMismatch, "%s() does not accept these arguments", name.Name)
		return types.Invalid
	}
	if name.Name == "range" && len(n.Args) == 3 && isConstIntLiteral(n.Args[2]) && constIntValue(n.Args[2]) == 0 {
		c.errs.Add(n.Args[2].Pos(), token.SyntaxError, "range() step must not be zero")
	}
	return rt
}

func (c *checker) constructorCallType(n *ast.CallExpr, cls *classInfo, argTypes []types.Type) types.Type {
	params, minArgs := constructorParams(cls)
	if len(argTypes) < minArgs || len(argTypes) > len(params) {
		c.errs.Add(n.Pos(), token.ArityMismatch, "%s() takes %d to %d arguments (%d given)", cls.name, minArgs, len(params), len(argTypes))
		return types.Invalid
	}
	for i, at := range argTypes {
		pt := params[i]
		if at != types.Invalid && pt != types.Invalid && !types.AssignableTo(at, pt) {
			c.errs.Add(n.Args[i].Pos(), token.TypeMismatch, "%s() argument %d must be %s, got %s", cls.name, i+1, pt, at)
		}
	}
	return cls.typ
}

func constructorParams(cls *classInfo) ([]types.Type, int) {
	if isExceptionInfo(cls) {
		return []types.Type{types.Str}, 0
	}
	if init := cls.methods["__init__"]; init != nil {
		params := srcTypes(init.params)
		if len(params) == 0 {
			return nil, 0
		}
		return params[1:], len(params) - 1
	}
	if !cls.dataclass {
		return nil, 0
	}
	params := make([]types.Type, len(cls.fields))
	minArgs := len(cls.fields)
	for i, field := range cls.fields {
		params[i] = field.typ
		if field.value != nil && i < minArgs {
			minArgs = i
		}
	}
	return params, minArgs
}

func isExceptionInfo(info *classInfo) bool {
	for info != nil {
		if info.name == "BaseException" {
			return true
		}
		info = info.base
	}
	return false
}

func (c *checker) callableCallType(n *ast.CallExpr, fnType types.Type) types.Type {
	callable, ok := fnType.(*types.Callable)
	if !ok {
		if fnType != types.Invalid {
			c.errs.Add(n.Pos(), token.TypeMismatch, "%s is not callable", fnType)
		}
		for _, arg := range n.Args {
			c.expr(arg)
		}
		return types.Invalid
	}
	if len(n.Args) != len(callable.Params) {
		c.errs.Add(n.Pos(), token.ArityMismatch, "callable takes exactly %d arguments (%d given)", len(callable.Params), len(n.Args))
		return types.Invalid
	}
	for i, arg := range n.Args {
		at := c.expr(arg)
		if at != types.Invalid && callable.Params[i] != types.Invalid && !types.AssignableTo(at, callable.Params[i]) {
			c.errs.Add(arg.Pos(), token.TypeMismatch, "callable argument %d must be %s, got %s", i+1, callable.Params[i], at)
		}
	}
	return callable.Return
}

func builtinArity(name string) (min int, max int, ok bool) {
	switch name {
	case "print", "str", "int", "float", "bool", "abs", "len", "enumerate", "iter", "next":
		return 1, 1, true
	case "zip":
		return 2, 2, true
	case "range":
		return 1, 3, true
	default:
		return 0, 0, false
	}
}

func (c *checker) methodCallType(n *ast.CallExpr, attr *ast.Attribute) types.Type {
	recv := c.expr(attr.X)
	args := make([]types.Type, len(n.Args))
	for i, a := range n.Args {
		args[i] = c.expr(a)
	}
	switch t := recv.(type) {
	case *types.Class:
		info := c.classes[t.Name]
		if info == nil {
			return types.Invalid
		}
		method := info.methods[attr.Name]
		if method == nil {
			c.errs.Add(n.Pos(), token.UnsupportedFeature, "method %s on %s is not supported", attr.Name, recv)
			return types.Invalid
		}
		params := srcTypes(method.params)
		if len(params) > 0 {
			params = params[1:]
		}
		if len(args) != len(params) {
			c.errs.Add(n.Pos(), token.ArityMismatch, "%s.%s() takes exactly %d arguments (%d given)", info.name, attr.Name, len(params), len(args))
			return types.Invalid
		}
		for i, at := range args {
			if at != types.Invalid && params[i] != types.Invalid && !types.AssignableTo(at, params[i]) {
				c.errs.Add(n.Args[i].Pos(), token.TypeMismatch, "%s.%s() argument %d must be %s, got %s", info.name, attr.Name, i+1, params[i], at)
			}
		}
		return method.ret
	case *types.List:
		switch attr.Name {
		case "append":
			if len(args) != 1 || !types.AssignableTo(args[0], t.Elem) {
				c.errs.Add(n.Pos(), token.TypeMismatch, "list.append expects %s", t.Elem)
				return types.Invalid
			}
			return types.None
		case "pop":
			if len(args) > 1 || (len(args) == 1 && !types.Equal(args[0], types.Int)) {
				c.errs.Add(n.Pos(), token.TypeMismatch, "list.pop expects optional int")
				return types.Invalid
			}
			return t.Elem
		}
	case *types.Dict:
		switch attr.Name {
		case "get":
			if len(args) < 1 || len(args) > 2 || !types.AssignableTo(args[0], t.Key) || (len(args) == 2 && !types.AssignableTo(args[1], t.Value)) {
				c.errs.Add(n.Pos(), token.TypeMismatch, "dict.get expects key and optional default")
				return types.Invalid
			}
			return t.Value
		case "keys":
			if len(args) == 0 {
				return types.NewList(t.Key)
			}
		case "values":
			if len(args) == 0 {
				return types.NewList(t.Value)
			}
		case "items":
			if len(args) == 0 {
				return types.NewList(types.NewTuple(t.Key, t.Value))
			}
		}
	default:
		if types.Equal(recv, types.Str) {
			switch attr.Name {
			case "upper", "lower":
				if len(args) == 0 {
					return types.Str
				}
			case "split":
				if len(args) <= 1 && (len(args) == 0 || types.Equal(args[0], types.Str)) {
					return types.NewList(types.Str)
				}
			case "join":
				if len(args) == 1 {
					if list, ok := args[0].(*types.List); ok && types.Equal(list.Elem, types.Str) {
						return types.Str
					}
				}
			case "find":
				if len(args) == 1 && types.Equal(args[0], types.Str) {
					return types.Int
				}
			}
		}
	}
	c.errs.Add(n.Pos(), token.UnsupportedFeature, "method %s on %s is not supported", attr.Name, recv)
	return types.Invalid
}

// isConstIntLiteral reports whether e is an int literal, optionally with a unary
// +/- sign; this catches range(..., 0) statically when possible.
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

// constIntValue evaluates a constant int literal accepted by isConstIntLiteral.
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
