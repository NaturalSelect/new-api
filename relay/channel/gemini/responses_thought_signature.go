package gemini

import (
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
)

const (
	includeEncryptedReasoning = "reasoning.encrypted_content"
	// NOTE: encrypted_content values issued by this channel start with one of these markers, so content from other providers is never mistaken for a Gemini signature.
	// The first marks the signature of the tool call after the item, the second that of the message before it.
	callSignaturePrefix    = "gemini-sig-v1:"
	messageSignaturePrefix = "gemini-msg-sig-v1:"
)

// encodeEncryptedReasoning binds a Gemini thought signature to the model that issued it.
func encodeEncryptedReasoning(prefix, model, signature string) string {
	return prefix + model + ":" + signature
}

// decodeEncryptedReasoning returns the signature only when it was issued by the same model.
// NOTE: Gemini rejects signatures from another model, so those fall back to the placeholder signature instead.
func decodeEncryptedReasoning(model, encrypted string) (signature string, forMessage, ok bool) {
	rest, ok := strings.CutPrefix(encrypted, callSignaturePrefix)
	if !ok {
		if rest, ok = strings.CutPrefix(encrypted, messageSignaturePrefix); !ok {
			return "", false, false
		}
		forMessage = true
	}
	sep := strings.LastIndex(rest, ":")
	if sep < 0 {
		return "", false, false
	}
	issuer, signature := rest[:sep], rest[sep+1:]
	if signature == "" || !strings.EqualFold(issuer, model) {
		return "", false, false
	}
	return signature, forMessage, true
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
