package openai

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

const poeSeedreamChatReply = `{
  "id": "chatcmpl-f174879267f2f31cb718a135",
  "object": "chat.completion",
  "created": 1795804549,
  "model": "seedream-5.0-pro",
  "choices": [{
    "index": 0,
    "message": {
      "role": "assistant",
      "content": "![bicycle.png](https://pfst.cf2.poecdn.net/base/image/5d72f378?w=1584&h=2816)\n\nhttps://pfst.cf2.poecdn.net/base/image/5d72f378?w=1584&h=2816"
    },
    "finish_reason": "stop"
  }],
  "usage": {"prompt_tokens": 239, "completion_tokens": 86, "total_tokens": 325}
}`

func imageViaChatCatalog() map[string]service.PoeModelInfo {
	return map[string]service.PoeModelInfo{
		"seedream-5.0-pro": {Params: map[string][]string{
			"aspect_ratio": {"1:1", "16:9", "9:16", "4:3", "3:4", "3:2", "2:3"},
			"quality":      {"1k", "1.5k", "2k"},
		}},
		"gpt-image-2": {Params: map[string][]string{
			"size":     {"auto", "1024x1024", "1536x1024", "1024x1536"},
			"quality":  {"low", "medium", "high"},
			"use_mask": {},
		}},
	}
}

func newPoeImageInfo(model string, relayMode int, request *dto.ImageRequest) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		OriginModelName: model,
		RelayMode:       relayMode,
		RequestURLPath:  "/v1/images/generations",
		Request:         request,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypePoeOpenAI,
			ChannelBaseUrl:    "https://api.poe.com",
			UpstreamModelName: model,
		},
	}
}

func newImageTestContext(contentType string, body io.Reader) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", body)
	c.Request.Header.Set("Content-Type", contentType)
	return c, rec
}

func convertToChatForTest(t *testing.T, a *Adaptor, c *gin.Context, info *relaycommon.RelayInfo, request dto.ImageRequest) *dto.GeneralOpenAIRequest {
	t.Helper()
	converted, err := a.ConvertImageRequest(c, info, request)
	require.NoError(t, err)
	chatRequest, ok := converted.(*dto.GeneralOpenAIRequest)
	require.True(t, ok, "expected a chat request, got %T", converted)
	return chatRequest
}

func TestShouldServeImageViaChatOnlyForPoeImageModes(t *testing.T) {
	require.True(t, ShouldServeImageViaChat(newPoeImageInfo("m", relayconstant.RelayModeImagesGenerations, nil)))
	require.True(t, ShouldServeImageViaChat(newPoeImageInfo("m", relayconstant.RelayModeImagesEdits, nil)))
	require.False(t, ShouldServeImageViaChat(newPoeImageInfo("m", relayconstant.RelayModeChatCompletions, nil)))

	openAIInfo := newPoeImageInfo("m", relayconstant.RelayModeImagesGenerations, nil)
	openAIInfo.ChannelType = constant.ChannelTypeOpenAI
	require.False(t, ShouldServeImageViaChat(openAIInfo))
	require.False(t, ShouldServeImageViaChat(nil))
}

func TestConvertImageRequestPoeGenerationUsesChat(t *testing.T) {
	setPoeCatalogForTest(t, imageViaChatCatalog())
	request := dto.ImageRequest{Model: "seedream-5.0-pro", Prompt: "an old bicycle", Size: "1024x1792", Quality: "2K"}
	info := newPoeImageInfo("seedream-5.0-pro", relayconstant.RelayModeImagesGenerations, &request)
	adaptor := &Adaptor{}
	adaptor.Init(info)

	chatRequest := convertToChatForTest(t, adaptor, nil, info, request)

	require.Equal(t, "seedream-5.0-pro", chatRequest.Model)
	require.Len(t, chatRequest.Messages, 1)
	require.Equal(t, "user", chatRequest.Messages[0].Role)
	require.Equal(t, "an old bicycle", chatRequest.Messages[0].StringContent())
	require.NotNil(t, chatRequest.N)
	require.Equal(t, 1, *chatRequest.N)
	require.Empty(t, chatRequest.Size, "size is a Poe model parameter, not a chat field")

	extraBody := unmarshalRawObject(t, chatRequest.ExtraBody)
	assertRawJSONEqual(t, extraBody["aspect_ratio"], `"9:16"`)
	assertRawJSONEqual(t, extraBody["quality"], `"2K"`)

	url, err := adaptor.GetRequestURL(info)
	require.NoError(t, err)
	require.Equal(t, "https://api.poe.com/v1/chat/completions", url)
}

func TestConvertImageRequestPoeDropsUnmappableOptions(t *testing.T) {
	setPoeCatalogForTest(t, imageViaChatCatalog())
	request := dto.ImageRequest{Model: "seedream-5.0-pro", Prompt: "p", Size: "auto", Quality: "high"}
	info := newPoeImageInfo("seedream-5.0-pro", relayconstant.RelayModeImagesGenerations, &request)

	chatRequest := convertToChatForTest(t, &Adaptor{}, nil, info, request)

	require.Empty(t, chatRequest.ExtraBody)
}

func TestConvertImageRequestNonPoeKeepsNativeRequest(t *testing.T) {
	request := dto.ImageRequest{Model: "dall-e-3", Prompt: "p"}
	info := newPoeImageInfo("dall-e-3", relayconstant.RelayModeImagesGenerations, &request)
	info.ChannelType = constant.ChannelTypeOpenAI
	adaptor := &Adaptor{}

	converted, err := adaptor.ConvertImageRequest(nil, info, request)
	require.NoError(t, err)
	require.IsType(t, dto.ImageRequest{}, converted)

	url, err := adaptor.GetRequestURL(info)
	require.NoError(t, err)
	require.Equal(t, "https://api.poe.com/v1/images/generations", url)
}

func TestConvertImageRequestPoeJSONEditAttachesImagesAndMask(t *testing.T) {
	setPoeCatalogForTest(t, imageViaChatCatalog())
	c, _ := newImageTestContext("application/json", nil)
	request := dto.ImageRequest{
		Model:  "gpt-image-2",
		Prompt: "remove the sign",
		Images: []byte(`[{"image_url":"https://example.com/a.png"},{"image_url":"data:image/png;base64,AAAA"}]`),
		Mask:   []byte(`{"image_url":"https://example.com/mask.png"}`),
	}
	info := newPoeImageInfo("gpt-image-2", relayconstant.RelayModeImagesEdits, &request)

	chatRequest := convertToChatForTest(t, &Adaptor{}, c, info, request)

	parts := chatRequest.Messages[0].ParseContent()
	require.Len(t, parts, 4)
	require.Equal(t, dto.ContentTypeText, parts[0].Type)
	require.Equal(t, "remove the sign", parts[0].Text)
	var urls []string
	for _, part := range parts[1:] {
		require.Equal(t, dto.ContentTypeImageURL, part.Type)
		urls = append(urls, part.GetImageMedia().Url)
	}
	require.Equal(t, []string{"https://example.com/a.png", "data:image/png;base64,AAAA", "https://example.com/mask.png"}, urls, "the mask goes last")
	assertRawJSONEqual(t, unmarshalRawObject(t, chatRequest.ExtraBody)["use_mask"], `true`)
}

func TestConvertImageRequestPoeEditRejectsMaskWithoutUseMask(t *testing.T) {
	setPoeCatalogForTest(t, imageViaChatCatalog())
	c, _ := newImageTestContext("application/json", nil)
	request := dto.ImageRequest{
		Model:  "seedream-5.0-pro",
		Prompt: "p",
		Image:  []byte(`"https://example.com/a.png"`),
		Mask:   []byte(`"https://example.com/mask.png"`),
	}
	info := newPoeImageInfo("seedream-5.0-pro", relayconstant.RelayModeImagesEdits, &request)

	_, err := (&Adaptor{}).ConvertImageRequest(c, info, request)

	require.ErrorContains(t, err, "does not support image masks")
}

func TestConvertImageRequestPoeEditRequiresImage(t *testing.T) {
	c, _ := newImageTestContext("application/json", nil)
	request := dto.ImageRequest{Model: "gpt-image-2", Prompt: "p"}
	info := newPoeImageInfo("gpt-image-2", relayconstant.RelayModeImagesEdits, &request)

	_, err := (&Adaptor{}).ConvertImageRequest(c, info, request)

	require.ErrorContains(t, err, "image is required")
}

func TestConvertImageRequestPoeMultipartEditInlinesUploadsAndForcesJSON(t *testing.T) {
	setPoeCatalogForTest(t, imageViaChatCatalog())
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("model", "gpt-image-2"))
	require.NoError(t, writer.WriteField("prompt", "make it blue"))
	for _, name := range []string{"image[1]", "image[0]"} {
		part, err := writer.CreateFormFile(name, name+".png")
		require.NoError(t, err)
		_, err = part.Write([]byte(name))
		require.NoError(t, err)
	}
	require.NoError(t, writer.Close())

	c, _ := newImageTestContext(writer.FormDataContentType(), &body)
	require.NoError(t, c.Request.ParseMultipartForm(1<<20))
	request := dto.ImageRequest{Model: "gpt-image-2", Prompt: "make it blue"}
	info := newPoeImageInfo("gpt-image-2", relayconstant.RelayModeImagesEdits, &request)
	adaptor := &Adaptor{}
	adaptor.Init(info)

	chatRequest := convertToChatForTest(t, adaptor, c, info, request)

	parts := chatRequest.Messages[0].ParseContent()
	require.Len(t, parts, 3)
	require.Equal(t, "data:image/png;base64,aW1hZ2VbMF0=", parts[1].GetImageMedia().Url, "image[0] sorts before image[1]")
	require.Equal(t, "data:image/png;base64,aW1hZ2VbMV0=", parts[2].GetImageMedia().Url)

	header := http.Header{}
	require.NoError(t, adaptor.SetupRequestHeader(c, &header, info))
	require.Equal(t, "application/json", header.Get("Content-Type"))
}

func TestOaiChatToImageHandlerBuildsImageResponseFromMarkdown(t *testing.T) {
	c, rec := newImageTestContext("application/json", nil)
	request := &dto.ImageRequest{Model: "seedream-5.0-pro", Prompt: "p"}
	info := newPoeImageInfo("seedream-5.0-pro", relayconstant.RelayModeImagesGenerations, request)
	info.PriceData.UsePrice = true
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(poeSeedreamChatReply))}

	usage, apiErr := OaiChatToImageHandler(c, info, resp)

	require.Nil(t, apiErr)
	require.Equal(t, 325, usage.TotalTokens)
	require.Equal(t, 1.0, info.PriceData.OtherRatios["n"])

	var got map[string]any
	require.NoError(t, common.Unmarshal(rec.Body.Bytes(), &got))
	require.NotZero(t, got["created"])
	data := got["data"].([]any)
	require.Len(t, data, 1, "the bare URL repeated after the markdown is not a second image")
	item := data[0].(map[string]any)
	require.Equal(t, "https://pfst.cf2.poecdn.net/base/image/5d72f378?w=1584&h=2816", item["url"])
	require.NotContains(t, item, "b64_json")
	require.NotContains(t, item, "revised_prompt")
}

func TestOaiChatToImageHandlerReportsReplyWithoutImage(t *testing.T) {
	c, rec := newImageTestContext("application/json", nil)
	info := newPoeImageInfo("seedream-5.0-pro", relayconstant.RelayModeImagesGenerations, &dto.ImageRequest{})
	reply := `{"choices":[{"index":0,"message":{"role":"assistant","content":"I cannot draw that."},"finish_reason":"stop"}]}`
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(reply))}

	_, apiErr := OaiChatToImageHandler(c, info, resp)

	require.NotNil(t, apiErr)
	require.Contains(t, apiErr.Error(), "I cannot draw that.")
	require.Empty(t, rec.Body.Bytes(), "nothing is written to the client when there is no image")
}

func TestOaiChatToImageHandlerPassesUpstreamError(t *testing.T) {
	c, _ := newImageTestContext("application/json", nil)
	info := newPoeImageInfo("seedream-5.0-pro", relayconstant.RelayModeImagesGenerations, &dto.ImageRequest{})
	reply := `{"error":{"message":"bad request","type":"invalid_request_error"}}`
	resp := &http.Response{StatusCode: http.StatusBadRequest, Body: io.NopCloser(strings.NewReader(reply))}

	_, apiErr := OaiChatToImageHandler(c, info, resp)

	require.NotNil(t, apiErr)
	require.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
	require.Contains(t, apiErr.Error(), "bad request")
}

func TestExtractMarkdownImageURLs(t *testing.T) {
	content := "text ![a](https://x.test/1.png) and ![b](http://x.test/2.png)\n\nhttps://x.test/1.png ![c](https://x.test/1.png)"
	require.Equal(t, []string{"https://x.test/1.png", "http://x.test/2.png"}, extractMarkdownImageURLs(content))
	require.Empty(t, extractMarkdownImageURLs("no images, only https://x.test/1.png"))
}
