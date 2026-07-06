package hostabi

import (
	"testing"

	vmtypes "github.com/siyul-park/minivm/types"

	"github.com/stretchr/testify/require"
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
		require.Equalf(t, tt.want, PyFloat(tt.in), "PyFloat(%v)", tt.in)
	}
}

func TestNewIterator(t *testing.T) {
	t.Run("empty is done", func(t *testing.T) {
		it := NewIterator("x", nil)
		require.True(t, it.Done())
	})

	t.Run("walks values then finishes", func(t *testing.T) {
		it := NewIterator("x", []vmtypes.Boxed{vmtypes.BoxI64(1), vmtypes.BoxI64(2)})
		require.False(t, it.Done())
		require.Equal(t, int64(1), it.Current().(vmtypes.Boxed).I64())
		require.True(t, it.Next())
		require.Equal(t, int64(2), it.Current().(vmtypes.Boxed).I64())
		require.False(t, it.Next())
		require.True(t, it.Done())
	})
}
