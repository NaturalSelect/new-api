package service

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPoeThoughtSignatureStoreAndLoad(t *testing.T) {
	StorePoeThoughtSignature(9001, "Gemini-4-Hydrogen", "call_store_load", "SIG")

	require.Equal(t, "SIG", LoadPoeThoughtSignature(9001, "gemini-4-hydrogen", "call_store_load"), "model names compare case-insensitively")
	require.Empty(t, LoadPoeThoughtSignature(9002, "gemini-4-hydrogen", "call_store_load"), "scoped to the user")
	require.Empty(t, LoadPoeThoughtSignature(9001, "gemini-3.8-flash", "call_store_load"), "scoped to the model")
	require.Empty(t, LoadPoeThoughtSignature(9001, "gemini-4-hydrogen", "call_other"), "scoped to the call id")
}

func TestPoeThoughtSignatureIgnoresEmptyInput(t *testing.T) {
	StorePoeThoughtSignature(9003, "m", "", "SIG")
	StorePoeThoughtSignature(9003, "m", "call_empty_sig", "")

	require.Empty(t, LoadPoeThoughtSignature(9003, "m", ""))
	require.Empty(t, LoadPoeThoughtSignature(9003, "m", "call_empty_sig"))
}

func TestPoeThoughtSignatureKeyIsBoundedForHugeCallIDs(t *testing.T) {
	huge := strings.Repeat("x", 1<<20)
	StorePoeThoughtSignature(9004, "m", huge, "SIG")

	require.Equal(t, "SIG", LoadPoeThoughtSignature(9004, "m", huge))
	require.Less(t, len(poeThoughtSignatureKey(9004, "m", huge)), 100)
}
