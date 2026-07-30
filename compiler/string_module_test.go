package compiler

import (
	"strings"
	"testing"

	"github.com/siyul-park/minipy/token"
	"github.com/stretchr/testify/require"
)

func TestStringModule(t *testing.T) {
	t.Run("digits", func(t *testing.T) {
		src := "import string\nprint(string.digits)\n"
		require.Equal(t, "0123456789\n", run(t, src))
	})

	t.Run("ascii_lowercase", func(t *testing.T) {
		src := "import string\nprint(string.ascii_lowercase)\n"
		require.Equal(t, "abcdefghijklmnopqrstuvwxyz\n", run(t, src))
	})

	t.Run("ascii_uppercase", func(t *testing.T) {
		src := "import string\nprint(string.ascii_uppercase)\n"
		require.Equal(t, "ABCDEFGHIJKLMNOPQRSTUVWXYZ\n", run(t, src))
	})

	t.Run("ascii_letters", func(t *testing.T) {
		src := "import string\nprint(string.ascii_letters)\n"
		require.Equal(t, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ\n", run(t, src))
	})

	t.Run("hexdigits", func(t *testing.T) {
		src := "import string\nprint(string.hexdigits)\n"
		require.Equal(t, "0123456789abcdefABCDEF\n", run(t, src))
	})

	t.Run("octdigits", func(t *testing.T) {
		src := "import string\nprint(string.octdigits)\n"
		require.Equal(t, "01234567\n", run(t, src))
	})

	t.Run("from import ascii_lowercase", func(t *testing.T) {
		src := "from string import ascii_lowercase\nprint(ascii_lowercase)\n"
		require.Equal(t, "abcdefghijklmnopqrstuvwxyz\n", run(t, src))
	})

	t.Run("from import digits", func(t *testing.T) {
		src := "from string import digits\nprint(digits)\n"
		require.Equal(t, "0123456789\n", run(t, src))
	})

	t.Run("assigned to variable", func(t *testing.T) {
		src := "import string\nx: str = string.digits\nprint(x)\n"
		require.Equal(t, "0123456789\n", run(t, src))
	})

	t.Run("punctuation", func(t *testing.T) {
		src := "import string\nprint(string.punctuation)\n"
		require.Equal(t, "!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~\n", run(t, src))
	})
}

func TestStringModuleErrors(t *testing.T) {
	t.Run("constant not callable", func(t *testing.T) {
		src := "import string\nstring.digits()\n"
		_, err := Compile(strings.NewReader(src))
		require.Error(t, err)
		code(t, err, token.TypeMismatch)
	})
}
