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
	errs    token.ErrorList
	types   map[ast.Expr]types.Type
	globals map[string]*global
	funcs   map[string]*fn
	lambdas map[*ast.LambdaExpr]*fn
	order   []string
	loops   int // enclosing-loop depth, for break/continue validation
	fn      *fn
}

func newChecker() *checker {
	return &checker{
		types:   map[ast.Expr]types.Type{},
		globals: map[string]*global{},
		funcs:   map[string]*fn{},
		lambdas: map[*ast.LambdaExpr]*fn{},
	}
}

// check walks every top-level statement, accumulating diagnostics.
func (c *checker) check(mod *ast.Module) {
	c.declareFuncs(mod.Body)
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
	case *ast.Function:
		c.function(n)
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
		c.errs.Add(n.Pos(), token.MissingAnnotation, "global %q needs a type annotation on its first assignment", name.Name)
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
			c.errs.Add(name.Pos(), token.MissingAnnotation, "global %q needs a type annotation on its first assignment", name.Name)
			g = c.declare(name.Name, elems[i], name.Pos())
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
		if t != types.Invalid && g.typ != types.Invalid && !types.Equal(g.typ, t) {
			c.errs.Add(pos, token.TypeMismatch, "cannot redeclare %q from %s to %s", name, g.typ, t)
		}
		return g
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
	info.local = c.declareLocal(info.name, types.CallableOf(srcTypes(info.params), info.ret), pos)
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
		if _, exists := c.globals[f.Name.Name]; exists {
			c.errs.Add(f.Name.Pos(), token.TypeMismatch, "cannot redeclare %q as a function", f.Name.Name)
			continue
		}
		info := &fn{
			name:      f.Name.Name,
			ret:       c.resolveType(f.Returns),
			generator: containsYield(f.Body),
			locals:    map[string]*local{},
			children:  map[string]*fn{},
			captures:  map[string]*capture{},
			globals:   map[string]bool{},
			nonlocal:  map[string]bool{},
		}
		for _, p := range f.Params {
			pt := c.resolveType(p.Ann)
			info.params = append(info.params, param{name: p.Name.Name, typ: pt})
		}
		info.slot = c.declare(f.Name.Name, types.Invalid, f.Pos())
		c.funcs[f.Name.Name] = info
	}
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
			ret:       c.resolveType(f.Returns),
			generator: containsYield(f.Body),
			locals:    map[string]*local{},
			parent:    info,
			children:  map[string]*fn{},
			captures:  map[string]*capture{},
			globals:   map[string]bool{},
			nonlocal:  map[string]bool{},
		}
		for _, p := range f.Params {
			child.params = append(child.params, param{name: p.Name.Name, typ: c.resolveType(p.Ann)})
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

func (c *checker) resolveType(e ast.Expr) types.Type {
	if name, ok := e.(*ast.Name); ok {
		if resolved, known := types.Resolve(name.Name); known {
			return resolved
		}
		c.errs.Add(e.Pos(), token.UnsupportedType, "unknown type %q", name.Name)
		return types.Invalid
	}
	if sub, ok := e.(*ast.Subscript); ok {
		base, ok := sub.X.(*ast.Name)
		if !ok {
			c.errs.Add(e.Pos(), token.UnsupportedType, "unsupported type annotation")
			return types.Invalid
		}
		switch base.Name {
		case "list":
			return types.ListOf(c.resolveType(sub.Index))
		case "set":
			elem := c.resolveType(sub.Index)
			if elem != types.Invalid && !hashableKey(elem) {
				c.errs.Add(sub.Index.Pos(), token.UnsupportedType, "set element type %s is not supported", elem)
				return types.Invalid
			}
			return types.SetOf(elem)
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
			return types.DictOf(key, c.resolveType(args.Elems[1]))
		case "tuple":
			if args, ok := sub.Index.(*ast.TupleLit); ok {
				elems := make([]types.Type, len(args.Elems))
				for i, elem := range args.Elems {
					elems[i] = c.resolveType(elem)
				}
				return types.TupleOf(elems...)
			}
			return types.TupleOf(c.resolveType(sub.Index))
		case "Iterator":
			return types.IteratorOf(c.resolveType(sub.Index))
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
			return types.CallableOf(params, c.resolveType(args.Elems[1]))
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
	rt := types.None
	if n.Value != nil {
		rt = c.exprWithHint(n.Value, c.fn.ret)
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
		return types.TupleOf(elems...)
	case *ast.LambdaExpr:
		return c.lambda(n, hint)
	case *ast.SetLit:
		return c.setType(n, hint)
	case *ast.ListComp:
		elem := c.compElem(n.Clauses, n.Elem)
		return types.ListOf(elem)
	case *ast.DictComp:
		cleanup := c.compClauses(n.Clauses)
		defer cleanup()
		kt := c.expr(n.Key)
		vt := c.expr(n.Value)
		if !hashableKey(kt) {
			c.errs.Add(n.Key.Pos(), token.UnsupportedType, "dict key type %s is not supported", kt)
			return types.Invalid
		}
		return types.DictOf(kt, vt)
	case *ast.SetComp:
		elem := c.compElem(n.Clauses, n.Elem)
		if !hashableKey(elem) {
			c.errs.Add(n.Elem.Pos(), token.UnsupportedType, "set element type %s is not supported", elem)
			return types.Invalid
		}
		return types.SetOf(elem)
	case *ast.Subscript:
		ct := c.expr(n.X)
		it := c.expr(n.Index)
		return c.indexResultType(n, ct, it)
	case *ast.Attribute:
		c.expr(n.X)
		c.errs.Add(n.Pos(), token.UnsupportedFeature, "attribute value access is M5; M3 supports method calls only")
		return types.Invalid
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
	return types.ListOf(elem)
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
	return types.DictOf(kt, vt)
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
		return types.CallableOf(srcTypes(fn.params), fn.ret)
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
	return types.SetOf(elem)
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
			return types.ListOf(ll.Elem)
		}
		return c.arith(lt, op, rt, pos)
	case token.STAR:
		if types.Equal(lt, types.Str) && types.Equal(rt, types.Int) {
			return types.Str
		}
		if ll, ok := lt.(*types.List); ok && types.Equal(rt, types.Int) {
			return types.ListOf(ll.Elem)
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
	if op == token.IN || op == token.NOTIN {
		if !containsType(lt, rt) {
			c.errs.Add(pos, token.NotIterable, "'%s' requires container RHS, got %s in %s", op, lt, rt)
		}
		return
	}
	if op == token.IS {
		return // already reported as UnsupportedFeature by the parser
	}
	if types.Equal(lt, types.None) || types.Equal(rt, types.None) {
		c.errs.Add(pos, token.UnsupportedFeature, "comparing to None uses 'is' (M7)")
		return
	}
	if !types.Equal(lt, rt) {
		c.errs.Add(pos, token.NotComparable, "'%s' not supported between instances of %s and %s", op, lt, rt)
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

func (c *checker) callType(n *ast.CallExpr) types.Type {
	name, ok := n.Fn.(*ast.Name)
	if !ok {
		if attr, ok := n.Fn.(*ast.Attribute); ok {
			return c.methodCallType(n, attr)
		}
		fnType := c.expr(n.Fn)
		return c.callableCallType(n, fnType)
	}

	argTypes := make([]types.Type, len(n.Args))
	for i, a := range n.Args {
		argTypes[i] = c.expr(a)
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
				return types.ListOf(t.Key)
			}
		case "values":
			if len(args) == 0 {
				return types.ListOf(t.Value)
			}
		case "items":
			if len(args) == 0 {
				return types.ListOf(types.TupleOf(t.Key, t.Value))
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
					return types.ListOf(types.Str)
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
// +/- sign; M6 uses this only to catch range(..., 0) statically when possible.
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
