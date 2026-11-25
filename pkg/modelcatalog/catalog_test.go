package modelcatalog

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func loadSample(t *testing.T) *Snapshot {
	t.Helper()
	body, err := os.ReadFile("testdata/openrouter_models_sample.json")
	require.NoError(t, err)
	snap, err := Parse(body)
	require.NoError(t, err)
	return snap
}

func TestSimplifyOpenRouterID(t *testing.T) {
	tests := []struct {
		name     string
		id       string
		expected string
		ok       bool
	}{
		{"claude dot version rewritten to dash", "anthropic/claude-opus-4.5", "claude-opus-4-5", true},
		{"claude dot version in the middle rewritten to dash", "anthropic/claude-3.7-sonnet", "claude-3-7-sonnet", true},
		{"claude id without dot version is unchanged", "anthropic/claude-sonnet-4", "claude-sonnet-4", true},
		{"claude id already dash-separated is unchanged", "anthropic/claude-3-haiku", "claude-3-haiku", true},
		{"claude variant entries are discarded", "anthropic/claude-opus-4.5:thinking", "", false},
		{"non-claude dot version is left untouched", "qwen/qwen3.8-max-0902", "qwen3.8-max-0902", true},
		{"id without provider prefix is left untouched", "claude-opus-4.1", "claude-opus-4-1", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := SimplifyOpenRouterID(tt.id)
			require.Equal(t, tt.ok, ok)
			require.Equal(t, tt.expected, got)
		})
	}
}

func TestParseMapsFieldsAndSkipsVariants(t *testing.T) {
	snap := loadSample(t)
	require.Equal(t, 4, snap.Len())

	claude, ok := snap.Lookup("anthropic/claude-fable-5.2")
	require.True(t, ok)
	require.Equal(t, 1_000_000, claude.ContextLength)
	require.Equal(t, 128_000, claude.MaxCompletionTokens)
	require.Equal(t, "", claude.KnowledgeCutoff)
	require.Equal(t, "Claude", claude.Tokenizer)

	luna, ok := snap.Lookup("openai/gpt-5.6-luna")
	require.True(t, ok)
	require.Equal(t, "2026-02-16", luna.KnowledgeCutoff)

	auto, ok := snap.Lookup("openrouter/auto")
	require.True(t, ok)
	require.Equal(t, 0, auto.MaxCompletionTokens)
}

func TestParseRejectsUnusablePayloads(t *testing.T) {
	for name, body := range map[string]string{
		"malformed json":     `{"data":`,
		"empty list":         `{"data":[]}`,
		"only variants":      `{"data":[{"id":"openai/gpt-5:batch"}]}`,
		"entries without id": `{"data":[{"name":"x"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			snap, err := Parse([]byte(body))
			require.Error(t, err)
			require.Nil(t, snap)
		})
	}
}

func TestParseFallsBackToTopProviderContextLength(t *testing.T) {
	snap, err := Parse([]byte(`{"data":[{"id":"acme/model-x","top_provider":{"context_length":32768}}]}`))
	require.NoError(t, err)
	entry, ok := snap.Lookup("acme/model-x")
	require.True(t, ok)
	require.Equal(t, 32768, entry.ContextLength)
}

func TestLookup(t *testing.T) {
	snap := loadSample(t)

	tests := []struct {
		name   string
		query  string
		wantID string
	}{
		{"full id", "anthropic/claude-fable-5.2", "anthropic/claude-fable-5.2"},
		{"id without provider", "claude-fable-5.2", "anthropic/claude-fable-5.2"},
		{"claude dash form", "claude-fable-5-2", "anthropic/claude-fable-5.2"},
		{"case insensitive with spaces", "  GPT-5.6-Luna ", "openai/gpt-5.6-luna"},
		{"dated snapshot suffix", "gpt-5.6-luna-20260301", "openai/gpt-5.6-luna"},
		{"iso dated suffix", "gpt-5.6-luna-2026-03-01", "openai/gpt-5.6-luna"},
		{"latest suffix", "gpt-5.6-luna-latest", "openai/gpt-5.6-luna"},
		{"unknown model", "", ""},
		{"unrelated model", "totally-unknown-model", ""},
		{"variant name is not matched", "gpt-5.6-luna:batch", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry, ok := snap.Lookup(tt.query)
			if tt.wantID == "" {
				require.False(t, ok)
				return
			}
			require.True(t, ok)
			require.Equal(t, tt.wantID, entry.ID)
		})
	}
}

func TestLookupKeepsFirstEntryOnSimplifiedCollision(t *testing.T) {
	snap, err := Parse([]byte(`{"data":[
		{"id":"first/shared-model","context_length":1000},
		{"id":"second/shared-model","context_length":2000}
	]}`))
	require.NoError(t, err)

	entry, ok := snap.Lookup("shared-model")
	require.True(t, ok)
	require.Equal(t, "first/shared-model", entry.ID)

	entry, ok = snap.Lookup("second/shared-model")
	require.True(t, ok)
	require.Equal(t, 2000, entry.ContextLength)
}

func TestNilSnapshotLookup(t *testing.T) {
	var snap *Snapshot
	_, ok := snap.Lookup("anything")
	require.False(t, ok)
	require.Equal(t, 0, snap.Len())
}

func TestSetClearAndGlobalLookup(t *testing.T) {
	t.Cleanup(Clear)

	Clear()
	_, ok := Lookup("openai/gpt-5.6-luna")
	require.False(t, ok)
	require.Nil(t, Current())

	Set(loadSample(t))
	entry, ok := Lookup("openai/gpt-5.6-luna")
	require.True(t, ok)
	require.Equal(t, "openai/gpt-5.6-luna", entry.ID)

	Clear()
	_, ok = Lookup("openai/gpt-5.6-luna")
	require.False(t, ok)
}

func TestReleaseDate(t *testing.T) {
	require.Equal(t, "", (&Entry{}).ReleaseDate())
	require.Equal(t, "2026-11-24", (&Entry{Created: 1795562304}).ReleaseDate())
}

func TestFrontendModalitiesFiltersAndDeduplicates(t *testing.T) {
	entry := &Entry{
		InputModalities:  []string{"text", "Image", "text", "embeddings", " file "},
		OutputModalities: []string{"embeddings"},
	}
	in, out := entry.FrontendModalities()
	require.Equal(t, []string{"text", "image", "file"}, in)
	require.Empty(t, out)
}

func TestCapabilities(t *testing.T) {
	snap := loadSample(t)

	claude, _ := snap.Lookup("anthropic/claude-fable-5.2")
	require.Equal(t,
		[]string{"function_calling", "tools", "json_mode", "structured_output", "reasoning", "vision"},
		claude.Capabilities(),
	)

	auto, _ := snap.Lookup("openrouter/auto")
	require.Contains(t, auto.Capabilities(), "web_search")

	require.Empty(t, (&Entry{}).Capabilities())
	require.Equal(t, []string{"vision"}, (&Entry{InputModalities: []string{"text", "image"}}).Capabilities())
}
