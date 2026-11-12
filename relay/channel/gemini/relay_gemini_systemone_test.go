package gemini

import (
	"bytes"
	"encoding/base64"
	"io"
	"mime/multipart"
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

var testPNG = append([]byte("\x89PNG\r\n\x1a\n"), []byte("0123456789abcdef")...)

func newSystemOneContext(t *testing.T, contentType string, body []byte) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/systemone", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", contentType)
	return c
}

func convertSystemOne(t *testing.T, body string) (string, error) {
	t.Helper()
	request, err := dto.NewTypeSafeRequest([]byte(body), false, false)
	require.NoError(t, err)
	out, err := ConvertSystemOneToDecision(newSystemOneContext(t, "application/json", []byte(body)), request, "gemini-decision-1")
	return string(out), err
}

func TestConvertSystemOneTextAndQuestions(t *testing.T) {
	out, err := convertSystemOne(t, `{
		"model":"jev-latest",
		"state":"The mug arrived cracked.",
		"questions":{
			"damaged":{"type":"noul","instructions":"Is it damaged?","criteria":{"true":"Broken.","false":null}},
			"queue":{"type":"choice","instructions":{"task":"route"},"criteria":{"billing":"Payments.","other":null}},
			"severity":{"type":"score","instructions":"How severe?","criteria":["Cosmetic.",{"level":"blocked"}]}
		}
	}`)
	require.NoError(t, err)
	require.JSONEq(t, `{
		"model":"gemini-decision-1",
		"input":"The mug arrived cracked.",
		"questions":{
			"damaged":{"type":"noul","instructions":"Is it damaged?","criteria":{"true_description":"Broken."}},
			"queue":{"type":"choice","instructions":"{\"task\":\"route\"}","criteria":{"billing":"Payments.","other":""}},
			"severity":{"type":"score","instructions":"How severe?","criteria":["Cosmetic.","{\"level\":\"blocked\"}"]}
		}
	}`, out)
}

func TestConvertSystemOneStructuredStateKeepsMemberOrder(t *testing.T) {
	out, err := convertSystemOne(t, `{"model":"m","state":{"z":1,"a":[2,3]},"questions":{"q":{"type":"noul","instructions":"ok?"}}}`)
	require.NoError(t, err)
	require.Contains(t, out, `"input":"{\"z\":1,\"a\":[2,3]}"`)
}

func TestConvertSystemOneRejectsUnsupportedInput(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr string
	}{
		{name: "box question", body: `{"model":"m","state":"s","questions":{"q":{"type":"box","instructions":"find it"}}}`, wantErr: "box questions are not supported"},
		{name: "missing instructions", body: `{"model":"m","state":"s","questions":{"q":{"type":"noul"}}}`, wantErr: "needs instructions"},
		{name: "null instructions", body: `{"model":"m","state":"s","questions":{"q":{"type":"noul","instructions":null}}}`, wantErr: "needs instructions"},
		{name: "empty instructions", body: `{"model":"m","state":"s","questions":{"q":{"type":"noul","instructions":""}}}`, wantErr: "needs instructions"},
		{name: "choice criteria not an object", body: `{"model":"m","state":"s","questions":{"q":{"type":"choice","instructions":"i","criteria":["a"]}}}`, wantErr: "criteria must be an object"},
		{name: "score criteria not an array", body: `{"model":"m","state":"s","questions":{"q":{"type":"score","instructions":"i","criteria":{"a":"b"}}}}`, wantErr: "criteria must be an array"},
		{name: "asset in instructions", body: `{"model":"m","state":"x PH x","questions":{"q":{"type":"noul","instructions":"look at PH"}},"assets":{"PH":{"format":"png","data_base64":"AAAA"}}}`, wantErr: "assets inside questions"},
		{name: "asset in criteria", body: `{"model":"m","state":"x PH x","questions":{"q":{"type":"choice","instructions":"i","criteria":{"a":"like PH"}}},"assets":{"PH":{"format":"png","data_base64":"AAAA"}}}`, wantErr: "assets inside questions"},
		{name: "unused asset", body: `{"model":"m","state":"no placeholder","questions":{"q":{"type":"noul","instructions":"i"}},"assets":{"PH":{"format":"png","data_base64":"AAAA"}}}`, wantErr: "is not used in state"},
		{name: "unsupported asset type", body: `{"model":"m","state":"PH","questions":{"q":{"type":"noul","instructions":"i"}},"assets":{"PH":{"type":"video","format":"png","data_base64":"AAAA"}}}`, wantErr: "only image assets"},
		{name: "unsupported image format", body: `{"model":"m","state":"PH","questions":{"q":{"type":"noul","instructions":"i"}},"assets":{"PH":{"format":"gif","data_base64":"AAAA"}}}`, wantErr: "needs format png"},
		{name: "asset without data", body: `{"model":"m","state":"PH","questions":{"q":{"type":"noul","instructions":"i"}},"assets":{"PH":{"format":"png"}}}`, wantErr: "needs data_base64 or part"},
		{name: "part on a json request", body: `{"model":"m","state":"PH","questions":{"q":{"type":"noul","instructions":"i"}},"assets":{"PH":{"part":"img"}}}`, wantErr: "not multipart"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := convertSystemOne(t, tt.body)
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestConvertSystemOneInlineAssets(t *testing.T) {
	t.Run("string state is split around the placeholder", func(t *testing.T) {
		out, err := convertSystemOne(t, `{"model":"m","state":"Photo: PH-1 and PH-2 done","questions":{"q":{"type":"noul","instructions":"ok?"}},"assets":{"PH-1":{"type":"image","format":"jpg","data_base64":"QUFB"},"PH-2":{"format":"png","data_base64":"QkJC"}}}`)
		require.NoError(t, err)
		require.JSONEq(t, `{
			"model":"gemini-decision-1",
			"input":[
				{"type":"text","text":"Photo: "},
				{"type":"image","data":"QUFB","mime_type":"image/jpeg"},
				{"type":"text","text":" and "},
				{"type":"image","data":"QkJC","mime_type":"image/png"},
				{"type":"text","text":" done"}
			],
			"questions":{"q":{"type":"noul","instructions":"ok?"}}
		}`, out)
	})

	t.Run("object state is split inside its JSON text", func(t *testing.T) {
		out, err := convertSystemOne(t, `{"model":"m","state":{"photo":"PH","note":"x"},"questions":{"q":{"type":"noul","instructions":"ok?"}},"assets":{"PH":{"format":"png","data_base64":"QUFB"}}}`)
		require.NoError(t, err)
		require.JSONEq(t, `{
			"model":"gemini-decision-1",
			"input":[
				{"type":"text","text":"{\"photo\":\""},
				{"type":"image","data":"QUFB","mime_type":"image/png"},
				{"type":"text","text":"\",\"note\":\"x\"}"}
			],
			"questions":{"q":{"type":"noul","instructions":"ok?"}}
		}`, out)
	})

	t.Run("the longest placeholder wins at the same position", func(t *testing.T) {
		out, err := convertSystemOne(t, `{"model":"m","state":"A-long then A","questions":{"q":{"type":"noul","instructions":"ok?"}},"assets":{"A":{"format":"png","data_base64":"QQ=="},"A-long":{"format":"jpg","data_base64":"TA=="}}}`)
		require.NoError(t, err)
		require.JSONEq(t, `{
			"model":"gemini-decision-1",
			"input":[
				{"type":"image","data":"TA==","mime_type":"image/jpeg"},
				{"type":"text","text":" then "},
				{"type":"image","data":"QQ==","mime_type":"image/png"}
			],
			"questions":{"q":{"type":"noul","instructions":"ok?"}}
		}`, out)
	})
}

func TestConvertSystemOneMultipartAssets(t *testing.T) {
	const requestJSON = `{"model":"m","state":"See PH now","questions":{"q":{"type":"noul","instructions":"ok?"}},"assets":{"PH":{"part":"img"}}}`

	build := func(t *testing.T, partName string, data []byte) (*gin.Context, *dto.TypeSafeRequest) {
		t.Helper()
		var buf bytes.Buffer
		writer := multipart.NewWriter(&buf)
		require.NoError(t, writer.WriteField("request", requestJSON))
		if partName != "" {
			file, err := writer.CreateFormFile(partName, "a.bin")
			require.NoError(t, err)
			_, err = file.Write(data)
			require.NoError(t, err)
		}
		require.NoError(t, writer.Close())
		c := newSystemOneContext(t, writer.FormDataContentType(), buf.Bytes())
		request, err := dto.ParseTypeSafeRequest(c)
		require.NoError(t, err)
		require.True(t, request.Multipart)
		return c, request
	}

	t.Run("png part becomes an inline image", func(t *testing.T) {
		c, request := build(t, "img", testPNG)
		out, err := ConvertSystemOneToDecision(c, request, "gemini-decision-1")
		require.NoError(t, err)
		require.JSONEq(t, `{
			"model":"gemini-decision-1",
			"input":[
				{"type":"text","text":"See "},
				{"type":"image","data":"`+base64.StdEncoding.EncodeToString(testPNG)+`","mime_type":"image/png"},
				{"type":"text","text":" now"}
			],
			"questions":{"q":{"type":"noul","instructions":"ok?"}}
		}`, string(out))
	})

	t.Run("missing part", func(t *testing.T) {
		c, request := build(t, "", nil)
		_, err := ConvertSystemOneToDecision(c, request, "gemini-decision-1")
		require.ErrorContains(t, err, `image part "img" is missing`)
	})

	t.Run("part that is not an image", func(t *testing.T) {
		c, request := build(t, "img", []byte("just text, not an image"))
		_, err := ConvertSystemOneToDecision(c, request, "gemini-decision-1")
		require.ErrorContains(t, err, "must be a PNG or JPEG image")
	})
}

func newSystemOneInfo(t *testing.T, body string, estimate int) *relaycommon.RelayInfo {
	t.Helper()
	request, err := dto.NewTypeSafeRequest([]byte(body), false, false)
	require.NoError(t, err)
	info := &relaycommon.RelayInfo{
		RelayMode:   relayconstant.RelayModeTypeSafeSystemOne,
		RelayFormat: types.RelayFormatTypeSafe,
		Request:     request,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeGemini,
			ChannelBaseUrl:    "https://generativelanguage.googleapis.com",
			UpstreamModelName: "gemini-decision-1",
		},
	}
	info.SetEstimatePromptTokens(estimate)
	return info
}

func runSystemOneHandler(t *testing.T, info *relaycommon.RelayInfo, body string) (*dto.Usage, *types.NewAPIError, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/systemone", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader([]byte(body))),
	}
	usage, apiErr := SystemOneHandler(c, info, resp)
	return usage, apiErr, recorder
}

const systemOneAllTypesRequest = `{"model":"jev-latest","state":"s","questions":{
	"damaged":{"type":"noul","instructions":"Is it damaged?"},
	"queue":{"type":"choice","instructions":"Which queue?","criteria":{"billing":"","other":""}},
	"severity":{"type":"score","instructions":"How severe?","criteria":["Cosmetic.",{"level":"blocked"}]}
}}`

func TestSystemOneHandlerConvertsAnswers(t *testing.T) {
	const googleBody = `{
		"answers":{
			"severity":{"type":"score","score":1.2,"legend":{"0":"x","1":"y"},"probabilities":{"0":0.8,"1":0.2},"confidence":0.8,"extra":true},
			"queue":{"type":"choice","choice":"billing","probabilities":{"billing":0.9,"other":0.1},"confidence":0.9},
			"damaged":{"type":"noul","noul":0.97,"confidence":0.97}
		},
		"usage":{"total_input_tokens":120,"input_tokens_by_modality":[{"modality":"text","tokens":120}],"total_output_tokens":0,"total_tokens":120}
	}`

	usage, apiErr, recorder := runSystemOneHandler(t, newSystemOneInfo(t, systemOneAllTypesRequest, 7), googleBody)

	require.Nil(t, apiErr)
	require.Equal(t, &dto.Usage{PromptTokens: 120, TotalTokens: 120}, usage)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.JSONEq(t, `{
		"model":"gemini-decision-1",
		"answers":{
			"damaged":{"type":"noul","noul":0.97},
			"queue":{"type":"choice","choice":"billing","confidence":0.9,"probabilities":{"billing":0.9,"other":0.1}},
			"severity":{"type":"score","score":1.2,"confidence":0.8,"legend":{"0":"Cosmetic.","1":{"level":"blocked"}},"probabilities":{"0":0.8,"1":0.2}}
		},
		"usage":{"input_tokens":120,"output_tokens":0}
	}`, recorder.Body.String())
}

func TestSystemOneHandlerKeepsRequestQuestionOrder(t *testing.T) {
	const googleBody = `{"answers":{
		"b":{"type":"noul","noul":0.2,"confidence":0.8},
		"a":{"type":"noul","noul":0.9,"confidence":0.9}
	},"usage":{"total_input_tokens":5}}`
	info := newSystemOneInfo(t, `{"model":"m","state":"s","questions":{"a":{"type":"noul","instructions":"i"},"b":{"type":"noul","instructions":"i"}}}`, 1)

	_, apiErr, recorder := runSystemOneHandler(t, info, googleBody)

	require.Nil(t, apiErr)
	require.Less(t, bytes.Index(recorder.Body.Bytes(), []byte(`"a":`)), bytes.Index(recorder.Body.Bytes(), []byte(`"b":`)))
}

func TestSystemOneHandlerFindsQuestionIDsWithPathCharacters(t *testing.T) {
	const googleBody = `{"answers":{"is.damaged*":{"type":"noul","noul":0.5,"confidence":0.5}},"usage":{"total_input_tokens":5}}`
	info := newSystemOneInfo(t, `{"model":"m","state":"s","questions":{"is.damaged*":{"type":"noul","instructions":"i"}}}`, 1)

	_, apiErr, recorder := runSystemOneHandler(t, info, googleBody)

	require.Nil(t, apiErr)
	require.JSONEq(t, `{"model":"gemini-decision-1","answers":{"is.damaged*":{"type":"noul","noul":0.5}},"usage":{"input_tokens":5,"output_tokens":0}}`, recorder.Body.String())
}

func TestSystemOneHandlerUsage(t *testing.T) {
	const answers = `"answers":{"q":{"type":"noul","noul":0.5,"confidence":0.5}}`
	const request = `{"model":"m","state":"s","questions":{"q":{"type":"noul","instructions":"i"}}}`

	t.Run("reported output tokens are passed on", func(t *testing.T) {
		usage, apiErr, recorder := runSystemOneHandler(t, newSystemOneInfo(t, request, 42), `{`+answers+`,"usage":{"total_input_tokens":10,"total_output_tokens":3}}`)
		require.Nil(t, apiErr)
		require.Equal(t, &dto.Usage{PromptTokens: 10, TotalTokens: 10}, usage)
		require.Contains(t, recorder.Body.String(), `"usage":{"input_tokens":10,"output_tokens":3}`)
	})

	for name, body := range map[string]string{
		"no usage":       `{` + answers + `}`,
		"string tokens":  `{` + answers + `,"usage":{"total_input_tokens":"10"}}`,
		"negative":       `{` + answers + `,"usage":{"total_input_tokens":-1}}`,
		"fractional":     `{` + answers + `,"usage":{"total_input_tokens":1.5}}`,
		"missing tokens": `{` + answers + `,"usage":{}}`,
	} {
		t.Run("estimate is billed for "+name, func(t *testing.T) {
			usage, apiErr, recorder := runSystemOneHandler(t, newSystemOneInfo(t, request, 42), body)
			require.Nil(t, apiErr)
			require.Equal(t, &dto.Usage{PromptTokens: 42, TotalTokens: 42}, usage)
			require.Contains(t, recorder.Body.String(), `"usage":{"input_tokens":42,"output_tokens":0}`)
		})
	}
}

func TestSystemOneHandlerRejectsBadAnswersWithoutWriting(t *testing.T) {
	for name, body := range map[string]string{
		"not json":          `nope`,
		"no answers":        `{"usage":{"total_input_tokens":5}}`,
		"answers not map":   `{"answers":[]}`,
		"missing answer":    `{"answers":{}}`,
		"wrong type":        `{"answers":{"q":{"type":"choice","choice":"a","confidence":1,"probabilities":{}}}}`,
		"noul not a number": `{"answers":{"q":{"type":"noul","noul":"high"}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			info := newSystemOneInfo(t, `{"model":"m","state":"s","questions":{"q":{"type":"noul","instructions":"i"}}}`, 42)

			usage, apiErr, recorder := runSystemOneHandler(t, info, body)

			require.Nil(t, usage)
			require.NotNil(t, apiErr)
			require.Empty(t, recorder.Body.String())
		})
	}
}

func TestSystemOneGeminiAdaptor(t *testing.T) {
	info := newSystemOneInfo(t, systemOneAllTypesRequest, 1)

	url, err := (&Adaptor{}).GetRequestURL(info)
	require.NoError(t, err)
	require.Equal(t, "https://generativelanguage.googleapis.com/v1beta/decisions", url)

	t.Run("a multipart client still sends json", func(t *testing.T) {
		c := newSystemOneContext(t, "multipart/form-data; boundary=xyz", nil)
		c.Request.Header.Set("Accept", "text/plain")
		info.ApiKey = "secret"
		header := http.Header{}

		require.NoError(t, (&Adaptor{}).SetupRequestHeader(c, &header, info))

		require.Equal(t, "application/json", header.Get("Content-Type"))
		require.Equal(t, "application/json", header.Get("Accept"))
		require.Equal(t, "secret", header.Get("x-goog-api-key"))
	})

	t.Run("responses are converted to System One", func(t *testing.T) {
		gin.SetMode(gin.TestMode)
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/systemone", nil)
		resp := &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewReader([]byte(`{"answers":{"damaged":{"type":"noul","noul":0.4,"confidence":0.6},"queue":{"type":"choice","choice":"billing","probabilities":{"billing":1},"confidence":1},"severity":{"type":"score","score":0,"probabilities":{"0":1},"confidence":1}},"usage":{"total_input_tokens":9}}`))),
		}

		usage, apiErr := (&Adaptor{}).DoResponse(c, resp, info)

		require.Nil(t, apiErr)
		require.Equal(t, &dto.Usage{PromptTokens: 9, TotalTokens: 9}, usage)
		require.Contains(t, recorder.Body.String(), `"model":"gemini-decision-1"`)
		require.Contains(t, recorder.Body.String(), `"damaged":{"noul":0.4,"type":"noul"}`)
	})
}
