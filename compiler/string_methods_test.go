package compiler

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCompileStringMethods(t *testing.T) {
	t.Run("strip no args", func(t *testing.T) {
		src := `print("  hello  ".strip())`
		require.Equal(t, "hello\n", run(t, src))
	})

	t.Run("strip with chars", func(t *testing.T) {
		src := `print("xxhelloxx".strip("x"))`
		require.Equal(t, "hello\n", run(t, src))
	})

	t.Run("lstrip no args", func(t *testing.T) {
		src := `print("  hello  ".lstrip())`
		require.Equal(t, "hello  \n", run(t, src))
	})

	t.Run("lstrip with chars", func(t *testing.T) {
		src := `print("xxhello".lstrip("x"))`
		require.Equal(t, "hello\n", run(t, src))
	})

	t.Run("rstrip no args", func(t *testing.T) {
		src := `print("  hello  ".rstrip())`
		require.Equal(t, "  hello\n", run(t, src))
	})

	t.Run("rstrip with chars", func(t *testing.T) {
		src := `print("helloxx".rstrip("x"))`
		require.Equal(t, "hello\n", run(t, src))
	})

	t.Run("startswith true", func(t *testing.T) {
		src := `print(str("hello".startswith("he")))`
		require.Equal(t, "True\n", run(t, src))
	})

	t.Run("startswith false", func(t *testing.T) {
		src := `print(str("hello".startswith("lo")))`
		require.Equal(t, "False\n", run(t, src))
	})

	t.Run("endswith true", func(t *testing.T) {
		src := `print(str("hello".endswith("lo")))`
		require.Equal(t, "True\n", run(t, src))
	})

	t.Run("endswith false", func(t *testing.T) {
		src := `print(str("hello".endswith("he")))`
		require.Equal(t, "False\n", run(t, src))
	})

	t.Run("replace all", func(t *testing.T) {
		src := `print("hello".replace("l", "r"))`
		require.Equal(t, "herro\n", run(t, src))
	})

	t.Run("replace with count", func(t *testing.T) {
		src := `print("hello".replace("l", "r", 1))`
		require.Equal(t, "herlo\n", run(t, src))
	})

	t.Run("count", func(t *testing.T) {
		src := `print(str("hello".count("l")))`
		require.Equal(t, "2\n", run(t, src))
	})

	t.Run("isdigit true", func(t *testing.T) {
		src := `print(str("123".isdigit()))`
		require.Equal(t, "True\n", run(t, src))
	})

	t.Run("isdigit false", func(t *testing.T) {
		src := `print(str("12a".isdigit()))`
		require.Equal(t, "False\n", run(t, src))
	})

	t.Run("isdigit empty", func(t *testing.T) {
		src := `print(str("".isdigit()))`
		require.Equal(t, "False\n", run(t, src))
	})

	t.Run("isalpha true", func(t *testing.T) {
		src := `print(str("hello".isalpha()))`
		require.Equal(t, "True\n", run(t, src))
	})

	t.Run("isalpha false", func(t *testing.T) {
		src := `print(str("hello1".isalpha()))`
		require.Equal(t, "False\n", run(t, src))
	})

	t.Run("isalnum true", func(t *testing.T) {
		src := `print(str("hello123".isalnum()))`
		require.Equal(t, "True\n", run(t, src))
	})

	t.Run("isalnum false", func(t *testing.T) {
		src := `print(str("hello 123".isalnum()))`
		require.Equal(t, "False\n", run(t, src))
	})

	t.Run("isspace true", func(t *testing.T) {
		src := `print(str("   ".isspace()))`
		require.Equal(t, "True\n", run(t, src))
	})

	t.Run("isspace false", func(t *testing.T) {
		src := `print(str(" a ".isspace()))`
		require.Equal(t, "False\n", run(t, src))
	})

	t.Run("capitalize", func(t *testing.T) {
		src := `print("hello WORLD".capitalize())`
		require.Equal(t, "Hello world\n", run(t, src))
	})

	t.Run("title", func(t *testing.T) {
		src := `print("hello world".title())`
		require.Equal(t, "Hello World\n", run(t, src))
	})

	t.Run("swapcase", func(t *testing.T) {
		src := `print("Hello World".swapcase())`
		require.Equal(t, "hELLO wORLD\n", run(t, src))
	})

	t.Run("center", func(t *testing.T) {
		src := `print("hi".center(10))`
		require.Equal(t, "    hi    \n", run(t, src))
	})

	t.Run("center with fill", func(t *testing.T) {
		src := `print("hi".center(10, "*"))`
		require.Equal(t, "****hi****\n", run(t, src))
	})

	t.Run("ljust", func(t *testing.T) {
		src := `print("hi".ljust(10))`
		require.Equal(t, "hi        \n", run(t, src))
	})

	t.Run("ljust with fill", func(t *testing.T) {
		src := `print("hi".ljust(10, "*"))`
		require.Equal(t, "hi********\n", run(t, src))
	})

	t.Run("rjust", func(t *testing.T) {
		src := `print("hi".rjust(10))`
		require.Equal(t, "        hi\n", run(t, src))
	})

	t.Run("rjust with fill", func(t *testing.T) {
		src := `print("hi".rjust(10, "*"))`
		require.Equal(t, "********hi\n", run(t, src))
	})

	t.Run("zfill", func(t *testing.T) {
		src := `print("42".zfill(5))`
		require.Equal(t, "00042\n", run(t, src))
	})

	t.Run("zfill with sign", func(t *testing.T) {
		src := `print("-42".zfill(5))`
		require.Equal(t, "-0042\n", run(t, src))
	})

	t.Run("encode", func(t *testing.T) {
		src := `print(str(len("hello".encode())))`
		require.Equal(t, "5\n", run(t, src))
	})
}

// TestCompileStrSplitNoSeparator pins the no-argument str.split(). CPython
// treats it as a different algorithm from split(sep) — splitting on runs of any
// whitespace and dropping leading and trailing empty fields — rather than as
// split(" "), which is what minipy previously lowered it to.
func TestCompileStrSplitNoSeparator(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "collapses runs of spaces and splits on tabs",
			src:  "print(\"  a  b\\tc \".split())\n",
			want: "['a', 'b', 'c']\n",
		},
		{
			name: "splits on newlines and drops empty fields",
			src:  "print(\"a\\nb\\n\\nc\".split())\n",
			want: "['a', 'b', 'c']\n",
		},
		{
			name: "splits on Python C0 whitespace separators",
			src:  "print(\"a\\x1cb\\x1dc\\x1ed\\x1fe\".split())\n",
			want: "['a', 'b', 'c', 'd', 'e']\n",
		},
		{
			name: "splits on Unicode whitespace",
			src:  "print(\"a\\u00a0b\\u2003c\".split())\n",
			want: "['a', 'b', 'c']\n",
		},
		{
			name: "all-whitespace input yields no fields",
			src:  "print(\"   \".split())\n",
			want: "[]\n",
		},
		{
			name: "an explicit separator still keeps empty fields",
			src:  "print(\"a,b,,c\".split(\",\"))\n",
			want: "['a', 'b', '', 'c']\n",
		},
		{
			name: "an explicit space separator does not collapse runs",
			src:  "print(\"a b  c\".split(\" \"))\n",
			want: "['a', 'b', '', 'c']\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, run(t, tt.src))
		})
	}
}

// TestCompileStrFormatSpecs pins str.format()'s support for the format-spec
// mini-language. The arguments used to be stringified before the host saw them,
// so every spec was discarded; they now arrive in their own types and go
// through the same pyFormat f-strings use.
func TestCompileStrFormatSpecs(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{name: "width and precision on a float", src: "print(\"{:>8.2f}\".format(3.14159))\n", want: "    3.14\n"},
		{name: "zero-padded integer at an explicit index", src: "print(\"{0:04d}\".format(7))\n", want: "0007\n"},
		{name: "hex presentation", src: "print(\"{:x}\".format(255))\n", want: "ff\n"},
		{name: "left alignment", src: "print(\"{:<6}|\".format(\"ab\"))\n", want: "ab    |\n"},
		{name: "center alignment", src: "print(\"{:^6}|\".format(\"ab\"))\n", want: "  ab  |\n"},
		{name: "explicit sign", src: "print(\"{:+d}\".format(5))\n", want: "+5\n"},
		{name: "percent presentation", src: "print(\"{:.1%}\".format(0.5))\n", want: "50.0%\n"},
		{name: "escaped braces alongside a field", src: "print(\"{{}} {}\".format(1))\n", want: "{} 1\n"},
		{name: "mixed argument types", src: "print(\"{} {}\".format(\"a\", 2))\n", want: "a 2\n"},
		{name: "no spec still renders the value", src: "print(\"{}\".format(42))\n", want: "42\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, run(t, tt.src))
		})
	}
}

// TestCompileFormatGroupingAndAlternate pins the two spec features the format
// parser accepted and then ignored: ',' / '_' thousands grouping and '#'
// alternate form. Both surfaces share one implementation, so both are covered.
func TestCompileFormatGroupingAndAlternate(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{name: "comma grouping via format", src: "print(\"{:,}\".format(1234567))\n", want: "1,234,567\n"},
		{name: "comma grouping via f-string", src: "n = 1234567\nprint(f\"{n:,}\")\n", want: "1,234,567\n"},
		{name: "underscore grouping", src: "print(\"{:_}\".format(1234567))\n", want: "1_234_567\n"},
		{name: "grouping leaves the fraction alone", src: "print(\"{:,.2f}\".format(1234567.891))\n", want: "1,234,567.89\n"},
		{name: "grouping below one thousand is a no-op", src: "print(\"{:,}\".format(123))\n", want: "123\n"},
		{name: "grouping keeps the sign outside", src: "print(\"{:,}\".format(-1234567))\n", want: "-1,234,567\n"},
		{name: "grouping combines with width", src: "print(\"{:>12,}\".format(1234567))\n", want: "   1,234,567\n"},
		{name: "alternate form on hex", src: "print(\"{:#x}\".format(255))\n", want: "0xff\n"},
		{name: "alternate form on binary", src: "print(\"{:#b}\".format(5))\n", want: "0b101\n"},
		{name: "alternate form on octal", src: "print(\"{:#o}\".format(8))\n", want: "0o10\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, run(t, tt.src))
		})
	}
}
