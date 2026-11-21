package gemini

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/service/openaicompat"
)

const (
	includeEncryptedReasoning = "reasoning.encrypted_content"
	// NOTE: encrypted_content values issued by this channel start with this marker, so content from other providers is never mistaken for a Gemini signature.
	encryptedReasoningPrefix = "gemini-sig-v1:"
)

// encodeEncryptedReasoning binds a Gemini thought signature to the model that issued it.
func encodeEncryptedReasoning(model, signature string) string {
	return encryptedReasoningPrefix + model + ":" + signature
}

// decodeEncryptedReasoning returns the signature only when it was issued by the same model.
// NOTE: Gemini rejects signatures from another model, so those fall back to the placeholder signature instead.
func decodeEncryptedReasoning(model, encrypted string) (string, bool) {
	rest, ok := strings.CutPrefix(encrypted, encryptedReasoningPrefix)
	if !ok {
		return "", false
	}
	sep := strings.LastIndex(rest, ":")
	if sep < 0 {
		return "", false
	}
	issuer, signature := rest[:sep], rest[sep+1:]
	if signature == "" || !strings.EqualFold(issuer, model) {
		return "", false
	}
	return signature, true
}

func geminiPartSignature(part *dto.GeminiPart) string {
	if len(part.ThoughtSignature) == 0 {
		return ""
	}
	var signature string
	if err := common.Unmarshal(part.ThoughtSignature, &signature); err != nil {
		return ""
	}
	return signature
}

func responsesIncludesEncryptedReasoning(request *dto.OpenAIResponsesRequest) bool {
	if request == nil || len(request.Include) == 0 {
		return false
	}
	var include []string
	if err := common.Unmarshal(request.Include, &include); err != nil {
		return false
	}
	return slices.Contains(include, includeEncryptedReasoning)
}

// restoreResponsesThoughtSignatures moves the signatures a Responses client sent back inside
// reasoning items onto the tool calls they belong to, where the Gemini converter picks them up.
func restoreResponsesThoughtSignatures(request *dto.GeneralOpenAIRequest, input json.RawMessage, model string) {
	signatures := openaicompat.ResponsesCallSignatures(input, func(encrypted string) (string, bool) {
		return decodeEncryptedReasoning(model, encrypted)
	})
	if len(signatures) == 0 {
		return
	}
	for i := range request.Messages {
		msg := &request.Messages[i]
		if msg.Role != "assistant" {
			continue
		}
		calls := msg.ParseToolCalls()
		restored := false
		for j := range calls {
			if signature := signatures[calls[j].ID]; signature != "" {
				calls[j].ExtraContent = openaicompat.ExtraContentFromThoughtSignature(signature)
				restored = true
			}
		}
		if restored {
			msg.SetToolCalls(calls)
		}
	}
}
