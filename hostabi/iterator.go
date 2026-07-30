package hostabi

import vmtypes "github.com/siyul-park/minivm/types"

var _ vmtypes.Traceable = (*Iterator)(nil)

// Iterator is an eager, in-memory iterator value over a fixed defensive copy of
// boxed values. While the iterator is live, Refs keeps every copied reference
// live, including values already consumed, so Current and later values cannot
// become stale during minivm graph walks.
type Iterator struct {
	name   string
	values []vmtypes.Boxed

	current vmtypes.Boxed
	index   int
	done    bool
}

// NewIterator builds an Iterator over a copy of the given values, positioned on
// the first element.
func NewIterator(name string, values []vmtypes.Boxed) *Iterator {
	iterator := &Iterator{
		name:   name,
		values: append([]vmtypes.Boxed(nil), values...),
		done:   true,
	}
	if len(iterator.values) > 0 {
		iterator.current = iterator.values[0]
		iterator.index = 1
		iterator.done = false
	}
	return iterator
}

func (iterator *Iterator) Current() vmtypes.Value {
	if iterator.done {
		return vmtypes.BoxedNull
	}
	return iterator.current
}

func (iterator *Iterator) Done() bool { return iterator.done }

func (iterator *Iterator) Next() bool {
	if iterator.index >= len(iterator.values) {
		iterator.current = vmtypes.BoxedNull
		iterator.done = true
		return false
	}
	iterator.current = iterator.values[iterator.index]
	iterator.index++
	iterator.done = false
	return true
}

func (iterator *Iterator) Kind() vmtypes.Kind { return vmtypes.KindRef }
func (iterator *Iterator) Type() vmtypes.Type { return vmtypes.TypeRef }
func (iterator *Iterator) String() string     { return iterator.name }

func (iterator *Iterator) Refs(refs []vmtypes.Ref) []vmtypes.Ref {
	for _, value := range iterator.values {
		if value.Kind() == vmtypes.KindRef && value.Ref() != 0 {
			refs = append(refs, vmtypes.Ref(value.Ref()))
		}
	}
	return refs
}
