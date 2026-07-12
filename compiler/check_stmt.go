package compiler

import (
	"strings"

	"github.com/siyul-park/minipy/ast"
	"github.com/siyul-park/minipy/token"
	"github.com/siyul-park/minipy/types"
)

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
		c.functionStmt(n)
	case *ast.Class:
		c.classStmt(n)
	case *ast.Import:
		c.importStmt(n)
	case *ast.ImportFrom:
		c.importFromStmt(n)
	case *ast.TypeAlias:
		c.aliases[c.key(n.Name.Name)] = &alias{expr: n.Value, pos: n.Name.Pos()}
	case *ast.Global:
		if c.current == nil {
			c.errs.Add(n.Pos(), token.SyntaxError, "'global' outside function")
			return
		}
		for _, name := range n.Names {
			c.current.globals[name] = true
		}
	case *ast.Nonlocal:
		if c.current == nil {
			c.errs.Add(n.Pos(), token.SyntaxError, "'nonlocal' outside function")
			return
		}
		for _, name := range n.Names {
			if c.findEnclosingLocal(name) == nil {
				c.errs.Add(n.Pos(), token.NoBindingForNonlocal, "no binding for nonlocal %q found", name)
				continue
			}
			c.current.nonlocal[name] = true
		}
	case *ast.Return:
		c.returnStmt(n)
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

func (c *checker) importStmt(n *ast.Import) {
	if c.current != nil {
		c.errs.Add(n.Pos(), token.UnsupportedFeature, "import is only supported at module top level")
		return
	}
	for _, a := range n.Names {
		m := c.loader.loadModule(a.Name, a.Pos())
		c.checkModule(m)
		if m == nil {
			continue
		}
		local := a.As
		target := a.Name
		if local == "" {
			local = strings.Split(a.Name, ".")[0]
			target = local
		}
		c.mod.bindings[local] = binding{module: target}
	}
}

func (c *checker) importFromStmt(n *ast.ImportFrom) {
	if c.current != nil {
		c.errs.Add(n.Pos(), token.UnsupportedFeature, "from import is only supported at module top level")
		return
	}
	if n.Level == 0 && n.Module == "__future__" {
		return
	}
	if len(n.Names) == 1 && n.Names[0].Name == "*" {
		c.importStar(n)
		return
	}
	base := c.loader.resolveFrom(c.mod, n)
	if base == "" {
		return
	}
	m := c.loader.loadModule(base, n.Pos())
	c.checkModule(m)
	if m == nil {
		return
	}
	for _, a := range n.Names {
		local := a.As
		if local == "" {
			local = a.Name
		}
		res := c.resolveModuleAttr(base, a.Name)
		switch res.kind {
		case "function", "class", "global", "native":
			c.mod.bindings[local] = binding{module: base, symbol: a.Name}
		case "module":
			c.mod.bindings[local] = binding{module: res.module}
		default:
			errMark := len(c.loader.errs)
			sub := c.loader.loadModule(base+"."+a.Name, a.Pos())
			if sub == nil {
				c.loader.errs = c.loader.errs[:errMark]
			}
			c.checkModule(sub)
			if sub != nil {
				c.mod.bindings[local] = binding{module: sub.name}
				continue
			}
			c.errs.Add(a.Pos(), token.ImportError, "cannot import name %q from %q", a.Name, base)
		}
	}
}

func (c *checker) importStar(n *ast.ImportFrom) {
	base := c.loader.resolveFrom(c.mod, n)
	if base == "" {
		return
	}
	m := c.loader.loadModule(base, n.Pos())
	c.checkModule(m)
	if m == nil {
		return
	}
	if m.allSeen && !m.allStatic {
		c.errs.Add(n.Pos(), token.UnsupportedFeature, "from-import * requires a static __all__ in %q", base)
		return
	}
	for _, name := range m.exports {
		if c.localBindingExists(name) {
			c.errs.Add(n.Names[0].Pos(), token.ImportError, "from-import * from %q conflicts with local name %q", base, name)
			continue
		}
		res := c.resolveModuleAttr(base, name)
		switch res.kind {
		case "function", "class", "global", "native":
			c.mod.bindings[name] = binding{module: base, symbol: name}
		case "module":
			c.mod.bindings[name] = binding{module: res.module}
		default:
			c.errs.Add(n.Names[0].Pos(), token.ImportError, "cannot import name %q from %q", name, base)
		}
	}
}

func (c *checker) localBindingExists(name string) bool {
	if _, ok := c.mod.bindings[name]; ok {
		return true
	}
	key := c.key(name)
	if _, ok := c.globals[key]; ok {
		return true
	}
	if _, ok := c.functions[key]; ok {
		return true
	}
	if _, ok := c.classes[key]; ok {
		return true
	}
	return false
}

func (c *checker) tryStmt(n *ast.Try) {
	state := c.snapshotInits()
	c.checkBlock(n.Body)
	for _, h := range n.Handlers {
		if h.Star {
			c.errs.Add(h.Pos(), token.UnsupportedFeature, "except* is not supported yet")
		}
		ht := c.handlerType(h)
		if h.Name != "" {
			c.bindCapture(h.Name, ht, h.Pos())
		}
		c.excepts++
		c.checkBlock(h.Body)
		c.excepts--
	}
	c.checkBlock(n.Orelse)
	c.restoreInits(state)
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
	if n.Cause != nil {
		c.expr(n.Cause)
	}
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
	if n.Async {
		c.errs.Add(n.Pos(), token.UnsupportedFeature, "async with is parse-only until scheduler support lands")
	}
	for _, item := range n.Items {
		receiver := c.expr(item.Context)
		cls, ok := receiver.(*types.Class)
		if !ok {
			if receiver != types.Invalid {
				c.errs.Add(item.Pos(), token.UnsupportedFeature, "with requires a context manager, got %s", receiver)
			}
			continue
		}
		info := c.classes[cls.Name]
		enter := method(info, "__enter__")
		exit := method(info, "__exit__")
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
			} else if c.current != nil {
				l := c.declareLocal(name.Name, enter.result, name.Pos())
				l.init = true
				c.types[name] = l.typ
			} else {
				g := c.declare(name.Name, enter.result, name.Pos())
				g.init = true
				c.types[name] = g.typ
			}
		}
	}
	c.checkBlock(n.Body)
}

func (c *checker) snapshotInits() initState {
	locals := map[string]bool{}
	if c.current != nil {
		for name, l := range c.current.locals {
			locals[name] = l.init
		}
	}
	globals := map[string]bool{}
	for name, g := range c.globals {
		globals[name] = g.init
	}
	return initState{locals: locals, globals: globals}
}

func (c *checker) restoreInits(state initState) {
	if c.current != nil {
		for name, l := range c.current.locals {
			l.init = state.locals[name]
		}
	}
	for name, g := range c.globals {
		g.init = state.globals[name]
	}
}

func (c *checker) mergeInits(left, right initState) {
	if c.current != nil {
		for name, l := range c.current.locals {
			l.init = left.locals[name] && right.locals[name]
		}
	}
	for name, g := range c.globals {
		g.init = left.globals[name] && right.globals[name]
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
			receiver := c.expr(t.X)
			if slice, ok := t.Index.(*ast.Slice); ok {
				c.listSliceMutation(t, slice, receiver, nil)
				continue
			}
			index := c.expr(t.Index)
			switch receiver.(type) {
			case *types.List, *types.Dict:
				c.indexResultType(t, receiver, index)
			default:
				if receiver != types.Invalid {
					c.errs.Add(t.Pos(), token.UnsupportedFeature, "cannot delete an item from %s", receiver)
				}
			}
		case *ast.Attribute:
			c.fieldType(t, c.expr(t.X))
		default:
			c.errs.Add(target.Pos(), token.SyntaxError, "cannot delete this expression")
		}
	}
}

func (c *checker) deleteName(n *ast.Name) {
	if c.current != nil {
		if c.current.globals[n.Name] {
			c.deleteGlobalName(n)
			return
		}
		if l, ok := c.current.locals[n.Name]; ok {
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
	g, ok := c.globals[c.resolveName(n.Name).key]
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
	state := c.snapshotInits()
	for _, cs := range n.Cases {
		c.restoreInits(state)
		c.checkPattern(cs.Pattern, subjT)
		if cs.Guard != nil {
			c.condition(cs.Guard)
		}
		c.checkBlock(cs.Body)
	}
	c.restoreInits(state)
}

// bindCapture declares a pattern capture variable in the current scope and marks
// it initialized; re-binding the same name must keep a consistent type.
func (c *checker) bindCapture(name string, t types.Type, pos token.Pos) {
	if name == "" || name == "_" {
		return
	}
	if c.current != nil {
		if l, ok := c.current.locals[name]; ok {
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
	if g, ok := c.globals[c.resolveName(name).key]; ok {
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
		value := c.expr(pat.Value)
		if value != types.Invalid && subjT != types.Invalid && !types.Equal(value, subjT) {
			c.errs.Add(pat.Pos(), token.PatternError, "pattern value %s is not comparable to subject %s", value, subjT)
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
			prefix := pat.Star
			suffix := len(pat.Elems) - pat.Star - 1
			if len(s.Elems) < prefix+suffix {
				c.errs.Add(pat.Pos(), token.PatternError, "sequence pattern expects at least %d elements, %s has %d", prefix+suffix, subjT, len(s.Elems))
				return
			}
			for i := 0; i < prefix; i++ {
				c.checkPattern(pat.Elems[i], s.Elems[i])
			}
			for j := 0; j < suffix; j++ {
				c.checkPattern(pat.Elems[prefix+1+j], s.Elems[len(s.Elems)-suffix+j])
			}
			// The captured rest must be homogeneous so it binds as list[T].
			star := pat.Elems[prefix].(*ast.StarPattern)
			elemType := homogeneous(s.Elems[prefix : len(s.Elems)-suffix])
			if elemType == types.Invalid {
				c.errs.Add(star.Pos(), token.TypeMismatch, "starred tuple rest must have homogeneous type")
				return
			}
			c.bindCapture(star.Name, types.NewList(elemType), star.Pos())
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
	display := ""
	key := ""
	if name, ok := pat.Class.(*ast.Name); ok {
		display = name.Name
		key = c.resolveName(name.Name).key
	} else if attr, ok := pat.Class.(*ast.Attribute); ok {
		display = attr.Name
		if mod, ok := c.moduleExpr(attr.X); ok {
			res := c.resolveModuleAttr(mod, attr.Name)
			key = res.key
			c.attrSym[attr] = key
		}
	} else {
		c.errs.Add(pat.Pos(), token.UnsupportedFeature, "class pattern target is not supported")
		return
	}
	info := c.classes[key]
	if info == nil {
		c.errs.Add(pat.Pos(), token.UndefinedName, "class %q is not defined", display)
		return
	}
	if subjT != types.Invalid && !types.Equal(subjT, info.typ) {
		c.errs.Add(pat.Pos(), token.PatternError, "class pattern %s does not match subject %s", display, subjT)
	}
	for i, sub := range pat.Args {
		if i < len(info.fields) {
			c.checkPattern(sub, info.fields[i].typ)
		} else {
			c.errs.Add(sub.Pos(), token.PatternError, "class %s has no positional field %d", display, i)
		}
	}
	for i, kw := range pat.KwNames {
		idx, ok := info.fieldIndex[kw]
		if !ok {
			c.errs.Add(pat.Kw[i].Pos(), token.UndefinedName, "field %q is not defined on %s", kw, display)
			continue
		}
		c.checkPattern(pat.Kw[i], info.fields[idx].typ)
	}
}

func (c *checker) ifStmt(n *ast.If) {
	c.condition(n.Cond)
	if known, truth := c.truth(n.Cond); known {
		if truth {
			c.checkBlock(n.Body)
		} else {
			c.checkBlock(n.Orelse)
		}
		return
	}
	pos, neg := c.narrowings(n.Cond)
	state := c.snapshotInits()
	c.withNarrow(pos, func() { c.checkBlock(n.Body) })
	left := c.snapshotInits()
	c.restoreInits(state)
	c.withNarrow(neg, func() { c.checkBlock(n.Orelse) })
	right := c.snapshotInits()
	c.mergeInits(left, right)
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
	if n.Async {
		c.errs.Add(n.Pos(), token.UnsupportedFeature, "async for is parse-only until scheduler support lands")
	}
	target := forTargetName(n.Target)
	iter := c.expr(n.Iter)
	elem := iterableElem(iter)
	if elem == types.Invalid {
		c.errs.Add(n.Iter.Pos(), token.NotIterable, "%s is not iterable", iter)
	}
	if tupleTarget, ok := n.Target.(*ast.TupleLit); ok {
		c.bindForTupleTarget(tupleTarget, elem)
	} else if c.current != nil {
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
		if types.Equal(t, types.Bytes) {
			return types.Int
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
		if c.current != nil {
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
	if types.Equal(t, types.TypeAlias) {
		c.typeAliasAssign(n)
		return
	}
	var g *global
	var l *local
	if c.current != nil {
		l = c.declareLocal(n.Target.Name, t, n.Pos())
	} else {
		g = c.declare(n.Target.Name, t, n.Pos())
	}
	if n.Value != nil {
		value := c.exprWithHint(n.Value, t)
		if t != types.Invalid && value != types.Invalid && !types.AssignableTo(value, t) {
			c.errs.Add(n.Value.Pos(), token.TypeMismatch, "cannot assign %s to %s %q", value, t, n.Target.Name)
		}
		if l != nil {
			l.init = true
		} else {
			g.init = true
		}
	}
}

func (c *checker) typeAliasAssign(n *ast.AnnAssign) {
	c.aliasDecls[n] = true
	if n.Value == nil {
		c.errs.Add(n.Pos(), token.MissingAnnotation, "type alias %q needs a value", n.Target.Name)
		return
	}
	c.aliases[c.key(n.Target.Name)] = &alias{expr: n.Value, pos: n.Target.Pos()}
}

// alias is a module-level type-alias declaration: its RHS, source position, and
// (once resolved) the concrete type it names.
type alias struct {
	expr      ast.Expr
	pos       token.Pos
	typ       types.Type
	resolving bool
}

// collectAliases records every type-alias RHS before any type is resolved, so
// aliases may reference one another regardless of declaration order. The
// legacy `Name: TypeAlias = expr` form is handled later by typeAliasAssign,
// which recognizes the alias via the resolved annotation type (so a shadowed
// `TypeAlias` name is not mistaken for it).
func (c *checker) collectAliases(body []ast.Stmt) {
	for _, s := range body {
		if n, ok := s.(*ast.TypeAlias); ok {
			c.aliases[c.key(n.Name.Name)] = &alias{expr: n.Value, pos: n.Name.Pos()}
		}
	}
}

// resolveAliases resolves every collected alias. Running it as a dedicated pass
// ensures unused aliases still report cycles or undefined targets.
func (c *checker) resolveAliases() {
	for key := range c.aliases {
		c.resolveAlias(key)
	}
}

// resolveAlias resolves a single alias, caching the result and guarding against
// recursive cycles.
func (c *checker) resolveAlias(key string) types.Type {
	a := c.aliases[key]
	if a == nil {
		return types.Invalid
	}
	if a.resolving {
		c.errs.Add(a.pos, token.CyclicAlias, "recursive type alias %q", key)
		return types.Invalid
	}
	if a.typ != nil {
		return a.typ
	}
	a.resolving = true
	defer func() { a.resolving = false }()
	a.typ = c.resolveType(a.expr)
	return a.typ
}

func (c *checker) assign(n *ast.Assign) {
	name, ok := n.Target.(*ast.Name)
	if !ok {
		c.assignTarget(n.Target, n.Value, n.Pos())
		return
	}
	if c.current == nil {
		if _, isFunc := c.functions[c.key(name.Name)]; isFunc {
			c.errs.Add(n.Pos(), token.TypeMismatch, "cannot assign to function %q", name.Name)
			c.expr(n.Value)
			return
		}
	}
	value := c.expr(n.Value)
	if c.current != nil {
		switch {
		case c.current.globals[name.Name]:
		case c.current.nonlocal[name.Name]:
			cap := c.capture(name)
			if cap == nil {
				return
			}
			if cap.typ != types.Invalid && value != types.Invalid && !types.AssignableTo(value, cap.typ) {
				c.errs.Add(n.Value.Pos(), token.TypeMismatch, "cannot assign %s to %s %q", value, cap.typ, name.Name)
			}
			cap.boxed = true
			cap.src.boxed = true
			c.types[name] = cap.typ
			return
		default:
			l, declared := c.current.locals[name.Name]
			if !declared {
				l = c.declareLocal(name.Name, value, n.Pos())
			}
			if l.typ != types.Invalid && value != types.Invalid && !types.AssignableTo(value, l.typ) {
				c.errs.Add(n.Value.Pos(), token.TypeMismatch, "cannot assign %s to %s %q", value, l.typ, name.Name)
			}
			l.init = true
			c.types[name] = l.typ
			return
		}
	}
	g, declared := c.globals[c.key(name.Name)]
	if !declared {
		// Whole-program inference: an unannotated global takes the type of its
		// first assignment instead of requiring an annotation.
		g = c.declare(name.Name, value, n.Pos())
		g.init = true
		c.types[name] = value
		return
	}
	if g.typ != types.Invalid && value != types.Invalid && !types.AssignableTo(value, g.typ) {
		c.errs.Add(n.Value.Pos(), token.TypeMismatch, "cannot assign %s to %s %q", value, g.typ, name.Name)
	}
	g.init = true
	c.types[name] = g.typ
}

func (c *checker) assignTarget(target ast.Expr, value ast.Expr, pos token.Pos) {
	switch t := target.(type) {
	case *ast.Subscript:
		receiver := c.expr(t.X)
		if slice, ok := t.Index.(*ast.Slice); ok {
			c.listSliceMutation(t, slice, receiver, value)
			return
		}
		if cls, ok := receiver.(*types.Class); ok {
			c.classSetItem(t, cls, value)
			return
		}
		if types.Equal(receiver, types.Bytes) {
			c.errs.Add(t.Pos(), token.NotIndexable, "bytes does not support item assignment")
			c.expr(t.Index)
			c.expr(value)
			return
		}
		index := c.expr(t.Index)
		valueType := c.expr(value)
		elem := c.indexResultType(t, receiver, index)
		if elem != types.Invalid && valueType != types.Invalid && !types.AssignableTo(valueType, elem) {
			c.errs.Add(value.Pos(), token.TypeMismatch, "cannot assign %s to indexed %s", valueType, elem)
		}
	case *ast.Attribute:
		receiver := c.expr(t.X)
		field := c.fieldType(t, receiver)
		valueType := c.expr(value)
		if field != types.Invalid && valueType != types.Invalid && !types.AssignableTo(valueType, field) {
			c.errs.Add(value.Pos(), token.TypeMismatch, "cannot assign %s to field %s", valueType, field)
		}
	case *ast.TupleLit:
		c.unpackAssign(t, c.expr(value), value.Pos())
	default:
		c.errs.Add(pos, token.SyntaxError, "cannot assign to this expression")
		c.expr(value)
	}
}

// classSetItem resolves obj[index] = value for a class instance via
// __setitem__, validating the index and value against the method's declared
// parameters and requiring a None result.
func (c *checker) classSetItem(t *ast.Subscript, cls *types.Class, value ast.Expr) {
	index := c.expr(t.Index)
	valueType := c.expr(value)
	info := c.classes[cls.Name]
	if info == nil {
		return
	}
	m := method(info, "__setitem__")
	if m == nil || len(m.params) != 3 {
		c.errs.Add(t.Pos(), token.NotIndexable, "%s does not support item assignment", cls)
		return
	}
	idxParam := m.params[1].typ
	if index != types.Invalid && idxParam != types.Invalid && !types.AssignableTo(index, idxParam) {
		c.errs.Add(t.Index.Pos(), token.TypeMismatch, "%s.__setitem__ index must be %s, got %s", info.name, idxParam, index)
	}
	valParam := m.params[2].typ
	if valueType != types.Invalid && valParam != types.Invalid && !types.AssignableTo(valueType, valParam) {
		c.errs.Add(value.Pos(), token.TypeMismatch, "%s.__setitem__ value must be %s, got %s", info.name, valParam, valueType)
	}
	if m.result != types.Invalid && !types.Equal(m.result, types.None) {
		c.errs.Add(t.Pos(), token.TypeMismatch, "%s.__setitem__ must return None, got %s", info.name, m.result)
	}
}

func (c *checker) listSliceMutation(target *ast.Subscript, slice *ast.Slice, receiver types.Type, value ast.Expr) {
	c.checkSliceBounds(slice)
	if !supportedMutationSliceStep(slice.Step) {
		c.errs.Add(slice.Step.Pos(), token.UnsupportedFeature, "extended slice assignment is not supported")
	}
	list, ok := receiver.(*types.List)
	if !ok {
		if types.Equal(receiver, types.Bytes) {
			c.errs.Add(target.Pos(), token.NotIndexable, "bytes is immutable and does not support slice assignment")
		} else if receiver != types.Invalid {
			c.errs.Add(target.Pos(), token.UnsupportedFeature, "cannot mutate a slice of %s", receiver)
		}
		if value != nil {
			c.expr(value)
		}
		return
	}
	if value == nil {
		return
	}
	valueType := c.exprWithHint(value, receiver)
	if valueType != types.Invalid && !types.Equal(valueType, receiver) {
		c.errs.Add(value.Pos(), token.TypeMismatch, "slice assignment expects list[%s], got %s", list.Elem, valueType)
	}
}

func supportedMutationSliceStep(step ast.Expr) bool {
	if step == nil {
		return true
	}
	if lit, ok := step.(*ast.IntLit); ok {
		return lit.Value == 1
	}
	return false
}

func (c *checker) unpackAssign(target *ast.TupleLit, value types.Type, pos token.Pos) {
	var elems []types.Type
	var listElem types.Type
	switch t := value.(type) {
	case *types.Tuple:
		elems = t.Elems
	case *types.List:
		listElem = t.Elem
		elems = make([]types.Type, len(target.Elems))
		for i := range elems {
			elems[i] = t.Elem
		}
	default:
		if value != types.Invalid {
			c.errs.Add(pos, token.TypeMismatch, "cannot unpack %s", value)
		}
		return
	}
	star := tupleStarIndex(target)
	if star == -2 {
		c.errs.Add(target.Pos(), token.SyntaxError, "multiple starred targets in assignment")
		return
	}
	if star < 0 && len(elems) != len(target.Elems) {
		c.errs.Add(pos, token.ArityMismatch, "unpack needs %d values, got %d", len(target.Elems), len(elems))
		return
	}
	if star >= 0 && len(elems) < len(target.Elems)-1 {
		c.errs.Add(pos, token.ArityMismatch, "not enough values to unpack")
		return
	}
	for i, elem := range target.Elems {
		if star, ok := elem.(*ast.Starred); ok {
			name, ok := star.X.(*ast.Name)
			if !ok {
				c.errs.Add(star.Pos(), token.SyntaxError, "starred assignment target must be a name")
				continue
			}
			elemType := listElem
			if elemType == nil {
				rest := elems[i : len(elems)-(len(target.Elems)-i-1)]
				elemType = homogeneous(rest)
				if elemType == types.Invalid {
					c.errs.Add(star.Pos(), token.TypeMismatch, "starred tuple rest must have homogeneous type")
					continue
				}
			}
			c.bindUnpackedName(name, types.NewList(elemType))
			continue
		}
		name, ok := elem.(*ast.Name)
		if !ok {
			c.errs.Add(elem.Pos(), token.SyntaxError, "tuple unpack target must be a name")
			continue
		}
		srcIdx := i
		if star >= 0 && i > star {
			srcIdx = len(elems) - (len(target.Elems) - i)
		}
		c.bindUnpackedName(name, elems[srcIdx])
	}
}

func tupleStarIndex(target *ast.TupleLit) int {
	star := -1
	for i, elem := range target.Elems {
		if _, ok := elem.(*ast.Starred); ok {
			if star >= 0 {
				return -2
			}
			star = i
		}
	}
	return star
}

func homogeneous(ts []types.Type) types.Type {
	if len(ts) == 0 {
		return types.Any
	}
	first := ts[0]
	for _, t := range ts[1:] {
		if !types.Equal(first, t) {
			return types.Invalid
		}
	}
	return first
}

func (c *checker) bindUnpackedName(name *ast.Name, t types.Type) {
	if c.current != nil {
		l, declared := c.current.locals[name.Name]
		if !declared {
			l = c.declareLocal(name.Name, t, name.Pos())
		}
		if !types.AssignableTo(t, l.typ) {
			c.errs.Add(name.Pos(), token.TypeMismatch, "cannot assign %s to %s %q", t, l.typ, name.Name)
		}
		l.init = true
		c.types[name] = l.typ
		return
	}
	g, declared := c.globals[c.key(name.Name)]
	if !declared {
		g = c.declare(name.Name, t, name.Pos())
		g.init = true
		c.types[name] = g.typ
		return
	}
	if !types.AssignableTo(t, g.typ) {
		c.errs.Add(name.Pos(), token.TypeMismatch, "cannot assign %s to %s %q", t, g.typ, name.Name)
	}
	g.init = true
	c.types[name] = g.typ
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
		receiver := c.expr(attr.X)
		field := c.fieldType(attr, receiver)
		value := c.expr(n.Value)
		result := c.binaryType(field, n.Op, value, n.Pos())
		if result != types.Invalid && field != types.Invalid && !types.AssignableTo(result, field) {
			c.errs.Add(n.Pos(), token.TypeMismatch, "result %s is not assignable to field %s", result, field)
		}
		return
	}
	if c.current != nil {
		switch {
		case c.current.globals[name.Name]:
		case c.current.nonlocal[name.Name]:
			cap := c.capture(name)
			if cap == nil {
				c.expr(n.Value)
				return
			}
			c.types[name] = cap.typ
			value := c.expr(n.Value)
			result := c.binaryType(cap.typ, n.Op, value, n.Pos())
			if result != types.Invalid && cap.typ != types.Invalid && !types.AssignableTo(result, cap.typ) {
				c.errs.Add(n.Pos(), token.TypeMismatch, "result %s is not assignable to %s %q", result, cap.typ, name.Name)
			}
			cap.boxed = true
			cap.src.boxed = true
			return
		default:
			l, declared := c.current.locals[name.Name]
			if !declared {
				c.errs.Add(n.Pos(), token.UndefinedName, "name %q is not defined", name.Name)
				c.expr(n.Value)
				return
			}
			if !l.init {
				c.errs.Add(n.Pos(), token.UseBeforeDefinition, "name %q used before assignment", name.Name)
			}
			c.types[name] = l.typ
			value := c.expr(n.Value)
			result := c.binaryType(l.typ, n.Op, value, n.Pos())
			if result != types.Invalid && l.typ != types.Invalid && !types.AssignableTo(result, l.typ) {
				c.errs.Add(n.Pos(), token.TypeMismatch, "result %s is not assignable to %s %q", result, l.typ, name.Name)
			}
			l.init = true
			return
		}
	}
	g, declared := c.globals[c.key(name.Name)]
	if !declared {
		c.errs.Add(n.Pos(), token.UndefinedName, "name %q is not defined", name.Name)
		c.expr(n.Value)
		return
	}
	if !g.init {
		c.errs.Add(n.Pos(), token.UseBeforeDefinition, "name %q used before assignment", name.Name)
	}
	c.types[name] = g.typ
	value := c.expr(n.Value)
	result := c.binaryType(g.typ, n.Op, value, n.Pos())
	if result != types.Invalid && g.typ != types.Invalid && !types.AssignableTo(result, g.typ) {
		c.errs.Add(n.Pos(), token.TypeMismatch, "result %s is not assignable to %s %q", result, g.typ, name.Name)
	}
	g.init = true
}

// declare registers a new global or returns the existing one, reporting a type
// change on redeclaration.
