package aws

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestDoAwsClientRequest_AppliesRuntimeHeaderOverrideToAnthropicBeta(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	info := &relaycommon.RelayInfo{
		OriginModelName:           "claude-3-5-sonnet-20240620",
		IsStream:                  false,
		UseRuntimeHeadersOverride: true,
		RuntimeHeadersOverride: map[string]any{
			"anthropic-beta": "computer-use-2025-01-24",
		},
		ChannelMeta: &relaycommon.ChannelMeta{
			ApiKey:            "access-key|secret-key|us-east-1",
			UpstreamModelName: "claude-3-5-sonnet-20240620",
		},
	}

	requestBody := bytes.NewBufferString(`{"messages":[{"role":"user","content":"hello"}],"max_tokens":128}`)
	adaptor := &Adaptor{}

	_, err := doAwsClientRequest(ctx, info, adaptor, requestBody)
	require.NoError(t, err)

	awsReq, ok := adaptor.AwsReq.(*bedrockruntime.InvokeModelInput)
	require.True(t, ok)

	var payload map[string]any
	require.NoError(t, common.Unmarshal(awsReq.Body, &payload))

	anthropicBeta, exists := payload["anthropic_beta"]
	require.True(t, exists)

	values, ok := anthropicBeta.([]any)
	require.True(t, ok)
	require.Equal(t, []any{"computer-use-2025-01-24"}, values)
}

func TestAdaptorConvertOpenAIResponsesRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("claude model converts to claude request", func(t *testing.T) {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "claude-3-5-sonnet-20240620"}}
		var req dto.OpenAIResponsesRequest
		require.NoError(t, common.UnmarshalJsonStr(`{"model":"claude-3-5-sonnet-20240620","input":"hi"}`, &req))

		adaptor := &Adaptor{}
		out, err := adaptor.ConvertOpenAIResponsesRequest(ctx, info, req)
		require.NoError(t, err)
		claudeReq, ok := out.(*dto.ClaudeRequest)
		require.True(t, ok)
		require.Equal(t, "claude-3-5-sonnet-20240620", claudeReq.Model)
		require.Equal(t, "claude-3-5-sonnet-20240620", info.UpstreamModelName)
	})

	t.Run("nova model returns unsupported error", func(t *testing.T) {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "amazon.nova-pro-v1:0"}}
		var req dto.OpenAIResponsesRequest
		require.NoError(t, common.UnmarshalJsonStr(`{"model":"amazon.nova-pro-v1:0","input":"hi"}`, &req))

		adaptor := &Adaptor{}
		_, err := adaptor.ConvertOpenAIResponsesRequest(ctx, info, req)
		require.Error(t, err)
	})
}
