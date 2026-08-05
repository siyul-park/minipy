package compiler

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCompileDictMethods(t *testing.T) {
	t.Run("pop removes and returns value", func(t *testing.T) {
		src := "d: dict[str, int] = {\"a\": 1, \"b\": 2}\n" +
			"v: int = d.pop(\"a\")\n" +
			"print(str(v) + \" \" + str(len(d)))\n"
		require.Equal(t, "1 1\n", run(t, src))
	})

	t.Run("pop raises KeyError for missing key", func(t *testing.T) {
		src := "d: dict[str, int] = {\"a\": 1}\n" +
			"try:\n" +
			"    d.pop(\"x\")\n" +
			"except KeyError:\n" +
			"    print(\"caught\")\n"
		require.Equal(t, "caught\n", run(t, src))
	})

	t.Run("update merges another dict", func(t *testing.T) {
		src := "d: dict[str, int] = {\"a\": 1, \"b\": 2}\n" +
			"d.update({\"b\": 3, \"c\": 4})\n" +
			"print(str(d[\"a\"]) + \" \" + str(d[\"b\"]) + \" \" + str(d[\"c\"]))\n"
		require.Equal(t, "1 3 4\n", run(t, src))
	})

	t.Run("setdefault returns existing value", func(t *testing.T) {
		src := "d: dict[str, int] = {\"a\": 1}\n" +
			"v: int = d.setdefault(\"a\", 99)\n" +
			"print(str(v) + \" \" + str(d[\"a\"]))\n"
		require.Equal(t, "1 1\n", run(t, src))
	})

	t.Run("setdefault inserts default when missing", func(t *testing.T) {
		src := "d: dict[str, int] = {\"a\": 1}\n" +
			"v: int = d.setdefault(\"b\", 42)\n" +
			"print(str(v) + \" \" + str(d[\"b\"]))\n"
		require.Equal(t, "42 42\n", run(t, src))
	})

	t.Run("clear empties the dict", func(t *testing.T) {
		src := "d: dict[str, int] = {\"a\": 1, \"b\": 2}\n" +
			"d.clear()\n" +
			"print(str(len(d)))\n"
		require.Equal(t, "0\n", run(t, src))
	})

	t.Run("copy returns independent dict", func(t *testing.T) {
		src := "d: dict[str, int] = {\"a\": 1, \"b\": 2}\n" +
			"e: dict[str, int] = d.copy()\n" +
			"e[\"c\"] = 3\n" +
			"print(str(len(d)) + \" \" + str(len(e)))\n"
		require.Equal(t, "2 3\n", run(t, src))
	})

	// A dict[str, str] literal passed inline is an unrooted temporary whose
	// values are reference-typed: get/setdefault/copy/items must retain the
	// borrowed entries they surface (or embed into a fresh struct/dict), or
	// releasing the temporary frees them out from under the caller.
	t.Run("get on temporary str-valued dict literal", func(t *testing.T) {
		got := run(t, `print({"a": "x", "b": "y"}.get("a", ""))`)
		require.Equal(t, "x\n", got)
	})

	t.Run("setdefault existing key on temporary str-valued dict literal", func(t *testing.T) {
		got := run(t, `print({"a": "x", "b": "y"}.setdefault("a", "z"))`)
		require.Equal(t, "x\n", got)
	})

	t.Run("copy of temporary str-valued dict literal", func(t *testing.T) {
		got := run(t, `print({"a": "x", "b": "y"}.copy())`)
		require.Equal(t, "{'a': 'x', 'b': 'y'}\n", got)
	})

	t.Run("items of temporary str-valued dict literal", func(t *testing.T) {
		got := run(t, `
for k, v in {"a": "x", "b": "y"}.items():
    print(k + "=" + v)
`)
		require.Equal(t, "a=x\nb=y\n", got)
	})

	t.Run("pop with int keys", func(t *testing.T) {
		src := "d: dict[int, str] = {1: \"one\", 2: \"two\"}\n" +
			"v: str = d.pop(1)\n" +
			"print(v + \" \" + str(len(d)))\n"
		require.Equal(t, "one 1\n", run(t, src))
	})

	t.Run("pop with default returns value when key exists", func(t *testing.T) {
		src := "d: dict[str, int] = {\"a\": 1, \"b\": 2}\n" +
			"v: int = d.pop(\"a\", 99)\n" +
			"print(str(v) + \" \" + str(len(d)))\n"
		require.Equal(t, "1 1\n", run(t, src))
	})

	t.Run("pop with default returns default when key missing", func(t *testing.T) {
		src := "d: dict[str, int] = {\"a\": 1}\n" +
			"v: int = d.pop(\"b\", 99)\n" +
			"print(str(v))\n"
		require.Equal(t, "99\n", run(t, src))
	})

	t.Run("pop with default does not modify dict when key missing", func(t *testing.T) {
		src := "d: dict[str, int] = {\"a\": 1, \"b\": 2}\n" +
			"v: int = d.pop(\"c\", 0)\n" +
			"print(str(v) + \" \" + str(len(d)))\n"
		require.Equal(t, "0 2\n", run(t, src))
	})
}
