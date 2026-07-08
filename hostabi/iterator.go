package hostabi

import vmtypes "github.com/siyul-park/minivm/types"

// Iterator is an eager, in-memory iterator value over a fixed slice of boxed
// values. It implements the minivm coroutine/iterator protocol used by list and
// string iteration and by the builtin iter().
type Iterator struct {
	values []vmtypes.Boxed

	current vmtypes.Boxed
	done    bool

	idx  int
	name string
}

// NewIterator builds an Iterator over a copy of the given values, positioned on
// the first element.
func NewIterator(name string, values []vmtypes.Boxed) *Iterator {
	it := &Iterator{name: name, values: append([]vmtypes.Boxed(nil), values...), done: true}
	if len(values) > 0 {
		it.current = values[0]
		it.idx = 1
		it.done = false
	}
	return it
}

func (it *Iterator) Kind() vmtypes.Kind { return vmtypes.KindRef }
func (it *Iterator) Type() vmtypes.Type { return vmtypes.TypeRef }
func (it *Iterator) String() string     { return it.name }

func (it *Iterator) Current() vmtypes.Value {
	if it.done {
		return vmtypes.BoxedNull
	}
	return it.current
}

func (it *Iterator) Done() bool { return it.done }

func (it *Iterator) Next() bool {
	if it.idx >= len(it.values) {
		it.current = vmtypes.BoxedNull
		it.done = true
		return false
	}
	it.current = it.values[it.idx]
	it.idx++
	it.done = false
	return true
}

func (it *Iterator) Refs() []vmtypes.Ref {
	var refs []vmtypes.Ref
	for _, v := range it.values {
		if v.Kind() == vmtypes.KindRef && v.Ref() != 0 {
			refs = append(refs, vmtypes.Ref(v.Ref()))
		}
	}
	return refs
}
