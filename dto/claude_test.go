package dto

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestClaudeTokenCountMetaToolResultImage(t *testing.T) {
	const image = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jRZkAAAAASUVORK5CYII="
	raw := []byte(`{
		"model":"claude-opus-4-6","max_tokens":4096,
		"system":[{"type":"text","text":"system instructions"}],
		"messages":[
			{"role":"assistant","content":[{"type":"tool_use","id":"tool_1","name":"Read","input":{"path":"image.png"}}]},
			{"role":"user","content":[{"type":"tool_result","tool_use_id":"tool_1","content":[
				{"type":"text","text":"screenshot description"},
				{"type":"image","source":{"type":"base64","media_type":"image/png","data":"` + image + `"}},
				{"type":"image","source":{"type":"url","url":"https://example.com/image.png"}}
			]}]}
		]
	}`)
	var request ClaudeRequest
	require.NoError(t, common.Unmarshal(raw, &request))
	before, err := common.Marshal(request)
	require.NoError(t, err)
	meta := request.GetTokenCountMeta()
	require.Contains(t, meta.CombineText, "system instructions")
	require.Contains(t, meta.CombineText, "screenshot description")
	require.Contains(t, meta.CombineText, "Read")
	require.Contains(t, meta.CombineText, `{"path":"image.png"}`)
	require.NotContains(t, meta.CombineText, image)
	require.NotContains(t, meta.CombineText, "https://example.com/image.png")
	require.Len(t, meta.Files, 2)
	require.Equal(t, types.FileTypeImage, meta.Files[0].FileType)
	require.Equal(t, image, meta.Files[0].Source.GetRawData())
	require.True(t, meta.Files[1].Source.IsURL())
	require.Equal(t, 2, meta.MessagesCount)
	require.Equal(t, 4096, meta.MaxTokens)
	after, err := common.Marshal(request)
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestClaudeTokenCountMetaToolResultText(t *testing.T) {
	for _, content := range []any{"tool output", []ClaudeMediaMessage{{Type: "text", Text: common.GetPointer("tool output")}}} {
		request := ClaudeRequest{Messages: []ClaudeMessage{{Role: "user", Content: []ClaudeMediaMessage{{Type: "tool_result", Content: content}}}}}
		meta := request.GetTokenCountMeta()
		require.Contains(t, meta.CombineText, "tool output")
		require.Empty(t, meta.Files)
	}
}
