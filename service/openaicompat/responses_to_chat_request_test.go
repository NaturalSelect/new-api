package openaicompat

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
)

func parseResponsesRequest(t *testing.T, body string) *dto.OpenAIResponsesRequest {
	t.Helper()
	var req dto.OpenAIResponsesRequest
	require.NoError(t, common.UnmarshalJsonStr(body, &req))
	return &req
}

func TestResponsesRequestToChatStringInputWithInstructions(t *testing.T) {
	out, err := ResponsesRequestToChatCompletionsRequest(parseResponsesRequest(t, `{
		"model": "gemini-2.5-flash",
		"instructions": "be brief",
		"input": "hello",
		"stream": true,
		"temperature": 0,
		"max_output_tokens": 256,
		"reasoning": {"effort": "low"}
	}`))
	require.NoError(t, err)

	require.Len(t, out.Messages, 2)
	require.Equal(t, "system", out.Messages[0].Role)
	require.Equal(t, "be brief", out.Messages[0].StringContent())
	require.Equal(t, "user", out.Messages[1].Role)
	require.Equal(t, "hello", out.Messages[1].ParseContent()[0].Text)

	require.NotNil(t, out.Stream)
	require.True(t, *out.Stream)
	require.NotNil(t, out.Temperature)
	require.Equal(t, 0.0, *out.Temperature)
	require.Equal(t, uint(256), out.GetMaxTokens())
	require.Equal(t, "low", out.ReasoningEffort)
}

func TestResponsesRequestToChatAgentConversation(t *testing.T) {
	out, err := ResponsesRequestToChatCompletionsRequest(parseResponsesRequest(t, `{
		"model": "gemini-2.5-flash",
		"input": [
			{"type": "message", "role": "developer", "content": [{"type": "input_text", "text": "rules"}]},
			{"type": "message", "role": "user", "content": [
				{"type": "input_text", "text": "what is in this image?"},
				{"type": "input_image", "image_url": "https://example.com/a.png", "detail": "low"}
			]},
			{"type": "reasoning", "id": "rs_1", "summary": []},
			{"type": "message", "role": "assistant", "content": [{"type": "output_text", "text": "let me check"}]},
			{"type": "function_call", "call_id": "call_a", "name": "shell", "arguments": "{\"cmd\":\"ls\"}"},
			{"type": "function_call", "call_id": "call_b", "name": "shell", "arguments": "{\"cmd\":\"pwd\"}"},
			{"type": "function_call_output", "call_id": "call_a", "output": "a.txt"},
			{"type": "function_call_output", "call_id": "call_b", "output": [{"type": "input_text", "text": "/tmp"}]}
		],
		"tools": [
			{"type": "function", "name": "shell", "description": "run a command", "parameters": {"type": "object", "properties": {"cmd": {"type": "string"}}}},
			{"type": "web_search"}
		],
		"tool_choice": {"type": "function", "name": "shell"},
		"parallel_tool_calls": false,
		"text": {"format": {"type": "json_schema", "name": "answer", "schema": {"type": "object"}, "strict": true}}
	}`))
	require.NoError(t, err)

	require.Len(t, out.Messages, 5)
	require.Equal(t, "system", out.Messages[0].Role)
	require.Equal(t, "rules", out.Messages[0].StringContent())

	user := out.Messages[1].ParseContent()
	require.Len(t, user, 2)
	require.Equal(t, dto.ContentTypeImageURL, user[1].Type)
	require.Equal(t, "https://example.com/a.png", user[1].GetImageMedia().Url)

	// Assistant text and the calls that follow it stay in one model turn.
	assistant := out.Messages[2]
	require.Equal(t, "assistant", assistant.Role)
	require.Equal(t, "let me check", assistant.ParseContent()[0].Text)
	calls := assistant.ParseToolCalls()
	require.Len(t, calls, 2)
	require.Equal(t, "call_a", calls[0].ID)
	require.Equal(t, "shell", calls[0].Function.Name)
	require.JSONEq(t, `{"cmd":"ls"}`, calls[0].Function.Arguments)
	require.Equal(t, "call_b", calls[1].ID)

	require.Equal(t, "tool", out.Messages[3].Role)
	require.Equal(t, "call_a", out.Messages[3].ToolCallId)
	require.Equal(t, "a.txt", out.Messages[3].StringContent())
	require.Equal(t, "shell", *out.Messages[3].Name)
	require.Equal(t, "/tmp", out.Messages[4].StringContent())

	// Hosted tools have no chat equivalent and are dropped.
	require.Len(t, out.Tools, 1)
	require.Equal(t, "shell", out.Tools[0].Function.Name)
	require.Equal(t, map[string]any{"type": "function", "function": map[string]any{"name": "shell"}}, out.ToolChoice)
	require.NotNil(t, out.ParallelTooCalls)
	require.False(t, *out.ParallelTooCalls)

	require.NotNil(t, out.ResponseFormat)
	require.Equal(t, "json_schema", out.ResponseFormat.Type)
	var schema dto.FormatJsonSchema
	require.NoError(t, common.Unmarshal(out.ResponseFormat.JsonSchema, &schema))
	require.Equal(t, "answer", schema.Name)
	require.Equal(t, map[string]any{"type": "object"}, schema.Schema)
}

func TestResponsesRequestToChatToolOutputWithoutCallBecomesUserText(t *testing.T) {
	out, err := ResponsesRequestToChatCompletionsRequest(parseResponsesRequest(t, `{
		"model": "gemini-2.5-flash",
		"input": [{"type": "function_call_output", "call_id": "call_missing", "output": "42"}]
	}`))
	require.NoError(t, err)

	require.Len(t, out.Messages, 1)
	require.Equal(t, "user", out.Messages[0].Role)
	require.Contains(t, out.Messages[0].ParseContent()[0].Text, "42")
}

func TestResponsesRequestToChatCustomTool(t *testing.T) {
	out, err := ResponsesRequestToChatCompletionsRequest(parseResponsesRequest(t, `{
		"model": "gemini-2.5-flash",
		"input": [
			{"type": "message", "role": "user", "content": "patch it"},
			{"type": "custom_tool_call", "call_id": "call_p", "name": "apply_patch", "input": "*** Begin Patch"},
			{"type": "custom_tool_call_output", "call_id": "call_p", "output": "done"}
		],
		"tools": [{"type": "custom", "name": "apply_patch", "description": "patch files", "format": {"type": "grammar", "syntax": "lark", "definition": "start: /.+/"}}],
		"tool_choice": "required"
	}`))
	require.NoError(t, err)

	require.Len(t, out.Tools, 1)
	params, ok := out.Tools[0].Function.Parameters.(map[string]any)
	require.True(t, ok)
	require.Equal(t, []string{CustomToolInputKey}, params["required"])
	require.Contains(t, params["properties"].(map[string]any)[CustomToolInputKey].(map[string]any)["description"], "lark")
	require.Equal(t, "required", out.ToolChoice)

	require.Len(t, out.Messages, 3)
	calls := out.Messages[1].ParseToolCalls()
	require.Len(t, calls, 1)
	require.JSONEq(t, `{"input":"*** Begin Patch"}`, calls[0].Function.Arguments)
	require.Equal(t, "tool", out.Messages[2].Role)
	require.Equal(t, "done", out.Messages[2].StringContent())
}

func TestResponsesRequestToChatRejectsStoredState(t *testing.T) {
	for _, body := range []string{
		`{"model": "m", "input": "hi", "previous_response_id": "resp_1"}`,
		`{"model": "m", "input": "hi", "conversation": "conv_1"}`,
		`{"model": "m"}`,
	} {
		_, err := ResponsesRequestToChatCompletionsRequest(parseResponsesRequest(t, body))
		require.Error(t, err, body)
	}
}

func TestResponsesRequestToChatClaudeThinkingBlocks(t *testing.T) {
	block1 := ClaudeThinkingBlock{Model: "claude-sonnet-4-5", Type: "thinking", Thinking: "step 1", Signature: "sig1"}
	block2 := ClaudeThinkingBlock{Model: "claude-sonnet-4-5", Type: "redacted_thinking", Data: "enc2"}
	block1JSON, err := common.Marshal(block1)
	require.NoError(t, err)
	block2JSON, err := common.Marshal(block2)
	require.NoError(t, err)

	decoder := func(encrypted string) (string, ReasoningSignatureKind) {
		switch encrypted {
		case "enc_1":
			return string(block1JSON), ReasoningSignatureForBlock
		case "enc_2":
			return string(block2JSON), ReasoningSignatureForBlock
		default:
			return "", ReasoningSignatureNone
		}
	}

	out, err := ResponsesRequestToChatCompletionsRequestWithSignatures(parseResponsesRequest(t, `{
		"model": "claude-sonnet-4-5",
		"input": [
			{"type": "message", "role": "user", "content": "hi"},
			{"type": "reasoning", "id": "rs_ignored", "encrypted_content": "unknown"},
			{"type": "reasoning", "id": "rs_1", "encrypted_content": "enc_1"},
			{"type": "reasoning", "id": "rs_2", "encrypted_content": "enc_2"},
			{"type": "message", "role": "assistant", "content": [{"type": "output_text", "text": "calling tool"}]},
			{"type": "function_call", "call_id": "call_1", "name": "lookup", "arguments": "{\"q\":\"x\"}"},
			{"type": "function_call_output", "call_id": "call_1", "output": "ok"},
			{"type": "reasoning", "id": "rs_orphan", "encrypted_content": "enc_1"},
			{"type": "message", "role": "user", "content": "next turn"},
			{"type": "message", "role": "assistant", "content": [{"type": "output_text", "text": "fresh"}]}
		]
	}`), decoder)
	require.NoError(t, err)

	require.Len(t, out.Messages, 5)
	assistant := out.Messages[1]
	require.Equal(t, "assistant", assistant.Role)
	require.Equal(t, []ClaudeThinkingBlock{block1, block2}, ClaudeThinkingBlocksFromExtraContent(assistant.ExtraContent))
	require.Len(t, assistant.ParseToolCalls(), 1)
	require.Equal(t, "call_1", assistant.ParseToolCalls()[0].ID)

	// Reasoning separated by a user message must not leak onto the following assistant message.
	laterAssistant := out.Messages[4]
	require.Equal(t, "assistant", laterAssistant.Role)
	require.Empty(t, ClaudeThinkingBlocksFromExtraContent(laterAssistant.ExtraContent))
}
