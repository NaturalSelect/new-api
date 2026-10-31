package opencodefree

import (
	"io"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
)

func newTestRequest(tools ...string) *dto.GeneralOpenAIRequest {
	request := &dto.GeneralOpenAIRequest{
		Model:    "big-pickle",
		Messages: []dto.Message{{Role: "user", Content: "hi"}},
	}
	for _, name := range tools {
		request.Tools = append(request.Tools, dto.ToolCallRequest{
			Type:     "function",
			Function: dto.FunctionRequest{Name: name, Description: "d", Parameters: map[string]any{"type": "object"}},
		})
	}
	return request
}

func TestIsAllowedModel(t *testing.T) {
	require.True(t, IsAllowedModel("big-pickle"))
	require.True(t, IsAllowedModel("glm-5.3-flash-free"))
	require.False(t, IsAllowedModel("gpt-4o"))
	require.False(t, IsAllowedModel("free-model"))
}

func TestSessionIDFormatAndReuse(t *testing.T) {
	id := newSessionID(1_700_000_000_000)
	require.Len(t, id, len("ses_")+12+14)
	require.True(t, strings.HasPrefix(id, "ses_"))

	registry := newSessionRegistry()
	first := registry.get("holder")
	require.Equal(t, first, registry.get("holder"))
	require.NotEqual(t, first, registry.get("other"))
}

func TestBuildUpstreamRequestAddsGateToolAndAliases(t *testing.T) {
	request := newTestRequest("read", "bash")
	plan, err := buildUpstreamRequest(request)
	require.NoError(t, err)

	names := lo.Map(request.Tools, func(tool dto.ToolCallRequest, _ int) string { return tool.Function.Name })
	require.Equal(t, []string{"oc2cpa_read", "oc2cpa_bash", "read"}, names)
	require.Equal(t, "read", plan.aliases["oc2cpa_read"])
	require.Equal(t, "bash", plan.aliases["oc2cpa_bash"])
	require.True(t, lo.FromPtr(request.Stream))
	require.NotNil(t, request.StreamOptions)
	require.NotNil(t, request.MaxTokens)
}

func TestBuildUpstreamRequestDisabledToolsKeepsOnlyGate(t *testing.T) {
	request := newTestRequest("bash")
	request.ToolChoice = "none"
	plan, err := buildUpstreamRequest(request)
	require.NoError(t, err)
	require.True(t, plan.toolsDisabled)
	require.Len(t, request.Tools, 1)
	require.Equal(t, gateToolName, request.Tools[0].Function.Name)
	require.Nil(t, request.ToolChoice)
}

func TestBuildUpstreamRequestRejectsUnknownModel(t *testing.T) {
	request := newTestRequest()
	request.Model = "gpt-4o"
	_, err := buildUpstreamRequest(request)
	require.Error(t, err)
}

func TestRewriteMessagesDropsToolNameAndRenamesCalls(t *testing.T) {
	messages := []dto.Message{
		{Role: "assistant", ToolCalls: []byte(`[{"id":"1","type":"function","function":{"name":"bash","arguments":"{}"}}]`)},
		{Role: "tool", Name: lo.ToPtr("bash"), Content: "ok"},
	}
	require.NoError(t, rewriteMessages(messages, map[string]string{"bash": "oc2cpa_bash"}))
	require.Nil(t, messages[1].Name)
	require.Contains(t, string(messages[0].ToolCalls), "oc2cpa_bash")
}

func TestAliasToolsAvoidsCollision(t *testing.T) {
	tools := newTestRequest("read", "oc2cpa_read").Tools
	_, names := aliasTools(tools)
	require.NotEqual(t, names["read"], names["oc2cpa_read"])
	require.Len(t, names, 2)
}

func sseBody(frames ...string) io.ReadCloser {
	var builder strings.Builder
	for _, frame := range frames {
		builder.WriteString("data: " + frame + "\n\n")
	}
	return io.NopCloser(strings.NewReader(builder.String()))
}

func toolPlan(t *testing.T, tools ...string) *requestPlan {
	plan, err := buildUpstreamRequest(newTestRequest(tools...))
	require.NoError(t, err)
	return plan
}

func readAll(t *testing.T, body io.ReadCloser) string {
	data, err := io.ReadAll(body)
	require.NoError(t, err)
	return string(data)
}

func TestAggregateRestoresToolNameAndNormalizesFinish(t *testing.T) {
	plan := toolPlan(t, "bash")
	body := sseBody(
		`{"id":"c1","created":1,"choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"oc2cpa_ba","arguments":""}}]}}]}`,
		`{"id":"c1","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"name":"sh","arguments":"{\"cmd\":\"ls\"}"}}]}}]}`,
		`{"id":"c1","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`,
		`[DONE]`,
	)
	parser := newFrameParser(body, plan)
	defer parser.close()
	payload, err := aggregateResponse(parser, plan, "big-pickle")
	require.NoError(t, err)

	var result struct {
		Model   string `json:"model"`
		Choices []struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
				ToolCalls []struct {
					ID       string `json:"id"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			TotalTokens int `json:"total_tokens"`
		} `json:"usage"`
	}
	require.NoError(t, common.Unmarshal(payload, &result))
	require.Equal(t, "big-pickle", result.Model)
	require.Equal(t, 5, result.Usage.TotalTokens)
	require.Len(t, result.Choices, 1)
	require.Equal(t, "tool_calls", result.Choices[0].FinishReason)
	require.Len(t, result.Choices[0].Message.ToolCalls, 1)
	call := result.Choices[0].Message.ToolCalls[0]
	require.Equal(t, "call_1", call.ID)
	require.Equal(t, "bash", call.Function.Name)
	require.JSONEq(t, `{"cmd":"ls"}`, call.Function.Arguments)
}

func TestParserRejectsReservedGateToolCall(t *testing.T) {
	plan := toolPlan(t, "bash")
	body := sseBody(
		`{"id":"c1","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"x","function":{"name":"read","arguments":"{\"path\":\"/\"}"}}]}}]}`,
	)
	parser := newFrameParser(body, plan)
	defer parser.close()
	_, err := parser.next()
	require.Error(t, err)
	require.Contains(t, err.Error(), "reserved tool")
}

func TestParserRejectsToolCallWhenToolsDisabled(t *testing.T) {
	request := newTestRequest("bash")
	request.ToolChoice = "none"
	plan, err := buildUpstreamRequest(request)
	require.NoError(t, err)
	body := sseBody(
		`{"id":"c1","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"x","function":{"name":"oc2cpa_bash","arguments":"{}"}}]}}]}`,
	)
	parser := newFrameParser(body, plan)
	defer parser.close()
	_, err = parser.next()
	require.Error(t, err)
	require.Contains(t, err.Error(), "tools are disabled")
}

func TestParserTruncatedStreamIsIncomplete(t *testing.T) {
	plan := toolPlan(t)
	body := sseBody(`{"id":"c1","choices":[{"index":0,"delta":{"content":"hel"}}]}`)
	parser := newFrameParser(body, plan)
	defer parser.close()
	_, err := aggregateResponse(parser, plan, "big-pickle")
	require.Error(t, err)
	require.Contains(t, err.Error(), "Incomplete")
}

func TestParserRawJSONErrorBody(t *testing.T) {
	plan := toolPlan(t)
	body := io.NopCloser(strings.NewReader(`{"error":{"message":"Free usage limit reached"}}`))
	parser := newFrameParser(body, plan)
	defer parser.close()
	_, err := parser.next()
	require.Error(t, err)
	require.Contains(t, err.Error(), "Free usage limit reached")
}

func TestStreamBodyEmitsOpenAIFramesAndDone(t *testing.T) {
	plan := toolPlan(t, "bash")
	upstream := sseBody(
		`{"id":"c1","choices":[{"index":0,"delta":{"role":"assistant"}}]}`,
		`{"id":"c1","choices":[{"index":0,"delta":{"content":"hi","reasoning":"think"}}]}`,
		`{"id":"c1","choices":[{"index":0,"delta":{"tool_calls":[{"id":"call_1","type":"function","function":{"name":"oc2cpa_bash","arguments":"{}"}}]}}]}`,
		`{"id":"c1","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`,
		`[DONE]`,
	)
	body, err := newStreamBody(upstream, plan, "big-pickle")
	require.NoError(t, err)
	defer body.Close()
	out := readAll(t, body)

	require.Contains(t, out, `"model":"big-pickle"`)
	require.Contains(t, out, `"reasoning_content":"think"`)
	require.Contains(t, out, `"name":"bash"`)
	require.NotContains(t, out, "oc2cpa_bash")
	require.Contains(t, out, `"finish_reason":"tool_calls"`)
	require.True(t, strings.HasSuffix(out, "data: [DONE]\n\n"))
}

func TestStreamBodyFailsBeforeContentAsError(t *testing.T) {
	plan := toolPlan(t)
	upstream := sseBody(
		`{"id":"c1","choices":[{"index":0,"delta":{"role":"assistant"}}]}`,
		`{"error":{"message":"boom"}}`,
	)
	_, err := newStreamBody(upstream, plan, "big-pickle")
	require.Error(t, err)
	require.Contains(t, err.Error(), "boom")
}

func TestStreamBodyFailsMidStreamBreaksPipe(t *testing.T) {
	plan := toolPlan(t)
	upstream := sseBody(
		`{"id":"c1","choices":[{"index":0,"delta":{"content":"hi"}}]}`,
		`{"error":{"message":"boom"}}`,
	)
	body, err := newStreamBody(upstream, plan, "big-pickle")
	require.NoError(t, err)
	defer body.Close()
	_, err = io.ReadAll(body)
	require.Error(t, err)
	require.Contains(t, err.Error(), "boom")
}

func TestNormalizeStreamToolCallsAssignsUniqueIndexes(t *testing.T) {
	book := newToolCallBook()
	calls := []any{
		map[string]any{"id": "a", "function": map[string]any{"name": "x"}},
		map[string]any{"id": "b", "function": map[string]any{"name": "y"}},
	}
	out := normalizeStreamToolCalls(calls, book)
	first := asObject(out[0])["index"]
	second := asObject(out[1])["index"]
	require.NotEqual(t, first, second)
}
