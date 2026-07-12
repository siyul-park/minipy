package compiler

import (
	"github.com/siyul-park/minipy/ast"
	"github.com/siyul-park/minipy/module"
	"github.com/siyul-park/minipy/operator"
	"github.com/siyul-park/minipy/token"
	"github.com/siyul-park/minipy/types"
)

func (c *checker) expr(e ast.Expr) types.Type {
	return c.exprWithHint(e, nil)
}

func (c *checker) exprWithHint(e ast.Expr, hint types.Type) types.Type {
	if value, ok := literalValue(e); ok {
		if lit := literalHint(hint, value); lit != nil {
			t := types.NewLiteral(value)
			c.types[e] = t
			return t
		}
	}
	if lit, ok := hint.(*types.Literal); ok {
		if lit.Base != nil {
			hint = lit.Base
		}
	}
	t := c.typeOf(e, hint)
	c.types[e] = t
	return t
}

func literalHint(hint types.Type, value types.LiteralValue) *types.Literal {
	switch t := hint.(type) {
	case *types.Literal:
		if t.Contains(value) {
			return t
		}
	case *types.Union:
		for _, member := range t.Members {
			if lit := literalHint(member, value); lit != nil {
				return lit
			}
		}
	}
	return nil
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
	case *ast.BytesLit:
		return types.Bytes
	case *ast.NoneLit:
		return types.None
	case *ast.EllipsisLit:
		return types.Ellipsis
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
		var key, value types.Type
		c.compClauses(n.Clauses, func() {
			key = c.expr(n.Key)
			value = c.expr(n.Value)
		})
		if !hashableKey(key) {
			c.errs.Add(n.Key.Pos(), token.UnsupportedType, "dict key type %s is not supported", key)
			return types.Invalid
		}
		return types.NewDict(key, value)
	case *ast.SetComp:
		elem := c.compElem(n.Clauses, n.Elem)
		if !hashableKey(elem) {
			c.errs.Add(n.Elem.Pos(), token.UnsupportedType, "set element type %s is not supported", elem)
			return types.Invalid
		}
		return types.NewSet(elem)
	case *ast.Subscript:
		receiver := c.expr(n.X)
		if slice, ok := n.Index.(*ast.Slice); ok {
			return c.sliceResultType(slice, receiver)
		}
		index := c.expr(n.Index)
		return c.indexResultType(n, receiver, index)
	case *ast.Slice:
		c.checkSliceBounds(n)
		return types.Invalid
	case *ast.Starred:
		c.expr(n.X)
		c.errs.Add(n.Pos(), token.UnsupportedFeature, "starred unpacking is not supported yet")
		return types.Invalid
	case *ast.NamedExpr:
		c.assign(&ast.Assign{Base: n.Base, Target: n.Target, Value: n.Value})
		return c.currentType(n.Target.Name)
	case *ast.AwaitExpr:
		c.expr(n.X)
		c.errs.Add(n.Pos(), token.UnsupportedFeature, "await is parse-only until scheduler support lands")
		return types.Invalid
	case *ast.YieldExpr:
		if c.current != nil {
			c.current.generator = true
		}
		c.checkYield(n.Value, n.From, n.Pos())
		return types.None
	case *ast.GeneratorExp:
		elem := c.compElem(n.Clauses, n.Elem)
		t := types.NewIterator(elem)
		if c.genExprs[n] == nil {
			c.genExprs[n] = c.synthGenExpr(n, elem)
		}
		return t
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
		if list, ok := hint.(*types.List); ok {
			return list
		}
		c.errs.Add(n.Pos(), token.UnsupportedType, "empty list needs list[T] annotation")
		return types.Invalid
	}
	elem := c.listElemType(n.Elems[0])
	for _, e := range n.Elems[1:] {
		et := c.listElemType(e)
		if elem != types.Invalid && et != types.Invalid && !types.Equal(elem, et) {
			c.errs.Add(e.Pos(), token.TypeMismatch, "list elements must have same type: %s and %s", elem, et)
			return types.Invalid
		}
	}
	return types.NewList(elem)
}

func (c *checker) listElemType(e ast.Expr) types.Type {
	if star, ok := e.(*ast.Starred); ok {
		t := c.expr(star.X)
		switch x := t.(type) {
		case *types.List:
			return x.Elem
		case *types.Tuple:
			return homogeneous(x.Elems)
		default:
			if t != types.Invalid {
				c.errs.Add(star.Pos(), token.TypeMismatch, "starred list element must be list or tuple, got %s", t)
			}
			return types.Invalid
		}
	}
	return c.expr(e)
}

func (c *checker) dictType(n *ast.DictLit, hint types.Type) types.Type {
	if len(n.Keys) == 0 {
		if dt, ok := hint.(*types.Dict); ok {
			return dt
		}
		c.errs.Add(n.Pos(), token.UnsupportedType, "empty dict needs dict[K, V] annotation")
		return types.Invalid
	}
	for i, keyExpr := range n.Keys {
		if star, ok := keyExpr.(*ast.Starred); ok || n.Values[i] == nil {
			if ok {
				c.expr(star.X)
			} else {
				c.errs.Add(keyExpr.Pos(), token.UnsupportedFeature, "dict unpacking is not supported yet")
				return types.Invalid
			}
		}
	}
	key, value := c.dictEntryType(n.Keys[0], n.Values[0])
	for i := 1; i < len(n.Keys); i++ {
		k, v := c.dictEntryType(n.Keys[i], n.Values[i])
		if key != types.Invalid && k != types.Invalid && !types.Equal(key, k) {
			c.errs.Add(n.Keys[i].Pos(), token.TypeMismatch, "dict keys must have same type: %s and %s", key, k)
			return types.Invalid
		}
		if value != types.Invalid && v != types.Invalid && !types.Equal(value, v) {
			c.errs.Add(n.Values[i].Pos(), token.TypeMismatch, "dict values must have same type: %s and %s", value, v)
			return types.Invalid
		}
	}
	if !hashableKey(key) {
		c.errs.Add(n.Keys[0].Pos(), token.UnsupportedType, "dict key type %s is not supported", key)
		return types.Invalid
	}
	return types.NewDict(key, value)
}

func (c *checker) dictEntryType(key, value ast.Expr) (types.Type, types.Type) {
	if star, ok := key.(*ast.Starred); ok && value == nil {
		t := c.expr(star.X)
		if d, ok := t.(*types.Dict); ok {
			return d.Key, d.Value
		}
		if t != types.Invalid {
			c.errs.Add(star.Pos(), token.TypeMismatch, "dict unpacking requires dict, got %s", t)
		}
		return types.Invalid, types.Invalid
	}
	return c.expr(key), c.expr(value)
}

func hashableKey(t types.Type) bool {
	t = types.Erase(t)
	return types.Equal(t, types.Int) || types.Equal(t, types.Float) || types.Equal(t, types.Bool) || types.Equal(t, types.Str)
}

func (c *checker) indexResultType(n *ast.Subscript, receiver, index types.Type) types.Type {
	if types.Equal(index, types.Ellipsis) {
		c.errs.Add(n.Index.Pos(), token.UnsupportedFeature, "ellipsis subscript is not supported")
		return types.Invalid
	}
	switch t := receiver.(type) {
	case *types.List:
		if index != types.Invalid && !types.Equal(index, types.Int) {
			c.errs.Add(n.Index.Pos(), token.TypeMismatch, "list index must be int, got %s", index)
		}
		return t.Elem
	case *types.Dict:
		if index != types.Invalid && !types.AssignableTo(index, t.Key) {
			c.errs.Add(n.Index.Pos(), token.TypeMismatch, "dict key must be %s, got %s", t.Key, index)
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
	case *types.Class:
		return c.classGetItemType(n, t, index)
	default:
		if types.Equal(receiver, types.Str) {
			if index != types.Invalid && !types.Equal(index, types.Int) {
				c.errs.Add(n.Index.Pos(), token.TypeMismatch, "str index must be int, got %s", index)
			}
			return types.Str
		}
		if types.Equal(receiver, types.Bytes) {
			if index != types.Invalid && !types.Equal(index, types.Int) {
				c.errs.Add(n.Index.Pos(), token.TypeMismatch, "bytes index must be int, got %s", index)
			}
			return types.Int
		}
		if receiver != types.Invalid {
			c.errs.Add(n.Pos(), token.NotIndexable, "%s is not indexable", receiver)
		}
		return types.Invalid
	}
}

// classGetItemType resolves obj[index] for a class instance via __getitem__,
// validating the index against the method's declared index parameter.
func (c *checker) classGetItemType(n *ast.Subscript, cls *types.Class, index types.Type) types.Type {
	info := c.classes[cls.Name]
	if info == nil {
		return types.Invalid
	}
	m := method(info, "__getitem__")
	if m == nil || len(m.params) != 2 {
		c.errs.Add(n.Pos(), token.NotIndexable, "%s is not indexable", cls)
		return types.Invalid
	}
	param := m.params[1].typ
	if index != types.Invalid && param != types.Invalid && !types.AssignableTo(index, param) {
		c.errs.Add(n.Index.Pos(), token.TypeMismatch, "%s.__getitem__ index must be %s, got %s", info.name, param, index)
	}
	return m.result
}

func (c *checker) sliceResultType(n *ast.Slice, receiver types.Type) types.Type {
	c.checkSliceBounds(n)
	switch receiver.(type) {
	case *types.List:
		return receiver
	default:
		if types.Equal(receiver, types.Str) {
			return types.Str
		}
		if types.Equal(receiver, types.Bytes) {
			return types.Bytes
		}
		if receiver != types.Invalid {
			c.errs.Add(n.Pos(), token.NotIndexable, "%s is not sliceable", receiver)
		}
		return types.Invalid
	}
}

func (c *checker) checkSliceBounds(n *ast.Slice) {
	for _, expr := range []ast.Expr{n.Lower, n.Upper, n.Step} {
		if expr == nil {
			continue
		}
		t := c.expr(expr)
		if t != types.Invalid && !types.Equal(t, types.Int) {
			c.errs.Add(expr.Pos(), token.TypeMismatch, "slice bounds must be int, got %s", t)
		}
	}
}

func (c *checker) fieldType(n *ast.Attribute, receiver types.Type) types.Type {
	if mod, ok := receiver.(*types.Module); ok {
		res := c.resolveModuleAttr(mod.Name, n.Name)
		switch res.kind {
		case "module":
			c.attrMod[n] = res.module
			return types.NewModule(res.module)
		case "native":
			c.attrNative[n] = res.native
			c.errs.Add(n.Pos(), token.UnsupportedFeature, "native function %q is not a first-class value", nativeDisplay(res.key))
			return types.Invalid
		case "function":
			info := c.functions[res.key]
			c.attrSym[n] = res.key
			return types.NewCallable(srcTypes(info.params), info.result)
		case "class":
			c.attrSym[n] = res.key
			c.errs.Add(n.Pos(), token.UnsupportedFeature, "class value %q is not supported", n.Name)
			return types.Invalid
		case "global":
			c.attrSym[n] = res.key
			return c.globals[res.key].typ
		}
		c.errs.Add(n.Pos(), token.UndefinedName, "module %q has no attribute %q", mod.Name, n.Name)
		return types.Invalid
	}
	cls, ok := receiver.(*types.Class)
	if !ok {
		if receiver != types.Invalid {
			c.errs.Add(n.Pos(), token.UnsupportedFeature, "attribute %s on %s is not supported", n.Name, receiver)
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
	expr, ok := part.(*ast.FStringExpr)
	if !ok {
		return
	}
	t := c.expr(expr.Expr)
	if t != types.Invalid && !types.Printable(t) {
		c.errs.Add(expr.Pos(), token.TypeMismatch, "unsupported type %s in f-string replacement field", t)
	}
	if expr.Conversion != 0 && expr.Conversion != 's' && expr.Conversion != 'r' && expr.Conversion != 'a' {
		c.errs.Add(expr.Pos(), token.UnsupportedFeature, "unsupported f-string conversion !%c", expr.Conversion)
	}
	for _, fp := range expr.Format {
		// Nested replacement fields inside a format spec may not carry their
		// own format spec: only one level of nesting is supported.
		if nested, ok := fp.(*ast.FStringExpr); ok && len(nested.Format) > 0 {
			c.errs.Add(nested.Pos(), token.UnsupportedFeature, "f-string format spec nesting is too deep")
		}
		c.fstringPart(fp)
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
	if t, ok := c.temps[n.Name]; ok {
		return t
	}
	if t, ok := c.narrowed[n.Name]; ok {
		return t
	}
	if c.current != nil {
		if c.current.globals[n.Name] {
			return c.global(n)
		}
		if l, ok := c.current.locals[n.Name]; ok {
			if !l.init {
				c.errs.Add(n.Pos(), token.UseBeforeDefinition, "name %q used before assignment", n.Name)
			}
			return l.typ
		}
		if cap := c.capture(n); cap != nil {
			return cap.typ
		}
	}
	res := c.resolveName(n.Name)
	switch res.kind {
	case "module":
		return types.NewModule(res.module)
	case "native":
		c.errs.Add(n.Pos(), token.UnsupportedFeature, "native function %q is not a first-class value", nativeDisplay(res.key))
		return types.Invalid
	case "function":
		info := c.functions[res.key]
		if !info.slot.init {
			c.errs.Add(n.Pos(), token.UseBeforeDefinition, "function %q used before definition", n.Name)
		}
		return types.NewCallable(srcTypes(info.params), info.result)
	case "class":
		c.errs.Add(n.Pos(), token.UnsupportedFeature, "class value %q is not supported", n.Name)
		return types.Invalid
	}
	return c.global(n)
}

func (c *checker) global(n *ast.Name) types.Type {
	res := c.resolveName(n.Name)
	if res.kind == "module" {
		c.errs.Add(n.Pos(), token.UnsupportedFeature, "module object is not a first-class value")
		return types.Invalid
	}
	g, ok := c.globals[res.key]
	if !ok {
		if n.Name == "Ellipsis" {
			return types.Ellipsis
		}
		c.errs.Add(n.Pos(), token.UndefinedName, "name %q is not defined", n.Name)
		return types.Invalid
	}
	if !g.init {
		c.errs.Add(n.Pos(), token.UseBeforeDefinition, "name %q used before assignment", n.Name)
	}
	return g.typ
}

func (c *checker) findEnclosingLocal(name string) *local {
	for scope := c.current.parent; scope != nil; scope = scope.parent {
		if l, ok := scope.locals[name]; ok {
			return l
		}
	}
	return nil
}

func (c *checker) capture(n *ast.Name) *capture {
	if c.current == nil {
		return nil
	}
	if cap, ok := c.current.captures[n.Name]; ok {
		return cap
	}
	for scope := c.current.parent; scope != nil; scope = scope.parent {
		if l, ok := scope.locals[n.Name]; ok {
			if c.current.parent != scope {
				ensureCapture(c.current.parent, n.Name, l)
			}
			cap := &capture{name: n.Name, typ: l.typ, index: len(c.current.capOrder), src: l, boxed: l.boxed}
			c.current.captures[n.Name] = cap
			c.current.capOrder = append(c.current.capOrder, n.Name)
			return cap
		}
	}
	if c.current.nonlocal[n.Name] {
		c.errs.Add(n.Pos(), token.NoBindingForNonlocal, "no binding for nonlocal %q found", n.Name)
		return nil
	}
	return nil
}

func ensureCapture(current *function, name string, src *local) {
	if current == nil {
		return
	}
	if _, ok := current.locals[name]; ok {
		return
	}
	if _, ok := current.captures[name]; ok {
		return
	}
	if current.parent != nil {
		ensureCapture(current.parent, name, src)
	}
	current.captures[name] = &capture{name: name, typ: src.typ, index: len(current.capOrder), src: src, boxed: src.boxed}
	current.capOrder = append(current.capOrder, name)
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
	info := newFunction("<lambda>")
	info.result = callable.Return
	info.parent = c.current
	for i, p := range n.Params {
		info.addParam(parameter{name: p.Name.Name, typ: callable.Params[i]})
		p.Ann = typeExpr(p.Pos(), callable.Params[i])
	}
	prev := c.current
	c.current = info
	for i, p := range info.params {
		info.locals[p.name] = &local{typ: p.typ, index: i, init: true}
	}
	bt := c.exprWithHint(n.Body, callable.Return)
	if bt != types.Invalid && callable.Return != types.Invalid && !types.AssignableTo(bt, callable.Return) {
		c.errs.Add(n.Body.Pos(), token.TypeMismatch, "cannot return %s from lambda returning %s", bt, callable.Return)
	}
	c.current = prev
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
	elem := c.setElemType(n.Elems[0])
	for _, e := range n.Elems[1:] {
		et := c.setElemType(e)
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

func (c *checker) setElemType(e ast.Expr) types.Type {
	if star, ok := e.(*ast.Starred); ok {
		t := c.expr(star.X)
		if s, ok := t.(*types.Set); ok {
			return s.Elem
		}
		if t != types.Invalid {
			c.errs.Add(star.Pos(), token.TypeMismatch, "starred set element must be set, got %s", t)
		}
		return types.Invalid
	}
	return c.expr(e)
}

func (c *checker) compElem(clauses []*ast.Comprehension, elem ast.Expr) types.Type {
	var typ types.Type
	c.compClauses(clauses, func() {
		typ = c.expr(elem)
	})
	return typ
}

func (c *checker) compClauses(clauses []*ast.Comprehension, body func()) {
	var walk func(int)
	walk = func(i int) {
		if i == len(clauses) {
			body()
			return
		}
		clause := clauses[i]
		if clause.Async {
			c.errs.Add(clause.Pos(), token.UnsupportedFeature, "async comprehensions are parse-only until scheduler support lands")
		}
		iter := c.expr(clause.Iter)
		elem := iterableElem(iter)
		if elem == types.Invalid {
			c.errs.Add(clause.Iter.Pos(), token.NotIterable, "%s is not iterable", iter)
		}

		prev, hadPrev := c.temps[clause.Target.Name]
		c.temps[clause.Target.Name] = elem
		c.types[clause.Target] = elem
		defer func() {
			if hadPrev {
				c.temps[clause.Target.Name] = prev
			} else {
				delete(c.temps, clause.Target.Name)
			}
		}()

		for _, ifExpr := range clause.Ifs {
			c.condition(ifExpr)
		}
		walk(i + 1)
	}
	walk(0)
}

// unaryType, binary, binaryType, and compareType delegate to the operator
// module, the single source of operator semantics (docs/spec/04-static-semantics.md).

func (c *checker) unaryType(n *ast.UnaryExpr) types.Type {
	return operator.UnaryType(c, n.Op, n.X)
}

func (c *checker) binary(n *ast.BinaryExpr) types.Type {
	left := c.expr(n.X)
	right := c.expr(n.Y)
	return c.binaryType(left, n.Op, right, n.Pos())
}

func (c *checker) binaryType(left types.Type, op token.Type, right types.Type, pos token.Pos) types.Type {
	return operator.BinaryType(c, left, op, right, pos)
}

func (c *checker) boolOpType(n *ast.BoolOp) types.Type {
	left := c.expr(n.X)
	right := c.expr(n.Y)
	if (!types.Equal(left, types.Bool) && left != types.Invalid) || (!types.Equal(right, types.Bool) && right != types.Invalid) {
		c.errs.Add(n.Pos(), token.TypeMismatch, "'%s' requires bool operands, got %s and %s", n.Op, left, right)
	}
	return types.Bool
}

func (c *checker) compareType(n *ast.Compare) types.Type {
	prev := c.expr(n.X)
	for i, op := range n.Ops {
		right := c.expr(n.Comparators[i])
		operator.Comparable(c, op, prev, right, n.Pos())
		prev = right
	}
	return types.Bool
}

func (c *checker) callType(n *ast.CallExpr) types.Type {
	if c.checkVariadicCallExtras(n) {
		return types.Invalid
	}
	name, ok := n.Fn.(*ast.Name)
	if !ok {
		if attr, ok := n.Fn.(*ast.Attribute); ok {
			return c.methodCallType(n, attr)
		}
		if len(n.Keywords) > 0 {
			for _, kw := range n.Keywords {
				c.expr(kw.Value)
				c.errs.Add(kw.Pos(), token.UnsupportedFeature, "keyword arguments for dynamic calls are not supported yet")
			}
			return types.Invalid
		}
		fnType := c.expr(n.Fn)
		return c.callableCallType(n, fnType)
	}

	res := c.resolveName(name.Name)
	if res.kind == "class" {
		cls := c.classes[res.key]
		argExprs, argTypes, ok := c.constructorArgs(n, cls)
		if !ok {
			return types.Invalid
		}
		return c.constructorCallType(n, cls, argExprs, argTypes)
	}
	if res.kind == "function" {
		return c.directFunctionCallType(n, c.functions[res.key], name.Name)
	}
	if c.current != nil {
		if l, ok := c.current.locals[name.Name]; ok {
			return c.callableCallType(n, l.typ)
		}
		if cap := c.capture(name); cap != nil {
			return c.callableCallType(n, cap.typ)
		}
	}
	if res.kind == "global" {
		g := c.globals[res.key]
		return c.callableCallType(n, g.typ)
	}
	if res.kind != "native" {
		for _, arg := range n.StarArgs {
			c.expr(arg)
			c.errs.Add(arg.Pos(), token.UnsupportedFeature, "starred calls require a known minipy function or method")
		}
		c.errs.Add(name.Pos(), token.UndefinedName, "name %q is not defined", name.Name)
		return types.Invalid
	}
	if name.Name == "len" && len(n.Args) == 1 && len(n.StarArgs) == 0 && len(n.Keywords) == 0 {
		if result, ok := c.classLenCall(n); ok {
			return result
		}
	}
	return c.checkNativeCall(res.native, n)
}

// classLenCall rewrites len(obj) to obj.__len__() when obj is a class instance
// exposing __len__. Built-in containers fall through to the native len builtin.
func (c *checker) classLenCall(n *ast.CallExpr) (types.Type, bool) {
	cls, ok := c.expr(n.Args[0]).(*types.Class)
	if !ok {
		return types.Invalid, false
	}
	info := c.classes[cls.Name]
	if info == nil {
		return types.Invalid, false
	}
	m := method(info, "__len__")
	if m == nil {
		c.errs.Add(n.Pos(), token.TypeMismatch, "len() does not accept these arguments")
		return types.Invalid, true
	}
	if m.result != types.Invalid && !types.Equal(m.result, types.Int) {
		c.errs.Add(n.Pos(), token.TypeMismatch, "%s.__len__ must return int, got %s", info.name, m.result)
	}
	c.lenDunder[n] = true
	return types.Int, true
}

// checkNativeCall type-checks a native call, rejecting starred and keyword
// arguments (unsupported for native functions) before delegating to the symbol.
func (c *checker) checkNativeCall(sym module.Symbol, n *ast.CallExpr) types.Type {
	if len(n.StarArgs) > 0 {
		for _, a := range n.StarArgs {
			c.expr(a)
			c.errs.Add(a.Pos(), token.UnsupportedFeature, "starred arguments for native functions are not supported yet")
		}
		return types.Invalid
	}
	if len(n.Keywords) > 0 {
		for _, kw := range n.Keywords {
			c.expr(kw.Value)
			c.errs.Add(kw.Pos(), token.UnsupportedFeature, "keyword arguments for native functions are not supported yet")
		}
		return types.Invalid
	}
	return sym.Check(c, n.Args, n.Pos())
}

func (c *checker) directFunctionCallType(n *ast.CallExpr, info *function, display string) types.Type {
	if c.current == nil && !info.slot.init {
		c.errs.Add(n.Fn.Pos(), token.UseBeforeDefinition, "function %q used before definition", display)
		return types.Invalid
	}
	argExprs, argTypes, ok := c.resolveFunctionArgs(n, info)
	if !ok {
		return types.Invalid
	}
	if spec := c.specialize(info, argTypes); spec != nil {
		c.callSpec[n] = spec
		return spec.info.result
	}
	for i, arg := range argTypes {
		pt := info.params[i].typ
		if arg != types.Invalid && pt != types.Invalid && !types.AssignableTo(arg, pt) {
			c.errs.Add(argExprs[i].Pos(), token.TypeMismatch, "%s() argument %d must be %s, got %s", display, i+1, pt, arg)
		}
	}
	return info.result
}

func (c *checker) checkVariadicCallExtras(n *ast.CallExpr) bool {
	unsupported := false
	if n.Kwargs != nil {
		c.expr(n.Kwargs)
		c.errs.Add(n.Kwargs.Pos(), token.UnsupportedFeature, "double-star call arguments are not supported yet")
		unsupported = true
	}
	return unsupported
}

func (c *checker) resolveFunctionArgs(n *ast.CallExpr, info *function) ([]ast.Expr, []types.Type, bool) {
	params := info.params
	varargIdx, kwargIdx := -1, -1
	for i, p := range params {
		if p.vararg {
			varargIdx = i
		}
		if p.kwarg {
			kwargIdx = i
		}
	}

	args := make([]ast.Expr, len(params))
	seen := make([]bool, len(params))

	positionalArgs := append([]ast.Expr(nil), n.Args...)
	for _, star := range n.StarArgs {
		expanded, ok := c.expandStarCallArg(star)
		if !ok {
			return nil, nil, false
		}
		positionalArgs = append(positionalArgs, expanded...)
	}

	// Positional slots are the ordered params that accept a positional argument
	// (everything before *args that is not keyword-only). Surplus positionals
	// spill into *args when present.
	posSlots := make([]int, 0, len(params))
	for i, p := range params {
		if p.vararg || p.kwarg || p.kind == ast.ParamKwOnly {
			continue
		}
		posSlots = append(posSlots, i)
	}
	var extraPositional []ast.Expr
	for k, arg := range positionalArgs {
		if k < len(posSlots) {
			args[posSlots[k]] = arg
			seen[posSlots[k]] = true
			continue
		}
		if varargIdx < 0 {
			c.errs.Add(arg.Pos(), token.ArityMismatch, "%s() takes too many positional arguments", info.name)
			return nil, nil, false
		}
		extraPositional = append(extraPositional, arg)
	}

	var extraKw []*ast.Keyword
	for _, kw := range n.Keywords {
		idx, ok := info.paramPosition(kw.Name)
		if !ok || params[idx].vararg || params[idx].kwarg {
			if kwargIdx >= 0 {
				extraKw = append(extraKw, kw)
				continue
			}
			c.errs.Add(kw.Pos(), token.ArityMismatch, "%s() got an unexpected keyword argument %q", info.name, kw.Name)
			return nil, nil, false
		}
		if params[idx].kind == ast.ParamPosOnly {
			c.errs.Add(kw.Pos(), token.ArityMismatch, "%s() got positional-only argument %q passed as keyword", info.name, kw.Name)
			return nil, nil, false
		}
		if seen[idx] {
			c.errs.Add(kw.Pos(), token.ArityMismatch, "%s() got multiple values for argument %q", info.name, kw.Name)
			return nil, nil, false
		}
		args[idx] = kw.Value
		seen[idx] = true
	}

	// Materialize *args / **kwargs aggregates as synthetic list/dict displays so
	// the existing display lowering builds the VM array/map parameter.
	if varargIdx >= 0 {
		args[varargIdx] = &ast.ListLit{Base: ast.Base{Position: n.Pos()}, Elems: extraPositional}
		seen[varargIdx] = true
	}
	if kwargIdx >= 0 {
		keys := make([]ast.Expr, len(extraKw))
		vals := make([]ast.Expr, len(extraKw))
		for i, kw := range extraKw {
			key := &ast.StrLit{Base: ast.Base{Position: kw.Pos()}, Value: kw.Name}
			c.types[key] = types.Str
			keys[i] = key
			vals[i] = kw.Value
		}
		args[kwargIdx] = &ast.DictLit{Base: ast.Base{Position: n.Pos()}, Keys: keys, Values: vals}
		seen[kwargIdx] = true
	}

	for i, p := range params {
		if seen[i] {
			continue
		}
		if p.defaultValue == nil {
			c.errs.Add(n.Pos(), token.ArityMismatch, "%s() missing required argument %q", info.name, p.name)
			return nil, nil, false
		}
		args[i] = p.defaultValue
	}
	argTypes := make([]types.Type, len(args))
	for i, arg := range args {
		argTypes[i] = c.exprWithHint(arg, params[i].typ)
	}
	c.callArgs[n] = args
	return args, argTypes, true
}

func (c *checker) expandStarCallArg(arg ast.Expr) ([]ast.Expr, bool) {
	t := c.expr(arg)
	tuple, ok := t.(*types.Tuple)
	if !ok {
		if t != types.Invalid {
			c.errs.Add(arg.Pos(), token.UnsupportedFeature, "starred calls require a statically-sized tuple, got %s", t)
		}
		return nil, false
	}
	out := make([]ast.Expr, len(tuple.Elems))
	for i := range tuple.Elems {
		idx := &ast.IntLit{Base: ast.Base{Position: arg.Pos()}, Value: int64(i)}
		sub := &ast.Subscript{Base: ast.Base{Position: arg.Pos()}, X: arg, Index: idx}
		c.types[idx] = types.Int
		c.types[sub] = tuple.Elems[i]
		out[i] = sub
	}
	return out, true
}

func (c *checker) constructorArgs(n *ast.CallExpr, cls *class) ([]ast.Expr, []types.Type, bool) {
	params := constructorParamInfos(cls)
	if params == nil {
		if len(n.StarArgs) > 0 {
			for _, arg := range n.StarArgs {
				c.expr(arg)
				c.errs.Add(arg.Pos(), token.UnsupportedFeature, "starred constructor arguments are not supported yet")
			}
			return nil, nil, false
		}
		if len(n.Keywords) > 0 {
			for _, kw := range n.Keywords {
				c.expr(kw.Value)
				c.errs.Add(kw.Pos(), token.UnsupportedFeature, "keyword constructor arguments are not supported yet")
			}
			return nil, nil, false
		}
		argTypes := make([]types.Type, len(n.Args))
		for i, a := range n.Args {
			argTypes[i] = c.expr(a)
		}
		return n.Args, argTypes, true
	}
	temp := newFunction(cls.name)
	temp.setParams(params)
	return c.resolveFunctionArgs(n, temp)
}

func (c *checker) resolveMethodArgs(n *ast.CallExpr, method *function) ([]ast.Expr, []types.Type, bool) {
	trimmed := *method
	if len(method.params) > 0 {
		trimmed.setParams(method.params[1:])
	}
	args, argTypes, ok := c.resolveFunctionArgs(n, &trimmed)
	return args, argTypes, ok
}

func (c *checker) positionalMethodArgs(n *ast.CallExpr) []types.Type {
	if len(n.StarArgs) > 0 {
		for _, arg := range n.StarArgs {
			c.expr(arg)
			c.errs.Add(arg.Pos(), token.UnsupportedFeature, "starred arguments for built-in methods are not supported yet")
		}
		return nil
	}
	if len(n.Keywords) > 0 {
		for _, kw := range n.Keywords {
			c.expr(kw.Value)
			c.errs.Add(kw.Pos(), token.UnsupportedFeature, "keyword arguments for built-in methods are not supported yet")
		}
		return nil
	}
	args := make([]types.Type, len(n.Args))
	for i, a := range n.Args {
		args[i] = c.expr(a)
	}
	return args
}

func (c *checker) constructorCallType(n *ast.CallExpr, cls *class, argExprs []ast.Expr, argTypes []types.Type) types.Type {
	params, minArgs := constructorParams(cls)
	if len(argTypes) < minArgs || len(argTypes) > len(params) {
		c.errs.Add(n.Pos(), token.ArityMismatch, "%s() takes %d to %d arguments (%d given)", cls.name, minArgs, len(params), len(argTypes))
		return types.Invalid
	}
	for i, arg := range argTypes {
		pt := params[i]
		if arg != types.Invalid && pt != types.Invalid && !types.AssignableTo(arg, pt) {
			c.errs.Add(argExprs[i].Pos(), token.TypeMismatch, "%s() argument %d must be %s, got %s", cls.name, i+1, pt, arg)
		}
	}
	return cls.typ
}

func constructorParams(cls *class) ([]types.Type, int) {
	if isException(cls) {
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

func constructorParamInfos(cls *class) []parameter {
	if isException(cls) {
		return []parameter{{name: "message", typ: types.Str, defaultValue: &ast.StrLit{}}}
	}
	if init := cls.methods["__init__"]; init != nil {
		if len(init.params) == 0 {
			return nil
		}
		return init.params[1:]
	}
	if !cls.dataclass {
		return nil
	}
	params := make([]parameter, len(cls.fields))
	for i, field := range cls.fields {
		params[i] = parameter{name: field.name, typ: field.typ, defaultValue: field.value}
	}
	return params
}

func isException(info *class) bool {
	for info != nil {
		if info.name == "BaseException" {
			return true
		}
		info = info.base
	}
	return false
}

func (c *checker) callableCallType(n *ast.CallExpr, value types.Type) types.Type {
	callable, ok := value.(*types.Callable)
	if !ok {
		if value != types.Invalid {
			c.errs.Add(n.Pos(), token.TypeMismatch, "%s is not callable", value)
		}
		for _, arg := range n.Args {
			c.expr(arg)
		}
		for _, kw := range n.Keywords {
			c.expr(kw.Value)
		}
		return types.Invalid
	}
	if len(n.Keywords) > 0 {
		for _, kw := range n.Keywords {
			c.expr(kw.Value)
			c.errs.Add(kw.Pos(), token.UnsupportedFeature, "keyword arguments for callable values are not supported yet")
		}
		return types.Invalid
	}
	if len(n.StarArgs) > 0 {
		for _, arg := range n.StarArgs {
			c.expr(arg)
			c.errs.Add(arg.Pos(), token.UnsupportedFeature, "starred arguments for callable values are not supported yet")
		}
		return types.Invalid
	}
	if len(n.Args) != len(callable.Params) {
		c.errs.Add(n.Pos(), token.ArityMismatch, "callable takes exactly %d arguments (%d given)", len(callable.Params), len(n.Args))
		return types.Invalid
	}
	for i, arg := range n.Args {
		got := c.expr(arg)
		if got != types.Invalid && callable.Params[i] != types.Invalid && !types.AssignableTo(got, callable.Params[i]) {
			c.errs.Add(arg.Pos(), token.TypeMismatch, "callable argument %d must be %s, got %s", i+1, callable.Params[i], got)
		}
	}
	return callable.Return
}

func (c *checker) methodCallType(n *ast.CallExpr, attr *ast.Attribute) types.Type {
	receiver := c.expr(attr.X)
	switch t := receiver.(type) {
	case *types.Module:
		res := c.resolveModuleAttr(t.Name, attr.Name)
		switch res.kind {
		case "native":
			c.attrNative[attr] = res.native
			return c.checkNativeCall(res.native, n)
		case "function":
			info := c.functions[res.key]
			c.attrSym[attr] = res.key
			return c.directFunctionCallType(n, info, attr.Name)
		case "class":
			cls := c.classes[res.key]
			c.attrSym[attr] = res.key
			argExprs, argTypes, ok := c.constructorArgs(n, cls)
			if !ok {
				return types.Invalid
			}
			return c.constructorCallType(n, cls, argExprs, argTypes)
		default:
			c.errs.Add(n.Pos(), token.UndefinedName, "module %q has no attribute %q", t.Name, attr.Name)
			for _, arg := range n.Args {
				c.expr(arg)
			}
			return types.Invalid
		}
	case *types.Class:
		info := c.classes[t.Name]
		if info == nil {
			return types.Invalid
		}
		method := method(info, attr.Name)
		if method == nil {
			c.errs.Add(n.Pos(), token.UnsupportedFeature, "method %s on %s is not supported", attr.Name, receiver)
			return types.Invalid
		}
		argExprs, argTypes, ok := c.resolveMethodArgs(n, method)
		if !ok {
			return types.Invalid
		}
		for i, got := range argTypes {
			pt := method.params[i+1].typ
			if got != types.Invalid && pt != types.Invalid && !types.AssignableTo(got, pt) {
				c.errs.Add(argExprs[i].Pos(), token.TypeMismatch, "%s.%s() argument %d must be %s, got %s", info.name, attr.Name, i+1, pt, got)
			}
		}
		return method.result
	case *types.List:
		args := c.positionalMethodArgs(n)
		switch attr.Name {
		case "append":
			if len(args) != 1 {
				c.errs.Add(n.Pos(), token.ArityMismatch, "list.append takes exactly 1 argument (%d given)", len(args))
				return types.Invalid
			}
			if !types.AssignableTo(args[0], t.Elem) {
				c.errs.Add(n.Args[0].Pos(), token.TypeMismatch, "list.append expects %s, got %s", t.Elem, args[0])
				return types.Invalid
			}
			return types.None
		case "pop":
			if len(args) > 1 {
				c.errs.Add(n.Pos(), token.ArityMismatch, "list.pop takes at most 1 argument (%d given)", len(args))
				return types.Invalid
			}
			if len(args) == 1 && !types.Equal(args[0], types.Int) {
				c.errs.Add(n.Args[0].Pos(), token.TypeMismatch, "list.pop index must be int, got %s", args[0])
				return types.Invalid
			}
			return t.Elem
		case "index":
			if len(args) != 1 {
				c.errs.Add(n.Pos(), token.ArityMismatch, "list.index takes exactly 1 argument (%d given)", len(args))
				return types.Invalid
			}
			if !types.AssignableTo(args[0], t.Elem) {
				c.errs.Add(n.Args[0].Pos(), token.TypeMismatch, "list.index expects %s, got %s", t.Elem, args[0])
				return types.Invalid
			}
			return types.Int
		case "insert":
			if len(args) != 2 {
				c.errs.Add(n.Pos(), token.ArityMismatch, "list.insert takes exactly 2 arguments (%d given)", len(args))
				return types.Invalid
			}
			if !types.Equal(args[0], types.Int) {
				c.errs.Add(n.Args[0].Pos(), token.TypeMismatch, "list.insert index must be int, got %s", args[0])
				return types.Invalid
			}
			if !types.AssignableTo(args[1], t.Elem) {
				c.errs.Add(n.Args[1].Pos(), token.TypeMismatch, "list.insert value must be %s, got %s", t.Elem, args[1])
				return types.Invalid
			}
			return types.None
		case "extend":
			if len(args) != 1 {
				c.errs.Add(n.Pos(), token.ArityMismatch, "list.extend takes exactly 1 argument (%d given)", len(args))
				return types.Invalid
			}
			if !types.Equal(args[0], t) {
				c.errs.Add(n.Args[0].Pos(), token.TypeMismatch, "list.extend expects list[%s], got %s", t.Elem, args[0])
				return types.Invalid
			}
			return types.None
		case "reverse":
			if len(args) != 0 {
				c.errs.Add(n.Pos(), token.ArityMismatch, "list.reverse takes no arguments (%d given)", len(args))
				return types.Invalid
			}
			return types.None
		}
	case *types.Dict:
		args := c.positionalMethodArgs(n)
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
		args := c.positionalMethodArgs(n)
		if types.Equal(receiver, types.Str) {
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
	c.errs.Add(n.Pos(), token.UnsupportedFeature, "method %s on %s is not supported", attr.Name, receiver)
	return types.Invalid
}
