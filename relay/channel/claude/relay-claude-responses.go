package claude

import (
	"encoding/json"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service/openaicompat"
	"github.com/gin-gonic/gin"
)

const (
	responsesItemReasoning  = "reasoning"
	responsesItemMessage    = "message"
	responsesItemFunction   = "function_call"
	responsesItemCustomTool = "custom_tool_call"
)

type claudeResponsesItem struct {
	kind           string
	id             string
	outputIndex    int
	callID         string
	name           string
	hasSummaryPart bool
	redacted       bool
	redactedData   string
	signature      strings.Builder
	encrypted      string
	text           strings.Builder
	customInput    string
	closed         bool
}

func (it *claudeResponsesItem) toMap(done bool) map[string]any {
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
		input := ""
		if done {
			input = it.customInput
		}
		return map[string]any{"type": it.kind, "id": it.id, "status": status, "call_id": it.callID, "name": it.name, "input": input}
	default:
		return map[string]any{"type": it.kind, "id": it.id, "status": status, "call_id": it.callID, "name": it.name, "arguments": text}
	}
}

func responsesOutputTextPart(text string) map[string]any {
	return map[string]any{"type": "output_text", "text": text, "annotations": []any{}}
}

type claudeResponsesTranscoder struct {
	c       *gin.Context
	info    *relaycommon.RelayInfo
	stream  bool
	request *dto.OpenAIResponsesRequest

	respID         string
	createdAt      int64
	customTools    map[string]struct{}
	signatureModel string

	seq        int
	started    bool
	items      []*claudeResponsesItem
	blocks     map[int]*claudeResponsesItem
	stopReason string
}

func newClaudeResponsesTranscoder(c *gin.Context, info *relaycommon.RelayInfo, stream bool) *claudeResponsesTranscoder {
	t := &claudeResponsesTranscoder{
		c:           c,
		info:        info,
		stream:      stream,
		respID:      "resp_" + strings.TrimPrefix(helper.GetResponseID(c), "chatcmpl-"),
		createdAt:   common.GetTimestamp(),
		customTools: make(map[string]struct{}),
		blocks:      make(map[int]*claudeResponsesItem),
	}
	if info != nil {
		t.request, _ = info.Request.(*dto.OpenAIResponsesRequest)
	}
	if c != nil {
		t.signatureModel = common.GetContextKeyString(c, constant.ContextKeyClaudeSignatureModel)
	}
	if t.signatureModel == "" && info != nil {
		t.signatureModel = info.UpstreamModelName
	}
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

func (t *claudeResponsesTranscoder) emit(eventType string, fields map[string]any) {
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
	if t.info != nil {
		t.info.SendResponseCount++
	}
}

func (t *claudeResponsesTranscoder) start() {
	if t.started {
		return
	}
	t.started = true
	t.emit("response.created", map[string]any{"response": t.responseObject("in_progress", "", nil, []any{})})
	t.emit("response.in_progress", map[string]any{"response": t.responseObject("in_progress", "", nil, []any{})})
}

func (t *claudeResponsesTranscoder) consumeEvent(ev *dto.ClaudeResponse) {
	if ev == nil {
		return
	}
	switch ev.Type {
	case "message_start":
		t.start()
	case "content_block_start":
		t.start()
		if ev.ContentBlock == nil || ev.Index == nil {
			return
		}
		idx := *ev.Index
		cb := ev.ContentBlock
		switch cb.Type {
		case "thinking":
			it := t.openItem(responsesItemReasoning, "", "", true)
			t.blocks[idx] = it
			if cb.Thinking != nil && *cb.Thinking != "" {
				it.text.WriteString(*cb.Thinking)
				t.emit("response.reasoning_summary_text.delta", map[string]any{
					"item_id": it.id, "output_index": it.outputIndex, "summary_index": 0, "delta": *cb.Thinking,
				})
			}
			if cb.Signature != "" {
				it.signature.WriteString(cb.Signature)
			}
		case "redacted_thinking":
			it := t.openItem(responsesItemReasoning, "", "", false)
			it.redacted = true
			it.redactedData = cb.Data
			t.blocks[idx] = it
		case "text":
			it := t.openItem(responsesItemMessage, "", "", false)
			t.blocks[idx] = it
			if cb.Text != nil && *cb.Text != "" {
				it.text.WriteString(*cb.Text)
				t.emit("response.output_text.delta", map[string]any{
					"item_id": it.id, "output_index": it.outputIndex, "content_index": 0, "delta": *cb.Text, "logprobs": []any{},
				})
			}
		case "tool_use":
			kind := responsesItemFunction
			if _, isCustom := t.customTools[cb.Name]; isCustom {
				kind = responsesItemCustomTool
			}
			it := t.openItem(kind, cb.Id, cb.Name, false)
			t.blocks[idx] = it
		}
	case "content_block_delta":
		if ev.Delta == nil || ev.Index == nil {
			return
		}
		it := t.blocks[*ev.Index]
		if it == nil || it.closed {
			return
		}
		switch ev.Delta.Type {
		case "thinking_delta":
			if ev.Delta.Thinking != nil && *ev.Delta.Thinking != "" {
				it.text.WriteString(*ev.Delta.Thinking)
				t.emit("response.reasoning_summary_text.delta", map[string]any{
					"item_id": it.id, "output_index": it.outputIndex, "summary_index": 0, "delta": *ev.Delta.Thinking,
				})
			}
		case "signature_delta":
			if ev.Delta.Signature != "" {
				it.signature.WriteString(ev.Delta.Signature)
			}
		case "text_delta":
			if ev.Delta.Text != nil && *ev.Delta.Text != "" {
				it.text.WriteString(*ev.Delta.Text)
				t.emit("response.output_text.delta", map[string]any{
					"item_id": it.id, "output_index": it.outputIndex, "content_index": 0, "delta": *ev.Delta.Text, "logprobs": []any{},
				})
			}
		case "input_json_delta":
			if ev.Delta.PartialJson != nil && *ev.Delta.PartialJson != "" {
				partial := *ev.Delta.PartialJson
				it.text.WriteString(partial)
				if it.kind == responsesItemFunction {
					t.emit("response.function_call_arguments.delta", map[string]any{
						"item_id": it.id, "output_index": it.outputIndex, "delta": partial,
					})
				}
			}
		}
	case "content_block_stop":
		if ev.Index == nil {
			return
		}
		if it := t.blocks[*ev.Index]; it != nil {
			t.closeItem(it)
		}
	case "message_delta":
		if ev.Delta != nil && ev.Delta.StopReason != nil && *ev.Delta.StopReason != "" {
			t.stopReason = *ev.Delta.StopReason
		}
	}
}

func (t *claudeResponsesTranscoder) consumeResponse(resp *dto.ClaudeResponse) {
	if resp == nil {
		return
	}
	t.start()
	t.stopReason = resp.StopReason
	for i := range resp.Content {
		block := &resp.Content[i]
		switch block.Type {
		case "thinking":
			it := t.openItem(responsesItemReasoning, "", "", true)
			if block.Thinking != nil {
				it.text.WriteString(*block.Thinking)
			}
			if block.Signature != "" {
				it.signature.WriteString(block.Signature)
			}
			t.closeItem(it)
		case "redacted_thinking":
			it := t.openItem(responsesItemReasoning, "", "", false)
			it.redacted = true
			it.redactedData = block.Data
			t.closeItem(it)
		case "text":
			it := t.openItem(responsesItemMessage, "", "", false)
			if block.Text != nil {
				it.text.WriteString(*block.Text)
			}
			t.closeItem(it)
		case "tool_use":
			if _, isCustom := t.customTools[block.Name]; isCustom {
				it := t.openItem(responsesItemCustomTool, block.Id, block.Name, false)
				if block.Input != nil {
					if raw, err := common.Marshal(block.Input); err == nil {
						it.text.Write(raw)
					}
				}
				t.closeItem(it)
			} else {
				it := t.openItem(responsesItemFunction, block.Id, block.Name, false)
				args := "{}"
				if block.Input != nil {
					if raw, err := common.Marshal(block.Input); err == nil && string(raw) != "null" {
						args = string(raw)
					}
				}
				it.text.WriteString(args)
				t.closeItem(it)
			}
		}
	}
}

func (t *claudeResponsesTranscoder) openItem(kind, callID, name string, hasSummaryPart bool) *claudeResponsesItem {
	prefix := "msg_"
	switch kind {
	case responsesItemReasoning:
		prefix = "rs_"
	case responsesItemFunction:
		prefix = "fc_"
	case responsesItemCustomTool:
		prefix = "ctc_"
	}
	it := &claudeResponsesItem{
		kind:           kind,
		id:             prefix + common.GetUUID(),
		outputIndex:    len(t.items),
		callID:         callID,
		name:           name,
		hasSummaryPart: hasSummaryPart,
	}
	t.items = append(t.items, it)
	t.emit("response.output_item.added", map[string]any{"output_index": it.outputIndex, "item": it.toMap(false)})
	switch kind {
	case responsesItemReasoning:
		if hasSummaryPart {
			t.emit("response.reasoning_summary_part.added", map[string]any{
				"item_id": it.id, "output_index": it.outputIndex, "summary_index": 0,
				"part": map[string]any{"type": "summary_text", "text": ""},
			})
		}
	case responsesItemMessage:
		t.emit("response.content_part.added", map[string]any{
			"item_id": it.id, "output_index": it.outputIndex, "content_index": 0,
			"part": responsesOutputTextPart(""),
		})
	}
	return it
}

func (t *claudeResponsesTranscoder) closeItem(it *claudeResponsesItem) {
	if it == nil || it.closed {
		return
	}
	it.closed = true

	switch it.kind {
	case responsesItemReasoning:
		text := it.text.String()
		if it.redacted {
			it.encrypted = encodeClaudeThinking(t.signatureModel, dto.ClaudeMediaMessage{
				Type: "redacted_thinking",
				Data: it.redactedData,
			})
		} else if sig := it.signature.String(); sig != "" {
			thinkingText := text
			it.encrypted = encodeClaudeThinking(t.signatureModel, dto.ClaudeMediaMessage{
				Type:      "thinking",
				Thinking:  &thinkingText,
				Signature: sig,
			})
		}
		if it.hasSummaryPart {
			t.emit("response.reasoning_summary_text.done", map[string]any{
				"item_id": it.id, "output_index": it.outputIndex, "summary_index": 0, "text": text,
			})
			t.emit("response.reasoning_summary_part.done", map[string]any{
				"item_id": it.id, "output_index": it.outputIndex, "summary_index": 0,
				"part": map[string]any{"type": "summary_text", "text": text},
			})
		}
	case responsesItemMessage:
		text := it.text.String()
		t.emit("response.output_text.done", map[string]any{
			"item_id": it.id, "output_index": it.outputIndex, "content_index": 0, "text": text, "logprobs": []any{},
		})
		t.emit("response.content_part.done", map[string]any{
			"item_id": it.id, "output_index": it.outputIndex, "content_index": 0,
			"part": responsesOutputTextPart(text),
		})
	case responsesItemFunction:
		if it.text.Len() == 0 {
			it.text.WriteString("{}")
		}
		t.emit("response.function_call_arguments.done", map[string]any{
			"item_id": it.id, "output_index": it.outputIndex, "name": it.name, "arguments": it.text.String(),
		})
	case responsesItemCustomTool:
		it.customInput = unwrapClaudeCustomToolInput(it.text.String())
		if it.customInput != "" {
			t.emit("response.custom_tool_call_input.delta", map[string]any{
				"item_id": it.id, "output_index": it.outputIndex, "delta": it.customInput,
			})
		}
		t.emit("response.custom_tool_call_input.done", map[string]any{
			"item_id": it.id, "output_index": it.outputIndex, "input": it.customInput,
		})
	}
	t.emit("response.output_item.done", map[string]any{"output_index": it.outputIndex, "item": it.toMap(true)})
}

func unwrapClaudeCustomToolInput(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	var obj map[string]any
	if err := common.UnmarshalJsonStr(trimmed, &obj); err == nil {
		if input, ok := obj[openaicompat.CustomToolInputKey].(string); ok {
			return input
		}
	}
	return raw
}

func claudeResponsesTerminalState(stopReason string) (status, incompleteReason string) {
	switch strings.ToLower(strings.TrimSpace(stopReason)) {
	case "max_tokens", "model_context_window_exceeded":
		return "incomplete", "max_output_tokens"
	case "refusal":
		return "incomplete", "content_filter"
	default:
		return "completed", ""
	}
}

func (t *claudeResponsesTranscoder) complete(usage *dto.Usage) (string, map[string]any) {
	for _, it := range t.items {
		if !it.closed {
			t.closeItem(it)
		}
	}
	status, reason := claudeResponsesTerminalState(t.stopReason)
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

func (t *claudeResponsesTranscoder) fail(code, message string) {
	if !t.started {
		return
	}
	output := make([]any, 0, len(t.items))
	for _, it := range t.items {
		output = append(output, it.toMap(it.closed))
	}
	obj := t.responseObject("failed", "", nil, output)
	obj["error"] = map[string]any{"code": code, "message": message}
	t.emit("response.failed", map[string]any{"response": obj})
}

func (t *claudeResponsesTranscoder) responseObject(status, incompleteReason string, usage *dto.Usage, output []any) map[string]any {
	model := ""
	if t.info != nil {
		model = t.info.OriginModelName
		if model == "" {
			model = t.info.UpstreamModelName
		}
	}
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
		"model":                model,
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
		obj["usage"] = claudeResponsesUsage(usage)
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

func claudeResponsesUsage(usage *dto.Usage) map[string]any {
	openAIUsage := buildOpenAIStyleUsageFromClaudeUsage(usage)
	return map[string]any{
		"input_tokens":          openAIUsage.InputTokens,
		"input_tokens_details":  map[string]any{"cached_tokens": openAIUsage.PromptTokensDetails.CachedTokens},
		"output_tokens":         openAIUsage.CompletionTokens,
		"output_tokens_details": map[string]any{"reasoning_tokens": 0},
		"total_tokens":          openAIUsage.TotalTokens,
	}
}
