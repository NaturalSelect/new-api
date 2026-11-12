package gemini

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newGeminiDecisionTestInfo(estimate int) *relaycommon.RelayInfo {
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeGeminiDecision,
		RelayFormat:     types.RelayFormatGeminiDecision,
		OriginModelName: "gemini-decision-1",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeGemini,
			ChannelBaseUrl:    "https://generativelanguage.googleapis.com",
			UpstreamModelName: "gemini-decision-1",
		},
	}
	info.SetEstimatePromptTokens(estimate)
	return info
}

func runGeminiDecisionHandler(t *testing.T, info *relaycommon.RelayInfo, body string) (*dto.Usage, *types.NewAPIError, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/decisions", nil)

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader([]byte(body))),
	}
	usage, apiErr := GeminiDecisionHandler(c, info, resp)
	return usage, apiErr, recorder
}

func TestGeminiDecisionHandlerBillsReportedInputTokens(t *testing.T) {
	const body = `{"answers":{"is_damaged":{"type":"noul","noul":0.99,"confidence":0.99}},"usage":{"total_input_tokens":100,"input_tokens_by_modality":[{"modality":"text","tokens":100}],"total_output_tokens":0,"total_tokens":100}}`

	usage, apiErr, recorder := runGeminiDecisionHandler(t, newGeminiDecisionTestInfo(7), body)

	require.Nil(t, apiErr)
	require.Equal(t, &dto.Usage{PromptTokens: 100, TotalTokens: 100}, usage)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.JSONEq(t, body, recorder.Body.String())
}

func TestGeminiDecisionHandlerFallsBackToEstimate(t *testing.T) {
	const answers = `"answers":{"q":{"type":"noul","noul":0.5,"confidence":0.5}}`

	tests := []struct {
		name string
		body string
	}{
		{name: "no usage", body: `{` + answers + `}`},
		{name: "usage without input tokens", body: `{` + answers + `,"usage":{"total_output_tokens":0}}`},
		{name: "string input tokens", body: `{` + answers + `,"usage":{"total_input_tokens":"100"}}`},
		{name: "fractional input tokens", body: `{` + answers + `,"usage":{"total_input_tokens":1.5}}`},
		{name: "negative input tokens", body: `{` + answers + `,"usage":{"total_input_tokens":-3}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			usage, apiErr, recorder := runGeminiDecisionHandler(t, newGeminiDecisionTestInfo(42), tt.body)

			require.Nil(t, apiErr)
			require.Equal(t, &dto.Usage{PromptTokens: 42, TotalTokens: 42}, usage)
			require.JSONEq(t, tt.body, recorder.Body.String())
		})
	}
}

func TestGeminiDecisionHandlerReportsZeroInputTokensAsZero(t *testing.T) {
	const body = `{"answers":{"q":{"type":"noul","noul":0.5,"confidence":0.5}},"usage":{"total_input_tokens":0}}`

	usage, apiErr, _ := runGeminiDecisionHandler(t, newGeminiDecisionTestInfo(42), body)

	require.Nil(t, apiErr)
	require.Equal(t, &dto.Usage{}, usage)
}

func TestGeminiDecisionHandlerRejectsResponsesWithoutAnswers(t *testing.T) {
	for name, body := range map[string]string{
		"empty object":    `{}`,
		"null answers":    `{"answers":null}`,
		"array answers":   `{"answers":[]}`,
		"not json":        `not json`,
		"only usage":      `{"usage":{"total_input_tokens":5}}`,
		"empty body":      ``,
		"string answers":  `{"answers":"yes"}`,
		"numeric answers": `{"answers":1}`,
	} {
		t.Run(name, func(t *testing.T) {
			usage, apiErr, recorder := runGeminiDecisionHandler(t, newGeminiDecisionTestInfo(42), body)

			require.Nil(t, usage)
			require.NotNil(t, apiErr)
			require.Equal(t, http.StatusInternalServerError, apiErr.StatusCode)
			require.Empty(t, recorder.Body.String())
		})
	}
}

func TestGeminiDecisionAdaptorRequestURLAndDispatch(t *testing.T) {
	info := newGeminiDecisionTestInfo(1)

	url, err := (&Adaptor{}).GetRequestURL(info)
	require.NoError(t, err)
	require.Equal(t, "https://generativelanguage.googleapis.com/v1beta/decisions", url)

	const body = `{"answers":{"q":{"type":"noul","noul":0.5,"confidence":0.5}},"usage":{"total_input_tokens":9}}`
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/decisions", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader([]byte(body))),
	}

	usage, apiErr := (&Adaptor{}).DoResponse(c, resp, info)
	require.Nil(t, apiErr)
	require.Equal(t, &dto.Usage{PromptTokens: 9, TotalTokens: 9}, usage)
	require.JSONEq(t, body, recorder.Body.String())
}
