package operation_setting

import "strings"

// ContentPolicyWarningKeywords are substrings matched (case-insensitively) against
// upstream error messages to detect content-policy rejections — a provider refusing
// a request because the prompt or generated output was flagged as violating its
// usage/safety policy. Wording differs by provider, so the list is kept
// admin-editable like AutomaticDisableKeywords rather than hardcoded.
var ContentPolicyWarningKeywords = []string{
	"your prompt was flagged as potentially violating our usage policy",
	"invalid prompt",
	"content management policy",
	"responsibleaipolicyviolation",
	"blocked by content filtering policy",
	"blocked due to safety",
	"prohibited_content",
	"your request was rejected by the safety system. if you believe this is an error, contact us at",
}

func ContentPolicyWarningKeywordsToString() string {
	return strings.Join(ContentPolicyWarningKeywords, "\n")
}

func ContentPolicyWarningKeywordsFromString(s string) {
	ContentPolicyWarningKeywords = []string{}
	ak := strings.Split(s, "\n")
	for _, k := range ak {
		k = strings.TrimSpace(k)
		k = strings.ToLower(k)
		if k != "" {
			ContentPolicyWarningKeywords = append(ContentPolicyWarningKeywords, k)
		}
	}
}
