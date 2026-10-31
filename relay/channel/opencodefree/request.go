package opencodefree

import (
	"errors"
	"fmt"
	"maps"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"

	"github.com/samber/lo"
)

// NOTE: aliases maps the upstream alias back to the original client tool name.
type requestPlan struct {
	aliases         map[string]string
	clientNames     []string
	reservedNames   []string
	toolsDisabled   bool
	thinking        bool
	expectedChoices int
}

func (p *requestPlan) candidates() []string {
	out := make([]string, 0, len(p.clientNames)+len(p.reservedNames))
	out = append(out, p.clientNames...)
	return append(out, p.reservedNames...)
}

func gateTool() dto.ToolCallRequest {
	return dto.ToolCallRequest{
		Type: "function",
		Function: dto.FunctionRequest{
			Name:        gateToolName,
			Description: gateToolDescription,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":   map[string]any{"type": "string", "description": "File or directory to read"},
					"offset": map[string]any{"type": "integer", "minimum": 0, "description": "Starting line or directory entry (1-based)"},
					"limit":  map[string]any{"type": "integer", "minimum": 0, "description": "Maximum lines or directory entries to read"},
				},
				"required":             []any{"path"},
				"additionalProperties": false,
			},
		},
	}
}

// NOTE: The request is rewritten in place; callers pass the per-attempt deep copy made by the relay handler.
func buildUpstreamRequest(request *dto.GeneralOpenAIRequest) (*requestPlan, error) {
	if len(request.Messages) == 0 {
		return nil, errors.New("opencode free channel: messages must be a non-empty array")
	}
	if !IsAllowedModel(request.Model) {
		return nil, fmt.Errorf("opencode free channel: only big-pickle and models ending in -free are allowed, got %q", request.Model)
	}

	toolsDisabled := request.ToolChoice == "none" || len(request.Tools) == 0
	clientTools := request.Tools
	if toolsDisabled {
		clientTools = nil
	}
	tools, names := aliasTools(clientTools)

	// NOTE: Explicit tool_choice none is rejected upstream, so disabled tools are expressed by sending only
	// the gate tool and rejecting any tool call in the response layer.
	request.Tools = append(tools, gateTool())
	if toolsDisabled || request.ToolChoice == nil {
		request.ToolChoice = nil
	} else {
		request.ToolChoice = renameToolChoice(request.ToolChoice, names)
	}
	if err := rewriteMessages(request.Messages, names); err != nil {
		return nil, err
	}

	plan := &requestPlan{
		aliases:         make(map[string]string, len(names)),
		clientNames:     make([]string, 0, len(names)),
		reservedNames:   []string{gateToolName},
		toolsDisabled:   toolsDisabled,
		thinking:        request.ReasoningEffort != "none",
		expectedChoices: 1,
	}
	for original, alias := range names {
		plan.aliases[alias] = original
		plan.clientNames = append(plan.clientNames, alias)
	}
	if n := lo.FromPtr(request.N); n > 0 {
		plan.expectedChoices = n
	}

	request.Stream = lo.ToPtr(true)
	request.StreamOptions = &dto.StreamOptions{IncludeUsage: true}
	if request.MaxTokens == nil && request.MaxCompletionTokens == nil {
		request.MaxTokens = lo.ToPtr(uint(defaultMaxTokens))
	}
	return plan, nil
}

// NOTE: Every client tool is renamed so none of them can collide with the reserved gate tool.
func aliasTools(clientTools []dto.ToolCallRequest) ([]dto.ToolCallRequest, map[string]string) {
	present := make(map[string]bool, len(clientTools))
	for _, tool := range clientTools {
		if tool.Function.Name != "" {
			present[tool.Function.Name] = true
		}
	}
	names := make(map[string]string, len(clientTools))
	tools := make([]dto.ToolCallRequest, 0, len(clientTools)+1)
	for _, tool := range clientTools {
		name := tool.Function.Name
		if name == "" {
			continue
		}
		if _, seen := names[name]; seen {
			continue
		}
		base := clampString(aliasPrefix+sanitizeToolName(name), maxToolNameLength-4)
		alias := base
		for suffix := 1; present[alias]; suffix++ {
			tail := fmt.Sprintf("_%d", suffix)
			alias = clampString(base, maxToolNameLength-len(tail)) + tail
		}
		present[alias] = true
		names[name] = alias

		renamed := tool
		renamed.Function.Name = alias
		renamed.Function.Description = fmt.Sprintf("Client tool %q; use %q for the client's %q operation. %s", name, alias, name, tool.Function.Description)
		tools = append(tools, renamed)
	}
	return tools, names
}

func sanitizeToolName(name string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			return r
		}
		return '_'
	}, name)
}

func clampString(value string, length int) string {
	if length >= len(value) {
		return value
	}
	return value[:length]
}

// NOTE: GLM rejects a name field on role:"tool" messages outright, so it is always dropped.
func rewriteMessages(messages []dto.Message, names map[string]string) error {
	for i := range messages {
		message := &messages[i]
		switch message.Role {
		case "tool":
			message.Name = nil
		case "function":
			if message.Name != nil {
				if alias, found := names[*message.Name]; found {
					message.Name = lo.ToPtr(alias)
				}
			}
		default:
			if len(names) == 0 || len(message.ToolCalls) == 0 {
				continue
			}
			renamed, err := renameToolCalls(message.ToolCalls, names)
			if err != nil {
				return err
			}
			message.ToolCalls = renamed
		}
	}
	return nil
}

func renameToolCalls(raw []byte, names map[string]string) ([]byte, error) {
	var calls []map[string]any
	if err := common.Unmarshal(raw, &calls); err != nil {
		return nil, fmt.Errorf("opencode free channel: invalid tool_calls in message: %w", err)
	}
	for i, call := range calls {
		calls[i] = renameToolRef(call, names)
	}
	return common.Marshal(calls)
}

func renameToolChoice(choice any, names map[string]string) any {
	ref, ok := choice.(map[string]any)
	if !ok {
		return choice
	}
	out := renameToolRef(ref, names)
	if list, ok := ref["tools"].([]any); ok {
		out["tools"] = renameToolRefs(list, names)
	}
	if allowed, ok := ref["allowed_tools"].(map[string]any); ok {
		allowedOut := maps.Clone(allowed)
		if list, ok := allowed["tools"].([]any); ok {
			allowedOut["tools"] = renameToolRefs(list, names)
		}
		out["allowed_tools"] = allowedOut
	}
	return out
}

func renameToolRefs(list []any, names map[string]string) []any {
	out := make([]any, len(list))
	for i, item := range list {
		if ref, ok := item.(map[string]any); ok {
			out[i] = renameToolRef(ref, names)
			continue
		}
		out[i] = item
	}
	return out
}

// NOTE: A tool reference names its target either as function.name or as a flat name.
func renameToolRef(ref map[string]any, names map[string]string) map[string]any {
	out := maps.Clone(ref)
	if fn, ok := ref["function"].(map[string]any); ok {
		if name, _ := fn["name"].(string); name != "" {
			if alias, found := names[name]; found {
				fnOut := maps.Clone(fn)
				fnOut["name"] = alias
				out["function"] = fnOut
			}
		}
	}
	if name, _ := ref["name"].(string); name != "" {
		if alias, found := names[name]; found {
			out["name"] = alias
		}
	}
	return out
}
