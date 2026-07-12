package compiler

import (
	"strings"

	"github.com/siyul-park/minipy/ast"
	"github.com/siyul-park/minipy/builtins"
	"github.com/siyul-park/minipy/parser"
	"github.com/siyul-park/minipy/token"
	"github.com/siyul-park/minipy/types"
)

func (c *checker) declare(name string, t types.Type, pos token.Pos) *global {
	key := c.key(name)
	if g, ok := c.globals[key]; ok {
		if _, isFunc := c.functions[key]; isFunc && t != types.Invalid {
			c.errs.Add(pos, token.TypeMismatch, "cannot redeclare function %q", name)
			return g
		}
		if _, isClass := c.classes[key]; isClass && t != types.Invalid {
			c.errs.Add(pos, token.TypeMismatch, "cannot redeclare class %q", name)
			return g
		}
		if t != types.Invalid && g.typ != types.Invalid && !types.Equal(g.typ, t) {
			c.errs.Add(pos, token.TypeMismatch, "cannot redeclare %q from %s to %s", name, g.typ, t)
		}
		return g
	}
	if _, isClass := c.classes[key]; isClass && t != types.Invalid {
		c.errs.Add(pos, token.TypeMismatch, "cannot redeclare class %q", name)
	}
	g := &global{typ: t, index: len(c.order)}
	c.globals[key] = g
	c.order = append(c.order, key)
	return g
}

func (c *checker) declareLocal(name string, t types.Type, pos token.Pos) *local {
	if l, ok := c.current.locals[name]; ok {
		if t != types.Invalid && l.typ != types.Invalid && !types.Equal(l.typ, t) {
			c.errs.Add(pos, token.TypeMismatch, "cannot redeclare %q from %s to %s", name, l.typ, t)
		}
		return l
	}
	l := &local{typ: t, index: len(c.current.params) + len(c.current.order)}
	c.current.locals[name] = l
	c.current.order = append(c.current.order, name)
	return l
}

func (c *checker) declareFuncLocal(info *function, pos token.Pos) {
	if info.local != nil {
		return
	}
	info.local = c.declareLocal(info.name, types.NewCallable(srcTypes(info.params), info.result), pos)
}

func (c *checker) declareFuncs(body []ast.Stmt) {
	for _, s := range body {
		f, ok := s.(*ast.Function)
		if !ok {
			continue
		}
		key := c.key(f.Name.Name)
		if _, exists := c.functions[key]; exists {
			c.errs.Add(f.Name.Pos(), token.TypeMismatch, "cannot redeclare function %q", f.Name.Name)
			continue
		}
		if _, exists := c.classes[key]; exists {
			c.errs.Add(f.Name.Pos(), token.TypeMismatch, "cannot redeclare class %q as a function", f.Name.Name)
			continue
		}
		if _, exists := c.globals[key]; exists {
			c.errs.Add(f.Name.Pos(), token.TypeMismatch, "cannot redeclare %q as a function", f.Name.Name)
			continue
		}
		info := newFunction(key)
		info.mod = c.mod
		info.generator = containsYield(f.Body)
		if f.Returns == nil {
			info.inferResult = true
			info.result = types.None // refined from collected returns after the body
		} else {
			info.result = c.resolveType(f.Returns)
		}
		for _, p := range f.Params {
			info.addParam(c.makeParam(p))
		}
		info.body = f.Body
		info.astParams = f.Params
		info.decorated = len(f.Decorators) > 0
		info.specializable = specializable(info)
		info.slot = c.declare(f.Name.Name, types.Invalid, f.Pos())
		c.functions[key] = info
	}
}

func (c *checker) declareClasses(body []ast.Stmt) {
	for _, s := range body {
		cls, ok := s.(*ast.Class)
		if !ok {
			continue
		}
		name := cls.Name.Name
		key := c.key(name)
		if _, exists := c.classes[key]; exists {
			c.errs.Add(cls.Name.Pos(), token.TypeMismatch, "cannot redeclare class %q", name)
			continue
		}
		if _, exists := c.globals[key]; exists {
			c.errs.Add(cls.Name.Pos(), token.TypeMismatch, "cannot redeclare %q as a class", name)
			continue
		}
		if _, exists := c.functions[key]; exists {
			c.errs.Add(cls.Name.Pos(), token.TypeMismatch, "cannot redeclare function %q as a class", name)
			continue
		}
		c.classes[key] = &class{
			name:       key,
			typ:        types.NewClass(key, nil),
			fieldIndex: map[string]int{},
			methods:    map[string]*function{},
			methodBody: map[string][]ast.Stmt{},
		}
		c.classOrder = append(c.classOrder, key)
	}
}

// declareBuiltinExceptions seeds the class table with the builtin exception
// hierarchy exported by the builtins module, so exception identity lives in the
// builtins module rather than being hardcoded here.
func (c *checker) declareBuiltinExceptions() {
	fields := []classField{
		{name: "__classid", typ: types.Int, index: 0},
		{name: "message", typ: types.Str, index: 1},
	}
	excs := builtins.Exceptions()
	for _, exc := range excs {
		info := &class{
			name:       exc.Name,
			typ:        types.NewClass(exc.Name, nil),
			fields:     append([]classField(nil), fields...),
			fieldIndex: map[string]int{"__classid": 0, "message": 1},
			methods:    map[string]*function{},
			methodBody: map[string][]ast.Stmt{},
		}
		c.classes["builtins."+exc.Name] = info
		c.classes[exc.Name] = info
		c.classOrder = append(c.classOrder, "builtins."+exc.Name)
	}
	for _, exc := range excs {
		if exc.Base != "" {
			c.classes["builtins."+exc.Name].base = c.classes["builtins."+exc.Base]
		}
	}
	for _, exc := range excs {
		c.classes["builtins."+exc.Name].typ.Fields = classTypeFields(c.classes["builtins."+exc.Name].fields)
	}
}

func (c *checker) computeClassIntervals() {
	children := map[string][]*class{}
	for _, name := range c.classOrder {
		info := c.classes[name]
		if info == nil || info.base == nil {
			continue
		}
		children[info.base.name] = append(children[info.base.name], info)
	}
	next := 1
	var dfs func(*class)
	dfs = func(info *class) {
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
	info := c.classes[c.key(n.Name.Name)]
	if info == nil {
		return
	}
	c.classDecorators(info, n.Decorators)
	if len(n.Bases) > 1 {
		c.errs.Add(n.Pos(), token.UnsupportedFeature, "multiple base classes are not supported yet (tracked by #16)")
	}
	for _, kw := range n.Keywords {
		switch {
		case kw.Name == "":
			c.errs.Add(kw.Pos(), token.UnsupportedFeature, "dynamic class keywords (**kwargs) are not supported")
		case kw.Name == "metaclass":
			c.errs.Add(kw.Pos(), token.UnsupportedFeature, "metaclass is not supported yet (tracked by #22)")
		default:
			c.errs.Add(kw.Pos(), token.UnsupportedFeature, "unknown class keyword %q", kw.Name)
		}
	}
	if len(n.Bases) > 0 {
		baseName, ok := n.Bases[0].(*ast.Name)
		if !ok {
			c.errs.Add(n.Bases[0].Pos(), token.UnsupportedType, "base class must be a name")
		} else if base := c.classes[c.resolveName(baseName.Name).key]; base == nil {
			c.errs.Add(baseName.Pos(), token.UnsupportedType, "unknown base class %q", baseName.Name)
		} else if base == info {
			c.errs.Add(baseName.Pos(), token.TypeMismatch, "class %q cannot inherit from itself", info.name)
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

// classDecorators supports only @dataclass and @dataclass(), both of which
// enable the existing dataclass constructor path with identical behavior.
// Dataclass options, any other class decorator, and metaclasses are diagnosed
// distinctly and tracked by follow-up issues (#32 and #22) rather than
// collapsed into one generic diagnostic.
func (c *checker) classDecorators(info *class, decorators []ast.Expr) {
	for _, dec := range decorators {
		if name, ok := dec.(*ast.Name); ok && name.Name == "dataclass" {
			info.dataclass = true
			continue
		}
		call, ok := dec.(*ast.CallExpr)
		if !ok {
			c.errs.Add(dec.Pos(), token.UnsupportedFeature, "class decorators other than @dataclass are not supported yet (tracked by #22)")
			continue
		}
		name, ok := call.Fn.(*ast.Name)
		if !ok || name.Name != "dataclass" {
			c.errs.Add(dec.Pos(), token.UnsupportedFeature, "class decorators other than @dataclass are not supported yet (tracked by #22)")
			continue
		}
		if len(call.Args) > 0 || len(call.Keywords) > 0 || len(call.StarArgs) > 0 || call.Kwargs != nil {
			c.errs.Add(dec.Pos(), token.UnsupportedFeature, "dataclass options are not supported yet (tracked by #32)")
			continue
		}
		info.dataclass = true
	}
}

func (c *checker) exceptionClass(e ast.Expr) *class {
	display := ""
	key := ""
	if name, ok := e.(*ast.Name); ok {
		display = name.Name
		key = c.resolveName(name.Name).key
	} else if attr, ok := e.(*ast.Attribute); ok {
		display = attr.Name
		if mod, ok := c.moduleExpr(attr.X); ok {
			res := c.resolveModuleAttr(mod, attr.Name)
			key = res.key
			c.attrSym[attr] = key
		}
	} else {
		c.errs.Add(e.Pos(), token.UnsupportedFeature, "exception type must be a class name")
		return nil
	}
	info := c.classes[key]
	if info == nil {
		if _, ok := types.Resolve(display); ok {
			c.errs.Add(e.Pos(), token.TypeMismatch, "except type must inherit from Exception, got %s", display)
			return nil
		}
		c.errs.Add(e.Pos(), token.UndefinedName, "class %q is not defined", display)
		return nil
	}
	if !c.isException(info.name) {
		c.errs.Add(e.Pos(), token.TypeMismatch, "except type must inherit from Exception, got %s", info.name)
		return nil
	}
	return info
}

func (c *checker) isException(name string) bool {
	return isException(c.classes[name])
}

func method(info *class, name string) *function {
	for info != nil {
		if method := info.methods[name]; method != nil {
			return method
		}
		info = info.base
	}
	return nil
}

func (c *checker) classField(info *class, n *ast.AnnAssign) {
	name := n.Target.Name
	if _, exists := info.fieldIndex[name]; exists {
		c.errs.Add(n.Target.Pos(), token.TypeMismatch, "cannot redeclare field %q", name)
	}
	t := c.resolveType(n.Ann)
	if recursiveByValue(t, info.typ) {
		c.errs.Add(n.Ann.Pos(), token.UnsupportedType, "field %q embeds %s by value; use an optional or reference-like type", name, info.name)
		t = types.Invalid
	}
	field := classField{name: name, typ: t, index: len(info.fields), value: n.Value, pos: n.Target.Pos()}
	info.fieldIndex[name] = field.index
	info.fields = append(info.fields, field)
	if n.Value != nil {
		value := c.exprWithHint(n.Value, t)
		if t != types.Invalid && value != types.Invalid && !types.AssignableTo(value, t) {
			c.errs.Add(n.Value.Pos(), token.TypeMismatch, "cannot assign %s to field %s %q", value, t, name)
		}
	}
}

func (c *checker) classMethod(info *class, n *ast.Function) {
	c.checkParamFeatures(n.Params)
	if _, exists := info.methods[n.Name.Name]; exists {
		c.errs.Add(n.Name.Pos(), token.TypeMismatch, "cannot redeclare method %q", n.Name.Name)
		return
	}
	if len(n.Params) == 0 || n.Params[0].Name.Name != "self" {
		c.errs.Add(n.Pos(), token.MissingAnnotation, "method %q needs self parameter", n.Name.Name)
		return
	}
	params := make([]parameter, 0, len(n.Params))
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
		params = append(params, c.makeParamWithType(p, pt))
	}
	inferResult := n.Returns == nil && n.Name.Name != "__init__"
	result := types.None
	if n.Returns != nil {
		result = c.resolveType(n.Returns)
		if n.Name.Name == "__init__" && result != types.Invalid && !types.Equal(result, types.None) {
			c.errs.Add(n.Returns.Pos(), token.TypeMismatch, "__init__ must return None, got %s", result)
		}
	}
	method := newFunction(info.name + "." + n.Name.Name)
	method.mod = c.mod
	method.setParams(params)
	method.result = result
	method.inferResult = inferResult
	method.generator = containsYield(n.Body)
	info.methods[n.Name.Name] = method
	info.methodBody[n.Name.Name] = n.Body
	c.checkSpecialMethod(info, n, method)
}

// checkSpecialMethod eagerly validates the signatures of the restricted set of
// special methods minipy statically dispatches (__len__, __getitem__,
// __setitem__). Parameter counts are checked here; return types are checked
// only when annotated, because unannotated results are inferred from the body
// after class checking and are re-validated at each use site.
func (c *checker) checkSpecialMethod(info *class, n *ast.Function, method *function) {
	switch n.Name.Name {
	case "__len__":
		if len(method.params) != 1 {
			c.errs.Add(n.Pos(), token.ArityMismatch, "%s.__len__ must take only self", info.name)
		}
		if n.Returns != nil && method.result != types.Invalid && !types.Equal(method.result, types.Int) {
			c.errs.Add(n.Returns.Pos(), token.TypeMismatch, "%s.__len__ must return int, got %s", info.name, method.result)
		}
	case "__getitem__":
		if len(method.params) != 2 {
			c.errs.Add(n.Pos(), token.ArityMismatch, "%s.__getitem__ must take self and an index", info.name)
		}
	case "__setitem__":
		if len(method.params) != 3 {
			c.errs.Add(n.Pos(), token.ArityMismatch, "%s.__setitem__ must take self, an index, and a value", info.name)
		}
		if n.Returns != nil && method.result != types.Invalid && !types.Equal(method.result, types.None) {
			c.errs.Add(n.Returns.Pos(), token.TypeMismatch, "%s.__setitem__ must return None, got %s", info.name, method.result)
		}
	}
}

func (c *checker) checkDataclassDefaults(info *class) {
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

func (c *checker) makeParam(p *ast.Param) parameter {
	t := c.paramType(p)
	// A *args parameter binds the collected surplus as list[T]; **kwargs binds
	// it as dict[str, T]. The annotation gives the element/value type T.
	if p.Vararg {
		t = types.NewList(t)
	} else if p.Kwarg {
		t = types.NewDict(types.Str, t)
	}
	return c.makeParamWithType(p, t)
}

func (c *checker) makeParamWithType(p *ast.Param, t types.Type) parameter {
	return parameter{name: p.Name.Name, typ: t, defaultValue: p.Default, kind: p.Kind, vararg: p.Vararg, kwarg: p.Kwarg}
}

func classTypeFields(fields []classField) []types.Field {
	out := make([]types.Field, len(fields))
	for i, f := range fields {
		out[i] = types.Field{Name: f.name, Type: f.typ}
	}
	return out
}

func (c *checker) nestedFuncs(info *function, body []ast.Stmt) {
	for _, s := range body {
		f, ok := s.(*ast.Function)
		if !ok {
			continue
		}
		if _, exists := info.children[f.Name.Name]; exists {
			c.errs.Add(f.Name.Pos(), token.TypeMismatch, "cannot redeclare function %q", f.Name.Name)
			continue
		}
		child := newFunction(f.Name.Name)
		child.mod = c.mod
		child.generator = containsYield(f.Body)
		child.parent = info
		c.checkParamFeatures(f.Params)
		if f.Returns == nil {
			child.inferResult = true
			child.result = types.None
		} else {
			child.result = c.resolveType(f.Returns)
		}
		for _, p := range f.Params {
			child.addParam(c.makeParam(p))
		}
		info.children[f.Name.Name] = child
		c.declareFuncLocal(child, f.Pos())
	}
}

func srcTypes(params []parameter) []types.Type {
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
	if lit, ok := e.(*ast.StrLit); ok {
		ann, err := parser.ParseType(lit.Value)
		if err != nil {
			c.errs.Add(e.Pos(), token.UnsupportedType, "invalid string annotation %q: %s", lit.Value, err)
			return types.Invalid
		}
		return c.resolveType(ann)
	}
	if name, ok := e.(*ast.Name); ok {
		key := c.resolveName(name.Name).key
		if _, ok := c.aliases[key]; ok {
			return c.resolveAlias(key)
		}
		if t, ok := c.typingScalar(name); ok {
			return t
		}
		if resolved, known := types.Resolve(name.Name); known {
			return resolved
		}
		if cls, known := c.classes[c.resolveName(name.Name).key]; known {
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
		base, ok := c.annotationHead(sub.X)
		if !ok {
			c.errs.Add(e.Pos(), token.UnsupportedType, "unsupported type annotation")
			return types.Invalid
		}
		switch base {
		case "Annotated":
			return c.annotatedType(sub)
		case "Literal":
			return c.literalType(sub)
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
			c.errs.Add(e.Pos(), token.UnsupportedType, "unknown generic type %q", base)
			return types.Invalid
		}
	}
	if attr, ok := e.(*ast.Attribute); ok {
		if t, ok := c.typingScalar(attr); ok {
			return t
		}
		if mod, ok := c.moduleExpr(attr.X); ok {
			res := c.resolveModuleAttr(mod, attr.Name)
			if res.kind == "class" {
				c.attrSym[attr] = res.key
				return c.classes[res.key].typ
			}
		}
		c.errs.Add(e.Pos(), token.UnsupportedType, "unsupported type annotation")
		return types.Invalid
	}
	c.errs.Add(e.Pos(), token.UnsupportedType, "unsupported type annotation")
	return types.Invalid
}

func (c *checker) typingScalar(e ast.Expr) (types.Type, bool) {
	name, ok := c.annotationHead(e)
	if !ok {
		return nil, false
	}
	switch name {
	case "Any":
		return types.Any, true
	case "TypeAlias":
		return types.TypeAlias, true
	default:
		return nil, false
	}
}

func (c *checker) annotationHead(e ast.Expr) (string, bool) {
	switch x := e.(type) {
	case *ast.Name:
		res := c.resolveName(x.Name)
		if res.kind == "native" && strings.HasPrefix(res.key, "typing.") {
			return strings.TrimPrefix(res.key, "typing."), true
		}
		switch x.Name {
		case "Annotated", "Literal", "TypeAlias":
			return "", false
		}
		return x.Name, true
	case *ast.Attribute:
		if mod, ok := c.moduleExpr(x.X); ok {
			res := c.resolveModuleAttr(mod, x.Name)
			if res.kind == "native" && strings.HasPrefix(res.key, "typing.") {
				c.attrNative[x] = res.native
				return x.Name, true
			}
			if res.kind == "class" {
				c.attrSym[x] = res.key
				return x.Name, true
			}
		}
		return "", false
	default:
		return "", false
	}
}

func (c *checker) annotatedType(sub *ast.Subscript) types.Type {
	args := typeArgs(sub.Index)
	if len(args) < 2 {
		c.errs.Add(sub.Pos(), token.UnsupportedType, "Annotated annotation needs a base type and metadata")
		return types.Invalid
	}
	base := c.resolveType(args[0])
	for _, meta := range args[1:] {
		if !literalMetadata(meta) {
			c.errs.Add(meta.Pos(), token.UnsupportedType, "Annotated metadata must be a literal")
			return types.Invalid
		}
	}
	return base
}

func (c *checker) literalType(sub *ast.Subscript) types.Type {
	args := typeArgs(sub.Index)
	if len(args) == 0 {
		c.errs.Add(sub.Pos(), token.UnsupportedType, "Literal annotation needs at least one value")
		return types.Invalid
	}
	values := make([]types.LiteralValue, len(args))
	for i, arg := range args {
		value, ok := literalValue(arg)
		if !ok {
			c.errs.Add(arg.Pos(), token.UnsupportedType, "unsupported Literal argument")
			return types.Invalid
		}
		values[i] = value
	}
	return types.NewLiteral(values...)
}

func typeArgs(e ast.Expr) []ast.Expr {
	if tuple, ok := e.(*ast.TupleLit); ok {
		return tuple.Elems
	}
	return []ast.Expr{e}
}

func literalMetadata(e ast.Expr) bool {
	switch e.(type) {
	case *ast.IntLit, *ast.FloatLit, *ast.BoolLit, *ast.StrLit, *ast.NoneLit:
		return true
	default:
		return false
	}
}

func literalValue(e ast.Expr) (types.LiteralValue, bool) {
	switch x := e.(type) {
	case *ast.IntLit:
		return types.IntLiteral(x.Value), true
	case *ast.BoolLit:
		return types.BoolLiteral(x.Value), true
	case *ast.StrLit:
		return types.StrLiteral(x.Value), true
	case *ast.NoneLit:
		return types.NoneLiteral(), true
	case *ast.UnaryExpr:
		if lit, ok := x.X.(*ast.IntLit); ok {
			switch x.Op {
			case token.MINUS:
				return types.IntLiteral(-lit.Value), true
			case token.PLUS:
				return types.IntLiteral(lit.Value), true
			}
		}
	}
	return types.LiteralValue{}, false
}

func recursiveByValue(t types.Type, cls *types.Class) bool {
	if t == nil || cls == nil || t == types.Invalid {
		return false
	}
	switch x := t.(type) {
	case *types.Class:
		return types.Equal(x, cls)
	case *types.List:
		return recursiveByValue(x.Elem, cls)
	case *types.Dict:
		return recursiveByValue(x.Key, cls) || recursiveByValue(x.Value, cls)
	case *types.Set:
		return recursiveByValue(x.Elem, cls)
	case *types.Tuple:
		for _, elem := range x.Elems {
			if recursiveByValue(elem, cls) {
				return true
			}
		}
		return false
	case *types.Union:
		return false
	default:
		return false
	}
}

func (c *checker) moduleExpr(e ast.Expr) (string, bool) {
	switch x := e.(type) {
	case *ast.Name:
		res := c.resolveName(x.Name)
		if res.kind == "module" {
			return res.module, true
		}
	case *ast.Attribute:
		if mod, ok := c.moduleExpr(x.X); ok {
			res := c.resolveModuleAttr(mod, x.Name)
			if res.kind == "module" {
				c.attrMod[x] = res.module
				return res.module, true
			}
		}
	}
	return "", false
}

func (c *checker) functionStmt(n *ast.Function) {
	if n.Async {
		c.errs.Add(n.Pos(), token.UnsupportedFeature, "async def is parse-only until scheduler support lands")
	}
	c.checkParamFeatures(n.Params)
	if c.current != nil {
		info := c.current.children[n.Name.Name]
		if info == nil {
			return
		}
		info.local.init = true
		c.checkFunctionBody(n.Body, n.Params, info, n.Pos())
		c.checkFunctionDecorators(n, info, &info.local.init)
		return
	}
	info := c.functions[c.key(n.Name.Name)]
	if info == nil {
		return
	}
	info.slot.init = true
	c.checkFunctionBody(n.Body, n.Params, info, n.Pos())
	c.checkFunctionDecorators(n, info, &info.slot.init)
}

// checkFunctionDecorators type-checks a function's decorator expressions in
// source order and requires each to evaluate to exactly Callable[[F], F],
// where F is the function's own (possibly inferred) signature. The binding is
// temporarily marked uninitialized while decorators are checked, so a
// decorator referring to the function it decorates is diagnosed as
// use-before-definition (Python evaluates decorators before the name binds).
func (c *checker) checkFunctionDecorators(n *ast.Function, info *function, init *bool) {
	if len(n.Decorators) == 0 {
		return
	}
	f := types.NewCallable(srcTypes(info.params), info.result)
	want := types.NewCallable([]types.Type{f}, f)
	*init = false
	for _, dec := range n.Decorators {
		got := c.decoratorExprType(dec)
		if got != types.Invalid && !types.Equal(got, want) {
			c.errs.Add(dec.Pos(), token.TypeMismatch, "decorator must be %s, got %s", want, got)
		}
	}
	*init = true
}

// decoratorExprType type-checks one decorator expression restricted to the
// statically resolvable subset accepted by this issue: a bare name, a
// module-qualified attribute, or a call of either. Other AST shapes (arbitrary
// PEP 614 decorator expressions) are rejected without evaluation.
func (c *checker) decoratorExprType(e ast.Expr) types.Type {
	if !c.decoratorShapeOK(e) {
		c.errs.Add(e.Pos(), token.UnsupportedFeature, "unsupported decorator expression")
		return types.Invalid
	}
	return c.expr(e)
}

func (c *checker) decoratorShapeOK(e ast.Expr) bool {
	switch n := e.(type) {
	case *ast.Name:
		return true
	case *ast.Attribute:
		_, ok := c.moduleExpr(n.X)
		return ok
	case *ast.CallExpr:
		return c.decoratorShapeOK(n.Fn)
	default:
		return false
	}
}

func (c *checker) checkParamFeatures(params []*ast.Param) {
	varargs := 0
	for _, p := range params {
		if p.Default != nil {
			dt := c.exprWithHint(p.Default, c.paramType(p))
			pt := c.paramType(p)
			if dt != types.Invalid && pt != types.Invalid && !types.AssignableTo(dt, pt) {
				c.errs.Add(p.Default.Pos(), token.TypeMismatch, "default for %q must be %s, got %s", p.Name.Name, pt, dt)
			}
		}
		if p.Vararg {
			varargs++
			if varargs > 1 {
				c.errs.Add(p.Pos(), token.SyntaxError, "multiple *args parameters are not allowed")
			}
		}
	}
}

func (c *checker) checkFunctionBody(body []ast.Stmt, params []*ast.Param, info *function, pos token.Pos) {
	prev := c.current
	prevMod := c.mod
	c.current = info
	if info.mod != nil {
		c.mod = info.mod
	}
	c.nestedFuncs(info, body)
	for i, p := range info.params {
		if _, exists := info.locals[p.name]; exists {
			c.errs.Add(params[i].Name.Pos(), token.TypeMismatch, "duplicate parameter %q", p.name)
			continue
		}
		info.locals[p.name] = &local{typ: p.typ, index: i, init: true}
	}
	if info.generator {
		if _, ok := info.result.(*types.Iterator); !ok && info.result != types.Invalid {
			c.errs.Add(pos, token.TypeMismatch, "generator function %q must return Iterator[T], got %s", info.name, info.result)
		}
	}
	c.checkBlock(body)
	if info.inferResult {
		// Infer the return type as the join of every return expression's type;
		// a body with no value-returns is None.
		if len(info.returns) == 0 {
			info.result = types.None
		} else {
			result := info.returns[0]
			for _, next := range info.returns[1:] {
				result = types.Join(result, next)
			}
			info.result = result
		}
	}
	if !info.generator && !types.Equal(info.result, types.None) && !blockReturns(body) {
		c.errs.Add(pos, token.TypeMismatch, "function %q may fall through without returning %s", info.name, info.result)
	}
	c.current = prev
	c.mod = prevMod
}

func (c *checker) returnStmt(n *ast.Return) {
	if c.current == nil {
		c.errs.Add(n.Pos(), token.SyntaxError, "'return' outside function")
		if n.Value != nil {
			c.expr(n.Value)
		}
		return
	}
	if c.current.generator {
		if n.Value != nil {
			c.errs.Add(n.Pos(), token.TypeMismatch, "generator function cannot return a value")
			c.expr(n.Value)
		}
		return
	}
	result := types.Type(types.None)
	if n.Value != nil {
		if c.current.inferResult {
			result = c.expr(n.Value)
		} else {
			result = c.exprWithHint(n.Value, c.current.result)
		}
	}
	if c.current.inferResult {
		// Return type is being inferred: collect this branch's type instead of
		// checking against a fixed annotation.
		c.current.returns = append(c.current.returns, result)
		return
	}
	if c.current.result != types.Invalid && result != types.Invalid && !types.AssignableTo(result, c.current.result) {
		c.errs.Add(n.Pos(), token.TypeMismatch, "cannot return %s from function returning %s", result, c.current.result)
	}
}

func (c *checker) yieldStmt(n *ast.Yield) {
	c.checkYield(n.Value, n.From, n.Pos())
}

func (c *checker) checkYield(value ast.Expr, from bool, pos token.Pos) {
	if c.current == nil {
		c.errs.Add(pos, token.SyntaxError, "'yield' outside function")
		if value != nil {
			c.expr(value)
		}
		return
	}
	iter, ok := c.current.result.(*types.Iterator)
	if !ok {
		if c.current.result != types.Invalid {
			c.errs.Add(pos, token.TypeMismatch, "yield in function returning %s; expected Iterator[T]", c.current.result)
		}
		if value != nil {
			c.expr(value)
		}
		return
	}
	if from {
		if value == nil {
			c.errs.Add(pos, token.SyntaxError, "'yield from' needs an iterable")
			return
		}
		vt := c.expr(value)
		if vt != types.Invalid && !c.iterableType(vt) {
			c.errs.Add(pos, token.TypeMismatch, "'yield from' needs an iterable, got %s", vt)
			return
		}
		et := types.None
		if vt != types.Invalid {
			et = iterableElem(vt)
		}
		if et != types.Invalid && iter.Elem != types.Invalid && !types.AssignableTo(et, iter.Elem) {
			c.errs.Add(pos, token.TypeMismatch, "cannot yield from %s into generator yielding %s", vt, iter.Elem)
		}
		return
	}
	yt := types.None
	if value != nil {
		yt = c.exprWithHint(value, iter.Elem)
	}
	if yt != types.Invalid && iter.Elem != types.Invalid && !types.AssignableTo(yt, iter.Elem) {
		c.errs.Add(pos, token.TypeMismatch, "cannot yield %s from generator yielding %s", yt, iter.Elem)
	}
}

func (c *checker) iterableType(t types.Type) bool {
	switch t.(type) {
	case *types.Iterator, *types.Dict, *types.Set, *types.List:
		return true
	default:
		return types.Equal(t, types.Str)
	}
}

// synthGenExpr builds a hidden generator function whose body is equivalent to
// `for target in iter: if cond: yield elem`, so a generator expression lowers
// to a lazily resumed coroutine instead of an eagerly built list.
func (c *checker) synthGenExpr(n *ast.GeneratorExp, elem types.Type) *function {
	info := newFunction("<genexpr>")
	info.result = types.NewIterator(elem)
	info.inferResult = false
	info.generator = true
	info.parent = c.current
	info.mod = c.mod
	info.body = c.synthGenBody(n.Clauses, n.Elem)
	c.checkFunctionBody(info.body, nil, info, n.Pos())
	return info
}

func (c *checker) synthGenBody(clauses []*ast.Comprehension, elem ast.Expr) []ast.Stmt {
	inner := []ast.Stmt{&ast.Yield{Base: ast.Base{Position: elem.Pos()}, Value: elem}}
	for i := len(clauses) - 1; i >= 0; i-- {
		clause := clauses[i]
		body := inner
		for j := len(clause.Ifs) - 1; j >= 0; j-- {
			body = []ast.Stmt{&ast.If{Base: ast.Base{Position: clause.Ifs[j].Pos()}, Cond: clause.Ifs[j], Body: body}}
		}
		inner = []ast.Stmt{&ast.For{Base: ast.Base{Position: clause.Pos()}, Target: clause.Target, Iter: clause.Iter, Body: body}}
	}
	return inner
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
			if len(n.Handlers) == 0 && blockReturns(n.Body) {
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
		if stmtHasYield(s) {
			return true
		}
	}
	return false
}

func stmtHasYield(s ast.Stmt) bool {
	switch n := s.(type) {
	case *ast.Yield:
		return true
	case *ast.ExprStmt:
		return exprHasYield(n.X)
	case *ast.Return:
		return n.Value != nil && exprHasYield(n.Value)
	case *ast.AnnAssign:
		return n.Value != nil && exprHasYield(n.Value)
	case *ast.Assign:
		return exprHasYield(n.Value) || exprHasYield(n.Target)
	case *ast.AugAssign:
		return exprHasYield(n.Value) || exprHasYield(n.Target)
	case *ast.Assert:
		return (n.Test != nil && exprHasYield(n.Test)) || (n.Msg != nil && exprHasYield(n.Msg))
	case *ast.Raise:
		return (n.Exc != nil && exprHasYield(n.Exc)) || (n.Cause != nil && exprHasYield(n.Cause))
	case *ast.If:
		return exprHasYield(n.Cond) || containsYield(n.Body) || containsYield(n.Orelse)
	case *ast.While:
		return exprHasYield(n.Cond) || containsYield(n.Body) || containsYield(n.Orelse)
	case *ast.For:
		return exprHasYield(n.Iter) || exprHasYield(n.Target) || containsYield(n.Body) || containsYield(n.Orelse)
	case *ast.Match:
		if n.Subject != nil && exprHasYield(n.Subject) {
			return true
		}
		for _, c := range n.Cases {
			if containsYield(c.Body) {
				return true
			}
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
		for _, item := range n.Items {
			if exprHasYield(item.Context) {
				return true
			}
		}
		return containsYield(n.Body)
	case *ast.Function, *ast.Class:
		return false
	}
	return false
}

func exprHasYield(e ast.Expr) bool {
	if e == nil {
		return false
	}
	switch n := e.(type) {
	case *ast.YieldExpr:
		return true
	case *ast.UnaryExpr:
		return exprHasYield(n.X)
	case *ast.BinaryExpr:
		return exprHasYield(n.X) || exprHasYield(n.Y)
	case *ast.BoolOp:
		return exprHasYield(n.X) || exprHasYield(n.Y)
	case *ast.Compare:
		if exprHasYield(n.X) {
			return true
		}
		for _, c := range n.Comparators {
			if exprHasYield(c) {
				return true
			}
		}
	case *ast.CallExpr:
		if exprHasYield(n.Fn) {
			return true
		}
		for _, a := range n.Args {
			if exprHasYield(a) {
				return true
			}
		}
	case *ast.IfExp:
		return exprHasYield(n.Body) || exprHasYield(n.Cond) || exprHasYield(n.Orelse)
	case *ast.NamedExpr:
		return exprHasYield(n.Value)
	case *ast.LambdaExpr:
		return false
	case *ast.AwaitExpr:
		return exprHasYield(n.X)
	case *ast.Starred:
		return exprHasYield(n.X)
	case *ast.Attribute:
		return exprHasYield(n.X)
	case *ast.Subscript:
		return exprHasYield(n.X) || exprHasYield(n.Index)
	case *ast.Slice:
		return exprHasYield(n.Lower) || exprHasYield(n.Upper) || exprHasYield(n.Step)
	case *ast.TupleLit:
		for _, e := range n.Elems {
			if exprHasYield(e) {
				return true
			}
		}
	case *ast.ListLit:
		for _, e := range n.Elems {
			if exprHasYield(e) {
				return true
			}
		}
	case *ast.DictLit:
		for i, k := range n.Keys {
			if exprHasYield(k) || exprHasYield(n.Values[i]) {
				return true
			}
		}
	case *ast.SetLit:
		for _, e := range n.Elems {
			if exprHasYield(e) {
				return true
			}
		}
	case *ast.GeneratorExp:
		if exprHasYield(n.Elem) {
			return true
		}
		return compHasYield(n.Clauses)
	case *ast.ListComp:
		if exprHasYield(n.Elem) {
			return true
		}
		return compHasYield(n.Clauses)
	case *ast.SetComp:
		if exprHasYield(n.Elem) {
			return true
		}
		return compHasYield(n.Clauses)
	case *ast.DictComp:
		if exprHasYield(n.Key) || exprHasYield(n.Value) {
			return true
		}
		return compHasYield(n.Clauses)
	case *ast.FString:
		for _, p := range n.Parts {
			if f, ok := p.(*ast.FStringExpr); ok && exprHasYield(f.Expr) {
				return true
			}
		}
	}
	return false
}

func compHasYield(clauses []*ast.Comprehension) bool {
	for _, cl := range clauses {
		if exprHasYield(cl.Iter) {
			return true
		}
		for _, f := range cl.Ifs {
			if exprHasYield(f) {
				return true
			}
		}
	}
	return false
}

// specializable reports whether a function may be monomorphized: it has at
// least one union/Any parameter (so distinct concrete call types are possible)
// and is not a generator. Whether a given instantiation actually type-checks is
// decided at the call site, which rolls back and falls back to the union/Any
// body on any error.
func specializable(info *function) bool {
	if info.generator || info.decorated {
		return false
	}
	for _, p := range info.params {
		if _, ok := p.typ.(*types.Union); ok {
			return true
		}
		if types.IsAny(p.typ) {
			return true
		}
	}
	return false
}

// specialize returns the monomorphic instantiation of function for the given concrete
// argument tuple, creating it on first use. It returns nil — falling back to the
// single union/Any-typed body — when the function is not specializable, an
// argument is not concrete, the per-function cap is reached, the instantiation
// recurses, or re-checking the reachable body under the concrete types produces
// any error.
func (c *checker) specialize(info *function, argTypes []types.Type) *specialization {
	if !info.specializable {
		return nil
	}
	params := make([]parameter, len(info.params))
	signature := make([]types.Type, len(info.params))
	for i, p := range info.params {
		arg := argTypes[i]
		if _, isU := p.typ.(*types.Union); isU || types.IsAny(p.typ) {
			if !concrete(arg) {
				return nil
			}
			params[i] = p
			params[i].typ = arg
		} else {
			if arg == types.Invalid || !types.AssignableTo(arg, p.typ) {
				return nil
			}
			params[i] = p
		}
		signature[i] = params[i].typ
	}

	key := specKey(signature)
	for _, s := range info.instances {
		if s.key == key {
			return s
		}
	}
	if len(info.instances) >= maxSpecializations {
		return nil
	}
	guard := info.name + "#" + key
	if c.specActive[guard] {
		return nil
	}

	clone := newFunction(info.name + "$" + key)
	clone.mod = info.mod
	clone.setParams(params)
	clone.inferResult = info.inferResult
	clone.result = info.result
	clone.generator = info.generator
	clone.body = info.body
	clone.astParams = info.astParams
	if clone.inferResult {
		clone.result = types.None
	}

	errMark := len(c.errs)
	savedTypes, savedNarrow, savedCalls, savedArgs := c.types, c.narrowed, c.callSpec, c.callArgs
	c.types = map[ast.Expr]types.Type{}
	c.narrowed = map[string]types.Type{}
	c.callSpec = map[*ast.CallExpr]*specialization{}
	c.callArgs = map[*ast.CallExpr][]ast.Expr{}
	c.specActive[guard] = true
	c.checkFunctionBody(clone.body, clone.astParams, clone, token.Pos{})
	delete(c.specActive, guard)
	itypes, icalls, iargs := c.types, c.callSpec, c.callArgs
	c.types, c.narrowed, c.callSpec, c.callArgs = savedTypes, savedNarrow, savedCalls, savedArgs

	if len(c.errs) > errMark {
		c.errs = c.errs[:errMark] // discard: this tuple cannot specialize
		return nil
	}
	spec := &specialization{key: key, params: signature, info: clone, types: itypes, calls: icalls, args: iargs}
	info.instances = append(info.instances, spec)
	return spec
}

// concrete reports whether a type is a single non-dynamic type that a
// specialization can bind a parameter to.
func concrete(t types.Type) bool {
	if t == nil || t == types.Invalid || types.IsAny(t) {
		return false
	}
	switch t.(type) {
	case *types.Union:
		return false
	}
	return true
}

// specKey is the canonical signature key for a concrete parameter tuple.
func specKey(ts []types.Type) string {
	parts := make([]string, len(ts))
	for i, t := range ts {
		parts[i] = t.String()
	}
	return strings.Join(parts, ",")
}

// expr types an expression, records the result, and returns it.
