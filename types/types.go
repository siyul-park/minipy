// Package types is the minipy source-level type system. It stays separate from
// minivm's runtime types because minipy distinguishes `bool` from `int` and
// forbids implicit `int`/`float` mixing (docs/spec/02-types.md) — distinctions
// the VM, where both are `i32`/`i64`, cannot express. Each source type maps to a
// minivm runtime type through VM, reusing minivm's types for the lowering rather
// than re-modelling them.
package types

import (
	"strings"

	vmtypes "github.com/siyul-park/minivm/types"
)

// Type is a minipy source-level type.
type Type interface {
	String() string
	IsNumeric() bool
	VM() vmtypes.Type
	Equal(Type) bool
	sealed()
}

type primitive struct {
	name string
	vm   vmtypes.Type
	num  bool
}

type List struct {
	Elem Type
}

type Dict struct {
	Key   Type
	Value Type
}

type Set struct {
	Elem Type
}

type Tuple struct {
	Elems []Type
}

type Field struct {
	Name string
	Type Type
}

type Class struct {
	Name   string
	Fields []Field
}

type Iterator struct {
	Elem Type
}

type Callable struct {
	Params []Type
	Return Type
}

var (
	Invalid Type = primitive{name: "<invalid>"}
	Int     Type = primitive{name: "int", vm: vmtypes.TypeI64, num: true}
	Float   Type = primitive{name: "float", vm: vmtypes.TypeF64, num: true}
	Bool    Type = primitive{name: "bool", vm: vmtypes.TypeI1}
	Str     Type = primitive{name: "str", vm: vmtypes.TypeString}
	None    Type = primitive{name: "None", vm: vmtypes.TypeRef}
)

func (primitive) sealed() {}
func (*List) sealed()     {}
func (*Dict) sealed()     {}
func (*Set) sealed()      {}
func (*Tuple) sealed()    {}
func (*Class) sealed()    {}
func (*Iterator) sealed() {}
func (*Callable) sealed() {}

func (t primitive) String() string { return t.name }
func (t primitive) IsNumeric() bool {
	return t.num
}
func (t primitive) VM() vmtypes.Type {
	return t.vm
}
func (t primitive) Equal(o Type) bool {
	other, ok := o.(primitive)
	return ok && t.name == other.name
}

func (t *List) String() string {
	if t == nil || t.Elem == nil {
		return "list[<invalid>]"
	}
	return "list[" + t.Elem.String() + "]"
}
func (*List) IsNumeric() bool { return false }
func (t *List) VM() vmtypes.Type {
	if t == nil || t.Elem == nil {
		return nil
	}
	return vmtypes.NewArrayType(t.Elem.VM())
}
func (t *List) Equal(o Type) bool {
	other, ok := o.(*List)
	return ok && Equal(t.Elem, other.Elem)
}

func (t *Dict) String() string {
	if t == nil || t.Key == nil || t.Value == nil {
		return "dict[<invalid>, <invalid>]"
	}
	return "dict[" + t.Key.String() + ", " + t.Value.String() + "]"
}
func (*Dict) IsNumeric() bool { return false }
func (t *Dict) VM() vmtypes.Type {
	if t == nil || t.Key == nil || t.Value == nil {
		return nil
	}
	return vmtypes.NewMapType(t.Key.VM(), t.Value.VM())
}
func (t *Dict) Equal(o Type) bool {
	other, ok := o.(*Dict)
	return ok && Equal(t.Key, other.Key) && Equal(t.Value, other.Value)
}

func (t *Set) String() string {
	if t == nil || t.Elem == nil {
		return "set[<invalid>]"
	}
	return "set[" + t.Elem.String() + "]"
}
func (*Set) IsNumeric() bool { return false }
func (t *Set) VM() vmtypes.Type {
	if t == nil || t.Elem == nil {
		return nil
	}
	return vmtypes.NewMapType(t.Elem.VM(), vmtypes.TypeI1)
}
func (t *Set) Equal(o Type) bool {
	other, ok := o.(*Set)
	return ok && Equal(t.Elem, other.Elem)
}

func (t *Tuple) String() string {
	if t == nil {
		return "tuple[<invalid>]"
	}
	parts := make([]string, len(t.Elems))
	for i, elem := range t.Elems {
		if elem == nil {
			parts[i] = "<invalid>"
		} else {
			parts[i] = elem.String()
		}
	}
	if len(parts) == 1 {
		parts[0] += ","
	}
	return "tuple[" + strings.Join(parts, ", ") + "]"
}
func (*Tuple) IsNumeric() bool { return false }
func (t *Tuple) VM() vmtypes.Type {
	if t == nil {
		return nil
	}
	fields := make([]vmtypes.StructField, len(t.Elems))
	for i, elem := range t.Elems {
		fields[i] = vmtypes.NewStructField(elem.VM())
	}
	return vmtypes.NewStructType(fields...)
}
func (t *Tuple) Equal(o Type) bool {
	other, ok := o.(*Tuple)
	if !ok || len(t.Elems) != len(other.Elems) {
		return false
	}
	for i := range t.Elems {
		if !Equal(t.Elems[i], other.Elems[i]) {
			return false
		}
	}
	return true
}

func (t *Class) String() string {
	if t == nil || t.Name == "" {
		return "<class>"
	}
	return t.Name
}
func (*Class) IsNumeric() bool { return false }
func (t *Class) VM() vmtypes.Type {
	if t == nil {
		return nil
	}
	fields := make([]vmtypes.StructField, len(t.Fields))
	for i, field := range t.Fields {
		fields[i] = vmtypes.NewStructField(field.Type.VM())
	}
	return vmtypes.NewStructType(fields...)
}
func (t *Class) Equal(o Type) bool {
	other, ok := o.(*Class)
	return ok && t.Name == other.Name
}

func (t *Iterator) String() string {
	if t == nil || t.Elem == nil {
		return "Iterator[<invalid>]"
	}
	return "Iterator[" + t.Elem.String() + "]"
}
func (*Iterator) IsNumeric() bool { return false }
func (t *Iterator) VM() vmtypes.Type {
	return vmtypes.TypeRef
}
func (t *Iterator) Equal(o Type) bool {
	other, ok := o.(*Iterator)
	return ok && Equal(t.Elem, other.Elem)
}

func (t *Callable) String() string {
	if t == nil {
		return "Callable[[<invalid>], <invalid>]"
	}
	params := make([]string, len(t.Params))
	for i, param := range t.Params {
		if param == nil {
			params[i] = "<invalid>"
		} else {
			params[i] = param.String()
		}
	}
	ret := "<invalid>"
	if t.Return != nil {
		ret = t.Return.String()
	}
	return "Callable[[" + strings.Join(params, ", ") + "], " + ret + "]"
}
func (*Callable) IsNumeric() bool { return false }
func (t *Callable) VM() vmtypes.Type {
	if t == nil {
		return nil
	}
	params := make([]vmtypes.Type, len(t.Params))
	for i, param := range t.Params {
		params[i] = param.VM()
	}
	returns := []vmtypes.Type{}
	if t.Return != nil {
		returns = append(returns, t.Return.VM())
	}
	return &vmtypes.FunctionType{Params: params, Returns: returns}
}
func (t *Callable) Equal(o Type) bool {
	other, ok := o.(*Callable)
	if !ok || len(t.Params) != len(other.Params) || !Equal(t.Return, other.Return) {
		return false
	}
	for i := range t.Params {
		if !Equal(t.Params[i], other.Params[i]) {
			return false
		}
	}
	return true
}

func ListOf(elem Type) Type {
	return &List{Elem: elem}
}

func DictOf(key, value Type) Type {
	return &Dict{Key: key, Value: value}
}

func SetOf(elem Type) Type {
	return &Set{Elem: elem}
}

func TupleOf(elems ...Type) Type {
	cp := append([]Type(nil), elems...)
	return &Tuple{Elems: cp}
}

func ClassOf(name string, fields []Field) *Class {
	cp := append([]Field(nil), fields...)
	return &Class{Name: name, Fields: cp}
}

func IteratorOf(elem Type) Type {
	return &Iterator{Elem: elem}
}

func CallableOf(params []Type, ret Type) Type {
	return &Callable{Params: append([]Type(nil), params...), Return: ret}
}

// Equal reports structural equality of two source types.
func Equal(a, b Type) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Equal(b)
}

// AssignableTo reports whether a value of type src may be stored where dst is
// expected. There is no implicit coercion, so structural equality is enough.
func AssignableTo(src, dst Type) bool {
	return src != nil && dst != nil && src != Invalid && dst != Invalid && Equal(src, dst)
}

// Printable reports whether str()/print() accept t.
func Printable(t Type) bool {
	if t == nil || t == Invalid {
		return false
	}
	if Equal(t, Int) || Equal(t, Float) || Equal(t, Bool) || Equal(t, Str) || Equal(t, None) {
		return true
	}
	switch t.(type) {
	case *List, *Dict, *Set, *Tuple:
		return true
	default:
		return false
	}
}

// Resolve maps a scalar annotation name to a source type. Container annotations
// are parsed structurally by the checker.
func Resolve(name string) (Type, bool) {
	switch name {
	case "int":
		return Int, true
	case "float":
		return Float, true
	case "bool":
		return Bool, true
	case "str":
		return Str, true
	case "None":
		return None, true
	default:
		return Invalid, false
	}
}
