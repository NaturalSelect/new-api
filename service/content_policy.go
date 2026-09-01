package service

import (
	"strings"

	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"
)

// MatchContentPolicyWarning reports whether err represents an upstream content-policy
// rejection — a provider refusing a request because the prompt or generated output was
// flagged as violating its usage/safety policy (e.g. OpenAI's "your prompt was flagged
// as potentially violating our usage policy"). When it matches, it also returns the
// keyword or error code that triggered the match, used as WarningLog.MatchedKeyword.
func MatchContentPolicyWarning(err *types.NewAPIError) (bool, string) {
	if err == nil {
		return false, ""
	}
	if err.GetErrorCode() == types.ErrorCodePromptBlocked {
		return true, string(types.ErrorCodePromptBlocked)
	}
	lowerMessage := strings.ToLower(err.Error())
	matched, words := AcSearch(lowerMessage, operation_setting.ContentPolicyWarningKeywords, true)
	if matched && len(words) > 0 {
		return true, words[0]
	}
	return false, ""
}
