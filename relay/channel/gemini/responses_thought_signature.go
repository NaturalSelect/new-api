package gemini

import (
	"encoding/base64"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/service/openaicompat"
)

const includeEncryptedReasoning = "reasoning.encrypted_content"

// NOTE: encrypted_content values issued by this channel start with one of these markers, so content from other providers is never mistaken for a Gemini signature.
var signatureMarkers = []struct {
	prefix string
	kind   openaicompat.ReasoningSignatureKind
}{
	{"gemini-sig-v1:", openaicompat.ReasoningSignatureForCall},
	{"gemini-msg-sig-v1:", openaicompat.ReasoningSignatureForMessage},
	{"gemini-thought-sig-v1:", openaicompat.ReasoningSignatureForThought},
}

// encodeEncryptedReasoning binds a Gemini thought signature to the model that issued it.
func encodeEncryptedReasoning(kind openaicompat.ReasoningSignatureKind, model, signature string) string {
	for _, marker := range signatureMarkers {
		if marker.kind == kind {
			return marker.prefix + model + ":" + signature
		}
	}
	return ""
}

// decodeEncryptedReasoning returns the signature only when this channel issued it for the same model
// and it is well-formed base64.
// NOTE: Gemini rejects signatures from another model or with a broken encoding, so those fall back to the placeholder signature instead.
func decodeEncryptedReasoning(model, encrypted string) (string, openaicompat.ReasoningSignatureKind) {
	for _, marker := range signatureMarkers {
		rest, ok := strings.CutPrefix(encrypted, marker.prefix)
		if !ok {
			continue
		}
		sep := strings.LastIndex(rest, ":")
		if sep < 0 {
			return "", openaicompat.ReasoningSignatureNone
		}
		issuer, signature := rest[:sep], rest[sep+1:]
		if !strings.EqualFold(issuer, model) || !isBase64Signature(signature) {
			return "", openaicompat.ReasoningSignatureNone
		}
		return signature, marker.kind
	}
	return "", openaicompat.ReasoningSignatureNone
}

func isBase64Signature(signature string) bool {
	if signature == "" {
		return false
	}
	for _, encoding := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if _, err := encoding.DecodeString(signature); err == nil {
			return true
		}
	}
	return false
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
