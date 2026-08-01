package helper

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// ResolveAutoEffort rewrites a request's reasoning effort from the literal
// value "auto" to the highest-IQ effort level known for upstreamModelName,
// based on the in-memory model intelligence score cache. It is a no-op when
// the feature is disabled, the request's effort isn't "auto", the model has
// no cached scores, or every scored effort level for the model is excluded
// by the admin's disabled-effort list.
func ResolveAutoEffort(request dto.Request, upstreamModelName string) {
	if !operation_setting.IsAutoEffortEnabled() {
		return
	}

	if !strings.EqualFold(getRequestEffort(request), "auto") {
		return
	}

	resolved, ok := resolveHighestIQEffort(upstreamModelName)
	if !ok {
		return
	}

	setRequestEffort(request, resolved)
}

func getRequestEffort(request dto.Request) string {
	switch r := request.(type) {
	case *dto.GeneralOpenAIRequest:
		return r.ReasoningEffort
	case *dto.OpenAIResponsesRequest:
		if r.Reasoning == nil {
			return ""
		}
		return r.Reasoning.Effort
	case *dto.ClaudeRequest:
		return r.GetEfforts()
	default:
		return ""
	}
}

func setRequestEffort(request dto.Request, effort string) {
	switch r := request.(type) {
	case *dto.GeneralOpenAIRequest:
		r.ReasoningEffort = effort
	case *dto.OpenAIResponsesRequest:
		if r.Reasoning == nil {
			r.Reasoning = &dto.Reasoning{}
		}
		r.Reasoning.Effort = effort
	case *dto.ClaudeRequest:
		setClaudeOutputConfigEffort(r, effort)
	}
}

// setClaudeOutputConfigEffort patches only the "effort" key of the client's
// output_config, preserving any other fields it may already contain instead
// of overwriting the whole raw JSON object.
func setClaudeOutputConfigEffort(r *dto.ClaudeRequest, effort string) {
	outputConfig := make(map[string]any)
	if len(r.OutputConfig) > 0 {
		_ = common.Unmarshal(r.OutputConfig, &outputConfig)
	}
	outputConfig["effort"] = effort
	if merged, err := common.Marshal(outputConfig); err == nil {
		r.OutputConfig = merged
	}
}

// resolveHighestIQEffort returns the effort level with the highest IQ score
// for modelName among the effort levels not excluded by admin configuration.
func resolveHighestIQEffort(modelName string) (string, bool) {
	scores, _ := service.GetIntelligenceScores()
	if len(scores) == 0 {
		return "", false
	}

	disabledEfforts := operation_setting.GetDisabledAutoEfforts()
	disabled := make(map[string]struct{}, len(disabledEfforts))
	for _, effort := range disabledEfforts {
		disabled[strings.ToLower(effort)] = struct{}{}
	}

	best := ""
	bestIQ := 0.0
	found := false
	for _, score := range scores {
		if score.Model != modelName {
			continue
		}
		if _, excluded := disabled[strings.ToLower(score.Effort)]; excluded {
			continue
		}
		if !found || score.IQ > bestIQ {
			best = score.Effort
			bestIQ = score.IQ
			found = true
		}
	}
	return best, found
}
