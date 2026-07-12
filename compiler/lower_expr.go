package compiler

import (
	"fmt"
	"math"
	"strings"

	"github.com/siyul-park/minipy/ast"
	"github.com/siyul-park/minipy/hostabi"
	"github.com/siyul-park/minipy/operator"
	"github.com/siyul-park/minipy/token"
	"github.com/siyul-park/minipy/types"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/interp"
	vmtypes "github.com/siyul-park/minivm/types"
)

func (c *lowerer) expr(n ast.Expr) {
	if c.failed() {
		return
	}
	switch x := n.(type) {
	case *ast.IntLit:
		c.emit(instr.I64_CONST, uint64(x.Value))
	case *ast.FloatLit:
		c.emit(instr.F64_CONST, math.Float64bits(x.Value))
	case *ast.BoolLit:
		c.emitBool(x.Value)
	case *ast.NoneLit:
		c.emit(instr.REF_NULL)
	case *ast.EllipsisLit:
		c.constGet(ellipsisValue)
	case *ast.StrLit:
		c.constGet(vmtypes.String(x.Value))
	case *ast.BytesLit:
		c.bytesLit(x)
	case *ast.Name:
		if x.Name == "Ellipsis" && types.Equal(c.types[x], types.Ellipsis) {
			c.constGet(ellipsisValue)
		} else {
			c.get(x.Name)
			c.narrowCast(x)
		}
	case *ast.UnaryExpr:
		c.unary(x)
	case *ast.BinaryExpr:
		c.emitBinary(x.Op, c.types[x.X], c.types[x.Y],
			func() { c.expr(x.X) },
			func() { c.expr(x.Y) })
	case *ast.BoolOp:
		c.boolOp(x)
	case *ast.Compare:
		c.compare(x)
	case *ast.CallExpr:
		c.call(x)
	case *ast.LambdaExpr:
		c.lambda(x)
	case *ast.IfExp:
		c.ifExp(x)
	case *ast.NamedExpr:
		c.namedExpr(x)
	case *ast.ListLit:
		c.listLit(x)
	case *ast.DictLit:
		c.dictLit(x)
	case *ast.SetLit:
		c.setLit(x)
	case *ast.ListComp:
		c.listComp(x)
	case *ast.DictComp:
		c.dictComp(x)
	case *ast.SetComp:
		c.setComp(x)
	case *ast.YieldExpr:
		c.yieldExpr(x)
	case *ast.GeneratorExp:
		c.generatorExp(x)
	case *ast.TupleLit:
		c.tupleLit(x)
	case *ast.Subscript:
		c.subscript(x)
	case *ast.Attribute:
		c.attribute(x)
	case *ast.FString:
		c.fstringConcat(x.Parts)
	}
}

func (c *lowerer) listLit(x *ast.ListLit) {
	t := c.types[x].(*types.List)
	if hasStarredExpr(x.Elems) {
		slot := c.tmp()
		c.emit(instr.I32_CONST, 0)
		c.emit(instr.ARRAY_NEW_DEFAULT, c.typeIndex(t))
		c.emit(instr.GLOBAL_SET, uint64(slot))
		for _, elem := range x.Elems {
			if star, ok := elem.(*ast.Starred); ok {
				if tuple, ok := c.types[star.X].(*types.Tuple); ok {
					c.appendTupleToListSlot(slot, star.X, tuple)
					continue
				}
				c.emit(instr.GLOBAL_GET, uint64(slot))
				c.expr(star.X)
				c.callHost(c.listExtend(c.types[x]))
				c.emit(instr.GLOBAL_SET, uint64(slot))
				continue
			}
			c.appendListSlot(slot, func() { c.expr(elem) })
		}
		c.emit(instr.GLOBAL_GET, uint64(slot))
		return
	}
	c.emit(instr.I32_CONST, uint64(len(x.Elems)))
	c.emit(instr.ARRAY_NEW_DEFAULT, c.typeIndex(t))
	for i, elem := range x.Elems {
		c.emit(instr.DUP)
		c.emit(instr.I32_CONST, uint64(i))
		c.expr(elem)
		c.emit(instr.ARRAY_SET)
	}
}

// bytesLit allocates an array[i8] sized to the decoded byte payload and fills
// it byte by byte. It never goes through list[int] lowering: bytes is its own
// primitive VM array type (types.Bytes), not types.NewList(types.Int).
func (c *lowerer) bytesLit(x *ast.BytesLit) {
	raw := []byte(x.Value)
	c.emit(instr.I32_CONST, uint64(len(raw)))
	c.emit(instr.ARRAY_NEW_DEFAULT, c.typeIndex(types.Bytes))
	for i, b := range raw {
		c.emit(instr.DUP)
		c.emit(instr.I32_CONST, uint64(i))
		c.emit(instr.I32_CONST, uint64(b))
		c.emit(instr.ARRAY_SET)
	}
}

func (c *lowerer) dictLit(x *ast.DictLit) {
	t := c.types[x].(*types.Dict)
	if hasDictUnpack(x) {
		slot := c.tmp()
		c.emit(instr.I32_CONST, 0)
		c.emit(instr.MAP_NEW, c.typeIndex(t))
		c.emit(instr.GLOBAL_SET, uint64(slot))
		for i := range x.Keys {
			if star, ok := x.Keys[i].(*ast.Starred); ok && x.Values[i] == nil {
				c.emit(instr.GLOBAL_GET, uint64(slot))
				c.expr(star.X)
				c.callHost(c.dictMerge(c.types[x]))
				c.emit(instr.GLOBAL_SET, uint64(slot))
				continue
			}
			c.emit(instr.GLOBAL_GET, uint64(slot))
			c.expr(x.Keys[i])
			c.expr(x.Values[i])
			c.emit(instr.MAP_SET)
		}
		c.emit(instr.GLOBAL_GET, uint64(slot))
		return
	}
	for i := range x.Keys {
		c.expr(x.Keys[i])
		c.expr(x.Values[i])
	}
	c.emit(instr.I32_CONST, uint64(len(x.Keys)))
	c.emit(instr.MAP_NEW, c.typeIndex(t))
}

func hasStarredExpr(exprs []ast.Expr) bool {
	for _, expr := range exprs {
		if _, ok := expr.(*ast.Starred); ok {
			return true
		}
	}
	return false
}

func hasDictUnpack(x *ast.DictLit) bool {
	for i, key := range x.Keys {
		if _, ok := key.(*ast.Starred); ok && x.Values[i] == nil {
			return true
		}
	}
	return false
}

func (c *lowerer) appendListSlot(slot int, emitElem func()) {
	c.emit(instr.GLOBAL_GET, uint64(slot))
	emitElem()
	c.emit(instr.I32_CONST, 1)
	c.emit(instr.ARRAY_APPEND)
	c.emit(instr.DROP)
}

func (c *lowerer) appendTupleToListSlot(slot int, tupleExpr ast.Expr, tuple *types.Tuple) {
	tupleSlot := c.tmp()
	c.expr(tupleExpr)
	c.emit(instr.GLOBAL_SET, uint64(tupleSlot))
	for i := range tuple.Elems {
		idx := i
		c.appendListSlot(slot, func() {
			c.emit(instr.GLOBAL_GET, uint64(tupleSlot))
			c.emit(instr.I32_CONST, uint64(idx))
			c.emit(instr.STRUCT_GET)
		})
	}
}

func (c *lowerer) setLit(x *ast.SetLit) {
	t := c.types[x].(*types.Set)
	if hasStarredExpr(x.Elems) {
		slot := c.tmp()
		c.emit(instr.I32_CONST, 0)
		c.emit(instr.MAP_NEW, c.typeIndex(t))
		c.emit(instr.GLOBAL_SET, uint64(slot))
		for _, elem := range x.Elems {
			if star, ok := elem.(*ast.Starred); ok {
				c.emit(instr.GLOBAL_GET, uint64(slot))
				c.expr(star.X)
				c.callHost(c.dictMerge(c.types[x]))
				c.emit(instr.GLOBAL_SET, uint64(slot))
				continue
			}
			c.emit(instr.GLOBAL_GET, uint64(slot))
			c.expr(elem)
			c.emit(instr.I32_CONST, 1)
			c.emit(instr.MAP_SET)
		}
		c.emit(instr.GLOBAL_GET, uint64(slot))
		return
	}
	for _, elem := range x.Elems {
		c.expr(elem)
		c.emit(instr.I32_CONST, 1)
	}
	c.emit(instr.I32_CONST, uint64(len(x.Elems)))
	c.emit(instr.MAP_NEW, c.typeIndex(t))
}

func (c *lowerer) lambda(x *ast.LambdaExpr) {
	info := c.lambdas[x]
	if info == nil {
		return
	}
	c.funcValue(info, []ast.Stmt{&ast.Return{Base: ast.Base{Position: x.Pos()}, Value: x.Body}})
}

func (c *lowerer) listComp(x *ast.ListComp) {
	t := c.types[x].(*types.List)
	slot := c.tmp()
	c.emit(instr.I32_CONST, 0)
	c.emit(instr.ARRAY_NEW_DEFAULT, c.typeIndex(t))
	c.emit(instr.GLOBAL_SET, uint64(slot))
	c.comp(x.Clauses, func() {
		c.appendListSlot(slot, func() { c.expr(x.Elem) })
	})
	c.emit(instr.GLOBAL_GET, uint64(slot))
}

func (c *lowerer) dictComp(x *ast.DictComp) {
	t := c.types[x].(*types.Dict)
	slot := c.tmp()
	c.emit(instr.I32_CONST, 0)
	c.emit(instr.MAP_NEW, c.typeIndex(t))
	c.emit(instr.GLOBAL_SET, uint64(slot))
	c.comp(x.Clauses, func() {
		c.emit(instr.GLOBAL_GET, uint64(slot))
		c.expr(x.Key)
		c.expr(x.Value)
		c.emit(instr.MAP_SET)
	})
	c.emit(instr.GLOBAL_GET, uint64(slot))
}

func (c *lowerer) setComp(x *ast.SetComp) {
	t := c.types[x].(*types.Set)
	slot := c.tmp()
	c.emit(instr.I32_CONST, 0)
	c.emit(instr.MAP_NEW, c.typeIndex(t))
	c.emit(instr.GLOBAL_SET, uint64(slot))
	c.comp(x.Clauses, func() {
		c.emit(instr.GLOBAL_GET, uint64(slot))
		c.expr(x.Elem)
		c.emit(instr.I32_CONST, 1)
		c.emit(instr.MAP_SET)
	})
	c.emit(instr.GLOBAL_GET, uint64(slot))
}

func (c *lowerer) generatorExp(x *ast.GeneratorExp) {
	info := c.genExprs[x]
	if info == nil {
		return
	}
	c.funcValue(info, info.body)
	c.emit(instr.CALL)
}

func (c *lowerer) comp(clauses []*ast.Comprehension, body func()) {
	var emit func(int)
	emit = func(i int) {
		if i == len(clauses) {
			body()
			return
		}
		clause := clauses[i]
		targetSlot := c.tmp()
		prev, hadPrev := c.temps[clause.Target.Name]
		c.temps[clause.Target.Name] = targetSlot
		defer func() {
			if hadPrev {
				c.temps[clause.Target.Name] = prev
			} else {
				delete(c.temps, clause.Target.Name)
			}
		}()
		if c.iterates(c.types[clause.Iter]) {
			c.iteratorComp(clause, targetSlot, func() {
				c.iterate(clause.Iter, c.types[clause.Iter])
			}, func() { emit(i + 1) })
			return
		}
		c.iterComp(clause, targetSlot, func() { emit(i + 1) })
	}
	emit(0)
}
func (c *lowerer) iterComp(clause *ast.Comprehension, targetSlot int, body func()) {
	iterSlot := c.tmp()
	idxSlot := c.tmp()
	c.expr(clause.Iter)
	c.emit(instr.GLOBAL_SET, uint64(iterSlot))
	c.emit(instr.I64_CONST, 0)
	c.emit(instr.GLOBAL_SET, uint64(idxSlot))
	top := c.label()
	cont := c.label()
	end := c.label()
	c.bind(top)
	c.emit(instr.GLOBAL_GET, uint64(idxSlot))
	c.emit(instr.GLOBAL_GET, uint64(iterSlot))
	c.emit(instr.ARRAY_LEN)
	c.emit(instr.I32_TO_I64_S)
	c.emit(instr.I64_LT_S)
	c.emit(instr.I32_EQZ)
	c.brIf(end)
	c.emit(instr.GLOBAL_GET, uint64(iterSlot))
	c.emit(instr.GLOBAL_GET, uint64(idxSlot))
	c.emit(instr.I64_TO_I32)
	c.emit(instr.ARRAY_GET)
	if types.Equal(c.types[clause.Iter], types.Bytes) {
		c.normalizeByteElem()
	}
	c.emit(instr.GLOBAL_SET, uint64(targetSlot))
	c.compFilters(clause.Ifs, cont)
	body()
	c.bind(cont)
	c.emit(instr.GLOBAL_GET, uint64(idxSlot))
	c.emit(instr.I64_CONST, 1)
	c.emit(instr.I64_ADD)
	c.emit(instr.GLOBAL_SET, uint64(idxSlot))
	c.br(top)
	c.bind(end)
}

func (c *lowerer) iteratorComp(clause *ast.Comprehension, targetSlot int, emitIter func(), body func()) {
	iterSlot := c.tmp()
	emitIter()
	c.emit(instr.GLOBAL_SET, uint64(iterSlot))
	top := c.label()
	cont := c.label()
	end := c.label()
	c.bind(top)
	c.emit(instr.GLOBAL_GET, uint64(iterSlot))
	c.emit(instr.CORO_DONE)
	c.brIf(end)
	c.emit(instr.GLOBAL_GET, uint64(iterSlot))
	c.emit(instr.CORO_VALUE)
	c.emit(instr.GLOBAL_SET, uint64(targetSlot))
	c.compFilters(clause.Ifs, cont)
	body()
	c.bind(cont)
	c.emitResumeIterator(iterSlot)
	c.br(top)
	c.bind(end)
}

func (c *lowerer) compFilters(filters []ast.Expr, cont instr.Label) {
	for _, filter := range filters {
		c.expr(filter)
		c.emit(instr.I32_EQZ)
		c.brIf(cont)
	}
}

func (c *lowerer) tupleLit(x *ast.TupleLit) {
	t := c.types[x].(*types.Tuple)
	c.emit(instr.STRUCT_NEW_DEFAULT, c.typeIndex(t))
	for i, elem := range x.Elems {
		c.emit(instr.DUP)
		c.emit(instr.I32_CONST, uint64(i))
		c.expr(elem)
		c.emit(instr.STRUCT_SET)
	}
}

func (c *lowerer) subscript(x *ast.Subscript) {
	if slice, ok := x.Index.(*ast.Slice); ok {
		c.slice(x, slice)
		return
	}
	c.expr(x.X)
	c.expr(x.Index)
	switch c.types[x.X].(type) {
	case *types.List:
		c.emit(instr.I64_TO_I32)
		c.emit(instr.ARRAY_GET)
	case *types.Dict:
		c.emit(instr.MAP_GET)
	case *types.Tuple:
		c.emit(instr.I64_TO_I32)
		c.emit(instr.STRUCT_GET)
	case *types.Class:
		cls := c.types[x.X].(*types.Class)
		owner, m := c.methodOwner(cls.Name, "__getitem__")
		c.funcValue(m, owner.methodBody["__getitem__"])
		c.emit(instr.CALL)
	default:
		if types.Equal(c.types[x.X], types.Bytes) {
			c.emit(instr.I64_TO_I32)
			c.emit(instr.ARRAY_GET)
			c.normalizeByteElem()
		} else if types.Equal(c.types[x.X], types.Str) {
			c.callHost(c.strIndex()) // stack has string and index; returns one-codepoint string
		}
	}
}

// normalizeByteElem reinterprets the array[i8] element ARRAY_GET just pushed
// as an unsigned byte in 0..255 and widens it to the int (i64) that source
// bytes indexing, direct iteration, and comprehensions all expose. It is the
// single place that undoes i8's sign extension so indexing, direct for
// loops, and comprehensions stay consistent with bytesIter's unsigned view
// (builtins/host.go).
func (c *lowerer) normalizeByteElem() {
	c.emit(instr.I32_CONST, 0xff)
	c.emit(instr.I32_AND)
	c.emit(instr.I32_TO_I64_S)
}

func (c *lowerer) namedExpr(x *ast.NamedExpr) {
	c.expr(x.Value)
	c.set(x.Target.Name)
	c.get(x.Target.Name)
}

func (c *lowerer) slice(x *ast.Subscript, s *ast.Slice) {
	c.expr(x.X)
	c.sliceBound(s.Lower)
	c.sliceBound(s.Upper)
	c.sliceBound(s.Step)
	switch c.types[x.X].(type) {
	case *types.List:
		c.callHost(c.arraySlice(c.types[x.X]))
	default:
		if types.Equal(c.types[x.X], types.Bytes) {
			c.callHost(c.arraySlice(c.types[x.X]))
			return
		}
		c.callHost(c.strSlice())
	}
}

func (c *lowerer) sliceBound(x ast.Expr) {
	if x == nil {
		c.emit(instr.I64_CONST, uint64(1)<<63)
		return
	}
	c.expr(x)
}

func (c *lowerer) attribute(x *ast.Attribute) {
	if key, ok := c.attrSym[x]; ok {
		c.emit(instr.GLOBAL_GET, uint64(c.globals[key].index))
		return
	}
	if _, ok := c.attrMod[x]; ok {
		return
	}
	c.expr(x.X)
	c.emit(instr.I32_CONST, uint64(c.fieldIndex(x)))
	c.emit(instr.STRUCT_GET)
}

func (c *lowerer) fieldIndex(x *ast.Attribute) int {
	cls := c.types[x.X].(*types.Class)
	return c.classes[cls.Name].fieldIndex[x.Name]
}

// fstringConcat lowers a sequence of f-string parts into a left-associated
// STRING_CONCAT chain seeded with the empty string. It is reused both for the
// whole f-string and for a nested format spec (f"{x:{w}.{p}f}").
func (c *lowerer) fstringConcat(parts []ast.FStringPart) {
	c.constGet(vmtypes.String(""))
	for _, part := range parts {
		c.fstringPart(part)
		c.emit(instr.STRING_CONCAT)
	}
}

func (c *lowerer) fstringPart(part ast.FStringPart) {
	switch p := part.(type) {
	case *ast.FStringText:
		c.constGet(vmtypes.String(p.Value))
	case *ast.FStringExpr:
		if p.Debug != "" {
			c.constGet(vmtypes.String(p.Debug))
			c.fstringValue(p)
			c.emit(instr.STRING_CONCAT)
			return
		}
		c.fstringValue(p)
	}
}

// fstringValue lowers a replacement field to a single string on the stack,
// applying the conversion (!s/!r/!a) first and then the format spec, matching
// Python's evaluation order: expression, then any nested format-spec fields.
func (c *lowerer) fstringValue(p *ast.FStringExpr) {
	c.expr(p.Expr)
	valType := c.types[p.Expr]

	conv := p.Conversion
	// A debug field with neither conversion nor format spec defaults to repr.
	if p.Debug != "" && conv == 0 && len(p.Format) == 0 {
		conv = 'r'
	}
	switch conv {
	case 'r', 'a':
		c.callHost(c.reprHost(valType, conv == 'a'))
		valType = types.Str
	case 's':
		if !types.Equal(valType, types.Str) {
			c.callHost(hostabi.StringFunction(valType))
		}
		valType = types.Str
	}

	if len(p.Format) > 0 {
		if spec, ok := staticFStringFormat(p.Format); ok {
			c.constGet(vmtypes.String(spec))
		} else {
			c.fstringConcat(p.Format)
		}
		c.callHost(c.format(valType))
		return
	}

	if conv == 0 && !types.Equal(valType, types.Str) {
		c.callHost(hostabi.StringFunction(valType))
	}
}

func staticFStringFormat(parts []ast.FStringPart) (string, bool) {
	var builder strings.Builder
	for _, part := range parts {
		text, ok := part.(*ast.FStringText)
		if !ok {
			return "", false
		}
		builder.WriteString(text.Value)
	}
	return builder.String(), true
}

// ifExp lowers the conditional expression `body if cond else orelse`
// (docs/spec/05-codegen.md): branch to the true arm when the condition holds,
// else fall through to the false arm.
func (c *lowerer) ifExp(x *ast.IfExp) {
	c.expr(x.Cond)
	trueL := c.label()
	end := c.label()
	c.brIf(trueL)
	c.expr(x.Orelse)
	c.br(end)
	c.bind(trueL)
	c.expr(x.Body)
	c.bind(end)
}

// unary and emitBinary delegate to the operator module, the single source of
// operator lowering.

func (c *lowerer) unary(x *ast.UnaryExpr) {
	operator.EmitUnary(c, x.Op, x.X)
}

func (c *lowerer) emitBinary(op token.Type, left, right types.Type, pushLeft, pushRight func()) {
	operator.EmitBinary(c, op, left, right, pushLeft, pushRight)
}

// boolOp lowers short-circuiting `and`/`or` (docs/spec/05-codegen.md).
func (c *lowerer) boolOp(x *ast.BoolOp) {
	c.expr(x.X)
	c.emit(instr.DUP)
	if x.Op == token.AND {
		eval := c.label()
		end := c.label()
		c.brIf(eval)
		c.br(end)
		c.bind(eval)
		c.emit(instr.DROP)
		c.expr(x.Y)
		c.bind(end)
		return
	}
	end := c.label()
	c.brIf(end)
	c.emit(instr.DROP)
	c.expr(x.Y)
	c.bind(end)
}

// compare lowers a (possibly chained) comparison to an i32 result. Chained
// comparisons evaluate each source operand once, matching Python semantics.
func (c *lowerer) compare(x *ast.Compare) {
	if len(x.Ops) == 1 {
		c.expr(x.X)
		c.expr(x.Comparators[0])
		c.emitCmpStack(x.Ops[0], c.types[x.X], c.types[x.Comparators[0]])
		return
	}
	leftSlot := c.tmp()
	end := c.label()
	c.expr(x.X)
	c.emit(instr.GLOBAL_SET, uint64(leftSlot))
	left := x.X
	for i, op := range x.Ops {
		c.emit(instr.GLOBAL_GET, uint64(leftSlot))
		c.expr(x.Comparators[i])
		if i+1 < len(x.Ops) {
			c.emit(instr.DUP)
			c.emit(instr.GLOBAL_SET, uint64(leftSlot))
		}
		c.emitCmpStack(op, c.types[left], c.types[x.Comparators[i]])
		left = x.Comparators[i]
		if i+1 < len(x.Ops) {
			c.emit(instr.DUP)
			c.emit(instr.I32_EQZ)
			c.brIf(end)
			c.emit(instr.DROP)
		}
	}
	c.bind(end)
}

func (c *lowerer) emitCmpStack(op token.Type, left types.Type, right types.Type) {
	operator.EmitCompareStack(c, op, left, right)
}

func (c *lowerer) emitResumeIterator(slot int) {
	c.emit(instr.GLOBAL_GET, uint64(slot))
	c.emit(instr.REF_NULL)
	c.emit(instr.RESUME)
	c.emit(instr.DROP)
}

// call lowers a direct native, class, function, or callable-value call.
func (c *lowerer) call(x *ast.CallExpr) {
	if attr, ok := x.Fn.(*ast.Attribute); ok {
		c.methodCall(x, attr)
		return
	}
	name, isName := x.Fn.(*ast.Name)
	if isName {
		sym := c.symbol(name.Name)
		if cls, ok := c.classes[sym]; ok {
			c.construct(x, cls)
			return
		}
		if info, ok := c.functions[sym]; ok {
			for _, arg := range c.functionCallArgs(x, info) {
				c.expr(arg)
			}
			if spec := c.callSpec[x]; spec != nil {
				c.emit(instr.CONST_GET, uint64(c.specs[spec]))
				c.emit(instr.CALL)
				return
			}
			c.emit(instr.GLOBAL_GET, uint64(info.slot.index))
			c.emit(instr.CALL)
			return
		}
		if _, ok := c.reg.SymbolByKey(sym); !ok {
			for _, arg := range x.Args {
				c.expr(arg)
			}
			c.get(name.Name)
			c.emit(instr.CALL)
			return
		}
	}
	if !isName {
		for _, arg := range x.Args {
			c.expr(arg)
		}
		c.expr(x.Fn)
		c.emit(instr.CALL)
		return
	}

	if c.lenDunder[x] {
		c.emitLenDunder(x)
		return
	}
	if sym, ok := c.reg.SymbolByKey(c.symbol(name.Name)); ok {
		sym.Emit(c, x.Args)
	}
}

// emitLenDunder lowers len(obj) to a direct obj.__len__() call and guards the
// result against negative values, raising ValueError to match CPython.
func (c *lowerer) emitLenDunder(x *ast.CallExpr) {
	arg := x.Args[0]
	cls := c.types[arg].(*types.Class)
	owner, m := c.methodOwner(cls.Name, "__len__")
	c.expr(arg)
	c.funcValue(m, owner.methodBody["__len__"])
	c.emit(instr.CALL)
	ok := c.label()
	c.emit(instr.DUP)
	c.emit(instr.I64_CONST, 0)
	c.emit(instr.I64_GE_S)
	c.brIf(ok)
	c.emit(instr.DROP)
	c.emitExceptionStruct(c.classes["ValueError"], func() {
		c.constGet(vmtypes.String("__len__() should return >= 0"))
	})
	c.emit(instr.I32_CONST, uint64(vmtypes.ErrorCodeNone))
	c.emit(instr.ERROR_NEW)
	c.emit(instr.THROW)
	c.bind(ok)
}

func (c *lowerer) nativeHost(moduleName, symbol string) *interp.HostFunction {
	fn, ok := c.native.Value(moduleName, symbol).(*interp.HostFunction)
	if !ok {
		c.fail(fmt.Errorf("native symbol %s.%s has no host function", moduleName, symbol))
		return nil
	}
	return fn
}

func (c *lowerer) functionCallArgs(x *ast.CallExpr, info *function) []ast.Expr {
	if args, ok := c.callArgs[x]; ok {
		return args
	}
	args := make([]ast.Expr, len(info.params))
	seen := make([]bool, len(info.params))
	positional := 0
	for _, arg := range x.Args {
		for positional < len(info.params) && info.params[positional].kind == ast.ParamKwOnly {
			positional++
		}
		if positional >= len(info.params) {
			break
		}
		args[positional] = arg
		seen[positional] = true
		positional++
	}
	for _, kw := range x.Keywords {
		if i, ok := info.paramPosition(kw.Name); ok {
			args[i] = kw.Value
			seen[i] = true
		}
	}
	for i, p := range info.params {
		if !seen[i] {
			args[i] = p.defaultValue
		}
	}
	return args
}

func (c *lowerer) construct(x *ast.CallExpr, cls *class) {
	if isException(cls) {
		c.emitExceptionInstance(cls, c.checkedArgs(x))
		return
	}
	c.emit(instr.STRUCT_NEW_DEFAULT, c.typeIndex(cls.typ))
	c.applyFieldDefaults(cls)
	args := c.checkedArgs(x)
	if init := cls.methods["__init__"]; init != nil {
		c.emit(instr.DUP)
		for _, arg := range args {
			c.expr(arg)
		}
		c.funcValue(init, cls.methodBody["__init__"])
		c.emit(instr.CALL)
		c.emit(instr.DROP)
		return
	}
	for i, arg := range args {
		c.emit(instr.DUP)
		c.emit(instr.I32_CONST, uint64(cls.fields[i].index))
		c.expr(arg)
		c.emit(instr.STRUCT_SET)
	}
}

func (c *lowerer) checkedArgs(x *ast.CallExpr) []ast.Expr {
	if args, ok := c.callArgs[x]; ok {
		return args
	}
	return x.Args
}

func (c *lowerer) applyFieldDefaults(cls *class) {
	for _, field := range cls.fields {
		if field.value == nil {
			continue
		}
		c.emit(instr.DUP)
		c.emit(instr.I32_CONST, uint64(field.index))
		c.expr(field.value)
		c.emit(instr.STRUCT_SET)
	}
}

func (c *lowerer) methodCall(x *ast.CallExpr, attr *ast.Attribute) {
	if native := c.attrNative[attr]; native != nil {
		native.Emit(c, x.Args)
		return
	}
	if key := c.attrSym[attr]; key != "" {
		if cls := c.classes[key]; cls != nil {
			c.construct(x, cls)
			return
		}
		if info := c.functions[key]; info != nil {
			for _, arg := range c.functionCallArgs(x, info) {
				c.expr(arg)
			}
			if spec := c.callSpec[x]; spec != nil {
				c.emit(instr.CONST_GET, uint64(c.specs[spec]))
				c.emit(instr.CALL)
				return
			}
			c.emit(instr.GLOBAL_GET, uint64(info.slot.index))
			c.emit(instr.CALL)
			return
		}
	}
	recvType := c.types[attr.X]
	if cls, ok := recvType.(*types.Class); ok {
		owner, method := c.methodOwner(cls.Name, attr.Name)
		c.expr(attr.X)
		args := c.callArgs[x]
		if args == nil {
			args = x.Args
		}
		for _, arg := range args {
			c.expr(arg)
		}
		c.funcValue(method, owner.methodBody[attr.Name])
		c.emit(instr.CALL)
		return
	}
	c.expr(attr.X)
	for _, arg := range x.Args {
		c.expr(arg)
	}
	switch attr.Name {
	case "get":
		if len(x.Args) == 1 {
			c.emitZeroValue(c.types[x])
		}
		c.callHost(c.dictGet(recvType, c.types[x]))
	case "keys":
		c.emit(instr.MAP_KEYS)
	case "values":
		c.callHost(c.dictValues(recvType, c.types[x]))
	case "items":
		c.callHost(c.dictItems(recvType, c.types[x]))
	case "append":
		c.emit(instr.I32_CONST, 1)
		c.emit(instr.ARRAY_APPEND)
		c.emit(instr.DROP)
		c.emit(instr.REF_NULL)
	case "pop":
		if len(x.Args) == 0 {
			c.emit(instr.I64_CONST, ^uint64(0))
		}
		c.emitArrayDelete()
	case "index":
		c.callHost(c.listIndex(recvType))
	case "insert":
		c.emitListInsert()
		c.emit(instr.REF_NULL)
	case "extend":
		c.emitListExtend()
		c.emit(instr.REF_NULL)
	case "reverse":
		c.emitListReverse()
		c.emit(instr.REF_NULL)
	case "upper":
		c.callHost(c.strUpper())
	case "lower":
		c.callHost(c.strLower())
	case "split":
		if len(x.Args) == 0 {
			c.constGet(vmtypes.String(" "))
		}
		c.callHost(c.strSplit())
	case "join":
		c.callHost(c.strJoin())
	case "find":
		c.callHost(c.strFind())
	default:
		c.fail(fmt.Errorf("lower method %s on %T: unsupported", attr.Name, recvType))
	}
}

// emitBool pushes a bool literal as an i1 value. There is no i1 const opcode,
// so the value is pushed as i32 and normalized to i1 via `!= 0`, giving the
// result KindI1 — matching comparison results so bool literals stay
// interchangeable with them (e.g. as map keys, where the verifier matches key
// kinds exactly).
func (c *lowerer) emitBool(v bool) {
	if v {
		c.emit(instr.I32_CONST, 1)
	} else {
		c.emit(instr.I32_CONST, 0)
	}
	c.emit(instr.I32_CONST, 0)
	c.emit(instr.I32_NE)
}

func (c *lowerer) emitZeroValue(t types.Type) {
	switch {
	case types.Equal(t, types.Int):
		c.emit(instr.I64_CONST, 0)
	case types.Equal(t, types.Float):
		c.emit(instr.F64_CONST, 0)
	case types.Equal(t, types.Bool):
		c.emitBool(false)
	case types.Equal(t, types.Str):
		c.constGet(vmtypes.String(""))
	default:
		c.emit(instr.REF_NULL)
	}
}

// absInt lowers abs() on an int inline: branch on the sign and negate when
// negative (the entry frame has no locals for a branchless trick).
func (c *lowerer) emitArrayDelete() {
	idxSlot := c.tmp()
	listSlot := c.tmp()
	c.emit(instr.GLOBAL_SET, uint64(idxSlot))
	c.emit(instr.GLOBAL_SET, uint64(listSlot))

	neg := c.label()
	norm := c.label()
	c.emit(instr.GLOBAL_GET, uint64(idxSlot))
	c.emit(instr.I64_CONST, 0)
	c.emit(instr.I64_LT_S)
	c.brIf(neg)
	c.br(norm)
	c.bind(neg)
	c.emit(instr.GLOBAL_GET, uint64(idxSlot))
	c.emit(instr.GLOBAL_GET, uint64(listSlot))
	c.emit(instr.ARRAY_LEN)
	c.emit(instr.I32_TO_I64_S)
	c.emit(instr.I64_ADD)
	c.emit(instr.GLOBAL_SET, uint64(idxSlot))
	c.bind(norm)

	c.emit(instr.GLOBAL_GET, uint64(listSlot))
	c.emit(instr.GLOBAL_GET, uint64(idxSlot))
	c.emit(instr.I64_TO_I32)
	c.emit(instr.ARRAY_DELETE)
}

func (c *lowerer) emitListInsert() {
	valueSlot := c.tmp()
	idxSlot := c.tmp()
	listSlot := c.tmp()
	lenSlot := c.tmp()
	iSlot := c.tmp()

	c.emit(instr.GLOBAL_SET, uint64(valueSlot))
	c.emit(instr.GLOBAL_SET, uint64(idxSlot))
	c.emit(instr.GLOBAL_SET, uint64(listSlot))

	c.emit(instr.GLOBAL_GET, uint64(listSlot))
	c.emit(instr.ARRAY_LEN)
	c.emit(instr.I32_TO_I64_S)
	c.emit(instr.GLOBAL_SET, uint64(lenSlot))

	neg := c.label()
	clampLow := c.label()
	clampHigh := c.label()
	grow := c.label()
	c.emit(instr.GLOBAL_GET, uint64(idxSlot))
	c.emit(instr.I64_CONST, 0)
	c.emit(instr.I64_LT_S)
	c.brIf(neg)
	c.br(clampLow)
	c.bind(neg)
	c.emit(instr.GLOBAL_GET, uint64(idxSlot))
	c.emit(instr.GLOBAL_GET, uint64(lenSlot))
	c.emit(instr.I64_ADD)
	c.emit(instr.GLOBAL_SET, uint64(idxSlot))

	c.bind(clampLow)
	c.emit(instr.GLOBAL_GET, uint64(idxSlot))
	c.emit(instr.I64_CONST, 0)
	c.emit(instr.I64_LT_S)
	c.emit(instr.I32_EQZ)
	c.brIf(clampHigh)
	c.emit(instr.I64_CONST, 0)
	c.emit(instr.GLOBAL_SET, uint64(idxSlot))

	c.bind(clampHigh)
	c.emit(instr.GLOBAL_GET, uint64(idxSlot))
	c.emit(instr.GLOBAL_GET, uint64(lenSlot))
	c.emit(instr.I64_GT_S)
	c.emit(instr.I32_EQZ)
	c.brIf(grow)
	c.emit(instr.GLOBAL_GET, uint64(lenSlot))
	c.emit(instr.GLOBAL_SET, uint64(idxSlot))

	c.bind(grow)
	c.emit(instr.GLOBAL_GET, uint64(listSlot))
	c.emit(instr.GLOBAL_GET, uint64(valueSlot))
	c.emit(instr.I32_CONST, 1)
	c.emit(instr.ARRAY_APPEND)
	c.emit(instr.DROP)

	c.emit(instr.GLOBAL_GET, uint64(lenSlot))
	c.emit(instr.GLOBAL_SET, uint64(iSlot))
	top := c.label()
	done := c.label()
	c.bind(top)
	c.emit(instr.GLOBAL_GET, uint64(iSlot))
	c.emit(instr.GLOBAL_GET, uint64(idxSlot))
	c.emit(instr.I64_GT_S)
	c.emit(instr.I32_EQZ)
	c.brIf(done)

	c.emit(instr.GLOBAL_GET, uint64(listSlot))
	c.emit(instr.GLOBAL_GET, uint64(iSlot))
	c.emit(instr.I64_TO_I32)
	c.emit(instr.GLOBAL_GET, uint64(listSlot))
	c.emit(instr.GLOBAL_GET, uint64(iSlot))
	c.emit(instr.I64_CONST, 1)
	c.emit(instr.I64_SUB)
	c.emit(instr.I64_TO_I32)
	c.emit(instr.ARRAY_GET)
	c.emit(instr.ARRAY_SET)

	c.emit(instr.GLOBAL_GET, uint64(iSlot))
	c.emit(instr.I64_CONST, 1)
	c.emit(instr.I64_SUB)
	c.emit(instr.GLOBAL_SET, uint64(iSlot))
	c.br(top)
	c.bind(done)

	c.emit(instr.GLOBAL_GET, uint64(listSlot))
	c.emit(instr.GLOBAL_GET, uint64(idxSlot))
	c.emit(instr.I64_TO_I32)
	c.emit(instr.GLOBAL_GET, uint64(valueSlot))
	c.emit(instr.ARRAY_SET)
}

func (c *lowerer) emitListExtend() {
	srcSlot := c.tmp()
	listSlot := c.tmp()
	lenSlot := c.tmp()
	iSlot := c.tmp()

	c.emit(instr.GLOBAL_SET, uint64(srcSlot))
	c.emit(instr.GLOBAL_SET, uint64(listSlot))
	c.emit(instr.GLOBAL_GET, uint64(srcSlot))
	c.emit(instr.ARRAY_LEN)
	c.emit(instr.I32_TO_I64_S)
	c.emit(instr.GLOBAL_SET, uint64(lenSlot))
	c.emit(instr.I64_CONST, 0)
	c.emit(instr.GLOBAL_SET, uint64(iSlot))

	top := c.label()
	done := c.label()
	c.bind(top)
	c.emit(instr.GLOBAL_GET, uint64(iSlot))
	c.emit(instr.GLOBAL_GET, uint64(lenSlot))
	c.emit(instr.I64_LT_S)
	c.emit(instr.I32_EQZ)
	c.brIf(done)
	c.emit(instr.GLOBAL_GET, uint64(listSlot))
	c.emit(instr.GLOBAL_GET, uint64(srcSlot))
	c.emit(instr.GLOBAL_GET, uint64(iSlot))
	c.emit(instr.I64_TO_I32)
	c.emit(instr.ARRAY_GET)
	c.emit(instr.I32_CONST, 1)
	c.emit(instr.ARRAY_APPEND)
	c.emit(instr.DROP)
	c.emit(instr.GLOBAL_GET, uint64(iSlot))
	c.emit(instr.I64_CONST, 1)
	c.emit(instr.I64_ADD)
	c.emit(instr.GLOBAL_SET, uint64(iSlot))
	c.br(top)
	c.bind(done)
}

func (c *lowerer) emitListReverse() {
	listSlot := c.tmp()
	iSlot := c.tmp()
	jSlot := c.tmp()
	tmpSlot := c.tmp()

	c.emit(instr.GLOBAL_SET, uint64(listSlot))
	c.emit(instr.I64_CONST, 0)
	c.emit(instr.GLOBAL_SET, uint64(iSlot))
	c.emit(instr.GLOBAL_GET, uint64(listSlot))
	c.emit(instr.ARRAY_LEN)
	c.emit(instr.I32_TO_I64_S)
	c.emit(instr.I64_CONST, 1)
	c.emit(instr.I64_SUB)
	c.emit(instr.GLOBAL_SET, uint64(jSlot))

	top := c.label()
	done := c.label()
	c.bind(top)
	c.emit(instr.GLOBAL_GET, uint64(iSlot))
	c.emit(instr.GLOBAL_GET, uint64(jSlot))
	c.emit(instr.I64_LT_S)
	c.emit(instr.I32_EQZ)
	c.brIf(done)

	c.emit(instr.GLOBAL_GET, uint64(listSlot))
	c.emit(instr.GLOBAL_GET, uint64(iSlot))
	c.emit(instr.I64_TO_I32)
	c.emit(instr.ARRAY_GET)
	c.emit(instr.GLOBAL_SET, uint64(tmpSlot))

	c.emit(instr.GLOBAL_GET, uint64(listSlot))
	c.emit(instr.GLOBAL_GET, uint64(iSlot))
	c.emit(instr.I64_TO_I32)
	c.emit(instr.GLOBAL_GET, uint64(listSlot))
	c.emit(instr.GLOBAL_GET, uint64(jSlot))
	c.emit(instr.I64_TO_I32)
	c.emit(instr.ARRAY_GET)
	c.emit(instr.ARRAY_SET)

	c.emit(instr.GLOBAL_GET, uint64(listSlot))
	c.emit(instr.GLOBAL_GET, uint64(jSlot))
	c.emit(instr.I64_TO_I32)
	c.emit(instr.GLOBAL_GET, uint64(tmpSlot))
	c.emit(instr.ARRAY_SET)

	c.emit(instr.GLOBAL_GET, uint64(iSlot))
	c.emit(instr.I64_CONST, 1)
	c.emit(instr.I64_ADD)
	c.emit(instr.GLOBAL_SET, uint64(iSlot))
	c.emit(instr.GLOBAL_GET, uint64(jSlot))
	c.emit(instr.I64_CONST, 1)
	c.emit(instr.I64_SUB)
	c.emit(instr.GLOBAL_SET, uint64(jSlot))
	c.br(top)
	c.bind(done)
}
