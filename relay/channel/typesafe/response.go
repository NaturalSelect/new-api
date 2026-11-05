package typesafe

import (
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// DoResponse relays a successful System One answer unchanged and bills the
// input tokens TypeSafe reports. Output is free, so completion tokens stay 0.
func (a *Adaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (any, *types.NewAPIError) {
	if resp == nil {
		return nil, types.NewError(errors.New("typesafe adaptor: empty response"), types.ErrorCodeBadResponse)
	}
	raw, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeReadResponseBodyFailed)
	}

	request, ok := info.Request.(*dto.TypeSafeRequest)
	if !ok {
		return nil, types.NewError(fmt.Errorf("typesafe adaptor: unexpected request type %T", info.Request), types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}
	if err := validateAnswers(raw, request); err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/json"
	}
	c.Writer.Header().Set("Content-Type", contentType)
	c.Writer.WriteHeader(http.StatusOK)
	_, _ = c.Writer.Write(raw)

	return billedUsage(c, raw, info), nil
}

// validateAnswers requires an answer of the asked type for every question,
// which is what the client SDKs rely on.
func validateAnswers(raw []byte, request *dto.TypeSafeRequest) error {
	root := gjson.ParseBytes(raw)
	if model := root.Get("model"); model.Type != gjson.String || model.String() == "" {
		return errors.New("TypeSafe response is missing model or answers")
	}
	answers := root.Get("answers")
	if !answers.IsObject() {
		return errors.New("TypeSafe response is missing model or answers")
	}
	var mismatch error
	gjson.GetBytes(request.RequestJSON, "questions").ForEach(func(id, question gjson.Result) bool {
		answer := answers.Get(escapePathKey(id.String()))
		if !answer.IsObject() || answer.Get("type").String() != question.Get("type").String() {
			mismatch = errors.New("TypeSafe response is missing a matching answer for a question")
			return false
		}
		return true
	})
	return mismatch
}

// billedUsage never bills zero: when the upstream usage is missing or outside
// the supported context budget, the reserved estimate is charged instead.
func billedUsage(c *gin.Context, raw []byte, info *relaycommon.RelayInfo) *dto.Usage {
	tokens := gjson.GetBytes(raw, "usage.input_tokens")
	value := tokens.Int()
	if tokens.Type != gjson.Number || float64(value) != tokens.Float() || value < 0 || value > dto.TypeSafeLongContextInputTokens {
		value = int64(info.GetEstimatePromptTokens())
		logger.LogError(c, "TypeSafe input token usage is missing or outside the supported context budget, billing the reserved estimate")
	}
	return &dto.Usage{
		PromptTokens: int(value),
		TotalTokens:  int(value),
	}
}

// ErrorFromResponse turns a failed upstream answer into a relay error. A body
// that carries a TypeSafe "detail" is kept so WriteError can return it as is.
func ErrorFromResponse(c *gin.Context, resp *http.Response) *types.NewAPIError {
	raw, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return types.NewErrorWithStatusCode(err, types.ErrorCodeReadResponseBodyFailed, resp.StatusCode)
	}

	message := fmt.Sprintf("bad response status code %d", resp.StatusCode)
	if detail := gjson.GetBytes(raw, "detail"); detail.Exists() {
		common.SetContextKey(c, constant.ContextKeyTypeSafeUpstreamError, raw)
		if detail.Type == gjson.String {
			message = detail.String()
		} else {
			message = detail.Raw
		}
	} else {
		logger.LogError(c, fmt.Sprintf("bad response status code %d, body: %s", resp.StatusCode, common.LocalLogPreview(string(raw))))
	}
	return types.NewErrorWithStatusCode(errors.New(message), types.ErrorCodeBadResponseStatusCode, resp.StatusCode)
}

// ClearUpstreamError drops the error kept from an earlier attempt so a retry
// that fails before reaching TypeSafe does not return a stale body.
func ClearUpstreamError(c *gin.Context) {
	common.SetContextKey(c, constant.ContextKeyTypeSafeUpstreamError, nil)
}

// WriteError answers in TypeSafe's own error shape, {"detail": ...}.
func WriteError(c *gin.Context, apiErr *types.NewAPIError) {
	c.Writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	if raw, ok := common.GetContextKeyType[[]byte](c, constant.ContextKeyTypeSafeUpstreamError); ok && len(raw) > 0 && apiErr.GetErrorCode() == types.ErrorCodeBadResponseStatusCode {
		c.Writer.WriteHeader(apiErr.StatusCode)
		_, _ = c.Writer.Write(raw)
		return
	}
	c.JSON(apiErr.StatusCode, gin.H{"detail": apiErr.Error()})
}

func escapePathKey(key string) string {
	var escaped []byte
	for i := 0; i < len(key); i++ {
		switch key[i] {
		case '.', '*', '?', '|', '#', '@', '\\', '!', '=', '<', '>', '%':
			escaped = append(escaped, '\\')
		}
		escaped = append(escaped, key[i])
	}
	return string(escaped)
}
