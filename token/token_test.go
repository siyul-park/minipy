package token

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLookup(t *testing.T) {
	t.Run("reserved word resolves to keyword", func(t *testing.T) {
		require.Equal(t, IF, Lookup("if"))
		require.Equal(t, TRUE, Lookup("True"))
		require.Equal(t, RETURN, Lookup("return"))
	})

	t.Run("plain identifier resolves to NAME", func(t *testing.T) {
		require.Equal(t, NAME, Lookup("x"))
		require.Equal(t, NAME, Lookup("print"))
	})
}

func TestType_IsKeyword(t *testing.T) {
	require.True(t, AND.IsKeyword())
	require.True(t, AWAIT.IsKeyword())
	require.False(t, NAME.IsKeyword())
	require.False(t, PLUS.IsKeyword())
}

func TestType_String(t *testing.T) {
	require.Equal(t, "NAME", NAME.String())
	require.Equal(t, "+", PLUS.String())
	require.Equal(t, "and", AND.String())
	require.Equal(t, "->", ARROW.String())
}

func TestPos_String(t *testing.T) {
	require.Equal(t, "3:7", Pos{Line: 3, Column: 7}.String())
}

func TestToken_String(t *testing.T) {
	require.Equal(t, "NAME(x)", Token{Type: NAME, Literal: "x"}.String())
	require.Equal(t, "+", Token{Type: PLUS, Literal: "+"}.String())
}

func TestErrorList(t *testing.T) {
	t.Run("empty list returns nil error", func(t *testing.T) {
		var l ErrorList
		require.NoError(t, l.Err())
	})

	t.Run("collects and renders diagnostics", func(t *testing.T) {
		var l ErrorList
		l.Add(Pos{Line: 1, Column: 1}, TypeMismatch, "want %s", "int")
		l.Add(Pos{Line: 2, Column: 4}, UndefinedName, "x")

		err := l.Err()
		require.Error(t, err)
		require.Equal(t, "TypeError: want int (line 1, column 1)\nNameError: x (line 2, column 4)", err.Error())
	})

	t.Run("codes map to python exception names", func(t *testing.T) {
		require.Equal(t, "TypeError", TypeMismatch.Python())
		require.Equal(t, "NameError", UndefinedName.Python())
		require.Equal(t, "SyntaxError", UnsupportedFeature.Python())
		require.Equal(t, "ValueError", IntOverflow.Python())
	})
}
