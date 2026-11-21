package gemini

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

const (
	responsesItemReasoning  = "reasoning"
	responsesItemMessage    = "message"
	responsesItemFunction   = "function_call"
	responsesItemCustomTool = "custom_tool_call"
)

type geminiResponsesItem struct {
	kind        string
	id          string
	outputIndex int
	callID      string
	name        string
	// NOTE: encrypted is the replayable encrypted_content of a reasoning item, empty when the client did not ask for it.
	encrypted string
	// NOTE: text holds the reasoning summary, message text, function arguments or custom tool input, depending on kind.
	text strings.Builder
}

func (it *geminiResponsesItem) toMap(done bool) map[string]any {
	status := "in_progress"
	if done {
		status = "completed"
	}
	text := it.text.String()
	switch it.kind {
	case responsesItemReasoning:
		summary := []any{}
		if done && text != "" {
			summary = append(summary, map[string]any{"type": "summary_text", "text": text})
		}
		obj := map[string]any{"type": it.kind, "id": it.id, "summary": summary}
		if done && it.encrypted != "" {
			obj["encrypted_content"] = it.encrypted
		}
		return obj
	case responsesItemMessage:
		content := []any{}
		if done {
			content = append(content, responsesOutputTextPart(text))
		}
		return map[string]any{"type": it.kind, "id": it.id, "status": status, "role": "assistant", "content": content}
	case responsesItemCustomTool:
		return map[string]any{"type": it.kind, "id": it.id, "status": status, "call_id": it.callID, "name": it.name, "input": text}
	default:
		return map[string]any{"type": it.kind, "id": it.id, "status": status, "call_id": it.callID, "name": it.name, "arguments": text}
	}
}

func responsesOutputTextPart(text string) map[string]any {
	return map[string]any{"type": "output_text", "text": text, "annotations": []any{}}
}

// NOTE: geminiResponsesTranscoder builds one Responses API response from Gemini chunks and, in stream mode, writes the SSE events as they arrive.
type geminiResponsesTranscoder struct {
	c       *gin.Context
	info    *relaycommon.RelayInfo
	stream  bool
	request *dto.OpenAIResponsesRequest

	respID      string
	createdAt   int64
	customTools map[string]struct{}

	encryptReasoning bool

	seq          int
	started      bool
	items        []*geminiResponsesItem
	current      *geminiResponsesItem
	finishReason string
	blocked      bool
}

func newGeminiResponsesTranscoder(c *gin.Context, info *relaycommon.RelayInfo, stream bool) *geminiResponsesTranscoder {
	t := &geminiResponsesTranscoder{
		c:           c,
		info:        info,
		stream:      stream,
		respID:      "resp_" + strings.TrimPrefix(helper.GetResponseID(c), "chatcmpl-"),
		createdAt:   common.GetTimestamp(),
		customTools: make(map[string]struct{}),
	}
	t.request, _ = info.Request.(*dto.OpenAIResponsesRequest)
	t.encryptReasoning = responsesIncludesEncryptedReasoning(t.request)
	if t.request != nil {
		for _, tool := range t.request.GetToolsMap() {
			if tool["type"] != "custom" {
				continue
			}
			if name, _ := tool["name"].(string); name != "" {
				t.customTools[name] = struct{}{}
			}
		}
	}
	return t
}

func (t *geminiResponsesTranscoder) emit(eventType string, fields map[string]any) {
	if !t.stream {
		return
	}
	fields["type"] = eventType
	fields["sequence_number"] = t.seq
	t.seq++
	data, err := common.Marshal(fields)
	if err != nil {
		logger.LogError(t.c, "failed to marshal responses stream event: "+err.Error())
		return
	}
	helper.ResponseChunkData(t.c, dto.ResponsesStreamResponse{Type: eventType}, string(data))
	t.info.SendResponseCount++
}

func (t *geminiResponsesTranscoder) start() {
	if t.started {
		return
	}
	t.started = true
	t.emit("response.created", map[string]any{"response": t.responseObject("in_progress", "", nil, []any{})})
	t.emit("response.in_progress", map[string]any{"response": t.responseObject("in_progress", "", nil, []any{})})
}

func (t *geminiResponsesTranscoder) consume(gr *dto.GeminiChatResponse) {
	t.start()
	if len(gr.Candidates) == 0 {
		if gr.PromptFeedback != nil && gr.PromptFeedback.BlockReason != nil {
			t.blocked = true
		}
		return
	}
	candidate := &gr.Candidates[0]
	for i := range candidate.Content.Parts {
		t.addPart(&candidate.Content.Parts[i])
	}
	if candidate.FinishReason != nil && *candidate.FinishReason != "" {
		t.finishReason = *candidate.FinishReason
	}
}

func (t *geminiResponsesTranscoder) addPart(part *dto.GeminiPart) {
	signature := geminiPartSignature(part)
	switch {
	case part.FunctionCall != nil:
		t.addToolCall(part.FunctionCall, signature)
	case part.Thought:
		if part.Text != "" {
			t.appendText(responsesItemReasoning, part.Text)
		}
	default:
		if text := geminiPartVisibleText(part); text != "" {
			if part.Text == "" && t.current != nil && t.current.kind == responsesItemMessage {
				if existing := t.current.text.String(); existing != "" && !strings.HasSuffix(existing, "\n") {
					text = "\n" + text
				}
			}
			t.appendText(responsesItemMessage, text)
		}
		t.carryMessageSignature(signature)
	}
}

func geminiPartVisibleText(part *dto.GeminiPart) string {
	switch {
	case part.InlineData != nil:
		prefix := "[media]"
		if strings.HasPrefix(part.InlineData.MimeType, "image") {
			prefix = "![image]"
		}
		return prefix + "(data:" + part.InlineData.MimeType + ";base64," + part.InlineData.Data + ")"
	case part.ExecutableCode != nil:
		return "```" + part.ExecutableCode.Language + "\n" + part.ExecutableCode.Code + "\n```"
	case part.CodeExecutionResult != nil:
		return "```output\n" + part.CodeExecutionResult.Output + "\n```"
	default:
		return part.Text
	}
}

func (t *geminiResponsesTranscoder) appendText(kind, delta string) {
	if t.current == nil || t.current.kind != kind {
		t.closeCurrent()
		t.current = t.openItem(kind, "", "")
	}
	it := t.current
	it.text.WriteString(delta)
	eventType := "response.output_text.delta"
	fields := map[string]any{"item_id": it.id, "output_index": it.outputIndex, "delta": delta}
	if kind == responsesItemReasoning {
		eventType = "response.reasoning_summary_text.delta"
		fields["summary_index"] = 0
	} else {
		fields["content_index"] = 0
		fields["logprobs"] = []any{}
	}
	t.emit(eventType, fields)
}

// NOTE: Gemini signs the first function call of a step, so its signature rides on the reasoning item placed in front of that call.
func (t *geminiResponsesTranscoder) carrySignature(signature string) {
	if signature == "" || !t.encryptReasoning {
		return
	}
	encrypted := encodeEncryptedReasoning(callSignaturePrefix, t.info.UpstreamModelName, signature)
	if t.current != nil && t.current.kind == responsesItemReasoning {
		t.current.encrypted = encrypted
		return
	}
	t.closeCurrent()
	it := t.newItem(responsesItemReasoning, "", "")
	it.encrypted = encrypted
	t.closeItem(it)
}

// NOTE: Gemini returns the signature of a text or image part with that part, often in the last chunk, so it rides on a reasoning item placed right behind the message it belongs to.
// A signed part ends its message item, so a message is split in two when more text follows.
func (t *geminiResponsesTranscoder) carryMessageSignature(signature string) {
	if signature == "" || !t.encryptReasoning || len(t.items) == 0 || t.items[len(t.items)-1].kind != responsesItemMessage {
		return
	}
	t.closeCurrent()
	it := t.newItem(responsesItemReasoning, "", "")
	it.encrypted = encodeEncryptedReasoning(messageSignaturePrefix, t.info.UpstreamModelName, signature)
	t.closeItem(it)
}

func (t *geminiResponsesTranscoder) addToolCall(call *dto.FunctionCall, signature string) {
	t.carrySignature(signature)
	t.closeCurrent()

	_, isCustom := t.customTools[call.FunctionName]
	kind := responsesItemFunction
	var text string
	if isCustom {
		kind = responsesItemCustomTool
		text = geminiCustomToolInput(call.Arguments)
	} else {
		text = "{}"
		if call.Arguments != nil {
			if b, err := common.Marshal(call.Arguments); err == nil && string(b) != "null" {
				text = string(b)
			}
		}
	}

	it := t.openItem(kind, "call_"+common.GetUUID(), call.FunctionName)
	it.text.WriteString(text)
	if text != "" {
		eventType := "response.function_call_arguments.delta"
		if isCustom {
			eventType = "response.custom_tool_call_input.delta"
		}
		t.emit(eventType, map[string]any{"item_id": it.id, "output_index": it.outputIndex, "delta": text})
	}
	t.closeItem(it)
}

func geminiCustomToolInput(args any) string {
	if m, ok := args.(map[string]any); ok {
		if input, ok := m["input"].(string); ok {
			return input
		}
	}
	if args == nil {
		return ""
	}
	b, err := common.Marshal(args)
	if err != nil {
		return ""
	}
	return string(b)
}

func (t *geminiResponsesTranscoder) newItem(kind, callID, name string) *geminiResponsesItem {
	prefix := "msg_"
	switch kind {
	case responsesItemReasoning:
		prefix = "rs_"
	case responsesItemFunction:
		prefix = "fc_"
	case responsesItemCustomTool:
		prefix = "ctc_"
	}
	it := &geminiResponsesItem{
		kind:        kind,
		id:          prefix + common.GetUUID(),
		outputIndex: len(t.items),
		callID:      callID,
		name:        name,
	}
	t.items = append(t.items, it)

	t.emit("response.output_item.added", map[string]any{"output_index": it.outputIndex, "item": it.toMap(false)})
	return it
}

func (t *geminiResponsesTranscoder) openItem(kind, callID, name string) *geminiResponsesItem {
	it := t.newItem(kind, callID, name)
	switch kind {
	case responsesItemReasoning:
		t.emit("response.reasoning_summary_part.added", map[string]any{
			"item_id": it.id, "output_index": it.outputIndex, "summary_index": 0,
			"part": map[string]any{"type": "summary_text", "text": ""},
		})
	case responsesItemMessage:
		t.emit("response.content_part.added", map[string]any{
			"item_id": it.id, "output_index": it.outputIndex, "content_index": 0,
			"part": responsesOutputTextPart(""),
		})
	}
	return it
}

func (t *geminiResponsesTranscoder) closeCurrent() {
	it := t.current
	if it == nil {
		return
	}
	t.current = nil
	t.closeItem(it)
}

func (t *geminiResponsesTranscoder) closeItem(it *geminiResponsesItem) {
	text := it.text.String()
	switch it.kind {
	case responsesItemReasoning:
		// NOTE: A reasoning item that only carries a signature has no summary part to close.
		if text == "" {
			break
		}
		t.emit("response.reasoning_summary_text.done", map[string]any{
			"item_id": it.id, "output_index": it.outputIndex, "summary_index": 0, "text": text,
		})
		t.emit("response.reasoning_summary_part.done", map[string]any{
			"item_id": it.id, "output_index": it.outputIndex, "summary_index": 0,
			"part": map[string]any{"type": "summary_text", "text": text},
		})
	case responsesItemMessage:
		t.emit("response.output_text.done", map[string]any{
			"item_id": it.id, "output_index": it.outputIndex, "content_index": 0, "text": text, "logprobs": []any{},
		})
		t.emit("response.content_part.done", map[string]any{
			"item_id": it.id, "output_index": it.outputIndex, "content_index": 0,
			"part": responsesOutputTextPart(text),
		})
	case responsesItemFunction:
		t.emit("response.function_call_arguments.done", map[string]any{
			"item_id": it.id, "output_index": it.outputIndex, "name": it.name, "arguments": text,
		})
	case responsesItemCustomTool:
		t.emit("response.custom_tool_call_input.done", map[string]any{
			"item_id": it.id, "output_index": it.outputIndex, "input": text,
		})
	}
	t.emit("response.output_item.done", map[string]any{"output_index": it.outputIndex, "item": it.toMap(true)})
}

func (t *geminiResponsesTranscoder) terminalState() (status, incompleteReason string) {
	if t.blocked {
		return "incomplete", "content_filter"
	}
	return geminiResponsesTerminalState(t.finishReason)
}

func geminiResponsesTerminalState(finishReason string) (status, incompleteReason string) {
	switch strings.ToUpper(strings.TrimSpace(finishReason)) {
	case "MAX_TOKENS":
		return "incomplete", "max_output_tokens"
	case "SAFETY", "RECITATION", "BLOCKLIST", "PROHIBITED_CONTENT", "SPII", "IMAGE_SAFETY", "MALFORMED_RESPONSE", "MALFORMED_FUNCTION_CALL":
		return "incomplete", "content_filter"
	default:
		return "completed", ""
	}
}

// NOTE: complete closes any open item and returns the terminal event type with the final response object.
func (t *geminiResponsesTranscoder) complete(usage *dto.Usage) (string, map[string]any) {
	t.closeCurrent()
	status, reason := t.terminalState()
	output := make([]any, 0, len(t.items))
	for _, it := range t.items {
		output = append(output, it.toMap(true))
	}
	eventType := "response.completed"
	if status == "incomplete" {
		eventType = "response.incomplete"
	}
	return eventType, t.responseObject(status, reason, usage, output)
}

func (t *geminiResponsesTranscoder) responseObject(status, incompleteReason string, usage *dto.Usage, output []any) map[string]any {
	obj := map[string]any{
		"id":                   t.respID,
		"object":               "response",
		"created_at":           t.createdAt,
		"status":               status,
		"background":           false,
		"error":                nil,
		"incomplete_details":   nil,
		"instructions":         nil,
		"max_output_tokens":    nil,
		"model":                t.info.OriginModelName,
		"output":               output,
		"parallel_tool_calls":  true,
		"previous_response_id": nil,
		"reasoning":            map[string]any{"effort": nil, "summary": nil},
		"store":                false,
		"temperature":          nil,
		"text":                 map[string]any{"format": map[string]any{"type": "text"}},
		"tool_choice":          "auto",
		"tools":                []any{},
		"top_p":                nil,
		"truncation":           "disabled",
		"usage":                nil,
		"user":                 nil,
		"metadata":             map[string]any{},
	}
	if incompleteReason != "" {
		obj["incomplete_details"] = map[string]any{"reason": incompleteReason}
	}
	if usage != nil {
		obj["usage"] = geminiResponsesUsage(usage)
	}
	if req := t.request; req != nil {
		setRaw := func(key string, raw json.RawMessage) {
			if len(raw) > 0 {
				obj[key] = raw
			}
		}
		setRaw("instructions", req.Instructions)
		setRaw("parallel_tool_calls", req.ParallelToolCalls)
		setRaw("store", req.Store)
		setRaw("text", req.Text)
		setRaw("tool_choice", req.ToolChoice)
		setRaw("tools", req.Tools)
		setRaw("truncation", req.Truncation)
		setRaw("user", req.User)
		setRaw("metadata", req.Metadata)
		if req.MaxOutputTokens != nil {
			obj["max_output_tokens"] = *req.MaxOutputTokens
		}
		if req.Temperature != nil {
			obj["temperature"] = *req.Temperature
		}
		if req.TopP != nil {
			obj["top_p"] = *req.TopP
		}
		if req.PreviousResponseID != "" {
			obj["previous_response_id"] = req.PreviousResponseID
		}
		if req.Reasoning != nil {
			reasoning := map[string]any{"effort": nil, "summary": nil}
			if req.Reasoning.Effort != "" {
				reasoning["effort"] = req.Reasoning.Effort
			}
			if req.Reasoning.Summary != "" {
				reasoning["summary"] = req.Reasoning.Summary
			}
			obj["reasoning"] = reasoning
		}
	}
	return obj
}

func geminiResponsesUsage(usage *dto.Usage) map[string]any {
	total := usage.TotalTokens
	if total <= 0 {
		total = usage.PromptTokens + usage.CompletionTokens
	}
	return map[string]any{
		"input_tokens":          usage.PromptTokens,
		"input_tokens_details":  map[string]any{"cached_tokens": usage.PromptTokensDetails.CachedTokens},
		"output_tokens":         usage.CompletionTokens,
		"output_tokens_details": map[string]any{"reasoning_tokens": usage.CompletionTokenDetails.ReasoningTokens},
		"total_tokens":          total,
	}
}

func GeminiResponsesHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	service.CloseResponseBodyGracefully(resp)
	logger.LogDebug(c, "Gemini response body: %s", responseBody)

	var geminiResponse dto.GeminiChatResponse
	if err := common.Unmarshal(responseBody, &geminiResponse); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	usage := buildUsageFromGeminiMetadata(geminiResponse.UsageMetadata, info.GetEstimatePromptTokens())

	if len(geminiResponse.Candidates) == 0 {
		var newAPIError *types.NewAPIError
		if geminiResponse.PromptFeedback != nil && geminiResponse.PromptFeedback.BlockReason != nil {
			common.SetContextKey(c, constant.ContextKeyAdminRejectReason, fmt.Sprintf("gemini_block_reason=%s", *geminiResponse.PromptFeedback.BlockReason))
			newAPIError = types.NewOpenAIError(
				errors.New("request blocked by Gemini API: "+*geminiResponse.PromptFeedback.BlockReason),
				types.ErrorCodePromptBlocked,
				http.StatusBadRequest,
			)
		} else {
			common.SetContextKey(c, constant.ContextKeyAdminRejectReason, "gemini_empty_candidates")
			newAPIError = types.NewOpenAIError(
				errors.New("empty response from Gemini API"),
				types.ErrorCodeEmptyResponse,
				http.StatusInternalServerError,
			)
		}
		service.ResetStatusCode(newAPIError, c.GetString("status_code_mapping"))
		c.JSON(newAPIError.StatusCode, gin.H{"error": newAPIError.ToOpenAIError()})
		return &usage, nil
	}

	t := newGeminiResponsesTranscoder(c, info, false)
	t.consume(&geminiResponse)
	_, responseObj := t.complete(&usage)
	body, err := common.Marshal(responseObj)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	service.IOCopyBytesGracefully(c, resp, body)
	return &usage, nil
}

func GeminiResponsesStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	t := newGeminiResponsesTranscoder(c, info, true)

	usage, err := geminiStreamHandler(c, info, resp, func(_ string, geminiResponse *dto.GeminiChatResponse) bool {
		t.consume(geminiResponse)
		return true
	})

	if info.StreamStatus.IsScannerError() {
		return usage, nil
	}
	if err != nil {
		return usage, err
	}
	if !t.started {
		return usage, types.NewOpenAIError(errors.New("empty response from Gemini API"), types.ErrorCodeEmptyResponse, http.StatusInternalServerError)
	}

	eventType, responseObj := t.complete(usage)
	t.emit(eventType, map[string]any{"response": responseObj})
	return usage, nil
}
