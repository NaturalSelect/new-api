package opencode

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newInfo(format types.RelayFormat, mode int) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		TokenId:     7,
		UserId:      3,
		RelayFormat: format,
		RelayMode:   mode,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeOpenCodeGo,
			ChannelBaseUrl:    "https://opencode.ai/inference/go",
			ApiKey:            "oc_sk_test",
			UpstreamModelName: "glm-5.3",
		},
	}
}

func newContext(headers map[string]string) *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Request.Header.Set("Content-Type", "application/json")
	for name, value := range headers {
		c.Request.Header.Set(name, value)
	}
	return c
}

func TestGetRequestURL(t *testing.T) {
	tests := []struct {
		name    string
		format  types.RelayFormat
		mode    int
		baseURL string
		want    string
	}{
		{name: "chat completions", format: types.RelayFormatOpenAI, mode: relayconstant.RelayModeChatCompletions, baseURL: "https://opencode.ai/inference/go", want: "https://opencode.ai/inference/go/openai/v1/chat/completions"},
		{name: "responses", format: types.RelayFormatOpenAIResponses, mode: relayconstant.RelayModeResponses, baseURL: "https://opencode.ai/inference", want: "https://opencode.ai/inference/openai/v1/responses"},
		{name: "claude messages", format: types.RelayFormatClaude, mode: relayconstant.RelayModeUnknown, baseURL: "https://opencode.ai/inference", want: "https://opencode.ai/inference/anthropic/v1/messages"},
		{name: "claude converted to responses", format: types.RelayFormatClaude, mode: relayconstant.RelayModeResponses, baseURL: "https://opencode.ai/inference/go", want: "https://opencode.ai/inference/go/openai/v1/responses"},
		{name: "trailing slash", format: types.RelayFormatOpenAI, mode: relayconstant.RelayModeChatCompletions, baseURL: "https://opencode.ai/inference/go/", want: "https://opencode.ai/inference/go/openai/v1/chat/completions"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := newInfo(tt.format, tt.mode)
			info.ChannelBaseUrl = tt.baseURL
			got, err := (&Adaptor{}).GetRequestURL(info)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestGetRequestURLRejectsUnsupportedMode(t *testing.T) {
	_, err := (&Adaptor{}).GetRequestURL(newInfo(types.RelayFormatOpenAI, relayconstant.RelayModeEmbeddings))
	require.Error(t, err)
}

func TestSetupRequestHeaderSetsAuthAndSession(t *testing.T) {
	info := newInfo(types.RelayFormatOpenAI, relayconstant.RelayModeChatCompletions)
	header := http.Header{}
	require.NoError(t, (&Adaptor{}).SetupRequestHeader(newContext(nil), &header, info))

	require.Equal(t, "Bearer oc_sk_test", header.Get("Authorization"))
	require.Regexp(t, `^ses_[0-9a-f]{26}$`, header.Get("x-opencode-session"))
	require.Empty(t, header.Get("anthropic-version"))
}

func TestSetupRequestHeaderMessagesAddsAnthropicVersion(t *testing.T) {
	info := newInfo(types.RelayFormatClaude, relayconstant.RelayModeUnknown)
	header := http.Header{}
	require.NoError(t, (&Adaptor{}).SetupRequestHeader(newContext(nil), &header, info))
	require.Equal(t, "2023-06-01", header.Get("anthropic-version"))
	require.NotEmpty(t, header.Get("x-opencode-session"))

	header = http.Header{}
	require.NoError(t, (&Adaptor{}).SetupRequestHeader(newContext(map[string]string{"anthropic-version": "2024-01-01"}), &header, info))
	require.Equal(t, "2024-01-01", header.Get("anthropic-version"))
}

func TestSessionIDKeepsClientValue(t *testing.T) {
	info := newInfo(types.RelayFormatOpenAI, relayconstant.RelayModeChatCompletions)
	header := http.Header{}
	require.NoError(t, (&Adaptor{}).SetupRequestHeader(newContext(map[string]string{"x-opencode-session": "ses_client"}), &header, info))
	require.Equal(t, "ses_client", header.Get("x-opencode-session"))
}

func TestSessionIDIsStableForOneConversation(t *testing.T) {
	sessionFor := func(info *relaycommon.RelayInfo, headers map[string]string) string {
		header := http.Header{}
		require.NoError(t, (&Adaptor{}).SetupRequestHeader(newContext(headers), &header, info))
		return header.Get("x-opencode-session")
	}

	withCacheKey := func(key string) *relaycommon.RelayInfo {
		info := newInfo(types.RelayFormatOpenAI, relayconstant.RelayModeChatCompletions)
		info.UpstreamPromptCacheKey = key
		return info
	}
	require.Equal(t, sessionFor(withCacheKey("conv-a"), nil), sessionFor(withCacheKey("conv-a"), nil))
	require.NotEqual(t, sessionFor(withCacheKey("conv-a"), nil), sessionFor(withCacheKey("conv-b"), nil))

	otherUser := withCacheKey("conv-a")
	otherUser.UserId = 4
	require.NotEqual(t, sessionFor(withCacheKey("conv-a"), nil), sessionFor(otherUser, nil))

	fromMetadata := newInfo(types.RelayFormatClaude, relayconstant.RelayModeUnknown)
	fromMetadata.UpstreamMetadataUserID = `{"session_id":"abc"}`
	require.Equal(t, sessionFor(fromMetadata, nil), sessionFor(fromMetadata, nil))

	plain := newInfo(types.RelayFormatOpenAI, relayconstant.RelayModeChatCompletions)
	require.Equal(t, sessionFor(plain, map[string]string{"X-Session-Id": "s1"}), sessionFor(plain, map[string]string{"X-Session-Id": "s1"}))
	require.NotEqual(t, sessionFor(plain, map[string]string{"X-Session-Id": "s1"}), sessionFor(plain, map[string]string{"X-Session-Id": "s2"}))
	require.Equal(t, sessionFor(plain, nil), sessionFor(plain, nil))
}

func TestConvertersPassRequestsThroughUnchanged(t *testing.T) {
	info := newInfo(types.RelayFormatOpenAI, relayconstant.RelayModeChatCompletions)
	adaptor := &Adaptor{}
	c := newContext(nil)

	openAIRequest := &dto.GeneralOpenAIRequest{Model: "glm-5.3"}
	converted, err := adaptor.ConvertOpenAIRequest(c, info, openAIRequest)
	require.NoError(t, err)
	require.Same(t, openAIRequest, converted)

	claudeRequest := &dto.ClaudeRequest{Model: "claude-sonnet-5-5"}
	converted, err = adaptor.ConvertClaudeRequest(c, info, claudeRequest)
	require.NoError(t, err)
	require.Same(t, claudeRequest, converted)

	responsesRequest := dto.OpenAIResponsesRequest{Model: "gpt-6.1-sol"}
	converted, err = adaptor.ConvertOpenAIResponsesRequest(c, info, responsesRequest)
	require.NoError(t, err)
	require.Equal(t, responsesRequest, converted)
}

func TestConvertersRejectNilAndUnsupportedFormats(t *testing.T) {
	info := newInfo(types.RelayFormatOpenAI, relayconstant.RelayModeChatCompletions)
	adaptor := &Adaptor{}
	c := newContext(nil)

	_, err := adaptor.ConvertOpenAIRequest(c, info, nil)
	require.Error(t, err)
	_, err = adaptor.ConvertClaudeRequest(c, info, nil)
	require.Error(t, err)
	_, err = adaptor.ConvertGeminiRequest(c, info, nil)
	require.Error(t, err)
}
