package openaicompat

import (
	"encoding/json"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
)

const (
	rsKindReasoning  = "reasoning"
	rsKindMessage    = "message"
	rsKindFunction   = "function_call"
	rsKindCustomTool = "custom_tool_call"
)

// ResponsesStreamEvent is one Responses API server-sent event. Data already carries the
// type and a monotonically increasing sequence_number.
type ResponsesStreamEvent struct {
	Type string
	Data map[string]any
}

type chatResponsesItem struct {
	kind        string
	id          string
	outputIndex int
	callID      string
	// upstreamCallID is the id the upstream gave the tool call, empty until it is seen.
	upstreamCallID string
	// thoughtSignature is the Gemini signature the upstream attached to the tool call.
	thoughtSignature string
	name             string
	// NOTE: text holds the reasoning summary, message text, function arguments or custom tool input JSON, depending on kind.
	text   strings.Builder
	closed bool
}

func (it *chatResponsesItem) toMap(done bool) map[string]any {
	status := "in_progress"
	if done {
		status = "completed"
	}
	text := it.text.String()
	switch it.kind {
	case rsKindReasoning:
		summary := []any{}
		if done {
			summary = append(summary, map[string]any{"type": "summary_text", "text": text})
		}
		return map[string]any{"type": it.kind, "id": it.id, "summary": summary}
	case rsKindMessage:
		content := []any{}
		if done {
			content = append(content, responsesOutputTextPart(text))
		}
		return map[string]any{"type": it.kind, "id": it.id, "status": status, "role": "assistant", "content": content}
	case rsKindCustomTool:
		input := ""
		if done {
			input = customToolInputFromArguments(text)
		}
		return map[string]any{"type": it.kind, "id": it.id, "status": status, "call_id": it.callID, "name": it.name, "input": input}
	default:
		if done && text == "" {
			text = "{}"
		}
		return map[string]any{"type": it.kind, "id": it.id, "status": status, "call_id": it.callID, "name": it.name, "arguments": text}
	}
}

func responsesOutputTextPart(text string) map[string]any {
	return map[string]any{"type": "output_text", "text": text, "annotations": []any{}}
}

// customToolInputFromArguments recovers the freeform input of a custom tool that was
// exposed to the chat upstream as a function with a single string argument.
func customToolInputFromArguments(args string) string {
	if strings.TrimSpace(args) == "" {
		return ""
	}
	var parsed map[string]any
	if err := common.UnmarshalJsonStr(args, &parsed); err == nil {
		if input, ok := parsed[CustomToolInputKey].(string); ok {
			return input
		}
	}
	return args
}

// ChatToResponsesTranscoder rebuilds one Responses API response from Chat Completions
// output. In stream mode it also returns the Responses events for every chat chunk.
type ChatToResponsesTranscoder struct {
	stream      bool
	respID      string
	model       string
	createdAt   int64
	request     *dto.OpenAIResponsesRequest
	customTools map[string]struct{}

	seq          int
	started      bool
	items        []*chatResponsesItem
	textItem     *chatResponsesItem
	toolItems    map[int]*chatResponsesItem
	lastTool     *chatResponsesItem
	finishReason string
	pending      []ResponsesStreamEvent
	outputText   strings.Builder
}

func NewChatToResponsesTranscoder(respID, model string, createdAt int64, request *dto.OpenAIResponsesRequest, stream bool) *ChatToResponsesTranscoder {
	t := &ChatToResponsesTranscoder{
		stream:      stream,
		respID:      respID,
		model:       model,
		createdAt:   createdAt,
		request:     request,
		customTools: make(map[string]struct{}),
		toolItems:   make(map[int]*chatResponsesItem),
	}
	if request != nil {
		for _, tool := range request.GetToolsMap() {
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

// ThoughtSignatures returns the Gemini thought signature of each tool call that carried
// one, keyed by the call_id the client sees.
func (t *ChatToResponsesTranscoder) ThoughtSignatures() map[string]string {
	signatures := make(map[string]string)
	for _, it := range t.items {
		if it.thoughtSignature != "" && it.callID != "" {
			signatures[it.callID] = it.thoughtSignature
		}
	}
	return signatures
}

// Started reports whether any chunk or message has been consumed.
func (t *ChatToResponsesTranscoder) Started() bool {
	return t.started
}

// OutputText returns everything the model produced, for token estimation when the
// upstream reports no usage.
func (t *ChatToResponsesTranscoder) OutputText() string {
	return t.outputText.String()
}

func (t *ChatToResponsesTranscoder) emit(eventType string, fields map[string]any) {
	if !t.stream {
		return
	}
	fields["type"] = eventType
	fields["sequence_number"] = t.seq
	t.seq++
	t.pending = append(t.pending, ResponsesStreamEvent{Type: eventType, Data: fields})
}

func (t *ChatToResponsesTranscoder) drain() []ResponsesStreamEvent {
	out := t.pending
	t.pending = nil
	return out
}

func (t *ChatToResponsesTranscoder) start() {
	if t.started {
		return
	}
	t.started = true
	t.emit("response.created", map[string]any{"response": t.responseObject("in_progress", "", nil, []any{})})
	t.emit("response.in_progress", map[string]any{"response": t.responseObject("in_progress", "", nil, []any{})})
}

// Start emits the opening events once.
func (t *ChatToResponsesTranscoder) Start() []ResponsesStreamEvent {
	t.start()
	return t.drain()
}

// ConsumeChunk folds one chat completion chunk into the response. Only the first choice is used.
func (t *ChatToResponsesTranscoder) ConsumeChunk(chunk *dto.ChatCompletionsStreamResponse) []ResponsesStreamEvent {
	t.start()
	if len(chunk.Choices) > 0 {
		choice := &chunk.Choices[0]
		if reasoning := choice.Delta.GetReasoningContent(); reasoning != "" {
			t.appendText(rsKindReasoning, reasoning)
		}
		if content := choice.Delta.GetContentString(); content != "" {
			t.appendText(rsKindMessage, content)
		}
		for _, call := range choice.Delta.ToolCalls {
			t.addToolCallDelta(call)
		}
		if choice.FinishReason != nil && *choice.FinishReason != "" {
			t.finishReason = *choice.FinishReason
		}
	}
	return t.drain()
}

// ConsumeMessage folds a complete (non-stream) chat message into the response.
func (t *ChatToResponsesTranscoder) ConsumeMessage(msg *dto.Message, finishReason string) {
	t.start()
	if reasoning := msg.GetReasoningContent(); reasoning != "" {
		t.appendText(rsKindReasoning, reasoning)
	}
	if content := msg.StringContent(); content != "" {
		t.appendText(rsKindMessage, content)
	}
	for i, call := range msg.ParseToolCalls() {
		index := i
		t.addToolCallDelta(dto.ToolCallResponse{
			Index:        &index,
			ID:           call.ID,
			Function:     dto.FunctionResponse{Name: call.Function.Name, Arguments: call.Function.Arguments},
			ExtraContent: call.ExtraContent,
		})
	}
	t.finishReason = finishReason
}

func (t *ChatToResponsesTranscoder) appendText(kind, delta string) {
	if t.textItem == nil || t.textItem.kind != kind {
		t.closeTextItem()
		t.closeToolItems()
		t.textItem = t.openItem(kind, "", "")
	}
	it := t.textItem
	it.text.WriteString(delta)
	t.outputText.WriteString(delta)

	eventType := "response.output_text.delta"
	fields := map[string]any{"item_id": it.id, "output_index": it.outputIndex, "delta": delta}
	if kind == rsKindReasoning {
		eventType = "response.reasoning_summary_text.delta"
		fields["summary_index"] = 0
	} else {
		fields["content_index"] = 0
		fields["logprobs"] = []any{}
	}
	t.emit(eventType, fields)
}

// addToolCallDelta routes one tool call fragment to its item. Fragments of one call share
// an index, but some upstreams reuse index 0 for every call, so a new non-empty id on a
// known index starts a new call.
func (t *ChatToResponsesTranscoder) addToolCallDelta(call dto.ToolCallResponse) {
	var it *chatResponsesItem
	hasIndex := call.Index != nil
	switch {
	case hasIndex:
		it = t.toolItems[*call.Index]
	case call.ID == "":
		it = t.lastTool
	}
	if it != nil && call.ID != "" {
		if it.upstreamCallID == "" {
			it.upstreamCallID = call.ID
		} else if it.upstreamCallID != call.ID {
			it = nil
		}
	}

	if it == nil {
		t.closeTextItem()
		callID := call.ID
		if callID == "" {
			callID = "call_" + common.GetUUID()
		}
		it = t.openItem(rsKindFunction, callID, call.Function.Name)
		it.upstreamCallID = call.ID
		if hasIndex {
			t.toolItems[*call.Index] = it
		}
	}
	t.lastTool = it
	if it.thoughtSignature == "" {
		it.thoughtSignature = ThoughtSignatureFromExtraContent(call.ExtraContent)
	}

	if call.Function.Arguments == "" {
		return
	}
	it.text.WriteString(call.Function.Arguments)
	t.outputText.WriteString(call.Function.Arguments)
	if it.kind == rsKindFunction {
		t.emit("response.function_call_arguments.delta", map[string]any{
			"item_id": it.id, "output_index": it.outputIndex, "delta": call.Function.Arguments,
		})
	}
}

func (t *ChatToResponsesTranscoder) openItem(kind, callID, name string) *chatResponsesItem {
	if kind == rsKindFunction {
		if _, custom := t.customTools[name]; custom {
			kind = rsKindCustomTool
		}
	}
	prefix := "msg_"
	switch kind {
	case rsKindReasoning:
		prefix = "rs_"
	case rsKindFunction:
		prefix = "fc_"
	case rsKindCustomTool:
		prefix = "ctc_"
	}
	it := &chatResponsesItem{
		kind:        kind,
		id:          prefix + common.GetUUID(),
		outputIndex: len(t.items),
		callID:      callID,
		name:        name,
	}
	t.items = append(t.items, it)

	t.emit("response.output_item.added", map[string]any{"output_index": it.outputIndex, "item": it.toMap(false)})
	switch kind {
	case rsKindReasoning:
		t.emit("response.reasoning_summary_part.added", map[string]any{
			"item_id": it.id, "output_index": it.outputIndex, "summary_index": 0,
			"part": map[string]any{"type": "summary_text", "text": ""},
		})
	case rsKindMessage:
		t.emit("response.content_part.added", map[string]any{
			"item_id": it.id, "output_index": it.outputIndex, "content_index": 0,
			"part": responsesOutputTextPart(""),
		})
	}
	return it
}

func (t *ChatToResponsesTranscoder) closeTextItem() {
	it := t.textItem
	if it == nil {
		return
	}
	t.textItem = nil
	t.closeItem(it)
}

func (t *ChatToResponsesTranscoder) closeToolItems() {
	for _, it := range t.items {
		if it.kind == rsKindFunction || it.kind == rsKindCustomTool {
			t.closeItem(it)
		}
	}
	t.toolItems = make(map[int]*chatResponsesItem)
	t.lastTool = nil
}

func (t *ChatToResponsesTranscoder) closeItem(it *chatResponsesItem) {
	if it.closed {
		return
	}
	it.closed = true

	text := it.text.String()
	switch it.kind {
	case rsKindReasoning:
		t.emit("response.reasoning_summary_text.done", map[string]any{
			"item_id": it.id, "output_index": it.outputIndex, "summary_index": 0, "text": text,
		})
		t.emit("response.reasoning_summary_part.done", map[string]any{
			"item_id": it.id, "output_index": it.outputIndex, "summary_index": 0,
			"part": map[string]any{"type": "summary_text", "text": text},
		})
	case rsKindMessage:
		t.emit("response.output_text.done", map[string]any{
			"item_id": it.id, "output_index": it.outputIndex, "content_index": 0, "text": text, "logprobs": []any{},
		})
		t.emit("response.content_part.done", map[string]any{
			"item_id": it.id, "output_index": it.outputIndex, "content_index": 0,
			"part": responsesOutputTextPart(text),
		})
	case rsKindFunction:
		if text == "" {
			text = "{}"
			t.emit("response.function_call_arguments.delta", map[string]any{
				"item_id": it.id, "output_index": it.outputIndex, "delta": text,
			})
		}
		t.emit("response.function_call_arguments.done", map[string]any{
			"item_id": it.id, "output_index": it.outputIndex, "name": it.name, "arguments": text,
		})
	case rsKindCustomTool:
		input := customToolInputFromArguments(text)
		if input != "" {
			t.emit("response.custom_tool_call_input.delta", map[string]any{
				"item_id": it.id, "output_index": it.outputIndex, "delta": input,
			})
		}
		t.emit("response.custom_tool_call_input.done", map[string]any{
			"item_id": it.id, "output_index": it.outputIndex, "input": input,
		})
	}
	t.emit("response.output_item.done", map[string]any{"output_index": it.outputIndex, "item": it.toMap(true)})
}

func (t *ChatToResponsesTranscoder) terminalState() (status, incompleteReason string) {
	switch strings.ToLower(strings.TrimSpace(t.finishReason)) {
	case "length":
		return "incomplete", "max_output_tokens"
	case "content_filter":
		return "incomplete", "content_filter"
	default:
		return "completed", ""
	}
}

// Complete closes every open item and returns the final response object together with the
// remaining events, the last of which is the terminal one.
func (t *ChatToResponsesTranscoder) Complete(usage *dto.Usage) (map[string]any, []ResponsesStreamEvent) {
	t.start()
	t.closeTextItem()
	t.closeToolItems()

	status, reason := t.terminalState()
	output := make([]any, 0, len(t.items))
	for _, it := range t.items {
		output = append(output, it.toMap(true))
	}
	obj := t.responseObject(status, reason, usage, output)

	eventType := "response.completed"
	if status == "incomplete" {
		eventType = "response.incomplete"
	}
	t.emit(eventType, map[string]any{"response": obj})
	return obj, t.drain()
}

// Fail ends the stream with a response.failed event, keeping whatever was produced so far.
func (t *ChatToResponsesTranscoder) Fail(code, message string) []ResponsesStreamEvent {
	t.start()
	output := make([]any, 0, len(t.items))
	for _, it := range t.items {
		output = append(output, it.toMap(it.closed))
	}
	obj := t.responseObject("failed", "", nil, output)
	obj["error"] = map[string]any{"code": code, "message": message}
	t.emit("response.failed", map[string]any{"response": obj})
	return t.drain()
}

func (t *ChatToResponsesTranscoder) responseObject(status, incompleteReason string, usage *dto.Usage, output []any) map[string]any {
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
		"model":                t.model,
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
		obj["usage"] = chatUsageToResponsesUsage(usage)
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

func chatUsageToResponsesUsage(usage *dto.Usage) map[string]any {
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
