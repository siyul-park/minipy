package hostabi

import (
	"testing"

	vmtypes "github.com/siyul-park/minivm/types"
)

func TestPyFloat(t *testing.T) {
	tests := []struct {
		in   float64
		want string
	}{
		{1, "1.0"},
		{1.5, "1.5"},
		{0, "0.0"},
		{-2, "-2.0"},
		{100, "100.0"},
	}
	for _, tt := range tests {
		if got := PyFloat(tt.in); got != tt.want {
			t.Errorf("PyFloat(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestNewIterator(t *testing.T) {
	t.Run("empty is done", func(t *testing.T) {
		it := NewIterator("x", nil)
		if !it.Done() {
			t.Fatal("empty iterator should be done")
		}
	})

	t.Run("walks values then finishes", func(t *testing.T) {
		it := NewIterator("x", []vmtypes.Boxed{vmtypes.BoxI64(1), vmtypes.BoxI64(2)})
		if it.Done() || it.Current().(vmtypes.Boxed).I64() != 1 {
			t.Fatal("first element should be 1")
		}
		if !it.Next() || it.Current().(vmtypes.Boxed).I64() != 2 {
			t.Fatal("second element should be 2")
		}
		if it.Next() || !it.Done() {
			t.Fatal("iterator should be exhausted")
		}
	})
}
