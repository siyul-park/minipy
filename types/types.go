// Package types is the minipy source-level type system. It stays separate from
// minivm's runtime types because minipy distinguishes `bool` from `int` and
// forbids implicit `int`/`float` mixing (docs/spec/02-types.md) — distinctions
// the VM, where both are `i32`/`i64`, cannot express. Each source type maps to a
// minivm runtime type through VM, reusing minivm's types for the lowering rather
// than re-modelling them.
package types

import (
	"sort"
	"strconv"
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

// Literal is a source-level refinement over scalar literal values. It lowers as
// its base type and exists only so the checker can validate exact constants.
type Literal struct {
	Base   Type
	Values []LiteralValue
}

// LiteralValue is one supported `typing.Literal[...]` value.
type LiteralValue struct {
	kind string
	Int  int64
	Bool bool
	Str  string
}

// Module is a compile-time-only imported module object. It is not a first-class
// runtime value; the checker permits it only as the receiver of attribute access.
type Module struct {
	Name string
}

// Union is a closed disjunction of member types (`A | B`). It is always kept
// normalized (flattened, deduped, sorted) by NewUnion, so two unions with the
// same members compare Equal. Optional[T] is the special case Union{T, None}.
// A union lowers to minivm's dynamic ref type and is unboxed by narrowing.
type Union struct {
	Members []Type
}

// TypeVar is an inference placeholder used by the whole-program solver while it
// resolves the types of unannotated bindings. It must be resolved to a concrete
// type, union, or Any before code generation; reaching VM() is a bug.
type TypeVar struct {
	ID    int
	Bound Type
}

// primitive is the shared representation for scalar source types.
type primitive struct {
	name string
	vm   vmtypes.Type
	num  bool
}

var (
	Invalid  Type = primitive{name: "<invalid>"}
	Int      Type = primitive{name: "int", vm: vmtypes.TypeI64, num: true}
	Float    Type = primitive{name: "float", vm: vmtypes.TypeF64, num: true}
	Bool     Type = primitive{name: "bool", vm: vmtypes.TypeI1}
	Str      Type = primitive{name: "str", vm: vmtypes.TypeString}
	Bytes    Type = primitive{name: "bytes", vm: vmtypes.NewArrayType(vmtypes.TypeI8)}
	Ellipsis Type = primitive{name: "EllipsisType", vm: vmtypes.NewStructType()}
	None     Type = primitive{name: "None", vm: vmtypes.TypeRef}
	// Any is the open top of the lattice (⊤) — the gradual fallback used only
	// when no bounded union fits. It is backed by minivm's dynamic ref type.
	Any       Type = primitive{name: "Any", vm: vmtypes.TypeRef}
	TypeAlias Type = primitive{name: "typing.TypeAlias"}
)

// NewList returns the list type with the given element type.
func NewList(elem Type) Type {
	return &List{Elem: elem}
}

// NewDict returns the dict type with the given key and value types.
func NewDict(key, value Type) Type {
	return &Dict{Key: key, Value: value}
}

// NewSet returns the set type with the given element type.
func NewSet(elem Type) Type {
	return &Set{Elem: elem}
}

// NewTuple returns the tuple type with the given element types.
func NewTuple(elems ...Type) Type {
	copied := append([]Type(nil), elems...)
	return &Tuple{Elems: copied}
}

// NewClass returns the class type with the given name and fields.
func NewClass(name string, fields []Field) *Class {
	copied := append([]Field(nil), fields...)
	return &Class{Name: name, Fields: copied}
}

// NewIterator returns the iterator type with the given element type.
func NewIterator(elem Type) Type {
	return &Iterator{Elem: elem}
}

// NewCallable returns the callable type with the given parameter and return types.
func NewCallable(params []Type, result Type) Type {
	return &Callable{Params: append([]Type(nil), params...), Return: result}
}

// IntLiteral returns a supported integer literal value.
func IntLiteral(v int64) LiteralValue {
	return LiteralValue{kind: "int", Int: v}
}

// BoolLiteral returns a supported boolean literal value.
func BoolLiteral(v bool) LiteralValue {
	return LiteralValue{kind: "bool", Bool: v}
}

// StrLiteral returns a supported string literal value.
func StrLiteral(v string) LiteralValue {
	return LiteralValue{kind: "str", Str: v}
}

// NoneLiteral returns the supported None literal value.
func NoneLiteral() LiteralValue {
	return LiteralValue{kind: "None"}
}

// NewLiteral returns a normalized literal-refinement type.
func NewLiteral(values ...LiteralValue) Type {
	if len(values) == 0 {
		return Invalid
	}
	uniq := make([]LiteralValue, 0, len(values))
	for _, value := range values {
		dup := false
		for _, existing := range uniq {
			if existing.Equal(value) {
				dup = true
				break
			}
		}
		if !dup {
			uniq = append(uniq, value)
		}
	}
	sort.Slice(uniq, func(i, j int) bool { return uniq[i].String() < uniq[j].String() })
	bases := make([]Type, len(uniq))
	for i, value := range uniq {
		bases[i] = value.Base()
	}
	return &Literal{Base: NewUnion(bases...), Values: uniq}
}

// NewModule returns the compile-time module type for a canonical module name.
func NewModule(name string) Type {
	return &Module{Name: name}
}

// NewUnion returns the normalized union of the given members. Nested unions are
// flattened, duplicates removed (by Equal), and members sorted by their string
// form for a canonical representation. A single distinct member collapses to
// that member; an Any member absorbs the whole union to Any; an Invalid member
// poisons the result to Invalid; an empty union is Invalid.
func NewUnion(members ...Type) Type {
	var flat []Type
	var add func(t Type)
	add = func(t Type) {
		switch m := t.(type) {
		case nil:
			return
		case *Union:
			for _, sub := range m.Members {
				add(sub)
			}
		default:
			flat = append(flat, t)
		}
	}
	for _, m := range members {
		add(m)
	}

	var uniq []Type
	for _, m := range flat {
		if m == Invalid {
			return Invalid
		}
		if IsAny(m) {
			return Any
		}
		dup := false
		for _, u := range uniq {
			if Equal(m, u) {
				dup = true
				break
			}
		}
		if !dup {
			uniq = append(uniq, m)
		}
	}

	switch len(uniq) {
	case 0:
		return Invalid
	case 1:
		return uniq[0]
	}
	sort.Slice(uniq, func(i, j int) bool { return uniq[i].String() < uniq[j].String() })
	return &Union{Members: uniq}
}

// newTypeVar returns a fresh inference type variable with the given id.
func newTypeVar(id int) *TypeVar {
	return &TypeVar{ID: id}
}

// isUnion reports whether t is a union and returns it.
func isUnion(t Type) (*Union, bool) {
	u, ok := t.(*Union)
	return u, ok
}

// IsAny reports whether t is the open top type.
func IsAny(t Type) bool { return Equal(t, Any) }

// Erase returns the runtime-representable type for static-only refinements.
func Erase(t Type) Type {
	switch x := t.(type) {
	case *Literal:
		if x == nil {
			return Invalid
		}
		return Erase(x.Base)
	case *Union:
		members := make([]Type, len(x.Members))
		for i, member := range x.Members {
			members[i] = Erase(member)
		}
		return NewUnion(members...)
	default:
		return t
	}
}

// isOptional reports whether t is a union that includes None (i.e. Optional).
func isOptional(t Type) bool {
	u, ok := isUnion(t)
	if !ok {
		return false
	}
	for _, m := range u.Members {
		if Equal(m, None) {
			return true
		}
	}
	return false
}

// Join returns the least upper bound of a and b in the lattice
// (⊥ < concrete < closed-union < Any). Invalid is treated as bottom so error
// operands do not poison inference. Distinct members merge into a closed union.
func Join(a, b Type) Type {
	switch {
	case a == nil || a == Invalid:
		return b
	case b == nil || b == Invalid:
		return a
	case Equal(a, b):
		return a
	case IsAny(a) || IsAny(b):
		return Any
	}
	return NewUnion(a, b)
}

// Without returns u with member t removed — the negative narrowing used in the
// else branch of an isinstance/None guard. Removing the sole remaining member
// yields Invalid (an unreachable branch).
func Without(u, t Type) Type {
	un, ok := isUnion(u)
	if !ok {
		if Equal(u, t) {
			return Invalid
		}
		return u
	}
	var kept []Type
	for _, m := range un.Members {
		if !Equal(m, t) {
			kept = append(kept, m)
		}
	}
	return NewUnion(kept...)
}

// Equal reports structural equality of two source types.
func Equal(a, b Type) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Equal(b)
}

// AssignableTo reports whether a value of type src may be stored where dst is
// expected. There is no implicit numeric coercion, but widening into a union or
// Any is free: a concrete value flows into any union that admits it, and a
// union flows into a wider union whose members cover it.
func AssignableTo(src, dst Type) bool {
	if src == nil || dst == nil || src == Invalid || dst == Invalid {
		return false
	}
	if Equal(src, dst) {
		return true
	}
	if dl, ok := dst.(*Literal); ok {
		sl, ok := src.(*Literal)
		if !ok {
			return false
		}
		for _, value := range sl.Values {
			if !dl.Contains(value) {
				return false
			}
		}
		return true
	}
	if sl, ok := src.(*Literal); ok {
		if du, ok := dst.(*Union); ok {
			return unionAdmits(du, src)
		}
		return AssignableTo(sl.Base, dst)
	}
	if IsAny(dst) {
		return true
	}
	if du, ok := dst.(*Union); ok {
		if su, ok := src.(*Union); ok {
			for _, m := range su.Members {
				if !unionAdmits(du, m) {
					return false
				}
			}
			return true
		}
		return unionAdmits(du, src)
	}
	return false
}

// unionAdmits reports whether union u has a member equal to t.
func unionAdmits(u *Union, t Type) bool {
	for _, m := range u.Members {
		if Equal(m, t) || AssignableTo(t, m) {
			return true
		}
	}
	return false
}

// Printable reports whether str()/print() accept t.
func Printable(t Type) bool {
	if t == nil || t == Invalid {
		return false
	}
	if Equal(t, Int) || Equal(t, Float) || Equal(t, Bool) || Equal(t, Str) || Equal(t, None) {
		return true
	}
	if IsAny(t) {
		return true
	}
	switch v := t.(type) {
	case *List, *Dict, *Set, *Tuple:
		return true
	case *Literal:
		return Printable(v.Base)
	case *Union:
		for _, m := range v.Members {
			if !Printable(m) {
				return false
			}
		}
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
	case "bytes":
		return Bytes, true
	case "EllipsisType":
		return Ellipsis, true
	case "None":
		return None, true
	case "Any":
		return Any, true
	default:
		return Invalid, false
	}
}

func (v LiteralValue) Base() Type {
	switch v.kind {
	case "int":
		return Int
	case "bool":
		return Bool
	case "str":
		return Str
	case "None":
		return None
	default:
		return Invalid
	}
}

func (v LiteralValue) String() string {
	switch v.kind {
	case "int":
		return strconv.FormatInt(v.Int, 10)
	case "bool":
		if v.Bool {
			return "True"
		}
		return "False"
	case "str":
		return strconv.Quote(v.Str)
	case "None":
		return "None"
	default:
		return "<invalid>"
	}
}

func (v LiteralValue) Equal(other LiteralValue) bool {
	return v.kind == other.kind && v.Int == other.Int && v.Bool == other.Bool && v.Str == other.Str
}

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
	result := "<invalid>"
	if t.Return != nil {
		result = t.Return.String()
	}
	return "Callable[[" + strings.Join(params, ", ") + "], " + result + "]"
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

func (t *Literal) String() string {
	if t == nil || len(t.Values) == 0 {
		return "Literal[<invalid>]"
	}
	parts := make([]string, len(t.Values))
	for i, value := range t.Values {
		parts[i] = value.String()
	}
	return "Literal[" + strings.Join(parts, ", ") + "]"
}
func (*Literal) IsNumeric() bool { return false }
func (t *Literal) VM() vmtypes.Type {
	if t == nil || t.Base == nil {
		return nil
	}
	return t.Base.VM()
}
func (t *Literal) Equal(o Type) bool {
	other, ok := o.(*Literal)
	if !ok || len(t.Values) != len(other.Values) {
		return false
	}
	for i := range t.Values {
		if !t.Values[i].Equal(other.Values[i]) {
			return false
		}
	}
	return true
}

// Contains reports whether the literal type admits value.
func (t *Literal) Contains(value LiteralValue) bool {
	if t == nil {
		return false
	}
	for _, existing := range t.Values {
		if existing.Equal(value) {
			return true
		}
	}
	return false
}

func (t *Module) String() string {
	if t == nil || t.Name == "" {
		return "<module>"
	}
	return "module " + t.Name
}
func (*Module) IsNumeric() bool { return false }
func (*Module) VM() vmtypes.Type {
	return vmtypes.TypeRef
}
func (t *Module) Equal(o Type) bool {
	other, ok := o.(*Module)
	return ok && t.Name == other.Name
}

func (t *Union) String() string {
	if t == nil || len(t.Members) == 0 {
		return "<invalid>"
	}
	parts := make([]string, len(t.Members))
	for i, m := range t.Members {
		if m == nil {
			parts[i] = "<invalid>"
		} else {
			parts[i] = m.String()
		}
	}
	return strings.Join(parts, " | ")
}
func (*Union) IsNumeric() bool { return false }
func (*Union) VM() vmtypes.Type {
	return vmtypes.TypeRef
}
func (t *Union) Equal(o Type) bool {
	other, ok := o.(*Union)
	if !ok || len(t.Members) != len(other.Members) {
		return false
	}
	for i := range t.Members {
		if !Equal(t.Members[i], other.Members[i]) {
			return false
		}
	}
	return true
}

func (t *TypeVar) String() string {
	if t == nil {
		return "<invalid>"
	}
	if t.Bound != nil {
		return t.Bound.String()
	}
	return "?"
}
func (t *TypeVar) IsNumeric() bool {
	return t != nil && t.Bound != nil && t.Bound.IsNumeric()
}
func (*TypeVar) VM() vmtypes.Type {
	// A type variable must be resolved before code generation; reaching here is
	// a compiler bug, so report the invalid (nil) VM type rather than guessing.
	return nil
}
func (t *TypeVar) Equal(o Type) bool {
	other, ok := o.(*TypeVar)
	return ok && t != nil && other != nil && t.ID == other.ID
}

// sealed restricts Type implementations to this package.
func (primitive) sealed() {}
func (*List) sealed()     {}
func (*Dict) sealed()     {}
func (*Set) sealed()      {}
func (*Tuple) sealed()    {}
func (*Class) sealed()    {}
func (*Iterator) sealed() {}
func (*Callable) sealed() {}
func (*Literal) sealed()  {}
func (*Module) sealed()   {}
func (*Union) sealed()    {}
func (*TypeVar) sealed()  {}
