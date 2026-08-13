package compiler

import (
	"github.com/siyul-park/minipy/ast"
	"github.com/siyul-park/minipy/module"
	"github.com/siyul-park/minipy/token"
	"github.com/siyul-park/minipy/types"
)

func (c *checker) checkSortedCall(sym module.Symbol, n *ast.CallExpr) types.Type {
	if len(n.Args) != 1 {
		c.errs.Add(n.Pos(), token.ArityMismatch, "sorted() takes exactly one positional argument")
		return types.Invalid
	}

	var key, reverse ast.Expr
	for _, kw := range n.Keywords {
		switch kw.Name {
		case "key":
			if key != nil {
				c.errs.Add(kw.Pos(), token.ArityMismatch, "sorted() got multiple values for argument %q", kw.Name)
				continue
			}
			key = kw.Value
		case "reverse":
			if reverse != nil {
				c.errs.Add(kw.Pos(), token.ArityMismatch, "sorted() got multiple values for argument %q", kw.Name)
				continue
			}
			reverse = kw.Value
		default:
			c.expr(kw.Value)
			c.errs.Add(kw.Pos(), token.UnsupportedFeature, "sorted() keyword argument %q is not supported yet", kw.Name)
		}
	}
	if reverse == nil {
		reverse = &ast.BoolLit{Base: ast.Base{Position: n.Pos()}, Value: false}
	}
	reverseType := c.expr(reverse)
	if reverseType != types.Invalid && !types.Equal(reverseType, types.Bool) {
		c.errs.Add(reverse.Pos(), token.TypeMismatch, "sorted() argument 'reverse' must be bool, got %s", reverseType)
	}

	listType := sym.Check(c, n.Args, n.Pos())
	list, ok := listType.(*types.List)
	if !ok || listType == types.Invalid {
		return types.Invalid
	}
	if key == nil {
		c.callArgs[n] = []ast.Expr{n.Args[0], reverse}
		return listType
	}

	keyType := c.exprWithHint(key, types.NewCallable([]types.Type{list.Elem}, types.Any))
	if keyType == types.Invalid {
		return types.Invalid
	}
	if types.Equal(keyType, types.None) {
		c.callArgs[n] = []ast.Expr{n.Args[0], reverse}
		return listType
	}
	callable, ok := keyType.(*types.Callable)
	if !ok || len(callable.Params) != 1 {
		c.errs.Add(key.Pos(), token.TypeMismatch, "sorted() argument 'key' must be Callable[[%s], T], got %s", list.Elem, keyType)
		return types.Invalid
	}
	if !types.AssignableTo(list.Elem, callable.Params[0]) {
		c.errs.Add(key.Pos(), token.TypeMismatch, "sorted() key must accept %s, got %s", list.Elem, callable.Params[0])
		return types.Invalid
	}
	if !types.Orderable(callable.Return) {
		c.errs.Add(key.Pos(), token.NotComparable, "sorted() key result %s is not orderable", callable.Return)
		return types.Invalid
	}

	rewrite := c.sortedKeyRewrite(n, list, key, reverse, callable.Return)
	if rewrite == nil {
		return types.Invalid
	}
	c.callRewrite[n] = rewrite
	return listType
}

func (c *checker) sortedKeyRewrite(n *ast.CallExpr, list *types.List, key, reverse ast.Expr, keyType types.Type) ast.Expr {
	pos := n.Pos()
	x := &ast.Name{Base: ast.Base{Position: pos}, Name: "__sorted_x"}
	fn := &ast.Name{Base: ast.Base{Position: pos}, Name: "__sorted_key"}
	rev := &ast.Name{Base: ast.Base{Position: pos}, Name: "__sorted_reverse"}
	index := &ast.Name{Base: ast.Base{Position: pos}, Name: "__sorted_i"}
	pair := &ast.Name{Base: ast.Base{Position: pos}, Name: "__sorted_pair"}

	subscript := func(base, index ast.Expr) ast.Expr {
		return &ast.Subscript{Base: ast.Base{Position: pos}, X: base, Index: index}
	}
	pairIndex := &ast.IfExp{
		Base:   ast.Base{Position: pos},
		Cond:   rev,
		Body:   &ast.UnaryExpr{Base: ast.Base{Position: pos}, Op: token.MINUS, X: index},
		Orelse: index,
	}
	decorated := &ast.ListComp{
		Base: ast.Base{Position: pos},
		Elem: &ast.TupleLit{Base: ast.Base{Position: pos}, Elems: []ast.Expr{
			&ast.CallExpr{Base: ast.Base{Position: pos}, Fn: fn, Args: []ast.Expr{subscript(x, index)}},
			pairIndex,
		}},
		Clauses: []*ast.Comprehension{{
			Base:   ast.Base{Position: pos},
			Target: index,
			Iter: &ast.CallExpr{Base: ast.Base{Position: pos}, Fn: &ast.Name{Base: ast.Base{Position: pos}, Name: "range"}, Args: []ast.Expr{
				&ast.CallExpr{Base: ast.Base{Position: pos}, Fn: &ast.Name{Base: ast.Base{Position: pos}, Name: "len"}, Args: []ast.Expr{x}},
			}},
		}},
	}
	sorted := &ast.CallExpr{
		Base: ast.Base{Position: pos},
		Fn:   &ast.Name{Base: ast.Base{Position: pos}, Name: "sorted"},
		Args: []ast.Expr{decorated},
		Keywords: []*ast.Keyword{{
			Base:  ast.Base{Position: pos},
			Name:  "reverse",
			Value: rev,
		}},
	}
	pairIndexAt := subscript(pair, &ast.IntLit{Base: ast.Base{Position: pos}, Value: 1})
	originalIndex := &ast.IfExp{
		Base:   ast.Base{Position: pos},
		Cond:   rev,
		Body:   &ast.UnaryExpr{Base: ast.Base{Position: pos}, Op: token.MINUS, X: pairIndexAt},
		Orelse: pairIndexAt,
	}
	result := &ast.ListComp{
		Base:    ast.Base{Position: pos},
		Elem:    subscript(x, originalIndex),
		Clauses: []*ast.Comprehension{{Base: ast.Base{Position: pos}, Target: pair, Iter: sorted}},
	}
	wrapper := &ast.LambdaExpr{
		Base: ast.Base{Position: pos},
		Params: []*ast.Param{
			{Name: x},
			{Name: fn},
			{Name: rev},
		},
		Body: result,
	}
	hint := types.NewCallable([]types.Type{
		list,
		types.NewCallable([]types.Type{list.Elem}, keyType),
		types.Bool,
	}, list)
	if c.exprWithHint(wrapper, hint) == types.Invalid {
		return nil
	}
	rewrite := &ast.CallExpr{
		Base: ast.Base{Position: pos},
		Fn:   wrapper,
		Args: []ast.Expr{n.Args[0], key, reverse},
	}
	c.types[rewrite] = list
	return rewrite
}
