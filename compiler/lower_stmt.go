package compiler

import (
	"fmt"
	"strings"

	"github.com/siyul-park/minipy/ast"
	"github.com/siyul-park/minipy/operator"
	"github.com/siyul-park/minipy/token"
	"github.com/siyul-park/minipy/types"
	"github.com/siyul-park/minivm/instr"
	vmtypes "github.com/siyul-park/minivm/types"
)

func (c *lowerer) stmt(s ast.Stmt) {
	switch n := s.(type) {
	case *ast.AnnAssign:
		if c.aliasDecls[n] {
			return
		}
		if n.Value != nil {
			c.expr(n.Value)
			c.set(n.Target.Name)
		}
	case *ast.Assign:
		if name, ok := n.Target.(*ast.Name); ok {
			c.expr(n.Value)
			c.set(name.Name)
		} else {
			c.assignTarget(n.Target, n.Value)
		}
	case *ast.AugAssign:
		if name, ok := n.Target.(*ast.Name); ok {
			t := c.typ(name.Name)
			c.emitBinary(n.Op, t, c.types[n.Value],
				func() { c.get(name.Name) },
				func() { c.expr(n.Value) })
			c.set(name.Name)
		} else {
			c.augAssignAttribute(n)
		}
	case *ast.ExprStmt:
		c.expr(n.X)
		c.emit(instr.DROP)
	case *ast.If:
		c.emitIf(n)
	case *ast.While:
		c.emitWhile(n)
	case *ast.For:
		c.emitFor(n)
	case *ast.Function:
		c.functionStmt(n)
	case *ast.Class:
		// Classes are compile-time metadata; instances lower as structs.
	case *ast.Import:
		c.importStmt(n)
	case *ast.ImportFrom:
		c.importFromStmt(n)
	case *ast.Global, *ast.Nonlocal, *ast.TypeAlias:
		// scope declarations affect checking only
	case *ast.Return:
		c.returnStmt(n)
	case *ast.Yield:
		c.yield(n)
	case *ast.Break:
		c.inlineFinalizers()
		c.br(c.loops[len(c.loops)-1].brk)
	case *ast.Continue:
		c.inlineFinalizers()
		c.br(c.loops[len(c.loops)-1].cont)
	case *ast.Pass:
		// no-op
	case *ast.Delete:
		c.deleteStmt(n)
	case *ast.Assert:
		c.assertStmt(n)
	case *ast.Match:
		c.emitMatch(n)
	case *ast.Try:
		c.emitTry(n)
	case *ast.Raise:
		c.emitRaise(n)
	case *ast.With:
		c.emitWith(n)
	}
}

func (c *lowerer) importStmt(n *ast.Import) {
	for _, a := range n.Names {
		c.emitImportChain(a.Name)
	}
}

func (c *lowerer) importFromStmt(n *ast.ImportFrom) {
	if n.Level == 0 && n.Module == "__future__" {
		return
	}
	base := relativeBase(c.mod, n)
	if base == "" {
		return
	}
	c.emitImportChain(base)
	if len(n.Names) == 1 && n.Names[0].Name == "*" {
		return
	}
	for _, a := range n.Names {
		if sub := c.modules[base+"."+a.Name]; sub != nil {
			c.emitImportChain(sub.name)
		}
	}
}

func (c *lowerer) emitImportChain(name string) {
	parts := strings.Split(name, ".")
	for i := range parts {
		m := c.modules[strings.Join(parts[:i+1], ".")]
		c.emitModule(m)
	}
}

// deleteStmt lowers `del`. A deleted name is overwritten with minivm's
// uninitialized slot value (REF_NULL for ref kinds, the typed zero for scalars);
// dict items use MAP_DELETE, list items use the native ARRAY_DELETE (remove +
// shift) via emitArrayDelete, and attributes are zeroed in place.
func (c *lowerer) deleteStmt(n *ast.Delete) {
	for _, target := range n.Targets {
		switch t := target.(type) {
		case *ast.Name:
			c.emitZeroValue(c.typ(t.Name))
			c.set(t.Name)
		case *ast.Subscript:
			if slice, ok := t.Index.(*ast.Slice); ok {
				c.expr(t.X)
				c.sliceBound(slice.Lower)
				c.sliceBound(slice.Upper)
				c.sliceBound(slice.Step)
				c.callHost(c.listSliceDelete(c.types[t.X]))
				continue
			}
			switch c.types[t.X].(type) {
			case *types.Dict:
				c.expr(t.X)
				c.expr(t.Index)
				c.emit(instr.MAP_DELETE)
			case *types.List:
				c.expr(t.X)
				c.expr(t.Index)
				c.emitArrayDelete()
				c.emit(instr.DROP)
			}
		case *ast.Attribute:
			cls := c.types[t.X].(*types.Class)
			info := c.classes[cls.Name]
			idx := info.fieldIndex[t.Name]
			c.expr(t.X)
			c.emit(instr.I32_CONST, uint64(idx))
			c.emitZeroValue(info.fields[idx].typ)
			c.emit(instr.STRUCT_SET)
		}
	}
}

// assertStmt lowers `assert test[, msg]`: on a false test it builds an error
// payload and throws it, which unwinds to an uncaught runtime exception.
func (c *lowerer) assertStmt(n *ast.Assert) {
	c.expr(n.Test)
	ok := c.label()
	c.brIf(ok)
	if n.Msg != nil {
		c.expr(n.Msg)
	} else {
		c.constGet(vmtypes.String("AssertionError"))
	}
	c.emit(instr.I32_CONST, uint64(vmtypes.ErrorCodeNone))
	c.emit(instr.ERROR_NEW)
	c.emit(instr.THROW)
	c.bind(ok)
}

// emitMatch lowers `match`/`case` into a linear decision tree: the subject is
// evaluated once into a temp slot, then each case's pattern test branches to the
// next case on mismatch and falls through to the body on a full match.
func (c *lowerer) emitMatch(n *ast.Match) {
	subjSlot := c.tmp()
	c.expr(n.Subject)
	c.emit(instr.GLOBAL_SET, uint64(subjSlot))
	subjT := c.types[n.Subject]
	end := c.label()
	for _, cs := range n.Cases {
		next := c.label()
		c.emitPatternTest(cs.Pattern, subjSlot, subjT, next)
		if cs.Guard != nil {
			c.expr(cs.Guard)
			c.emit(instr.I32_EQZ)
			c.brIf(next)
		}
		c.block(cs.Body)
		c.br(end)
		c.bind(next)
	}
	c.bind(end)
}

func (c *lowerer) emitTry(n *ast.Try) {
	finalizer := c.finalizer(n.Finalbody)
	start := c.label()
	end := c.label()
	catch := c.label()
	after := c.label()

	c.bind(start)
	if finalizer != nil {
		c.finally = append(c.finally, finallyFrame{emit: finalizer})
	}
	c.block(n.Body)
	c.emit(instr.NOP)
	c.bind(end)
	c.block(n.Orelse)
	if finalizer != nil {
		c.finally = c.finally[:len(c.finally)-1]
		finalizer()
	}
	c.br(after)

	c.bind(catch)
	errSlot := c.tmp()
	c.emit(instr.GLOBAL_SET, uint64(errSlot))
	if len(n.Handlers) == 0 {
		if finalizer != nil {
			finalizer()
		}
		c.emit(instr.GLOBAL_GET, uint64(errSlot))
		c.emit(instr.THROW)
		c.bind(after)
		c.tryRegion(start, end, catch, c.tryDepth())
		return
	}
	instSlot := c.tmp()
	c.emitCaughtInstance(errSlot, instSlot)
	for _, h := range n.Handlers {
		next := c.label()
		if h.Type != nil {
			c.emitExceptionTest(instSlot, h.Type, next)
		}
		if h.Name != "" {
			c.emit(instr.GLOBAL_GET, uint64(instSlot))
			c.set(h.Name)
		}
		if finalizer != nil {
			c.finally = append(c.finally, finallyFrame{emit: finalizer})
		}
		c.excepts = append(c.excepts, errSlot)
		c.block(h.Body)
		c.excepts = c.excepts[:len(c.excepts)-1]
		if finalizer != nil {
			c.finally = c.finally[:len(c.finally)-1]
			finalizer()
		}
		c.br(after)
		c.bind(next)
	}
	if finalizer != nil {
		finalizer()
	}
	c.emit(instr.GLOBAL_GET, uint64(errSlot))
	c.emit(instr.THROW)
	c.bind(after)
	c.tryRegion(start, end, catch, c.tryDepth())
}

func (c *lowerer) emitTryFinally(body func(), finalizer func()) {
	start := c.label()
	end := c.label()
	catch := c.label()
	after := c.label()

	c.bind(start)
	c.finally = append(c.finally, finallyFrame{emit: finalizer})
	body()
	c.finally = c.finally[:len(c.finally)-1]
	c.emit(instr.NOP)
	c.bind(end)
	finalizer()
	c.br(after)

	c.bind(catch)
	errSlot := c.tmp()
	c.emit(instr.GLOBAL_SET, uint64(errSlot))
	finalizer()
	c.emit(instr.GLOBAL_GET, uint64(errSlot))
	c.emit(instr.THROW)

	c.bind(after)
	c.tryRegion(start, end, catch, c.tryDepth())
}

func (c *lowerer) finalizer(body []ast.Stmt) func() {
	if len(body) == 0 {
		return nil
	}
	return func() { c.block(body) }
}

func (c *lowerer) inlineFinalizers() {
	for i := len(c.finally) - 1; i >= 0; i-- {
		c.finally[i].emit()
	}
}

func (c *lowerer) emitCaughtInstance(errSlot, instSlot int) {
	trap := c.label()
	done := c.label()
	c.emit(instr.GLOBAL_GET, uint64(errSlot))
	c.emit(instr.ERROR_GET)
	c.emit(instr.DUP)
	c.emit(instr.REF_IS_NULL)
	c.brIf(trap)
	c.br(done)
	c.bind(trap)
	c.emit(instr.DROP)
	c.emitTrapInstance(errSlot)
	c.bind(done)
	c.emit(instr.GLOBAL_SET, uint64(instSlot))
}

// emitTrapInstance lowers a caught VM trap (a null-payload types.Error) into
// an exception instance. It reads the trap's numeric code natively via
// ERROR_CODE and matches it against trapClasses entirely in bytecode,
// skipping excInstance's host round trip for the traps minipy classifies most
// often; an unrecognized code still defers to the host for its message text.
func (c *lowerer) emitTrapInstance(errSlot int) {
	codeSlot := c.tmp()
	c.emit(instr.GLOBAL_GET, uint64(errSlot))
	c.emit(instr.ERROR_CODE)
	c.emit(instr.GLOBAL_SET, uint64(codeSlot))

	done := c.label()
	fallback := c.label()
	matched := make([]instr.Label, len(trapClasses))
	for i, tc := range trapClasses {
		matched[i] = c.label()
		c.emit(instr.GLOBAL_GET, uint64(codeSlot))
		c.emit(instr.I32_CONST, uint64(uint32(tc.code)))
		c.emit(instr.I32_EQ)
		c.brIf(matched[i])
	}
	c.br(fallback)
	for i, tc := range trapClasses {
		c.bind(matched[i])
		c.emitExceptionStruct(c.classes[tc.class], func() { c.constGet(vmtypes.String(tc.message)) })
		c.br(done)
	}
	c.bind(fallback)
	c.emit(instr.GLOBAL_GET, uint64(errSlot))
	c.callHost(c.exc())
	c.bind(done)
}

// emitExceptionStruct allocates a BaseException-backed struct: field 0 is the
// class id and field 1 is the message produced by pushMessage. Every exception
// class inherits the same {classID, message} layout, and runtime dispatch
// (emitExceptionClassID) only inspects the classID, not the struct's nominal type.
func (c *lowerer) emitExceptionStruct(cls *class, pushMessage func()) {
	c.emit(instr.STRUCT_NEW_DEFAULT, c.typeIndex(c.classes["BaseException"].typ))
	c.emit(instr.DUP)
	c.emit(instr.I32_CONST, 0)
	c.emit(instr.I64_CONST, uint64(cls.classID))
	c.emit(instr.STRUCT_SET)
	c.emit(instr.DUP)
	c.emit(instr.I32_CONST, 1)
	pushMessage()
	c.emit(instr.STRUCT_SET)
}

func (c *lowerer) emitExceptionTest(instSlot int, typ ast.Expr, next instr.Label) {
	info := c.classForExpr(typ)
	c.emitExceptionClassID(instSlot)
	c.emit(instr.I64_CONST, uint64(info.low))
	c.emit(instr.I64_GE_S)
	c.emitExceptionClassID(instSlot)
	c.emit(instr.I64_CONST, uint64(info.high))
	c.emit(instr.I64_LE_S)
	c.emit(instr.I32_AND)
	c.emit(instr.I32_EQZ)
	c.brIf(next)
}

func (c *lowerer) classForExpr(e ast.Expr) *class {
	switch x := e.(type) {
	case *ast.Name:
		return c.classes[c.symbol(x.Name)]
	case *ast.Attribute:
		if key := c.attrSym[x]; key != "" {
			return c.classes[key]
		}
	}
	return nil
}

func (c *lowerer) emitExceptionClassID(instSlot int) {
	c.emit(instr.GLOBAL_GET, uint64(instSlot))
	c.emit(instr.I32_CONST, 0)
	c.emit(instr.STRUCT_GET)
}

func (c *lowerer) emitRaise(n *ast.Raise) {
	if n.Cause != nil {
		c.expr(n.Cause)
		c.emit(instr.DROP)
	}
	if n.Exc == nil {
		c.emit(instr.GLOBAL_GET, uint64(c.excepts[len(c.excepts)-1]))
		c.emit(instr.THROW)
		return
	}
	if call, ok := n.Exc.(*ast.CallExpr); ok {
		if name, ok := call.Fn.(*ast.Name); ok {
			if cls := c.classes[c.symbol(name.Name)]; cls != nil && isException(cls) {
				c.emitExceptionInstance(cls, c.checkedArgs(call))
				c.emit(instr.I32_CONST, uint64(vmtypes.ErrorCodeNone))
				c.emit(instr.ERROR_NEW)
				c.emit(instr.THROW)
				return
			}
		} else if attr, ok := call.Fn.(*ast.Attribute); ok {
			if cls := c.classes[c.attrSym[attr]]; cls != nil && isException(cls) {
				c.emitExceptionInstance(cls, c.checkedArgs(call))
				c.emit(instr.I32_CONST, uint64(vmtypes.ErrorCodeNone))
				c.emit(instr.ERROR_NEW)
				c.emit(instr.THROW)
				return
			}
		}
	}
	c.expr(n.Exc)
	c.emit(instr.I32_CONST, uint64(vmtypes.ErrorCodeNone))
	c.emit(instr.ERROR_NEW)
	c.emit(instr.THROW)
}

func (c *lowerer) emitExceptionInstance(cls *class, args []ast.Expr) {
	c.emitExceptionStruct(cls, func() {
		if len(args) > 0 {
			c.expr(args[0])
		} else {
			c.constGet(vmtypes.String(""))
		}
	})
}

func (c *lowerer) emitWith(n *ast.With) {
	var emit func(int)
	emit = func(i int) {
		item := n.Items[i]
		name := c.types[item.Context].(*types.Class).Name
		ctxSlot := c.tmp()
		c.expr(item.Context)
		c.emit(instr.GLOBAL_SET, uint64(ctxSlot))
		owner, enter := c.methodOwner(name, "__enter__")
		c.emit(instr.GLOBAL_GET, uint64(ctxSlot))
		c.funcValue(enter, owner.methodBody["__enter__"])
		c.emit(instr.CALL)
		if item.OptionalVars != nil {
			c.set(item.OptionalVars.(*ast.Name).Name)
		} else {
			c.emit(instr.DROP)
		}
		exitOwner, exit := c.methodOwner(name, "__exit__")
		c.emitTryFinally(func() {
			if i+1 < len(n.Items) {
				emit(i + 1)
				return
			}
			c.block(n.Body)
		}, func() {
			c.emit(instr.GLOBAL_GET, uint64(ctxSlot))
			c.funcValue(exit, exitOwner.methodBody["__exit__"])
			c.emit(instr.CALL)
			c.emit(instr.DROP)
		})
	}
	emit(0)
}

func (c *lowerer) methodOwner(name, method string) (*class, *function) {
	for info := c.classes[name]; info != nil; info = info.base {
		if found := info.methods[method]; found != nil {
			return info, found
		}
	}
	return nil, nil
}

// emitPatternTest tests the value in global slot `slot` (static type typ) against
// p, binding captures as it goes, and branches to next on mismatch.
func (c *lowerer) emitPatternTest(p ast.Pattern, slot int, typ types.Type, next instr.Label) {
	switch pat := p.(type) {
	case *ast.WildcardPattern:
		// always matches
	case *ast.CapturePattern:
		c.bindSlot(pat.Name, slot)
	case *ast.StarPattern:
		c.bindSlot(pat.Name, slot)
	case *ast.AsPattern:
		c.emitPatternTest(pat.Pattern, slot, typ, next)
		c.bindSlot(pat.Name, slot)
	case *ast.OrPattern:
		succ := c.label()
		for _, alt := range pat.Alts {
			altNext := c.label()
			c.emitPatternTest(alt, slot, typ, altNext)
			c.br(succ)
			c.bind(altNext)
		}
		c.br(next)
		c.bind(succ)
	case *ast.ValuePattern:
		c.emit(instr.GLOBAL_GET, uint64(slot))
		c.expr(pat.Value)
		c.emit(operator.CmpOpcode(token.EQ, typ))
		c.emit(instr.I32_EQZ)
		c.brIf(next)
	case *ast.SequencePattern:
		c.emitSequenceTest(pat, slot, typ, next)
	case *ast.MappingPattern:
		c.emitMappingTest(pat, slot, typ, next)
	case *ast.ClassPattern:
		c.emitClassTest(pat, slot, typ, next)
	}
}

// bindSlot stores the value in global slot into the named capture variable.
func (c *lowerer) bindSlot(name string, slot int) {
	if name == "" || name == "_" {
		return
	}
	c.emit(instr.GLOBAL_GET, uint64(slot))
	c.set(name)
}

// childSlot extracts a sub-value of the slot value at the given index (a
// list/tuple/struct element) into a fresh temp slot and returns it.
func (c *lowerer) childSlot(parent int, index int, op instr.Opcode) int {
	child := c.tmp()
	c.emit(instr.GLOBAL_GET, uint64(parent))
	c.emit(instr.I32_CONST, uint64(index))
	c.emit(op)
	c.emit(instr.GLOBAL_SET, uint64(child))
	return child
}

func (c *lowerer) emitSequenceTest(pat *ast.SequencePattern, slot int, typ types.Type, next instr.Label) {
	switch s := typ.(type) {
	case *types.Tuple:
		if pat.Star < 0 {
			for i, e := range pat.Elems {
				child := c.childSlot(slot, i, instr.STRUCT_GET)
				c.emitPatternTest(e, child, s.Elems[i], next)
			}
			return
		}
		// Tuple length is static, so no runtime length guard is needed: the
		// checker already validated prefix+suffix against the tuple arity.
		prefix := pat.Star
		suffix := len(pat.Elems) - pat.Star - 1
		for i := 0; i < prefix; i++ {
			child := c.childSlot(slot, i, instr.STRUCT_GET)
			c.emitPatternTest(pat.Elems[i], child, s.Elems[i], next)
		}
		for j := 0; j < suffix; j++ {
			srcIdx := len(s.Elems) - suffix + j
			child := c.childSlot(slot, srcIdx, instr.STRUCT_GET)
			c.emitPatternTest(pat.Elems[prefix+1+j], child, s.Elems[srcIdx], next)
		}
		star := pat.Elems[prefix].(*ast.StarPattern)
		if star.Name != "" && star.Name != "_" {
			c.emitTupleRestList(slot, s, prefix, suffix)
			c.set(star.Name)
		}
	case *types.List:
		if pat.Star < 0 {
			c.emit(instr.GLOBAL_GET, uint64(slot))
			c.emit(instr.ARRAY_LEN)
			c.emit(instr.I32_CONST, uint64(len(pat.Elems)))
			c.emit(instr.I32_EQ)
			c.emit(instr.I32_EQZ)
			c.brIf(next)
			for i, e := range pat.Elems {
				child := c.childSlot(slot, i, instr.ARRAY_GET)
				c.emitPatternTest(e, child, s.Elem, next)
			}
			return
		}
		prefix := pat.Star
		suffix := len(pat.Elems) - pat.Star - 1
		c.emit(instr.GLOBAL_GET, uint64(slot))
		c.emit(instr.ARRAY_LEN)
		c.emit(instr.I32_CONST, uint64(prefix+suffix))
		c.emit(instr.I32_LT_S)
		c.brIf(next)
		for i := 0; i < prefix; i++ {
			child := c.childSlot(slot, i, instr.ARRAY_GET)
			c.emitPatternTest(pat.Elems[i], child, s.Elem, next)
		}
		for j := 0; j < suffix; j++ {
			child := c.tmp()
			c.emit(instr.GLOBAL_GET, uint64(slot))
			c.emit(instr.GLOBAL_GET, uint64(slot))
			c.emit(instr.ARRAY_LEN)
			c.emit(instr.I32_CONST, uint64(suffix-j))
			c.emit(instr.I32_SUB)
			c.emit(instr.ARRAY_GET)
			c.emit(instr.GLOBAL_SET, uint64(child))
			c.emitPatternTest(pat.Elems[prefix+1+j], child, s.Elem, next)
		}
		star := pat.Elems[prefix].(*ast.StarPattern)
		if star.Name != "" && star.Name != "_" {
			c.emit(instr.GLOBAL_GET, uint64(slot))
			c.emit(instr.I32_CONST, uint64(prefix))
			c.emit(instr.GLOBAL_GET, uint64(slot))
			c.emit(instr.ARRAY_LEN)
			c.emit(instr.I32_CONST, uint64(suffix))
			c.emit(instr.I32_SUB)
			c.emit(instr.ARRAY_SLICE)
			c.set(star.Name)
		}
	}
}

func (c *lowerer) emitMappingTest(pat *ast.MappingPattern, slot int, typ types.Type, next instr.Label) {
	d := typ.(*types.Dict)
	for i, keyExpr := range pat.Keys {
		child := c.tmp()
		c.emit(instr.GLOBAL_GET, uint64(slot))
		c.expr(keyExpr)
		c.emit(instr.MAP_LOOKUP)
		// MAP_LOOKUP leaves [value, present]; bring value to the top to capture it,
		// leaving the presence flag to branch on.
		c.emit(instr.SWAP)
		c.emit(instr.GLOBAL_SET, uint64(child))
		c.emit(instr.I32_EQZ)
		c.brIf(next)
		c.emitPatternTest(pat.Values[i], child, d.Value, next)
	}
	if pat.Rest != "" && pat.Rest != "_" {
		keysT := types.NewList(d.Key)
		c.emit(instr.I32_CONST, uint64(len(pat.Keys)))
		c.emit(instr.ARRAY_NEW_DEFAULT, c.typeIndex(keysT))
		for i, keyExpr := range pat.Keys {
			c.emit(instr.DUP)
			c.emit(instr.I32_CONST, uint64(i))
			c.expr(keyExpr)
			c.emit(instr.ARRAY_SET)
		}
		keysSlot := c.tmp()
		c.emit(instr.GLOBAL_SET, uint64(keysSlot))
		c.emit(instr.GLOBAL_GET, uint64(slot))
		c.emit(instr.GLOBAL_GET, uint64(keysSlot))
		c.callHost(c.dictRest(d))
		c.set(pat.Rest)
	}
}

func (c *lowerer) emitClassTest(pat *ast.ClassPattern, slot int, typ types.Type, next instr.Label) {
	cls := typ.(*types.Class)
	info := c.classes[cls.Name]
	for i, sub := range pat.Args {
		child := c.childSlot(slot, i, instr.STRUCT_GET)
		c.emitPatternTest(sub, child, info.fields[i].typ, next)
	}
	for i, kw := range pat.KwNames {
		idx := info.fieldIndex[kw]
		child := c.childSlot(slot, idx, instr.STRUCT_GET)
		c.emitPatternTest(pat.Kw[i], child, info.fields[idx].typ, next)
	}
}

func (c *lowerer) assignTarget(target ast.Expr, value ast.Expr) {
	switch t := target.(type) {
	case *ast.Subscript:
		if slice, ok := t.Index.(*ast.Slice); ok {
			c.expr(t.X)
			c.sliceBound(slice.Lower)
			c.sliceBound(slice.Upper)
			c.sliceBound(slice.Step)
			c.expr(value)
			c.callHost(c.listSliceAssign(c.types[t.X]))
			return
		}
		c.expr(t.X)
		c.expr(t.Index)
		c.expr(value)
		switch recv := c.types[t.X].(type) {
		case *types.List:
			c.emit(instr.SWAP)
			c.emit(instr.I64_TO_I32)
			c.emit(instr.SWAP)
			c.emit(instr.ARRAY_SET)
		case *types.Dict:
			c.emit(instr.MAP_SET)
		case *types.Class:
			owner, m := c.methodOwner(recv.Name, "__setitem__")
			c.funcValue(m, owner.methodBody["__setitem__"])
			c.emit(instr.CALL)
			c.emit(instr.DROP)
		default:
			c.fail(fmt.Errorf("lower subscript assignment for %T: unsupported receiver type %T", t, c.types[t.X]))
		}
	case *ast.TupleLit:
		c.unpackAssign(t, value)
	case *ast.Attribute:
		if key := c.attrSym[t]; key != "" {
			c.expr(value)
			c.emit(instr.GLOBAL_SET, uint64(c.globals[key].index))
			return
		}
		c.expr(t.X)
		c.emit(instr.I32_CONST, uint64(c.fieldIndex(t)))
		c.expr(value)
		c.emit(instr.STRUCT_SET)
	default:
		c.fail(fmt.Errorf("lower assignment target %T: unsupported", target))
	}
}

func (c *lowerer) augAssignAttribute(n *ast.AugAssign) {
	attr := n.Target.(*ast.Attribute)
	if key := c.attrSym[attr]; key != "" {
		c.emitBinary(n.Op, c.types[attr], c.types[n.Value],
			func() { c.emit(instr.GLOBAL_GET, uint64(c.globals[key].index)) },
			func() { c.expr(n.Value) })
		c.emit(instr.GLOBAL_SET, uint64(c.globals[key].index))
		return
	}
	c.emitBinary(n.Op, c.types[attr], c.types[n.Value],
		func() { c.attribute(attr) },
		func() { c.expr(n.Value) })
	c.expr(attr.X)
	c.emit(instr.SWAP)
	c.emit(instr.I32_CONST, uint64(c.fieldIndex(attr)))
	c.emit(instr.SWAP)
	c.emit(instr.STRUCT_SET)
}

func (c *lowerer) unpackAssign(target *ast.TupleLit, value ast.Expr) {
	if tupleStarIndex(target) < 0 {
		if tupleValue, ok := value.(*ast.TupleLit); ok {
			for i, elem := range target.Elems {
				name := elem.(*ast.Name)
				c.expr(tupleValue.Elems[i])
				c.set(name.Name)
			}
			return
		}
	}
	c.expr(value)
	valueSlot := c.tmp()
	c.emit(instr.GLOBAL_SET, uint64(valueSlot))
	star := tupleStarIndex(target)
	if star >= 0 {
		c.unpackAssignStar(target, value, valueSlot, star)
		return
	}
	for i, elem := range target.Elems {
		name := elem.(*ast.Name)
		c.emitUnpackIndex(value, valueSlot, i)
		c.set(name.Name)
	}
}

func (c *lowerer) unpackAssignStar(target *ast.TupleLit, value ast.Expr, valueSlot int, star int) {
	suffix := len(target.Elems) - star - 1
	if _, ok := c.types[value].(*types.List); ok {
		for i, elem := range target.Elems {
			if starred, ok := elem.(*ast.Starred); ok {
				name := starred.X.(*ast.Name)
				c.emit(instr.GLOBAL_GET, uint64(valueSlot))
				c.emit(instr.I64_CONST, uint64(star))
				c.emit(instr.GLOBAL_GET, uint64(valueSlot))
				c.emit(instr.ARRAY_LEN)
				c.emit(instr.I32_TO_I64_S)
				c.emit(instr.I64_CONST, uint64(suffix))
				c.emit(instr.I64_SUB)
				c.emit(instr.I64_CONST, 1)
				c.callHost(c.arraySlice(c.types[value]))
				c.set(name.Name)
				continue
			}
			name := elem.(*ast.Name)
			idx := i
			if i > star {
				idx = -suffix + (i - star - 1)
			}
			c.emitListUnpackIndex(valueSlot, idx)
			c.set(name.Name)
		}
		return
	}
	for i, elem := range target.Elems {
		if starred, ok := elem.(*ast.Starred); ok {
			name := starred.X.(*ast.Name)
			c.emitTupleRestList(valueSlot, c.types[value].(*types.Tuple), star, suffix)
			c.set(name.Name)
			continue
		}
		name := elem.(*ast.Name)
		idx := i
		if i > star {
			idx = len(c.types[value].(*types.Tuple).Elems) - suffix + (i - star - 1)
		}
		c.emitUnpackIndex(value, valueSlot, idx)
		c.set(name.Name)
	}
}

func (c *lowerer) emitUnpackIndex(value ast.Expr, valueSlot int, idx int) {
	c.emit(instr.GLOBAL_GET, uint64(valueSlot))
	c.emit(instr.I32_CONST, uint64(idx))
	switch c.types[value].(type) {
	case *types.Tuple:
		c.emit(instr.STRUCT_GET)
	case *types.List:
		c.emit(instr.ARRAY_GET)
	default:
		c.fail(fmt.Errorf("lower unpack value %T: unsupported type %T", value, c.types[value]))
	}
}

func (c *lowerer) emitListUnpackIndex(valueSlot int, idx int) {
	c.emit(instr.GLOBAL_GET, uint64(valueSlot))
	if idx < 0 {
		c.emit(instr.GLOBAL_GET, uint64(valueSlot))
		c.emit(instr.ARRAY_LEN)
		c.emit(instr.I32_TO_I64_S)
		c.emit(instr.I64_CONST, uint64(-idx))
		c.emit(instr.I64_SUB)
		c.emit(instr.I64_TO_I32)
	} else {
		c.emit(instr.I32_CONST, uint64(idx))
	}
	c.emit(instr.ARRAY_GET)
}

func (c *lowerer) emitTupleRestList(valueSlot int, tuple *types.Tuple, star int, suffix int) {
	restLen := len(tuple.Elems) - star - suffix
	elemType := homogeneous(tuple.Elems[star : star+restLen])
	listType := types.NewList(elemType)
	c.emit(instr.I32_CONST, uint64(restLen))
	c.emit(instr.ARRAY_NEW_DEFAULT, c.typeIndex(listType))
	for i := 0; i < restLen; i++ {
		c.emit(instr.DUP)
		c.emit(instr.I32_CONST, uint64(i))
		c.emit(instr.GLOBAL_GET, uint64(valueSlot))
		c.emit(instr.I32_CONST, uint64(star+i))
		c.emit(instr.STRUCT_GET)
		c.emit(instr.ARRAY_SET)
	}
}

func (c *lowerer) get(name string) {
	if slot, ok := c.temps[name]; ok {
		c.emit(instr.GLOBAL_GET, uint64(slot))
		return
	}
	if c.locals != nil {
		if l, ok := c.locals[name]; ok {
			c.emit(instr.LOCAL_GET, uint64(l.index))
			if l.boxed {
				c.emit(instr.REF_GET)
			}
			return
		}
	}
	if c.current != nil {
		if cap, ok := c.current.captures[name]; ok {
			c.emit(instr.UPVAL_GET, uint64(cap.index))
			if cap.boxed || cap.src.boxed {
				c.emit(instr.REF_GET)
			}
			return
		}
	}
	c.emit(instr.GLOBAL_GET, uint64(c.globals[c.symbol(name)].index))
}

func (c *lowerer) set(name string) {
	if slot, ok := c.temps[name]; ok {
		c.emit(instr.GLOBAL_SET, uint64(slot))
		return
	}
	if c.locals != nil {
		if l, ok := c.locals[name]; ok {
			if l.boxed {
				if !c.boxed[l] {
					c.emit(instr.REF_NEW)
					c.emit(instr.LOCAL_SET, uint64(l.index))
					c.boxed[l] = true
					return
				}
				c.emit(instr.LOCAL_GET, uint64(l.index))
				c.emit(instr.SWAP)
				c.emit(instr.REF_SET)
				return
			}
			c.emit(instr.LOCAL_SET, uint64(l.index))
			return
		}
	}
	if c.current != nil {
		if cap, ok := c.current.captures[name]; ok {
			c.emit(instr.UPVAL_GET, uint64(cap.index))
			c.emit(instr.SWAP)
			if cap.boxed || cap.src.boxed {
				c.emit(instr.REF_SET)
			} else {
				c.emit(instr.UPVAL_SET, uint64(cap.index))
			}
			return
		}
	}
	c.emit(instr.GLOBAL_SET, uint64(c.globals[c.symbol(name)].index))
}

func (c *lowerer) symbol(name string) string {
	if c.mod != nil {
		if b, ok := c.mod.bindings[name]; ok && b.symbol != "" {
			return moduleKey(b.module, b.symbol)
		}
		if c.mod.name != "__main__" {
			key := c.mod.name + "." + name
			if _, ok := c.globals[key]; ok {
				return key
			}
			if _, ok := c.functions[key]; ok {
				return key
			}
			if _, ok := c.classes[key]; ok {
				return key
			}
		}
	}
	if _, ok := c.globals[name]; ok {
		return name
	}
	if _, ok := c.functions[name]; ok {
		return name
	}
	if _, ok := c.classes[name]; ok {
		return name
	}
	if _, ok := c.reg.FallbackSymbol(name); ok {
		return c.reg.FallbackName() + "." + name
	}
	return name
}

// narrowCast unboxes a ref-backed binding (union/Any) to the concrete type the
// checker narrowed this use to. Flow-proven narrowing (isinstance / is-None)
// recorded a concrete type on the use node while the slot stays a ref, so a
// checked REF_CAST recovers the unboxed value. No cast is emitted when the use
// itself is still dynamic or None.
func (c *lowerer) narrowCast(x *ast.Name) {
	use := c.types[x]
	if use == nil || refDynamic(use) || types.Equal(use, types.None) {
		return
	}
	if !refDynamic(c.typ(x.Name)) {
		return
	}
	c.emit(instr.REF_CAST, c.typeIndex(use))
}

// refDynamic reports whether a type is represented as minivm's dynamic ref —
// a union or Any — whose members are recovered with REF_TEST / REF_CAST.
func refDynamic(t types.Type) bool {
	if _, ok := t.(*types.Union); ok {
		return true
	}
	return types.IsAny(t)
}

func (c *lowerer) typ(name string) types.Type {
	if _, ok := c.temps[name]; ok {
		return types.Int
	}
	if c.locals != nil {
		if l, ok := c.locals[name]; ok {
			return l.typ
		}
	}
	if c.current != nil {
		if cap, ok := c.current.captures[name]; ok {
			return cap.typ
		}
	}
	return c.globals[c.symbol(name)].typ
}

// emitIf lowers `if`/`elif`/`else`: invert the condition and branch over the
// then-block to the else-block (docs/spec/05-codegen.md).
func (c *lowerer) emitIf(n *ast.If) {
	if known, truth := c.truth(n.Cond); known {
		if truth {
			c.block(n.Body)
		} else {
			c.block(n.Orelse)
		}
		return
	}
	c.expr(n.Cond)
	c.emit(instr.I32_EQZ)
	elseL := c.label()
	end := c.label()
	c.brIf(elseL)
	c.block(n.Body)
	c.br(end)
	c.bind(elseL)
	c.block(n.Orelse)
	c.bind(end)
}

// emitWhile lowers `while`: re-test at the top, run the else block on natural
// exit (not after a break). continue → top, break → past the else block.
func (c *lowerer) emitWhile(n *ast.While) {
	top := c.label()
	elseL := c.label()
	end := c.label()

	c.bind(top)
	c.expr(n.Cond)
	c.emit(instr.I32_EQZ)
	c.brIf(elseL)

	c.loops = append(c.loops, loopLabels{cont: top, brk: end})
	c.block(n.Body)
	c.loops = c.loops[:len(c.loops)-1]

	c.br(top)
	c.bind(elseL)
	c.block(n.Orelse)
	c.bind(end)
}

// emitFor lowers array-backed iterables with indexed loops and Iterator[T]
// values with the minivm coroutine/iterator protocol. continue → increment or
// resume, break → past the else block.
func (c *lowerer) emitFor(n *ast.For) {
	if c.iterates(c.types[n.Iter]) {
		c.emitIteratorFor(n, func() {
			c.iterate(n.Iter, c.types[n.Iter])
		})
		return
	}
	c.emitIterableFor(n)
}

func (c *lowerer) emitIteratorFor(n *ast.For, emitIter func()) {
	iterSlot := c.tmp()
	emitIter()
	c.emit(instr.GLOBAL_SET, uint64(iterSlot))
	top := c.label()
	cont := c.label()
	elseL := c.label()
	end := c.label()

	c.bind(top)
	c.emit(instr.GLOBAL_GET, uint64(iterSlot))
	c.emit(instr.CORO_DONE)
	c.brIf(elseL)
	c.emit(instr.GLOBAL_GET, uint64(iterSlot))
	c.emit(instr.CORO_VALUE)
	c.setLoopTarget(n.Target)

	c.loops = append(c.loops, loopLabels{cont: cont, brk: end})
	c.block(n.Body)
	c.loops = c.loops[:len(c.loops)-1]

	c.bind(cont)
	c.emitResumeIterator(iterSlot)
	c.br(top)

	c.bind(elseL)
	c.block(n.Orelse)
	c.bind(end)
}

func (c *lowerer) iterates(t types.Type) bool {
	switch t.(type) {
	case *types.Iterator, *types.Dict, *types.Set:
		return true
	default:
		return types.Equal(t, types.Str)
	}
}

func (c *lowerer) iterate(expr ast.Expr, typ types.Type) {
	if _, ok := typ.(*types.Iterator); ok {
		c.expr(expr)
		return
	}
	c.expr(expr)
	switch typ.(type) {
	case *types.Dict, *types.Set:
		c.emit(instr.MAP_ITER)
	default:
		if types.Equal(typ, types.Str) {
			c.callHost(c.strIter())
		}
	}
}

func (c *lowerer) emitIterableFor(n *ast.For) {
	iterSlot := c.tmp()
	idxSlot := c.tmp()

	c.expr(n.Iter)
	c.emit(instr.GLOBAL_SET, uint64(iterSlot))
	c.emit(instr.I64_CONST, 0)
	c.emit(instr.GLOBAL_SET, uint64(idxSlot))

	top := c.label()
	cont := c.label()
	elseL := c.label()
	end := c.label()

	c.bind(top)
	c.emit(instr.GLOBAL_GET, uint64(idxSlot))
	c.emit(instr.GLOBAL_GET, uint64(iterSlot))
	c.emit(instr.ARRAY_LEN)
	c.emit(instr.I32_TO_I64_S)
	c.emit(instr.I64_LT_S)
	c.emit(instr.I32_EQZ)
	c.brIf(elseL)

	c.emit(instr.GLOBAL_GET, uint64(iterSlot))
	c.emit(instr.GLOBAL_GET, uint64(idxSlot))
	c.emit(instr.I64_TO_I32)
	c.emit(instr.ARRAY_GET)
	if types.Equal(c.types[n.Iter], types.Bytes) {
		c.normalizeByteElem()
	}
	c.setLoopTarget(n.Target)

	c.loops = append(c.loops, loopLabels{cont: cont, brk: end})
	c.block(n.Body)
	c.loops = c.loops[:len(c.loops)-1]

	c.bind(cont)
	c.emit(instr.GLOBAL_GET, uint64(idxSlot))
	c.emit(instr.I64_CONST, 1)
	c.emit(instr.I64_ADD)
	c.emit(instr.GLOBAL_SET, uint64(idxSlot))
	c.br(top)

	c.bind(elseL)
	c.block(n.Orelse)
	c.bind(end)
}

func (c *lowerer) setLoopTarget(target ast.Expr) {
	switch t := target.(type) {
	case *ast.Name:
		c.set(t.Name)
	case *ast.TupleLit:
		for i, elem := range t.Elems {
			name := elem.(*ast.Name)
			c.emit(instr.DUP)
			c.emit(instr.I32_CONST, uint64(i))
			c.emit(instr.STRUCT_GET)
			c.set(name.Name)
		}
		c.emit(instr.DROP)
	default:
		c.fail(fmt.Errorf("lower for target %T: unsupported", target))
	}
}

func (c *lowerer) functionStmt(n *ast.Function) {
	info := c.function(n)
	if info == nil {
		return
	}
	if c.current == nil && info.specializable {
		// Emit each monomorphic instantiation as its own function constant; the
		// Any-typed body below still goes to the global slot as the fallback.
		for _, spec := range info.instances {
			c.buildSpec(spec)
		}
	}
	decSlots := c.emitDecoratorValues(n.Decorators)
	c.funcValue(info, n.Body)
	c.emitFunctionDecorators(decSlots)
	if c.current != nil {
		c.set(n.Name.Name)
		return
	}
	c.emit(instr.GLOBAL_SET, uint64(info.slot.index))
}

// emitDecoratorValues evaluates decorator expressions in source order, each
// into its own temp global slot, so evaluation happens exactly once and
// stays separate from application order.
func (c *lowerer) emitDecoratorValues(decorators []ast.Expr) []int {
	if len(decorators) == 0 {
		return nil
	}
	slots := make([]int, len(decorators))
	for i, dec := range decorators {
		slots[i] = c.tmp()
		c.expr(dec)
		c.emit(instr.GLOBAL_SET, uint64(slots[i]))
	}
	return slots
}

// emitFunctionDecorators applies each previously evaluated decorator to the
// undecorated function value left on the stack by funcValue, in reverse
// (bottom-to-top) order, leaving the final decorated value on the stack.
func (c *lowerer) emitFunctionDecorators(slots []int) {
	for i := len(slots) - 1; i >= 0; i-- {
		c.emit(instr.GLOBAL_GET, uint64(slots[i]))
		c.emit(instr.CALL)
	}
}

// buildFunction finalizes fb, recording rather than panicking on an assembly
// failure (unbound label, branch-offset overflow). kind is "function" or
// "specialization"; name identifies the failing function in the wrapped error.
func (c *lowerer) buildFunction(fb *vmtypes.FunctionBuilder, kind, name string) (*vmtypes.Function, bool) {
	f, err := fb.Build()
	if err != nil {
		c.fail(fmt.Errorf("build %s %s: %w", kind, name, err))
		return nil, false
	}
	return f, true
}

// buildSpec compiles one specialization to a function constant, recording its
// index on the instance. Specializations are top-level and capture nothing.
func (c *lowerer) buildSpec(spec *specialization) {
	if c.failed() || spec == nil || c.building[spec] {
		return
	}
	if _, ok := c.specs[spec]; ok {
		return
	}
	c.building[spec] = true
	defer delete(c.building, spec)
	c.buildCallSpecs(spec.calls)
	if c.failed() {
		return
	}

	info := spec.info
	fb := vmtypes.NewFunctionBuilder(&vmtypes.FunctionType{
		Params:  vmParams(info),
		Returns: vmReturns(info.result),
	})
	fb.WithLocals(vmLocals(info)...)

	child := c.child(fnTarget(fb), info, spec.types, spec.calls, spec.args)
	child.block(info.body)
	child.emitNoneReturn()
	c.adopt(child)

	f, ok := c.buildFunction(fb, "specialization", spec.key)
	if !ok {
		return
	}
	c.specs[spec] = c.prog.Const(f)
}

func (c *lowerer) buildCallSpecs(calls map[*ast.CallExpr]*specialization) {
	for _, spec := range calls {
		c.buildSpec(spec)
	}
}

func (c *lowerer) function(n *ast.Function) *function {
	if c.current != nil {
		return c.current.children[n.Name.Name]
	}
	return c.functions[c.symbol(n.Name.Name)]
}

func (c *lowerer) funcValue(info *function, body []ast.Stmt) {
	if c.failed() {
		return
	}
	fb := vmtypes.NewFunctionBuilder(&vmtypes.FunctionType{
		Params:  vmParams(info),
		Returns: vmReturns(info.result),
	})
	fb.WithLocals(vmLocals(info)...)
	fb.WithCaptures(vmCaps(info)...)

	child := c.child(fnTarget(fb), info, nil, nil, nil)
	child.block(body)
	child.emitNoneReturn()
	c.adopt(child)

	function, ok := c.buildFunction(fb, "function", info.name)
	if !ok {
		return
	}
	for _, name := range info.capOrder {
		cap := info.captures[name]
		c.emitCapture(cap)
	}
	c.constGet(function)
	if len(info.capOrder) > 0 {
		c.emit(instr.CLOSURE_NEW)
	}
}

// child derives a fresh lowerer for a nested function or specialization body:
// code, current, mod, and locals switch to the child's function; types,
// callSpec, and callArgs are overridden only when non-nil (specializations
// narrow them, plain nested functions inherit the parent's); loops, finally,
// excepts, temps, boxed, and err reset to fresh zero values. The caller must
// call adopt(child) once the child finishes lowering its body.
func (c *lowerer) child(code target, info *function, types map[ast.Expr]types.Type, callSpec map[*ast.CallExpr]*specialization, callArgs map[*ast.CallExpr][]ast.Expr) *lowerer {
	child := *c
	child.code = code
	child.current = info
	child.mod = info.mod
	child.locals = info.locals
	if types != nil {
		child.types = types
	}
	if callSpec != nil {
		child.callSpec = callSpec
	}
	if callArgs != nil {
		child.callArgs = callArgs
	}
	child.loops = nil
	child.finally = nil
	child.excepts = nil
	child.temps = map[string]int{}
	child.boxed = map[*local]bool{}
	child.err = nil
	return &child
}

// adopt propagates a finished child's first lowering failure and temp-slot
// high-water mark back onto the parent.
func (c *lowerer) adopt(child *lowerer) {
	if c.err == nil {
		c.err = child.err
	}
	if child.next > c.next {
		c.next = child.next
	}
}

func (c *lowerer) emitCapture(cap *capture) {
	if c.locals != nil {
		for _, l := range c.locals {
			if l == cap.src {
				c.emit(instr.LOCAL_GET, uint64(l.index))
				return
			}
		}
	}
	c.get(cap.name)
}

func (c *lowerer) returnStmt(n *ast.Return) {
	if n.Value != nil {
		c.expr(n.Value)
	} else {
		c.emit(instr.REF_NULL)
	}
	c.inlineFinalizers()
	c.emit(instr.RETURN)
}

func (c *lowerer) yield(n *ast.Yield) {
	c.yieldCore(n.Value, n.From)
	c.emit(instr.DROP)
}

func (c *lowerer) yieldExpr(n *ast.YieldExpr) {
	c.yieldCore(n.Value, n.From)
}

// yieldCore emits a yield (or yield from) and leaves the resume value on the
// stack as the expression result. For v1 the resume value type is None, so
// resumed generators observe None through the result.
func (c *lowerer) yieldCore(value ast.Expr, from bool) {
	if from {
		iterSlot := c.tmp()
		if lt, ok := c.types[value].(*types.List); ok {
			c.expr(value)
			c.callHost(c.listIter(lt))
		} else {
			c.iterate(value, c.types[value])
		}
		c.emit(instr.GLOBAL_SET, uint64(iterSlot))
		top := c.label()
		end := c.label()
		c.bind(top)
		c.emit(instr.GLOBAL_GET, uint64(iterSlot))
		c.emit(instr.CORO_DONE)
		c.brIf(end)
		c.emit(instr.GLOBAL_GET, uint64(iterSlot))
		c.emit(instr.CORO_VALUE)
		c.emit(instr.YIELD)
		c.emit(instr.DROP)
		c.emitResumeIterator(iterSlot)
		c.br(top)
		c.bind(end)
		c.emit(instr.GLOBAL_GET, uint64(iterSlot))
		c.emit(instr.DROP)
		c.emit(instr.REF_NULL)
		return
	}
	if value != nil {
		c.expr(value)
	} else {
		c.emit(instr.REF_NULL)
	}
	c.emit(instr.YIELD)
}

func (c *lowerer) emitNoneReturn() {
	c.emit(instr.REF_NULL)
	c.emit(instr.RETURN)
}

func vmParams(info *function) []vmtypes.Type {
	out := make([]vmtypes.Type, 0, len(info.params))
	for _, p := range info.params {
		out = append(out, p.typ.VM())
	}
	return out
}

func vmLocals(info *function) []vmtypes.Type {
	out := make([]vmtypes.Type, 0, len(info.order))
	for _, name := range info.order {
		l := info.locals[name]
		if l.boxed {
			out = append(out, vmtypes.TypeRef)
		} else {
			out = append(out, l.typ.VM())
		}
	}
	return out
}

func vmCaps(info *function) []vmtypes.Type {
	out := make([]vmtypes.Type, 0, len(info.capOrder))
	for _, name := range info.capOrder {
		cap := info.captures[name]
		if cap.boxed || cap.src.boxed {
			out = append(out, vmtypes.TypeRef)
		} else {
			out = append(out, cap.typ.VM())
		}
	}
	return out
}

func vmReturns(t types.Type) []vmtypes.Type {
	if types.Equal(t, types.None) {
		return []vmtypes.Type{vmtypes.TypeRef}
	}
	return []vmtypes.Type{t.VM()}
}

// expr lowers an expression, leaving exactly one value on the stack.
