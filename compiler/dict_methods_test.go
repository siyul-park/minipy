package compiler

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/siyul-park/minivm/interp"
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
		// Dict iteration order is unspecified (docs/spec/02-types.md), so the
		// pairs are collected and sorted before printing. Asserting the raw
		// iteration order makes this test flaky, not stricter.
		got := run(t, `
pairs: list[str] = []
for k, v in {"a": "x", "b": "y"}.items():
    pairs.append(k + "=" + v)
for pair in sorted(pairs):
    print(pair)
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

// runUncaught compiles and runs src, expecting the program to end with an
// uncaught VM error (unlike run, which requires success), and returns that
// error for the caller to inspect.
func runUncaught(t *testing.T, src string) error {
	t.Helper()
	var buf bytes.Buffer
	prog, err := Compile(strings.NewReader(src), WithOutput(&buf))
	require.NoError(t, err)

	vm := interp.New(prog)
	defer vm.Close()
	runErr := vm.Run(context.Background())
	require.Error(t, runErr)
	return runErr
}

// TestCompileDictSubscriptKeyError covers B1: `d[k]` for a missing key used
// to silently return the value type's zero value (or trap with a type
// mismatch) instead of raising KeyError, because the subscript lowering
// used MAP_GET with no presence check. Every dict-keyed read site needs the
// same presence check: plain reads, augmented assignment, del, chained
// reads, and reads reached through comprehensions or f-strings.
func TestCompileDictSubscriptKeyError(t *testing.T) {
	t.Run("missing key raises KeyError instead of a silent zero value", func(t *testing.T) {
		src := "d: dict[str, int] = {\"a\": 1}\n" +
			"try:\n" +
			"    print(d[\"zz\"])\n" +
			"except KeyError:\n" +
			"    print(\"caught\")\n"
		require.Equal(t, "caught\n", run(t, src))
	})

	t.Run("missing key raises KeyError for str-valued dict", func(t *testing.T) {
		src := "d: dict[str, str] = {\"a\": \"1\"}\n" +
			"try:\n" +
			"    print(d[\"zz\"])\n" +
			"except KeyError:\n" +
			"    print(\"caught\")\n"
		require.Equal(t, "caught\n", run(t, src))
	})

	t.Run("missing key raises KeyError for int-keyed dict", func(t *testing.T) {
		src := "d: dict[int, int] = {1: 1}\n" +
			"try:\n" +
			"    print(d[99])\n" +
			"except KeyError:\n" +
			"    print(\"caught\")\n"
		require.Equal(t, "caught\n", run(t, src))
	})

	t.Run("present key still returns its value", func(t *testing.T) {
		src := "d: dict[str, int] = {\"a\": 1}\n" +
			"print(str(d[\"a\"]))\n"
		require.Equal(t, "1\n", run(t, src))
	})

	t.Run("uncaught missing key reports KeyError with a quoted string key", func(t *testing.T) {
		src := "d: dict[str, int] = {\"a\": 1}\n" +
			"print(d[\"zz\"])\n"
		err := runUncaught(t, src)
		require.ErrorContains(t, err, "KeyError: 'zz'")
	})

	t.Run("uncaught missing key reports KeyError with an unquoted int key", func(t *testing.T) {
		src := "d: dict[int, int] = {1: 1}\n" +
			"print(d[99])\n"
		err := runUncaught(t, src)
		require.ErrorContains(t, err, "KeyError: 99")
	})

	t.Run("augmented assignment on a missing key raises KeyError", func(t *testing.T) {
		src := "d: dict[str, int] = {\"a\": 1}\n" +
			"try:\n" +
			"    d[\"zz\"] += 1\n" +
			"except KeyError:\n" +
			"    print(\"caught\")\n"
		require.Equal(t, "caught\n", run(t, src))
	})

	t.Run("augmented assignment on a present key still updates", func(t *testing.T) {
		src := "d: dict[str, int] = {\"a\": 1}\n" +
			"d[\"a\"] += 41\n" +
			"print(str(d[\"a\"]))\n"
		require.Equal(t, "42\n", run(t, src))
	})

	t.Run("del on a missing key raises KeyError", func(t *testing.T) {
		src := "d: dict[str, int] = {\"a\": 1}\n" +
			"try:\n" +
			"    del d[\"zz\"]\n" +
			"except KeyError:\n" +
			"    print(\"caught\")\n"
		require.Equal(t, "caught\n", run(t, src))
	})

	t.Run("del on a present key still removes it", func(t *testing.T) {
		src := "d: dict[str, int] = {\"a\": 1, \"b\": 2}\n" +
			"del d[\"a\"]\n" +
			"print(str(len(d)))\n"
		require.Equal(t, "1\n", run(t, src))
	})

	t.Run("assignment to a missing key inserts rather than raising", func(t *testing.T) {
		src := "d: dict[str, int] = {\"a\": 1}\n" +
			"d[\"zz\"] = 5\n" +
			"print(str(d[\"zz\"]) + \" \" + str(len(d)))\n"
		require.Equal(t, "5 2\n", run(t, src))
	})

	t.Run("chained subscript raises KeyError from the inner read", func(t *testing.T) {
		src := "d: dict[str, dict[str, int]] = {\"a\": {\"b\": 1}}\n" +
			"try:\n" +
			"    print(d[\"a\"][\"zz\"])\n" +
			"except KeyError:\n" +
			"    print(\"caught\")\n"
		require.Equal(t, "caught\n", run(t, src))
	})

	t.Run("chained subscript raises KeyError from the outer read", func(t *testing.T) {
		src := "d: dict[str, dict[str, int]] = {\"a\": {\"b\": 1}}\n" +
			"try:\n" +
			"    print(d[\"zz\"][\"b\"])\n" +
			"except KeyError:\n" +
			"    print(\"caught\")\n"
		require.Equal(t, "caught\n", run(t, src))
	})

	t.Run("dict read inside a list comprehension raises KeyError", func(t *testing.T) {
		src := "d: dict[str, int] = {\"a\": 1}\n" +
			"try:\n" +
			"    r: list[int] = [d[k] for k in [\"a\", \"zz\"]]\n" +
			"    print(str(r))\n" +
			"except KeyError:\n" +
			"    print(\"caught\")\n"
		require.Equal(t, "caught\n", run(t, src))
	})

	t.Run("dict read inside an f-string raises KeyError", func(t *testing.T) {
		src := "d: dict[str, int] = {\"a\": 1}\n" +
			"try:\n" +
			"    print(f\"{d['zz']}\")\n" +
			"except KeyError:\n" +
			"    print(\"caught\")\n"
		require.Equal(t, "caught\n", run(t, src))
	})
}
