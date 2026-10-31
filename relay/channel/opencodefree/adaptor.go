package opencodefree

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

type Adaptor struct {
	plan *requestPlan
	// NOTE: The upstream always streams, so the client's own stream preference has to be remembered separately.
	clientStream bool
}

func (a *Adaptor) Init(info *relaycommon.RelayInfo) {
	a.clientStream = info.IsStream
}

func (a *Adaptor) GetRequestURL(info *relaycommon.RelayInfo) (string, error) {
	return strings.TrimRight(info.ChannelBaseUrl, "/") + chatCompletionsPath, nil
}

func (a *Adaptor) SetupRequestHeader(c *gin.Context, req *http.Header, info *relaycommon.RelayInfo) error {
	// NOTE: Sessions are per channel key so several keys of one channel look like separate OpenCode clients.
	sessionID := sessions.get(fmt.Sprintf("%d:%d", info.ChannelId, info.ChannelMultiKeyIndex))
	req.Set("Accept", "text/event-stream")
	req.Set("Content-Type", "application/json")
	req.Set("Authorization", "Bearer "+upstreamToken)
	req.Set("User-Agent", upstreamUserAgent)
	req.Set("x-opencode-client", "cli")
	req.Set("x-opencode-project", "global")
	req.Set("x-opencode-session", sessionID)
	req.Set("x-opencode-session-id", sessionID)
	req.Set("x-session-affinity", sessionID)
	req.Set("X-Session-Id", sessionID)
	return nil
}

func (a *Adaptor) ConvertOpenAIRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest) (any, error) {
	if request == nil {
		return nil, errors.New("request is nil")
	}
	plan, err := buildUpstreamRequest(request)
	if err != nil {
		return nil, err
	}
	a.plan = plan
	return request, nil
}

func (a *Adaptor) ConvertClaudeRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.ClaudeRequest) (any, error) {
	openAIRequest, err := service.ClaudeToOpenAIRequest(*request, info)
	if err != nil {
		return nil, err
	}
	return a.ConvertOpenAIRequest(c, info, openAIRequest)
}

func (a *Adaptor) ConvertGeminiRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeminiChatRequest) (any, error) {
	openAIRequest, err := service.GeminiToOpenAIRequest(request, info)
	if err != nil {
		return nil, err
	}
	return a.ConvertOpenAIRequest(c, info, openAIRequest)
}

func (a *Adaptor) ConvertRerankRequest(c *gin.Context, relayMode int, request dto.RerankRequest) (any, error) {
	return nil, errors.New("opencode free channel: endpoint not supported")
}

func (a *Adaptor) ConvertEmbeddingRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.EmbeddingRequest) (any, error) {
	return nil, errors.New("opencode free channel: endpoint not supported")
}

func (a *Adaptor) ConvertAudioRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.AudioRequest) (io.Reader, error) {
	return nil, errors.New("opencode free channel: endpoint not supported")
}

func (a *Adaptor) ConvertImageRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.ImageRequest) (any, error) {
	return nil, errors.New("opencode free channel: endpoint not supported")
}

func (a *Adaptor) ConvertOpenAIResponsesRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.OpenAIResponsesRequest) (any, error) {
	return nil, errors.New("opencode free channel: endpoint not supported")
}

func (a *Adaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (any, error) {
	return channel.DoApiRequest(a, c, info, requestBody)
}

func (a *Adaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (usage any, err *types.NewAPIError) {
	if a.plan == nil {
		return nil, types.NewError(errors.New("opencode free channel: request was not converted"), types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}
	if a.clientStream {
		body, streamErr := newStreamBody(resp.Body, a.plan, info.UpstreamModelName)
		if streamErr != nil {
			return nil, upstreamStreamError(streamErr)
		}
		resp.Body = body
		resp.Header = resp.Header.Clone()
		resp.Header.Del("Content-Length")
		info.IsStream = true
		return openai.OaiStreamHandler(c, info, resp)
	}

	parser := newFrameParser(resp.Body, a.plan)
	defer parser.close()
	payload, aggregateErr := aggregateResponse(parser, a.plan, info.UpstreamModelName)
	if aggregateErr != nil {
		return nil, upstreamStreamError(aggregateErr)
	}
	resp.Body = io.NopCloser(bytes.NewReader(payload))
	resp.Header = resp.Header.Clone()
	resp.Header.Set("Content-Type", "application/json")
	resp.Header.Del("Content-Length")
	resp.Header.Del("Content-Encoding")
	// NOTE: The shared stream flag is flipped by the relay once it sees an event-stream content type from the upstream.
	info.IsStream = false
	return openai.OpenaiHandler(c, info, resp)
}

func upstreamStreamError(err error) *types.NewAPIError {
	return types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusBadGateway)
}

func (a *Adaptor) GetModelList() []string {
	return ModelList
}

func (a *Adaptor) GetChannelName() string {
	return ChannelName
}
