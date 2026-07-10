from pathlib import Path


def replace(path: str, old: str, new: str) -> None:
    file = Path(path)
    text = file.read_text()
    if new in text:
        return
    if old not in text:
        raise RuntimeError(f"anchor not found in {path}: {old[:80]!r}")
    file.write_text(text.replace(old, new, 1))


replace(
    "token/token.go",
    "\tDOT           // .\n\tSEMICOLON     // ;",
    "\tDOT           // .\n\tELLIPSIS      // ...\n\tSEMICOLON     // ;",
)
replace(
    "token/token.go",
    '\tDOT:           ".",\n\tSEMICOLON:     ";",',
    '\tDOT:           ".",\n\tELLIPSIS:      "...",\n\tSEMICOLON:     ";",',
)

replace(
    "lexer/lexer.go",
    "\tcase '.':\n\t\temit(token.DOT, 1)",
    "\tcase '.':\n\t\tif la(1) == '.' && la(2) == '.' {\n\t\t\temit(token.ELLIPSIS, 3)\n\t\t} else {\n\t\t\temit(token.DOT, 1)\n\t\t}",
)

replace(
    "ast/ast.go",
    "// NoneLit is `None`.\ntype NoneLit struct {\n\tBase\n}\n",
    "// NoneLit is `None`.\ntype NoneLit struct {\n\tBase\n}\n\n// EllipsisLit is `...`.\ntype EllipsisLit struct {\n\tBase\n}\n",
)
replace(
    "ast/ast.go",
    "func (*NoneLit) exprNode()      {}\nfunc (*UnaryExpr) exprNode()",
    "func (*NoneLit) exprNode()      {}\nfunc (*EllipsisLit) exprNode()  {}\nfunc (*UnaryExpr) exprNode()",
)

replace(
    "parser/parser.go",
    "\tcase token.NONE:\n\t\tp.advance()\n\t\treturn &ast.NoneLit{Base: ast.Base{Position: t.Pos}}\n\tcase token.INT:",
    "\tcase token.NONE:\n\t\tp.advance()\n\t\treturn &ast.NoneLit{Base: ast.Base{Position: t.Pos}}\n\tcase token.ELLIPSIS:\n\t\tp.advance()\n\t\treturn &ast.EllipsisLit{Base: ast.Base{Position: t.Pos}}\n\tcase token.INT:",
)

replace(
    "types/types.go",
    '\tBytes   Type = primitive{name: "bytes", vm: vmtypes.NewArrayType(vmtypes.TypeI8)}\n\tNone    Type = primitive{name: "None", vm: vmtypes.TypeRef}',
    '\tBytes      Type = primitive{name: "bytes", vm: vmtypes.NewArrayType(vmtypes.TypeI8)}\n\tellipsisVM      = vmtypes.NewStructType(vmtypes.NewStructField(vmtypes.TypeI1))\n\tEllipsis   Type = primitive{name: "EllipsisType", vm: ellipsisVM}\n\tNone       Type = primitive{name: "None", vm: vmtypes.TypeRef}',
)
replace(
    "types/types.go",
    '\tcase "bytes":\n\t\treturn Bytes, true\n\tcase "None":',
    '\tcase "bytes":\n\t\treturn Bytes, true\n\tcase "EllipsisType":\n\t\treturn Ellipsis, true\n\tcase "None":',
)

replace(
    "compiler/check.go",
    "\tcase *ast.NoneLit:\n\t\treturn types.None\n\tcase *ast.Name:",
    "\tcase *ast.NoneLit:\n\t\treturn types.None\n\tcase *ast.EllipsisLit:\n\t\treturn types.Ellipsis\n\tcase *ast.Name:",
)
replace(
    "compiler/check.go",
    "func (c *checker) indexResultType(n *ast.Subscript, receiver, index types.Type) types.Type {\n\tswitch t := receiver.(type) {",
    "func (c *checker) indexResultType(n *ast.Subscript, receiver, index types.Type) types.Type {\n\tif types.Equal(index, types.Ellipsis) {\n\t\tc.errs.Add(n.Index.Pos(), token.UnsupportedFeature, \"ellipsis subscript is not supported\")\n\t\treturn types.Invalid\n\t}\n\tswitch t := receiver.(type) {",
)
replace(
    "compiler/check.go",
    "\tg, ok := c.globals[res.key]\n\tif !ok {\n\t\tc.errs.Add(n.Pos(), token.UndefinedName, \"name %q is not defined\", n.Name)",
    "\tg, ok := c.globals[res.key]\n\tif !ok {\n\t\tif n.Name == \"Ellipsis\" {\n\t\t\treturn types.Ellipsis\n\t\t}\n\t\tc.errs.Add(n.Pos(), token.UndefinedName, \"name %q is not defined\", n.Name)",
)

replace(
    "compiler/lower.go",
    "var (\n\terrListIndexValue  = errors.New(\"list.index value not found\")",
    "var (\n\tellipsisValue      = vmtypes.NewStruct(types.Ellipsis.VM().(*vmtypes.StructType), vmtypes.BoxI1(true))\n\terrListIndexValue  = errors.New(\"list.index value not found\")",
)
replace(
    "compiler/lower.go",
    "\tcase *ast.NoneLit:\n\t\tc.emit(instr.REF_NULL)\n\tcase *ast.StrLit:",
    "\tcase *ast.NoneLit:\n\t\tc.emit(instr.REF_NULL)\n\tcase *ast.EllipsisLit:\n\t\tc.constGet(ellipsisValue)\n\tcase *ast.StrLit:",
)
replace(
    "compiler/lower.go",
    "\tcase *ast.Name:\n\t\tc.get(x.Name)\n\t\tc.narrowCast(x)",
    "\tcase *ast.Name:\n\t\tif x.Name == \"Ellipsis\" && types.Equal(c.types[x], types.Ellipsis) {\n\t\t\tc.constGet(ellipsisValue)\n\t\t} else {\n\t\t\tc.get(x.Name)\n\t\t\tc.narrowCast(x)\n\t\t}",
)

replace(
    "operator/types.go",
    "\tif op == token.IS || op == token.ISNOT {\n\t\tif !identityComparable(left) || !identityComparable(right) {",
    "\tif op == token.IS || op == token.ISNOT {\n\t\tif types.Equal(left, types.Ellipsis) || types.Equal(right, types.Ellipsis) {\n\t\t\tif !types.Equal(left, types.Ellipsis) || !types.Equal(right, types.Ellipsis) {\n\t\t\t\tc.Error(pos, token.TypeMismatch, \"'%s' requires matching Ellipsis operands, got %s and %s\", op, left, right)\n\t\t\t}\n\t\t\treturn\n\t\t}\n\t\tif !identityComparable(left) || !identityComparable(right) {",
)
replace(
    "operator/types.go",
    "\tif types.Equal(left, types.None) || types.Equal(right, types.None) {",
    "\tif types.Equal(left, types.Ellipsis) || types.Equal(right, types.Ellipsis) {\n\t\tif (op == token.EQ || op == token.NE) && types.Equal(left, types.Ellipsis) && types.Equal(right, types.Ellipsis) {\n\t\t\treturn\n\t\t}\n\t\tc.Error(pos, token.NotComparable, \"'%s' not supported between instances of %s and %s\", op, left, right)\n\t\treturn\n\t}\n\tif types.Equal(left, types.None) || types.Equal(right, types.None) {",
)
replace(
    "operator/types.go",
    "\tif types.Equal(t, types.None) || types.Equal(t, types.Str) || types.IsAny(t) {",
    "\tif types.Equal(t, types.None) || types.Equal(t, types.Ellipsis) || types.Equal(t, types.Str) || types.IsAny(t) {",
)

replace(
    "operator/emit.go",
    "\tif left == types.Bytes || right == types.Bytes {",
    "\tif types.Equal(left, types.Ellipsis) || types.Equal(right, types.Ellipsis) {\n\t\te.Emit(instr.REF_EQ)\n\t\tif op == token.NE {\n\t\t\te.Emit(instr.I32_EQZ)\n\t\t}\n\t\treturn\n\t}\n\tif left == types.Bytes || right == types.Bytes {",
)
