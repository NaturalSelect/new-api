package novelai

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

const (
	browserOrigin    = "https://novelai.net"
	browserUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/136.0.0.0 Safari/537.36"
	browserSecChUa   = `"Chromium";v="136", "Google Chrome";v="136", "Not.A/Brand";v="99"`
)

type Adaptor struct {
	requestContentType string
}

func (a *Adaptor) Init(info *relaycommon.RelayInfo) {
}

func (a *Adaptor) GetRequestURL(info *relaycommon.RelayInfo) (string, error) {
	if info == nil {
		return "", errors.New("novelai adaptor: relay info is nil")
	}
	baseURL := info.ChannelBaseUrl
	if baseURL == "" {
		baseURL = constant.ChannelBaseURLs[constant.ChannelTypeNovelAI]
	}
	return relaycommon.GetFullRequestURL(baseURL, generateImagePath, info.ChannelType), nil
}

func (a *Adaptor) SetupRequestHeader(c *gin.Context, req *http.Header, info *relaycommon.RelayInfo) error {
	if info == nil {
		return errors.New("novelai adaptor: relay info is nil")
	}
	if info.ApiKey == "" {
		return errors.New("novelai adaptor: api key is required")
	}
	channel.SetupApiRequestHeader(info, c, req)

	req.Set("Authorization", "Bearer "+info.ApiKey)
	req.Set("Content-Type", a.requestContentType)
	req.Set("Accept", "*/*")
	req.Set("Accept-Language", "zh-CN,zh;q=0.9")
	req.Set("Cache-Control", "no-cache")
	req.Set("Pragma", "no-cache")
	req.Set("Priority", "u=1, i")
	req.Set("Origin", browserOrigin)
	req.Set("Referer", browserOrigin+"/")
	req.Set("Sec-Ch-Ua", browserSecChUa)
	req.Set("Sec-Ch-Ua-Mobile", "?0")
	req.Set("Sec-Ch-Ua-Platform", `"Windows"`)
	req.Set("Sec-Fetch-Dest", "empty")
	req.Set("Sec-Fetch-Mode", "cors")
	req.Set("Sec-Fetch-Site", "same-site")
	req.Set("User-Agent", browserUserAgent)
	req.Set("X-Correlation-Id", fmt.Sprintf("%06x", rand.Uint32()&0xffffff))
	req.Set("X-Initiated-At", time.Now().UTC().Format("2006-01-02T15:04:05.000Z"))
	return nil
}

func (a *Adaptor) ConvertImageRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.ImageRequest) (any, error) {
	if info == nil {
		return nil, errors.New("novelai adaptor: relay info is nil")
	}
	if strings.TrimSpace(request.Prompt) == "" {
		return nil, errors.New("novelai adaptor: prompt is required")
	}
	if request.N != nil && *request.N > 1 {
		return nil, errors.New("novelai adaptor: only n=1 is supported")
	}

	model := strings.TrimSpace(info.UpstreamModelName)
	if model == "" {
		model = strings.TrimSpace(request.Model)
	}
	if model == "" {
		return nil, errors.New("novelai adaptor: model is required")
	}

	width, height, err := parseSize(request.Size)
	if err != nil {
		return nil, err
	}
	opts, err := parseUserOptions(request)
	if err != nil {
		return nil, err
	}
	if err := validateSubscriptionLimits(width, height, opts); err != nil {
		return nil, err
	}
	seed := valueOr(opts.Seed, rand.Uint32())

	body, contentType, err := buildMultipartBody(buildGenerateRequest(model, request.Prompt, width, height, opts, seed))
	if err != nil {
		return nil, err
	}
	a.requestContentType = contentType
	return body, nil
}

func (a *Adaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (any, error) {
	return channel.DoApiRequest(a, c, info, requestBody)
}

func (a *Adaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (any, *types.NewAPIError) {
	if resp == nil {
		return nil, types.NewError(errors.New("novelai adaptor: empty response"), types.ErrorCodeBadResponse)
	}
	raw, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeReadResponseBodyFailed)
	}

	image, err := extractImage(raw)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}

	responseBytes, err := common.Marshal(dto.ImageResponse{
		Created: common.GetTimestamp(),
		Data:    []dto.ImageData{{B64Json: base64.StdEncoding.EncodeToString(image)}},
	})
	if err != nil {
		return nil, types.NewError(fmt.Errorf("novelai adaptor: encode response failed: %w", err), types.ErrorCodeBadResponseBody)
	}

	c.Writer.Header().Set("Content-Type", "application/json")
	c.Writer.WriteHeader(http.StatusOK)
	_, _ = c.Writer.Write(responseBytes)
	return &dto.Usage{}, nil
}

func (a *Adaptor) GetModelList() []string {
	return ModelList
}

func (a *Adaptor) GetChannelName() string {
	return ChannelName
}

func (a *Adaptor) ConvertOpenAIRequest(*gin.Context, *relaycommon.RelayInfo, *dto.GeneralOpenAIRequest) (any, error) {
	return nil, errors.New("novelai adaptor: ConvertOpenAIRequest is not implemented")
}

func (a *Adaptor) ConvertRerankRequest(*gin.Context, int, dto.RerankRequest) (any, error) {
	return nil, errors.New("novelai adaptor: ConvertRerankRequest is not implemented")
}

func (a *Adaptor) ConvertEmbeddingRequest(*gin.Context, *relaycommon.RelayInfo, dto.EmbeddingRequest) (any, error) {
	return nil, errors.New("novelai adaptor: ConvertEmbeddingRequest is not implemented")
}

func (a *Adaptor) ConvertAudioRequest(*gin.Context, *relaycommon.RelayInfo, dto.AudioRequest) (io.Reader, error) {
	return nil, errors.New("novelai adaptor: ConvertAudioRequest is not implemented")
}

func (a *Adaptor) ConvertOpenAIResponsesRequest(*gin.Context, *relaycommon.RelayInfo, dto.OpenAIResponsesRequest) (any, error) {
	return nil, errors.New("novelai adaptor: ConvertOpenAIResponsesRequest is not implemented")
}

func (a *Adaptor) ConvertClaudeRequest(*gin.Context, *relaycommon.RelayInfo, *dto.ClaudeRequest) (any, error) {
	return nil, errors.New("novelai adaptor: ConvertClaudeRequest is not implemented")
}

func (a *Adaptor) ConvertGeminiRequest(*gin.Context, *relaycommon.RelayInfo, *dto.GeminiChatRequest) (any, error) {
	return nil, errors.New("novelai adaptor: ConvertGeminiRequest is not implemented")
}
