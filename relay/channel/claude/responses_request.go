package claude

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/openaicompat"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/setting/reasoning"
	"github.com/gin-gonic/gin"
)

func RequestResponses2ClaudeMessage(c *gin.Context, info *relaycommon.RelayInfo, req dto.OpenAIResponsesRequest) (*dto.ClaudeRequest, error) {
	if req.Model == "" {
		return nil, errors.New("model is required")
	}
	if req.PreviousResponseID != "" {
		return nil, errors.New("previous_response_id is not supported by this channel, send the full conversation in input")
	}
	if !isEmptyResponsesRawJSON(req.Conversation) {
		return nil, errors.New("conversation is not supported by this channel, send the full conversation in input")
	}

	claudeTools, err := convertResponsesToolsToClaude(req.Tools)
	if err != nil {
		return nil, err
	}

	claudeReq := &dto.ClaudeRequest{
		Model:       req.Model,
		Temperature: req.Temperature,
		Tools:       claudeTools,
		Metadata:    req.Metadata,
	}
	explicitMaxTokens := false
	if req.MaxOutputTokens != nil && *req.MaxOutputTokens > 0 {
		claudeReq.MaxTokens = common.GetPointer(*req.MaxOutputTokens)
		explicitMaxTokens = true
	} else {
		defaultMaxTokens := uint(model_setting.GetClaudeSettings().GetDefaultMaxTokens(req.Model))
		claudeReq.MaxTokens = &defaultMaxTokens
	}
	if req.TopP != nil {
		claudeReq.TopP = common.GetPointer(*req.TopP)
	}
	if req.Stream != nil && *req.Stream {
		claudeReq.Stream = common.GetPointer(true)
	}

	var parallelToolCalls *bool
	if !isEmptyResponsesRawJSON(req.ParallelToolCalls) {
		var parallel bool
		if err := common.Unmarshal(req.ParallelToolCalls, &parallel); err == nil {
			parallelToolCalls = &parallel
		}
	}
	if len(claudeTools) > 0 {
		rawToolChoice := convertResponsesToolChoiceToOpenAI(req.ToolChoice)
		if rawToolChoice != nil || parallelToolCalls != nil {
			if tc := mapToolChoice(rawToolChoice, parallelToolCalls); tc != nil {
				claudeReq.ToolChoice = tc
			}
		}
	}

	var effort, summary string
	if req.Reasoning != nil {
		effort = req.Reasoning.Effort
		summary = req.Reasoning.Summary
	}
	applyClaudeThinkingAndSampling(claudeReq, effort, summary, explicitMaxTokens)

	builder := &claudeResponsesInputBuilder{
		c:              c,
		thinkingActive: isClaudeThinkingActive(claudeReq),
		targetModel:    claudeReq.Model,
		callIDs:        make(map[string]struct{}),
	}
	if instructions := common.JsonRawMessageToString(req.Instructions); strings.TrimSpace(instructions) != "" {
		builder.appendSystemText(instructions)
	}
	if err := builder.addInput(req.Input); err != nil {
		return nil, err
	}
	builder.flush()

	builder.messages = stripTrailingClaudeThinkingBlocks(builder.messages)
	if len(builder.messages) == 0 {
		if len(builder.system) == 0 {
			return nil, errors.New("input is required")
		}
		placeholder := "..."
		builder.messages = []dto.ClaudeMessage{{
			Role: "user",
			Content: []dto.ClaudeMediaMessage{{
				Type: "text",
				Text: &placeholder,
			}},
		}}
	} else if builder.messages[0].Role != "user" {
		placeholder := "..."
		builder.messages = append([]dto.ClaudeMessage{{
			Role: "user",
			Content: []dto.ClaudeMediaMessage{{
				Type: "text",
				Text: &placeholder,
			}},
		}}, builder.messages...)
	}

	if len(builder.system) > 0 {
		claudeReq.System = builder.system
	}
	claudeReq.Messages = builder.messages
	service.EnsureClaudeMetadataUserIDFromPromptCacheKey(claudeReq, common.JsonRawMessageToString(req.PromptCacheKey))

	ensureClaudeThinkingReplayable(c, claudeReq)

	if info != nil {
		if isClaudeThinkingActive(claudeReq) {
			if strings.TrimSpace(effort) != "" {
				info.ReasoningEffort = strings.ToLower(strings.TrimSpace(effort))
			} else {
				info.ReasoningEffort = claudeReq.GetEfforts()
			}
		} else {
			info.ReasoningEffort = ""
		}
	}
	if c != nil {
		common.SetContextKey(c, constant.ContextKeyClaudeSignatureModel, claudeReq.Model)
	}
	return claudeReq, nil
}

func isClaudeThinkingActive(req *dto.ClaudeRequest) bool {
	if req == nil || req.Thinking == nil {
		return false
	}
	return !strings.EqualFold(strings.TrimSpace(req.Thinking.Type), "disabled")
}

func isForcedClaudeToolChoice(toolChoice any) bool {
	if toolChoice == nil {
		return false
	}
	switch tc := toolChoice.(type) {
	case *dto.ClaudeToolChoice:
		return tc != nil && (tc.Type == "any" || tc.Type == "tool")
	case dto.ClaudeToolChoice:
		return tc.Type == "any" || tc.Type == "tool"
	case map[string]any:
		t, _ := tc["type"].(string)
		return t == "any" || t == "tool"
	default:
		return false
	}
}

func applyClaudeThinkingAndSampling(claudeReq *dto.ClaudeRequest, effort, summary string, explicitMaxTokens bool) {
	if claudeReq == nil {
		return
	}

	if baseModel, effortLevel, ok := reasoning.TrimEffortSuffix(claudeReq.Model); ok && effortLevel != "" && isClaudeAdaptiveThinkingModel(baseModel) {
		claudeReq.Model = baseModel
		applyClaudeAdaptiveEffort(claudeReq, effortLevel)
	} else if model_setting.GetClaudeSettings().ThinkingAdapterEnabled && strings.HasSuffix(claudeReq.Model, "-thinking") {
		trimmedModel := strings.TrimSuffix(claudeReq.Model, "-thinking")
		if isClaudeOpus47Model(trimmedModel) {
			claudeReq.Thinking = &dto.Thinking{Type: "adaptive", Display: "summarized"}
			setClaudeOutputConfigField(claudeReq, "effort", "high")
		} else {
			if claudeReq.MaxTokens == nil || *claudeReq.MaxTokens < 1280 {
				claudeReq.MaxTokens = common.GetPointer[uint](1280)
			}
			budget := int(float64(*claudeReq.MaxTokens) * model_setting.GetClaudeSettings().ThinkingAdapterBudgetTokensPercentage)
			if budget < 1024 {
				budget = 1024
			}
			claudeReq.Thinking = &dto.Thinking{
				Type:         "enabled",
				BudgetTokens: common.GetPointer(budget),
			}
		}
		if !model_setting.ShouldPreserveThinkingSuffix(claudeReq.Model) {
			claudeReq.Model = trimmedModel
		}
	}

	if normalizedEffort := strings.ToLower(strings.TrimSpace(effort)); normalizedEffort != "" {
		applyClaudeReasoningEffort(claudeReq, normalizedEffort, explicitMaxTokens)
	}

	applyClaudeThinkingSummary(claudeReq, summary, explicitMaxTokens)

	// NOTE: Anthropic rejects thinking when tool_choice forces tool execution (any/tool).
	if isClaudeThinkingActive(claudeReq) && isForcedClaudeToolChoice(claudeReq.ToolChoice) {
		claudeReq.Thinking = nil
		deleteClaudeOutputConfigField(claudeReq, "effort")
	}

	normalizeClaudeSampling(claudeReq)
}

func applyClaudeAdaptiveEffort(claudeReq *dto.ClaudeRequest, effort string) {
	switch strings.ToLower(strings.TrimSpace(effort)) {
	case "none":
		claudeReq.Thinking = &dto.Thinking{Type: "disabled"}
		deleteClaudeOutputConfigField(claudeReq, "effort")
	case "auto":
		claudeReq.Thinking = &dto.Thinking{Type: "adaptive"}
		deleteClaudeOutputConfigField(claudeReq, "effort")
	case "minimal":
		claudeReq.Thinking = &dto.Thinking{Type: "adaptive"}
		setClaudeOutputConfigField(claudeReq, "effort", "low")
	case "low", "medium", "high", "max":
		claudeReq.Thinking = &dto.Thinking{Type: "adaptive"}
		setClaudeOutputConfigField(claudeReq, "effort", strings.ToLower(strings.TrimSpace(effort)))
	case "xhigh":
		mapped := "max"
		if isClaudeOpus47Model(claudeReq.Model) {
			mapped = "xhigh"
		}
		claudeReq.Thinking = &dto.Thinking{Type: "adaptive"}
		setClaudeOutputConfigField(claudeReq, "effort", mapped)
	}
}

func applyClaudeReasoningEffort(claudeReq *dto.ClaudeRequest, effort string, explicitMaxTokens bool) {
	if isClaudeAdaptiveThinkingModel(claudeReq.Model) {
		applyClaudeAdaptiveEffort(claudeReq, effort)
		return
	}

	if effort == "none" {
		claudeReq.Thinking = &dto.Thinking{Type: "disabled"}
		return
	}

	var budget int
	switch effort {
	case "minimal":
		budget = 1024
	case "low":
		budget = 1280
	case "medium":
		budget = 2048
	case "high", "auto":
		budget = 4096
	case "xhigh":
		budget = 16384
	case "max":
		budget = 32768
	default:
		return
	}

	budget = fitClaudeThinkingBudget(claudeReq, budget, explicitMaxTokens)
	claudeReq.Thinking = &dto.Thinking{
		Type:         "enabled",
		BudgetTokens: common.GetPointer(budget),
	}
}

func fitClaudeThinkingBudget(claudeReq *dto.ClaudeRequest, budget int, explicitMaxTokens bool) int {
	if budget < 1024 {
		budget = 1024
	}
	if claudeReq.MaxTokens == nil || *claudeReq.MaxTokens == 0 {
		claudeReq.MaxTokens = common.GetPointer(uint(budget + 1024))
		return budget
	}
	if *claudeReq.MaxTokens <= uint(budget) {
		if !explicitMaxTokens {
			claudeReq.MaxTokens = common.GetPointer(uint(budget + 1024))
		} else if *claudeReq.MaxTokens > 1024 {
			budget = int(*claudeReq.MaxTokens) - 1
		} else {
			claudeReq.MaxTokens = common.GetPointer[uint](1280)
			budget = 1024
		}
	}
	return budget
}

func applyClaudeThinkingSummary(claudeReq *dto.ClaudeRequest, summary string, explicitMaxTokens bool) {
	var display string
	enableSummary := false
	switch strings.ToLower(strings.TrimSpace(summary)) {
	case "none":
		display = "omitted"
	case "auto", "concise", "detailed":
		display = "summarized"
		enableSummary = true
	}

	if enableSummary && claudeReq.Thinking == nil {
		if isClaudeAdaptiveThinkingModel(claudeReq.Model) {
			claudeReq.Thinking = &dto.Thinking{Type: "adaptive"}
		} else {
			budget := fitClaudeThinkingBudget(claudeReq, 2048, explicitMaxTokens)
			claudeReq.Thinking = &dto.Thinking{
				Type:         "enabled",
				BudgetTokens: common.GetPointer(budget),
			}
		}
	}

	if !isClaudeThinkingActive(claudeReq) {
		return
	}
	if display != "" {
		claudeReq.Thinking.Display = display
	} else if isClaudeOpus47Model(claudeReq.Model) && claudeReq.Thinking.Display == "" {
		claudeReq.Thinking.Display = "summarized"
	}
}

func normalizeClaudeSampling(claudeReq *dto.ClaudeRequest) {
	if isClaudeOpus47Model(claudeReq.Model) {
		claudeReq.Temperature = nil
		claudeReq.TopP = nil
		claudeReq.TopK = nil
		return
	}
	if isClaudeThinkingActive(claudeReq) {
		claudeReq.TopP = nil
		claudeReq.TopK = nil
		if claudeReq.Temperature != nil && *claudeReq.Temperature != 1.0 {
			claudeReq.Temperature = nil
		}
	}
}

func setClaudeOutputConfigField(claudeReq *dto.ClaudeRequest, key string, val any) {
	obj := make(map[string]any)
	if len(claudeReq.OutputConfig) > 0 {
		_ = common.Unmarshal(claudeReq.OutputConfig, &obj)
	}
	obj[key] = val
	if encoded, err := common.Marshal(obj); err == nil {
		claudeReq.OutputConfig = encoded
	}
}

func deleteClaudeOutputConfigField(claudeReq *dto.ClaudeRequest, key string) {
	if len(claudeReq.OutputConfig) == 0 {
		return
	}
	obj := make(map[string]any)
	if err := common.Unmarshal(claudeReq.OutputConfig, &obj); err != nil {
		return
	}
	delete(obj, key)
	if len(obj) == 0 {
		claudeReq.OutputConfig = nil
		return
	}
	if encoded, err := common.Marshal(obj); err == nil {
		claudeReq.OutputConfig = encoded
	}
}

type claudeResponsesInputBuilder struct {
	c              *gin.Context
	thinkingActive bool
	targetModel    string

	system        []dto.ClaudeMediaMessage
	messages      []dto.ClaudeMessage
	pendingRole   string
	pendingBlocks []dto.ClaudeMediaMessage
	callIDs       map[string]struct{}
}

func (b *claudeResponsesInputBuilder) appendSystemText(text string) {
	if text == "" {
		return
	}
	b.system = append(b.system, dto.ClaudeMediaMessage{
		Type: "text",
		Text: common.GetPointer(text),
	})
}

func (b *claudeResponsesInputBuilder) flush() {
	if b.pendingRole == "" || len(b.pendingBlocks) == 0 {
		b.pendingRole = ""
		b.pendingBlocks = nil
		return
	}
	b.messages = append(b.messages, dto.ClaudeMessage{
		Role:    b.pendingRole,
		Content: b.pendingBlocks,
	})
	b.pendingRole = ""
	b.pendingBlocks = nil
}

func (b *claudeResponsesInputBuilder) switchRole(role string) {
	if b.pendingRole != "" && b.pendingRole != role {
		b.flush()
	}
	b.pendingRole = role
}

func (b *claudeResponsesInputBuilder) addInput(raw json.RawMessage) error {
	switch common.GetJsonType(raw) {
	case "string":
		var text string
		if err := common.Unmarshal(raw, &text); err != nil {
			return fmt.Errorf("invalid input: %w", err)
		}
		if text != "" {
			b.switchRole("user")
			b.pendingBlocks = append(b.pendingBlocks, dto.ClaudeMediaMessage{
				Type: "text",
				Text: common.GetPointer(text),
			})
		}
	case "array":
		var items []map[string]any
		if err := common.Unmarshal(raw, &items); err != nil {
			return fmt.Errorf("invalid input: %w", err)
		}
		for _, item := range items {
			if err := b.addItem(item); err != nil {
				return err
			}
		}
	}
	return nil
}

func (b *claudeResponsesInputBuilder) addItem(item map[string]any) error {
	itemType := responsesAsString(item["type"])
	role := responsesAsString(item["role"])
	if itemType == "" && role != "" {
		itemType = "message"
	}
	if isResponsesPartType(itemType) {
		item = map[string]any{"type": "message", "role": "user", "content": []any{item}}
		itemType = "message"
		role = "user"
	}

	switch itemType {
	case "reasoning":
		b.addReasoning(item)
	case "message":
		return b.addMessage(role, item["content"])
	case "function_call":
		b.addToolCall(item, responsesAsString(item["arguments"]))
	case "custom_tool_call":
		args, _ := common.Marshal(map[string]any{openaicompat.CustomToolInputKey: responsesAsString(item["input"])})
		b.addToolCall(item, string(args))
	case "function_call_output", "custom_tool_call_output":
		b.addToolOutput(item)
	}
	return nil
}

func (b *claudeResponsesInputBuilder) addReasoning(item map[string]any) {
	if !b.thinkingActive {
		return
	}
	block, ok := decodeClaudeThinkingBlock(responsesAsString(item["encrypted_content"]))
	if !ok || !claudeModelsMatch(block.Model, b.targetModel) {
		return
	}
	claudeBlock := block.toClaudeMediaMessage()
	b.switchRole("assistant")
	// NOTE: Collapse consecutive regular thinking blocks to the latest one, matching Anthropic's turn rules;
	// redacted_thinking blocks are never collapsed.
	if claudeBlock.Type == "thinking" && len(b.pendingBlocks) > 0 && b.pendingBlocks[len(b.pendingBlocks)-1].Type == "thinking" {
		b.pendingBlocks[len(b.pendingBlocks)-1] = claudeBlock
		return
	}
	b.pendingBlocks = append(b.pendingBlocks, claudeBlock)
}

func (b *claudeResponsesInputBuilder) addMessage(role string, content any) error {
	switch strings.ToLower(role) {
	case "assistant", "model":
		role = "assistant"
	case "system", "developer":
		role = "system"
	default:
		role = "user"
	}

	if role == "system" {
		if text := strings.TrimSpace(extractResponsesPlainText(content)); text != "" {
			b.appendSystemText(text)
		}
		return nil
	}

	blocks, err := convertResponsesContentToClaudeBlocks(b.c, role, content)
	if err != nil {
		return err
	}
	if len(blocks) == 0 {
		return nil
	}
	b.switchRole(role)
	b.pendingBlocks = append(b.pendingBlocks, blocks...)
	return nil
}

func (b *claudeResponsesInputBuilder) addToolCall(item map[string]any, arguments string) {
	name := strings.TrimSpace(responsesAsString(item["name"]))
	callID := extractResponsesCallID(item)
	if name == "" || callID == "" {
		return
	}
	b.callIDs[callID] = struct{}{}

	inputObj := make(map[string]any)
	if arguments != "" {
		if err := common.UnmarshalJsonStr(arguments, &inputObj); err != nil {
			common.SysLog("tool call function arguments is not a map[string]any: " + arguments)
		}
	}
	b.switchRole("assistant")
	b.pendingBlocks = append(b.pendingBlocks, dto.ClaudeMediaMessage{
		Type:  "tool_use",
		Id:    callID,
		Name:  name,
		Input: inputObj,
	})
}

func (b *claudeResponsesInputBuilder) addToolOutput(item map[string]any) {
	callID := extractResponsesCallID(item)
	output := extractResponsesOutputText(item["output"])
	b.switchRole("user")
	if _, paired := b.callIDs[callID]; paired && callID != "" {
		b.pendingBlocks = append(b.pendingBlocks, dto.ClaudeMediaMessage{
			Type:      "tool_result",
			ToolUseId: callID,
			Content:   output,
		})
		return
	}
	// NOTE: An unpaired tool_result is rejected upstream, so preserve its text in the user turn.
	b.pendingBlocks = append(b.pendingBlocks, dto.ClaudeMediaMessage{
		Type: "text",
		Text: common.GetPointer("[tool_output_missing_call] " + output),
	})
}

func convertResponsesContentToClaudeBlocks(c *gin.Context, role string, content any) ([]dto.ClaudeMediaMessage, error) {
	switch v := content.(type) {
	case string:
		if v == "" {
			return nil, nil
		}
		return []dto.ClaudeMediaMessage{{
			Type: "text",
			Text: common.GetPointer(v),
		}}, nil
	case []any:
		blocks := make([]dto.ClaudeMediaMessage, 0, len(v))
		for _, raw := range v {
			part, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			block, ok, err := convertResponsesPartToClaudeBlock(c, role, part)
			if err != nil {
				return nil, err
			}
			if ok {
				blocks = append(blocks, block)
			}
		}
		return blocks, nil
	default:
		return nil, nil
	}
}

func convertResponsesPartToClaudeBlock(c *gin.Context, role string, part map[string]any) (dto.ClaudeMediaMessage, bool, error) {
	switch responsesAsString(part["type"]) {
	case "input_text", "output_text", "text":
		text := responsesAsString(part["text"])
		if text == "" {
			return dto.ClaudeMediaMessage{}, false, nil
		}
		return dto.ClaudeMediaMessage{Type: "text", Text: common.GetPointer(text)}, true, nil
	case "refusal":
		text := responsesAsString(part["refusal"])
		if text == "" {
			return dto.ClaudeMediaMessage{}, false, nil
		}
		return dto.ClaudeMediaMessage{Type: "text", Text: common.GetPointer(text)}, true, nil
	}

	if role != "user" {
		return dto.ClaudeMediaMessage{}, false, nil
	}

	switch responsesAsString(part["type"]) {
	case "input_image":
		url := responsesAsString(part["image_url"])
		if nested, ok := part["image_url"].(map[string]any); ok {
			url = responsesAsString(nested["url"])
		}
		if url == "" {
			return dto.ClaudeMediaMessage{}, false, nil
		}
		media := dto.MediaContent{
			Type:     dto.ContentTypeImageURL,
			ImageUrl: &dto.MessageImageUrl{Url: url, Detail: responsesAsString(part["detail"])},
		}
		source := media.ToFileSource()
		if source == nil {
			return dto.ClaudeMediaMessage{}, false, nil
		}
		base64Data, mimeType, err := service.GetBase64Data(c, source, "formatting image for Claude")
		if err != nil {
			return dto.ClaudeMediaMessage{}, false, fmt.Errorf("get file data failed: %s", err.Error())
		}
		blockType := "image"
		if strings.HasPrefix(mimeType, "application/pdf") {
			blockType = "document"
		}
		return dto.ClaudeMediaMessage{
			Type: blockType,
			Source: &dto.ClaudeMessageSource{
				Type:      "base64",
				MediaType: mimeType,
				Data:      base64Data,
			},
		}, true, nil
	case "input_file":
		data := responsesAsString(part["file_data"])
		if data == "" {
			data = responsesAsString(part["file_url"])
		}
		if data == "" {
			return dto.ClaudeMediaMessage{}, false, nil
		}
		media := dto.MediaContent{
			Type: dto.ContentTypeFile,
			File: &dto.MessageFile{FileName: responsesAsString(part["filename"]), FileData: data},
		}
		block, ok := claudeFileBlock(c, media)
		return block, ok, nil
	}
	return dto.ClaudeMediaMessage{}, false, nil
}

func convertResponsesToolsToClaude(raw json.RawMessage) ([]any, error) {
	if isEmptyResponsesRawJSON(raw) {
		return nil, nil
	}
	var tools []map[string]any
	if err := common.Unmarshal(raw, &tools); err != nil {
		return nil, fmt.Errorf("invalid tools: %w", err)
	}
	out := make([]any, 0, len(tools))
	for _, tool := range tools {
		switch responsesAsString(tool["type"]) {
		case "function":
			spec := tool
			if nested, ok := tool["function"].(map[string]any); ok {
				spec = nested
			}
			name := strings.TrimSpace(responsesAsString(spec["name"]))
			if name == "" {
				continue
			}
			schema := make(map[string]any)
			if params, ok := spec["parameters"].(map[string]any); ok {
				if params["type"] != nil {
					schema["type"] = params["type"]
				} else {
					schema["type"] = "object"
				}
				schema["properties"] = params["properties"]
				schema["required"] = params["required"]
				for k, v := range params {
					if k == "type" || k == "properties" || k == "required" {
						continue
					}
					schema[k] = v
				}
			} else {
				schema["type"] = "object"
			}
			out = append(out, &dto.Tool{
				Name:        name,
				Description: responsesAsString(spec["description"]),
				InputSchema: schema,
			})
		case "custom":
			name := strings.TrimSpace(responsesAsString(tool["name"]))
			if name == "" {
				continue
			}
			out = append(out, &dto.Tool{
				Name:        name,
				Description: responsesAsString(tool["description"]),
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						openaicompat.CustomToolInputKey: map[string]any{
							"type":        "string",
							"description": claudeCustomToolDescription(tool),
						},
					},
					"required": []string{openaicompat.CustomToolInputKey},
				},
			})
		case "web_search", "web_search_preview", "web_search_preview_2025_03_11":
			ws := &dto.ClaudeWebSearchTool{
				Type: "web_search_20250305",
				Name: "web_search",
			}
			switch responsesAsString(tool["search_context_size"]) {
			case "low":
				ws.MaxUses = WebSearchMaxUsesLow
			case "medium":
				ws.MaxUses = WebSearchMaxUsesMedium
			case "high":
				ws.MaxUses = WebSearchMaxUsesHigh
			}
			if loc, ok := tool["user_location"].(map[string]any); ok {
				ws.UserLocation = &dto.ClaudeWebSearchUserLocation{
					Type:     "approximate",
					Timezone: responsesAsString(loc["timezone"]),
					Country:  responsesAsString(loc["country"]),
					Region:   responsesAsString(loc["region"]),
					City:     responsesAsString(loc["city"]),
				}
			}
			out = append(out, ws)
		}
	}
	return out, nil
}

func claudeCustomToolDescription(tool map[string]any) string {
	format, ok := tool["format"].(map[string]any)
	if !ok {
		return "Freeform tool input."
	}
	description := "Freeform tool input."
	if syntax := responsesAsString(format["syntax"]); syntax != "" {
		description = fmt.Sprintf("Freeform tool input in %s syntax.", syntax)
	}
	if definition := responsesAsString(format["definition"]); definition != "" {
		description += "\n" + definition
	}
	return description
}

func convertResponsesToolChoiceToOpenAI(raw json.RawMessage) any {
	switch common.GetJsonType(raw) {
	case "string":
		return common.JsonRawMessageToString(raw)
	case "object":
		var choice map[string]any
		if err := common.Unmarshal(raw, &choice); err != nil {
			return nil
		}
		switch responsesAsString(choice["type"]) {
		case "function", "custom":
			if name := responsesAsString(choice["name"]); name != "" {
				return map[string]any{"type": "function", "function": map[string]any{"name": name}}
			}
		case "allowed_tools":
			if responsesAsString(choice["mode"]) == "required" {
				return "required"
			}
			return "auto"
		}
	}
	return nil
}

func stripTrailingClaudeThinkingBlocks(messages []dto.ClaudeMessage) []dto.ClaudeMessage {
	for len(messages) > 0 {
		lastIdx := len(messages) - 1
		if messages[lastIdx].Role != "assistant" {
			break
		}
		blocks, ok := messages[lastIdx].Content.([]dto.ClaudeMediaMessage)
		if !ok {
			break
		}
		end := len(blocks)
		for end > 0 && (blocks[end-1].Type == "thinking" || blocks[end-1].Type == "redacted_thinking") {
			end--
		}
		if end == len(blocks) {
			break
		}
		if end == 0 {
			messages = messages[:lastIdx]
			continue
		}
		messages[lastIdx].Content = blocks[:end]
		break
	}
	return messages
}

// NOTE: Anthropic rejects tool-use continuations with thinking enabled when an assistant message
// in the active tool loop does not start with a signed thinking or redacted_thinking block;
// disable thinking and strip replayed blocks as a graceful fallback instead of returning 400.
func ensureClaudeThinkingReplayable(c *gin.Context, claudeReq *dto.ClaudeRequest) {
	if !isClaudeThinkingActive(claudeReq) || len(claudeReq.Messages) < 2 {
		return
	}
	for i := len(claudeReq.Messages) - 1; i >= 1; i -= 2 {
		userMsg := claudeReq.Messages[i]
		prevMsg := claudeReq.Messages[i-1]
		if userMsg.Role != "user" || prevMsg.Role != "assistant" {
			return
		}
		if !claudeMessageHasBlockType(userMsg, "tool_result") || !claudeMessageHasBlockType(prevMsg, "tool_use") {
			return
		}
		if !claudeMessageStartsWithThinking(prevMsg) {
			claudeReq.Thinking = nil
			deleteClaudeOutputConfigField(claudeReq, "effort")
			stripClaudeThinkingBlocks(claudeReq)
			if c != nil {
				logger.LogInfo(c, "disabled claude thinking for responses tool loop without replayable thinking block")
			}
			return
		}
	}
}

func claudeMessageHasBlockType(msg dto.ClaudeMessage, blockType string) bool {
	blocks, ok := msg.Content.([]dto.ClaudeMediaMessage)
	if !ok {
		return false
	}
	for _, b := range blocks {
		if b.Type == blockType {
			return true
		}
	}
	return false
}

func claudeMessageStartsWithThinking(msg dto.ClaudeMessage) bool {
	blocks, ok := msg.Content.([]dto.ClaudeMediaMessage)
	if !ok || len(blocks) == 0 {
		return false
	}
	return blocks[0].Type == "thinking" || blocks[0].Type == "redacted_thinking"
}

func stripClaudeThinkingBlocks(claudeReq *dto.ClaudeRequest) {
	for i := range claudeReq.Messages {
		blocks, ok := claudeReq.Messages[i].Content.([]dto.ClaudeMediaMessage)
		if !ok {
			continue
		}
		filtered := make([]dto.ClaudeMediaMessage, 0, len(blocks))
		for _, b := range blocks {
			if b.Type == "thinking" || b.Type == "redacted_thinking" {
				continue
			}
			filtered = append(filtered, b)
		}
		if len(filtered) == 0 {
			claudeReq.Messages[i].Content = "..."
		} else {
			claudeReq.Messages[i].Content = filtered
		}
	}
}

func extractResponsesPlainText(content any) string {
	if s, ok := content.(string); ok {
		return s
	}
	parts, ok := content.([]any)
	if !ok {
		return ""
	}
	var texts []string
	for _, raw := range parts {
		part, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		switch responsesAsString(part["type"]) {
		case "input_text", "output_text", "text":
			if t := responsesAsString(part["text"]); t != "" {
				texts = append(texts, t)
			}
		case "refusal":
			if t := responsesAsString(part["refusal"]); t != "" {
				texts = append(texts, t)
			}
		}
	}
	return strings.Join(texts, "\n")
}

func extractResponsesOutputText(output any) string {
	switch v := output.(type) {
	case nil:
		return ""
	case string:
		return v
	case []any:
		var texts []string
		for _, raw := range v {
			if part, ok := raw.(map[string]any); ok {
				if text := responsesAsString(part["text"]); text != "" {
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

func extractResponsesCallID(item map[string]any) string {
	if callID := strings.TrimSpace(responsesAsString(item["call_id"])); callID != "" {
		return callID
	}
	return strings.TrimSpace(responsesAsString(item["id"]))
}

func isResponsesPartType(itemType string) bool {
	switch itemType {
	case "input_text", "output_text", "input_image", "input_file", "input_audio":
		return true
	}
	return false
}

func isEmptyResponsesRawJSON(raw json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(raw))
	return trimmed == "" || trimmed == "null"
}

func responsesAsString(v any) string {
	s, _ := v.(string)
	return s
}
