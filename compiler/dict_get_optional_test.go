package compiler

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestCompileDictGetOptional regression-tests the single-argument dict.get()
// form: it previously returned the value type's zero value for a missing
// key, and the checker typed the result as V, rejecting `is None`. CPython
// returns None for a missing key and the two-argument form (with an
// explicit default) is unaffected. See docs/spec/04-static-semantics.md and
// docs/spec/06-builtins.md.
func TestCompileDictGetOptional(t *testing.T) {
	t.Run("missing key with no default prints None", func(t *testing.T) {
		src := "d: dict[str, int] = {\"a\": 1}\nprint(d.get(\"zz\"))\n"
		require.Equal(t, "None\n", run(t, src))
	})

	t.Run("present key with no default still returns the value", func(t *testing.T) {
		src := "d: dict[str, int] = {\"a\": 1}\nprint(d.get(\"a\"))\n"
		require.Equal(t, "1\n", run(t, src))
	})

	t.Run("is None narrows a missing single-argument get", func(t *testing.T) {
		src := "d: dict[str, int] = {\"a\": 1}\nprint(d.get(\"zz\") is None)\nprint(d.get(\"a\") is None)\n"
		require.Equal(t, "True\nFalse\n", run(t, src))
	})

	t.Run("narrowed result is usable as the value type", func(t *testing.T) {
		src := "d: dict[str, int] = {\"a\": 1}\n" +
			"v = d.get(\"zz\")\n" +
			"if v is None:\n" +
			"    print(\"none\")\n" +
			"else:\n" +
			"    print(str(v))\n"
		require.Equal(t, "none\n", run(t, src))
	})

	t.Run("two-argument default is still V typed and unaffected", func(t *testing.T) {
		src := "d: dict[str, int] = {\"a\": 1}\ncount: int = d.get(\"zz\", 0) + 1\nprint(str(count))\n"
		require.Equal(t, "1\n", run(t, src))
	})

	t.Run("missing key on str-valued dict still prints None", func(t *testing.T) {
		src := "d: dict[str, str] = {\"a\": \"x\"}\nprint(d.get(\"zz\"))\n"
		require.Equal(t, "None\n", run(t, src))
	})
}
