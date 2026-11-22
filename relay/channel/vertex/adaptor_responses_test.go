package vertex

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAdaptorConvertOpenAIResponsesRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("claude mode returns vertex claude request with anthropic_version", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "claude-sonnet-4-5"}}
		var req dto.OpenAIResponsesRequest
		require.NoError(t, common.UnmarshalJsonStr(`{"model":"claude-sonnet-4-5","input":"hi"}`, &req))

		adaptor := &Adaptor{RequestMode: RequestModeClaude}
		out, err := adaptor.ConvertOpenAIResponsesRequest(c, info, req)
		require.NoError(t, err)

		vertexReq, ok := out.(*VertexAIClaudeRequest)
		require.True(t, ok)
		require.Equal(t, anthropicVersion, vertexReq.AnthropicVersion)
		require.Equal(t, "claude-sonnet-4-5", c.GetString("request_model"))
		require.Equal(t, "claude-sonnet-4-5", info.UpstreamModelName)
	})

	t.Run("non-claude mode returns not implemented", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gemini-2.5-pro"}}
		var req dto.OpenAIResponsesRequest
		require.NoError(t, common.UnmarshalJsonStr(`{"model":"gemini-2.5-pro","input":"hi"}`, &req))

		adaptor := &Adaptor{RequestMode: RequestModeGemini}
		_, err := adaptor.ConvertOpenAIResponsesRequest(c, info, req)
		require.Error(t, err)
	})
}
