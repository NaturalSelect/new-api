package openaicompat

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
)

// CustomToolInputKey is the single string argument a Responses custom (freeform) tool is exposed
// with when the upstream only understands JSON-schema function tools.
const CustomToolInputKey = "input"

// ReasoningSignatureKind tells which part of a replayed turn a reasoning item's signature belongs to.
type ReasoningSignatureKind int

const (
	ReasoningSignatureNone ReasoningSignatureKind = iota
	// ReasoningSignatureForCall signs the tool call behind the reasoning item.
	ReasoningSignatureForCall
	// ReasoningSignatureForMessage signs the assistant message in front of the reasoning item.
	ReasoningSignatureForMessage
	// ReasoningSignatureForThought signs the summary text of the reasoning item itself.
	ReasoningSignatureForThought
	// ReasoningSignatureForBlock carries a JSON-encoded ClaudeThinkingBlock to replay verbatim.
	ReasoningSignatureForBlock
)

// ReasoningSignatureDecoder recognizes the encrypted_content a channel issued for a reasoning item.
// It returns ReasoningSignatureNone for content the channel did not issue or cannot use.
type ReasoningSignatureDecoder func(encrypted string) (signature string, kind ReasoningSignatureKind)

// ResponsesRequestToChatCompletionsRequest converts a Responses API request into the equivalent
// Chat Completions request so providers that only implement the chat shape can reuse their
// existing converters. Reasoning items, hosted tools and stored-conversation state have no chat
// equivalent and are not carried over.
func ResponsesRequestToChatCompletionsRequest(req *dto.OpenAIResponsesRequest) (*dto.GeneralOpenAIRequest, error) {
	return ResponsesRequestToChatCompletionsRequestWithSignatures(req, nil)
}

// ResponsesRequestToChatCompletionsRequestWithSignatures is ResponsesRequestToChatCompletionsRequest
// for channels whose reasoning items carry an upstream thought signature. The signatures decode
// recognizes are moved to the extra_content of the assistant message or tool call they belong to,
// and signed thought summaries to the extra_content of the assistant message they lead.
func ResponsesRequestToChatCompletionsRequestWithSignatures(req *dto.OpenAIResponsesRequest, decode ReasoningSignatureDecoder) (*dto.GeneralOpenAIRequest, error) {
	if req == nil {
		return nil, errors.New("request is nil")
	}
	if req.Model == "" {
		return nil, errors.New("model is required")
	}
	if req.PreviousResponseID != "" {
		return nil, errors.New("previous_response_id is not supported by this channel, send the full conversation in input")
	}
	if !isEmptyRawJSON(req.Conversation) {
		return nil, errors.New("conversation is not supported by this channel, send the full conversation in input")
	}

	b := &responsesChatBuilder{callNames: make(map[string]string), decode: decode, lastMessage: -1}
	if instructions := common.JsonRawMessageToString(req.Instructions); strings.TrimSpace(instructions) != "" {
		b.addMessage("system", instructions)
	}
	if err := b.addInput(req.Input); err != nil {
		return nil, err
	}
	if len(b.messages) == 0 {
		return nil, errors.New("input is required")
	}

	tools, err := responsesToolsToChatTools(req.Tools)
	if err != nil {
		return nil, err
	}
	responseFormat, err := responsesTextToChatResponseFormat(req.Text)
	if err != nil {
		return nil, err
	}

	out := &dto.GeneralOpenAIRequest{
		Model:               req.Model,
		Messages:            b.messages,
		Stream:              req.Stream,
		Temperature:         req.Temperature,
		TopP:                req.TopP,
		MaxCompletionTokens: req.MaxOutputTokens,
		Tools:               tools,
		ResponseFormat:      responseFormat,
	}
	if len(tools) > 0 {
		out.ToolChoice = responsesToolChoiceToChat(req.ToolChoice)
	}
	if req.Reasoning != nil {
		out.ReasoningEffort = req.Reasoning.Effort
	}
	if !isEmptyRawJSON(req.ParallelToolCalls) {
		var parallel bool
		if err := common.Unmarshal(req.ParallelToolCalls, &parallel); err == nil {
			out.ParallelTooCalls = &parallel
		}
	}
	return out, nil
}

type responsesChatBuilder struct {
	messages []dto.Message
	// callNames maps a function call id to its function name, so outputs can be paired with their call.
	callNames map[string]string

	decode ReasoningSignatureDecoder
	// pendingCallSignature belongs to the tool call that comes next.
	pendingCallSignature string
	// pendingThoughts belong to the assistant message that comes next.
	pendingThoughts []ReasoningThought
	// pendingBlocks belong to the assistant message that comes next.
	pendingBlocks []ClaudeThinkingBlock
	// lastMessage is the index of the assistant message the previous input item went into, or -1.
	lastMessage int
}

func (b *responsesChatBuilder) lastAssistant() *dto.Message {
	if len(b.messages) == 0 {
		return nil
	}
	last := &b.messages[len(b.messages)-1]
	if last.Role != "assistant" {
		return nil
	}
	return last
}

func (b *responsesChatBuilder) addInput(raw json.RawMessage) error {
	switch common.GetJsonType(raw) {
	case "string":
		var text string
		if err := common.Unmarshal(raw, &text); err != nil {
			return fmt.Errorf("invalid input: %w", err)
		}
		b.addMessage("user", text)
	case "array":
		var items []map[string]any
		if err := common.Unmarshal(raw, &items); err != nil {
			return fmt.Errorf("invalid input: %w", err)
		}
		for _, item := range items {
			b.addItem(item)
		}
	}
	return nil
}

func (b *responsesChatBuilder) addItem(item map[string]any) {
	itemType := asString(item["type"])
	role := asString(item["role"])
	if itemType == "" && role != "" {
		itemType = "message"
	}
	if isResponsesContentPartType(itemType) {
		item = map[string]any{"role": "user", "content": []any{item}}
		itemType = "message"
	}
	if itemType == "reasoning" {
		b.addReasoning(item)
		return
	}

	b.lastMessage = -1
	switch itemType {
	case "message":
		if b.addMessage(role, item["content"]) && b.lastAssistant() != nil {
			b.lastMessage = len(b.messages) - 1
			b.attachThoughts(b.lastMessage)
			b.attachBlocks(b.lastMessage)
		}
	case "function_call":
		if b.addToolCall(item, asString(item["arguments"])) {
			b.attachThoughts(len(b.messages) - 1)
			b.attachBlocks(len(b.messages) - 1)
		}
	case "custom_tool_call":
		args, _ := common.Marshal(map[string]any{CustomToolInputKey: asString(item["input"])})
		if b.addToolCall(item, string(args)) {
			b.attachThoughts(len(b.messages) - 1)
			b.attachBlocks(len(b.messages) - 1)
		}
	case "function_call_output", "custom_tool_call_output":
		b.addToolOutput(item)
	}
	b.pendingCallSignature = ""
	b.pendingThoughts = nil
	b.pendingBlocks = nil
}

func (b *responsesChatBuilder) addReasoning(item map[string]any) {
	if b.decode == nil {
		return
	}
	signature, kind := b.decode(asString(item["encrypted_content"]))
	if signature == "" {
		return
	}
	switch kind {
	case ReasoningSignatureForCall:
		b.pendingCallSignature = signature
	case ReasoningSignatureForThought:
		if text := responsesReasoningSummaryText(item); text != "" {
			b.pendingThoughts = append(b.pendingThoughts, ReasoningThought{Text: text, ThoughtSignature: signature})
		}
	case ReasoningSignatureForMessage:
		// NOTE: A message split into several items yields several signatures, and the first one wins.
		if b.lastMessage >= 0 {
			msg := &b.messages[b.lastMessage]
			if ThoughtSignatureFromExtraContent(msg.ExtraContent) == "" {
				msg.ExtraContent = withGoogleExtra(msg.ExtraContent, "thought_signature", signature)
			}
		}
	case ReasoningSignatureForBlock:
		var block ClaudeThinkingBlock
		if err := common.UnmarshalJsonStr(signature, &block); err == nil {
			b.pendingBlocks = append(b.pendingBlocks, block)
		}
	}
}

func (b *responsesChatBuilder) attachBlocks(index int) {
	if len(b.pendingBlocks) == 0 {
		return
	}
	msg := &b.messages[index]
	blocks := append(ClaudeThinkingBlocksFromExtraContent(msg.ExtraContent), b.pendingBlocks...)
	msg.ExtraContent = withAnthropicExtra(msg.ExtraContent, "thinking_blocks", blocks)
}

func (b *responsesChatBuilder) attachThoughts(index int) {
	if len(b.pendingThoughts) == 0 {
		return
	}
	msg := &b.messages[index]
	thoughts := append(ThoughtsFromExtraContent(msg.ExtraContent), b.pendingThoughts...)
	msg.ExtraContent = withGoogleExtra(msg.ExtraContent, "thoughts", thoughts)
}

func responsesReasoningSummaryText(item map[string]any) string {
	summary, _ := item["summary"].([]any)
	var texts []string
	for _, raw := range summary {
		part, _ := raw.(map[string]any)
		if text := asString(part["text"]); text != "" {
			texts = append(texts, text)
		}
	}
	return strings.Join(texts, "\n")
}

// addToolCall reports whether the call went into the last message.
func (b *responsesChatBuilder) addToolCall(item map[string]any, arguments string) bool {
	name := strings.TrimSpace(asString(item["name"]))
	callID := responsesCallID(item)
	if name == "" || callID == "" {
		return false
	}
	b.callNames[callID] = name

	call := dto.ToolCallRequest{
		ID:           callID,
		Type:         "function",
		Function:     dto.FunctionRequest{Name: name, Arguments: arguments},
		ExtraContent: ExtraContentFromThoughtSignature(b.pendingCallSignature),
	}
	if last := b.lastAssistant(); last != nil {
		last.SetToolCalls(append(last.ParseToolCalls(), call))
		return true
	}
	msg := dto.Message{Role: "assistant"}
	msg.SetToolCalls([]dto.ToolCallRequest{call})
	b.messages = append(b.messages, msg)
	return true
}

func (b *responsesChatBuilder) addToolOutput(item map[string]any) {
	callID := responsesCallID(item)
	output := responsesOutputText(item["output"])
	name, paired := b.callNames[callID]
	if !paired {
		// NOTE: A function response without its call is rejected upstream, so keep the text as a user turn.
		b.addMessage("user", fmt.Sprintf("[tool_output_missing_call] %s", output))
		return
	}
	msg := dto.Message{Role: "tool", ToolCallId: callID, Name: &name}
	msg.SetStringContent(output)
	b.messages = append(b.messages, msg)
}

// addMessage reports whether the content went into a message.
func (b *responsesChatBuilder) addMessage(role string, content any) bool {
	switch strings.ToLower(role) {
	case "assistant", "model":
		role = "assistant"
	case "system", "developer":
		role = "system"
	default:
		role = "user"
	}

	if role == "system" {
		if text := strings.TrimSpace(responsesContentText(content)); text != "" {
			b.messages = append(b.messages, dto.Message{Role: role, Content: text})
			return true
		}
		return false
	}

	parts := responsesContentToChatParts(role, content)
	if len(parts) == 0 {
		return false
	}
	// NOTE: Gemini rejects a function call that does not directly follow a user or function-response turn,
	// so assistant text and the tool calls that follow it must stay in one model turn.
	if last := b.lastAssistant(); role == "assistant" && last != nil && len(last.ParseToolCalls()) == 0 {
		last.SetMediaContent(append(last.ParseContent(), parts...))
		return true
	}
	msg := dto.Message{Role: role}
	msg.SetMediaContent(parts)
	b.messages = append(b.messages, msg)
	return true
}

func responsesContentToChatParts(role string, content any) []dto.MediaContent {
	switch v := content.(type) {
	case string:
		if v == "" {
			return nil
		}
		return []dto.MediaContent{{Type: dto.ContentTypeText, Text: v}}
	case []any:
		parts := make([]dto.MediaContent, 0, len(v))
		for _, raw := range v {
			part, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if converted, ok := responsesPartToChatPart(role, part); ok {
				parts = append(parts, converted)
			}
		}
		return parts
	}
	return nil
}

func responsesPartToChatPart(role string, part map[string]any) (dto.MediaContent, bool) {
	switch asString(part["type"]) {
	case "input_text", "output_text", "text":
		text := asString(part["text"])
		return dto.MediaContent{Type: dto.ContentTypeText, Text: text}, text != ""
	case "refusal":
		text := asString(part["refusal"])
		return dto.MediaContent{Type: dto.ContentTypeText, Text: text}, text != ""
	}
	if role != "user" {
		return dto.MediaContent{}, false
	}

	switch asString(part["type"]) {
	case "input_image":
		url := asString(part["image_url"])
		if nested, ok := part["image_url"].(map[string]any); ok {
			url = asString(nested["url"])
		}
		if url == "" {
			// NOTE: file_id references point at OpenAI storage and cannot be resolved here.
			return dto.MediaContent{}, false
		}
		return dto.MediaContent{
			Type:     dto.ContentTypeImageURL,
			ImageUrl: &dto.MessageImageUrl{Url: url, Detail: asString(part["detail"])},
		}, true
	case "input_file":
		data := asString(part["file_data"])
		if data == "" {
			data = asString(part["file_url"])
		}
		if data == "" {
			return dto.MediaContent{}, false
		}
		return dto.MediaContent{
			Type: dto.ContentTypeFile,
			File: &dto.MessageFile{FileName: asString(part["filename"]), FileData: data},
		}, true
	case "input_audio":
		audio, ok := part["input_audio"].(map[string]any)
		if !ok || asString(audio["data"]) == "" {
			return dto.MediaContent{}, false
		}
		return dto.MediaContent{
			Type:       dto.ContentTypeInputAudio,
			InputAudio: &dto.MessageInputAudio{Data: asString(audio["data"]), Format: asString(audio["format"])},
		}, true
	}
	return dto.MediaContent{}, false
}

// responsesContentText flattens message content to plain text for roles that only carry text.
func responsesContentText(content any) string {
	if text, ok := content.(string); ok {
		return text
	}
	var texts []string
	for _, part := range responsesContentToChatParts("assistant", content) {
		texts = append(texts, part.Text)
	}
	return strings.Join(texts, "\n")
}

// responsesOutputText renders a function_call_output payload as the single string chat tool messages carry.
func responsesOutputText(output any) string {
	switch v := output.(type) {
	case nil:
		return ""
	case string:
		return v
	case []any:
		var texts []string
		for _, raw := range v {
			if part, ok := raw.(map[string]any); ok {
				if text := asString(part["text"]); text != "" {
					texts = append(texts, text)
				}
			}
		}
		if len(texts) > 0 {
			return strings.Join(texts, "\n")
		}
	}
	encoded, err := common.Marshal(output)
	if err != nil {
		return fmt.Sprintf("%v", output)
	}
	return string(encoded)
}

func responsesToolsToChatTools(raw json.RawMessage) ([]dto.ToolCallRequest, error) {
	if isEmptyRawJSON(raw) {
		return nil, nil
	}
	var tools []map[string]any
	if err := common.Unmarshal(raw, &tools); err != nil {
		return nil, fmt.Errorf("invalid tools: %w", err)
	}

	out := make([]dto.ToolCallRequest, 0, len(tools))
	for _, tool := range tools {
		switch asString(tool["type"]) {
		case "function":
			spec := tool
			if nested, ok := tool["function"].(map[string]any); ok {
				spec = nested
			}
			name := strings.TrimSpace(asString(spec["name"]))
			if name == "" {
				continue
			}
			out = append(out, dto.ToolCallRequest{
				Type: "function",
				Function: dto.FunctionRequest{
					Name:        name,
					Description: asString(spec["description"]),
					Parameters:  spec["parameters"],
				},
			})
		case "custom":
			name := strings.TrimSpace(asString(tool["name"]))
			if name == "" {
				continue
			}
			out = append(out, dto.ToolCallRequest{
				Type: "function",
				Function: dto.FunctionRequest{
					Name:        name,
					Description: asString(tool["description"]),
					Parameters: map[string]any{
						"type": "object",
						"properties": map[string]any{
							CustomToolInputKey: map[string]any{
								"type":        "string",
								"description": customToolInputDescription(tool),
							},
						},
						"required": []string{CustomToolInputKey},
					},
				},
			})
		}
	}
	return out, nil
}

func customToolInputDescription(tool map[string]any) string {
	format, ok := tool["format"].(map[string]any)
	if !ok {
		return "Freeform tool input."
	}
	description := "Freeform tool input."
	if syntax := asString(format["syntax"]); syntax != "" {
		description = fmt.Sprintf("Freeform tool input in %s syntax.", syntax)
	}
	if definition := asString(format["definition"]); definition != "" {
		description += "\n" + definition
	}
	return description
}

func responsesToolChoiceToChat(raw json.RawMessage) any {
	switch common.GetJsonType(raw) {
	case "string":
		return common.JsonRawMessageToString(raw)
	case "object":
		var choice map[string]any
		if err := common.Unmarshal(raw, &choice); err != nil {
			return nil
		}
		switch asString(choice["type"]) {
		case "function", "custom":
			if name := asString(choice["name"]); name != "" {
				return map[string]any{"type": "function", "function": map[string]any{"name": name}}
			}
		case "allowed_tools":
			if asString(choice["mode"]) == "required" {
				return "required"
			}
			return "auto"
		}
	}
	return nil
}

func responsesTextToChatResponseFormat(raw json.RawMessage) (*dto.ResponseFormat, error) {
	if isEmptyRawJSON(raw) {
		return nil, nil
	}
	var text struct {
		Format map[string]any `json:"format"`
	}
	if err := common.Unmarshal(raw, &text); err != nil {
		return nil, fmt.Errorf("invalid text: %w", err)
	}

	switch asString(text.Format["type"]) {
	case "json_object":
		return &dto.ResponseFormat{Type: "json_object"}, nil
	case "json_schema":
		schema := map[string]any{}
		for _, key := range []string{"name", "description", "schema", "strict"} {
			if value, ok := text.Format[key]; ok {
				schema[key] = value
			}
		}
		encoded, err := common.Marshal(schema)
		if err != nil {
			return nil, fmt.Errorf("invalid text.format: %w", err)
		}
		return &dto.ResponseFormat{Type: "json_schema", JsonSchema: encoded}, nil
	}
	return nil, nil
}

func isResponsesContentPartType(itemType string) bool {
	switch itemType {
	case "input_text", "output_text", "input_image", "input_file", "input_audio":
		return true
	}
	return false
}

func responsesCallID(item map[string]any) string {
	if callID := strings.TrimSpace(asString(item["call_id"])); callID != "" {
		return callID
	}
	return strings.TrimSpace(asString(item["id"]))
}

func isEmptyRawJSON(raw json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(raw))
	return trimmed == "" || trimmed == "null"
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}
