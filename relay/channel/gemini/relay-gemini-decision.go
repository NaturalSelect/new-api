package gemini

import (
	"errors"
	"io"
	"net/http"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// GeminiDecisionHandler relays a successful Decisions answer unchanged and
// bills the input tokens Google reports. Output is always 0 tokens.
func GeminiDecisionHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}

	logger.LogDebug(c, "Gemini decision response body: %s", responseBody)

	if !gjson.GetBytes(responseBody, "answers").IsObject() {
		return nil, types.NewOpenAIError(errors.New("gemini decision response is missing answers"), types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}

	usage := buildDecisionUsage(c, responseBody, info)
	service.IOCopyBytesGracefully(c, resp, responseBody)
	return usage, nil
}

// NOTE: Billing never drops to zero because of a missing field, so the estimate is charged when Google reports no usable input token count.
func buildDecisionUsage(c *gin.Context, responseBody []byte, info *relaycommon.RelayInfo) *dto.Usage {
	tokens := gjson.GetBytes(responseBody, "usage.total_input_tokens")
	value := tokens.Int()
	if tokens.Type != gjson.Number || float64(value) != tokens.Float() || value < 0 {
		value = int64(info.GetEstimatePromptTokens())
		logger.LogError(c, "Gemini decision input token usage is missing or invalid, billing the estimate")
	}
	return &dto.Usage{
		PromptTokens: int(value),
		TotalTokens:  int(value),
	}
}
