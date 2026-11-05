package typesafe

import (
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
)

type Adaptor struct{}

func (a *Adaptor) Init(info *relaycommon.RelayInfo) {
}

// GetRequestURL accepts a base URL with or without a /v1 suffix. A base that
// ends in /typesafe reaches another gateway's vendor-prefixed route.
func (a *Adaptor) GetRequestURL(info *relaycommon.RelayInfo) (string, error) {
	if info == nil {
		return "", errors.New("typesafe adaptor: relay info is nil")
	}
	baseURL := info.ChannelBaseUrl
	if baseURL == "" {
		baseURL = constant.ChannelBaseURLs[constant.ChannelTypeTypeSafe]
	}
	baseURL = strings.TrimRight(baseURL, "/")
	baseURL = strings.TrimSuffix(baseURL, "/v1")
	return relaycommon.GetFullRequestURL(baseURL, systemOnePath, info.ChannelType), nil
}

func (a *Adaptor) SetupRequestHeader(c *gin.Context, req *http.Header, info *relaycommon.RelayInfo) error {
	if info == nil {
		return errors.New("typesafe adaptor: relay info is nil")
	}
	if info.ApiKey == "" {
		return errors.New("typesafe adaptor: api key is required")
	}
	request, ok := info.Request.(*dto.TypeSafeRequest)
	if !ok {
		return errors.New("typesafe adaptor: request is not a TypeSafe request")
	}
	channel.SetupApiRequestHeader(info, c, req)

	req.Set("Authorization", "Bearer "+info.ApiKey)
	req.Set("Accept", "application/json")
	if request.Multipart {
		req.Set("Content-Type", c.Request.Header.Get("Content-Type"))
	} else {
		req.Set("Content-Type", "application/json")
	}
	if request.NeedsImages {
		req.Set(dto.TypeSafeImagesHeader, "true")
	}
	if request.LongContext {
		req.Set(dto.TypeSafeLongContextHeader, "true")
	}
	return nil
}

func (a *Adaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (any, error) {
	return channel.DoApiRequest(a, c, info, requestBody)
}

func (a *Adaptor) GetModelList() []string {
	return ModelList
}

func (a *Adaptor) GetChannelName() string {
	return ChannelName
}

func (a *Adaptor) ConvertOpenAIRequest(*gin.Context, *relaycommon.RelayInfo, *dto.GeneralOpenAIRequest) (any, error) {
	return nil, errors.New("typesafe adaptor: ConvertOpenAIRequest is not implemented")
}

func (a *Adaptor) ConvertRerankRequest(*gin.Context, int, dto.RerankRequest) (any, error) {
	return nil, errors.New("typesafe adaptor: ConvertRerankRequest is not implemented")
}

func (a *Adaptor) ConvertEmbeddingRequest(*gin.Context, *relaycommon.RelayInfo, dto.EmbeddingRequest) (any, error) {
	return nil, errors.New("typesafe adaptor: ConvertEmbeddingRequest is not implemented")
}

func (a *Adaptor) ConvertAudioRequest(*gin.Context, *relaycommon.RelayInfo, dto.AudioRequest) (io.Reader, error) {
	return nil, errors.New("typesafe adaptor: ConvertAudioRequest is not implemented")
}

func (a *Adaptor) ConvertImageRequest(*gin.Context, *relaycommon.RelayInfo, dto.ImageRequest) (any, error) {
	return nil, errors.New("typesafe adaptor: ConvertImageRequest is not implemented")
}

func (a *Adaptor) ConvertOpenAIResponsesRequest(*gin.Context, *relaycommon.RelayInfo, dto.OpenAIResponsesRequest) (any, error) {
	return nil, errors.New("typesafe adaptor: ConvertOpenAIResponsesRequest is not implemented")
}

func (a *Adaptor) ConvertClaudeRequest(*gin.Context, *relaycommon.RelayInfo, *dto.ClaudeRequest) (any, error) {
	return nil, errors.New("typesafe adaptor: ConvertClaudeRequest is not implemented")
}

func (a *Adaptor) ConvertGeminiRequest(*gin.Context, *relaycommon.RelayInfo, *dto.GeminiChatRequest) (any, error) {
	return nil, errors.New("typesafe adaptor: ConvertGeminiRequest is not implemented")
}
