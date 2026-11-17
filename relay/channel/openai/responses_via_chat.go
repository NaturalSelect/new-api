package openai

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/openaicompat"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

// ShouldServeResponsesViaChat reports whether a Responses request must be sent upstream
// as Chat Completions and answered in Responses format, because Poe lists no
// /v1/responses endpoint for the model.
func ShouldServeResponsesViaChat(info *relaycommon.RelayInfo) bool {
	return info != nil && info.ChannelMeta != nil &&
		info.ChannelType == constant.ChannelTypePoeOpenAI &&
		info.RelayMode == relayconstant.RelayModeResponses &&
		service.ShouldPoeResponsesUseChat(info.UpstreamModelName)
}

func newChatToResponsesTranscoder(c *gin.Context, info *relaycommon.RelayInfo, stream bool) *openaicompat.ChatToResponsesTranscoder {
	request, _ := info.Request.(*dto.OpenAIResponsesRequest)
	respID := "resp_" + strings.TrimPrefix(helper.GetResponseID(c), "chatcmpl-")
	return openaicompat.NewChatToResponsesTranscoder(respID, info.OriginModelName, common.GetTimestamp(), request, stream)
}

// restoreThoughtSignatures puts back the Gemini thought signatures that a Responses client
// cannot echo, so replayed tool calls reach the upstream with the signature they were given.
func restoreThoughtSignatures(request *dto.GeneralOpenAIRequest, info *relaycommon.RelayInfo) {
	for i := range request.Messages {
		msg := &request.Messages[i]
		if msg.Role != "assistant" {
			continue
		}
		calls := msg.ParseToolCalls()
		restored := false
		for j := range calls {
			if len(calls[j].ExtraContent) > 0 {
				continue
			}
			signature := service.LoadPoeThoughtSignature(info.UserId, info.UpstreamModelName, calls[j].ID)
			if signature == "" {
				continue
			}
			calls[j].ExtraContent = openaicompat.ExtraContentFromThoughtSignature(signature)
			restored = true
		}
		if restored {
			msg.SetToolCalls(calls)
		}
	}
}

// rememberThoughtSignatures stores signatures not yet in stored. It runs before the events
// that announce a tool call are sent, so a client that replies at once still finds them.
func rememberThoughtSignatures(info *relaycommon.RelayInfo, t *openaicompat.ChatToResponsesTranscoder, stored map[string]struct{}) {
	for callID, signature := range t.ThoughtSignatures() {
		if _, done := stored[callID]; done {
			continue
		}
		stored[callID] = struct{}{}
		service.StorePoeThoughtSignature(info.UserId, info.UpstreamModelName, callID, signature)
	}
}

// OaiChatToResponsesHandler answers a Responses request from a Chat Completions upstream response.
func OaiChatToResponsesHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, types.NewOpenAIError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}
	defer service.CloseResponseBodyGracefully(resp)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}
	logger.LogDebug(c, "upstream chat response body: %s", body)

	var chatResp dto.OpenAITextResponse
	if err := common.Unmarshal(body, &chatResp); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if oaiError := chatResp.GetOpenAIError(); oaiError != nil && oaiError.Type != "" {
		return nil, types.WithOpenAIError(*oaiError, resp.StatusCode)
	}
	if len(chatResp.Choices) == 0 {
		return nil, types.NewOpenAIError(errors.New("empty response from upstream"), types.ErrorCodeEmptyResponse, http.StatusInternalServerError)
	}

	t := newChatToResponsesTranscoder(c, info, false)
	choice := &chatResp.Choices[0]
	t.ConsumeMessage(&choice.Message, choice.FinishReason)
	rememberThoughtSignatures(info, t, map[string]struct{}{})

	usage := chatResp.Usage
	if !service.ValidUsage(&usage) {
		usage = *service.ResponseText2Usage(c, t.OutputText(), info.UpstreamModelName, info.GetEstimatePromptTokens())
	}

	final, _ := t.Complete(&usage)
	responseBody, err := common.Marshal(final)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeJsonMarshalFailed, http.StatusInternalServerError)
	}
	service.IOCopyBytesGracefully(c, resp, responseBody)
	return &usage, nil
}

// OaiChatToResponsesStreamHandler answers a streaming Responses request from a Chat
// Completions upstream stream.
func OaiChatToResponsesStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, types.NewOpenAIError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}
	defer service.CloseResponseBodyGracefully(resp)

	t := newChatToResponsesTranscoder(c, info, true)
	storedSignatures := make(map[string]struct{})
	var (
		upstreamUsage *dto.Usage
		streamErr     *types.OpenAIError
	)

	send := func(events []openaicompat.ResponsesStreamEvent) {
		for _, ev := range events {
			data, err := common.Marshal(ev.Data)
			if err != nil {
				logger.LogError(c, "failed to marshal responses stream event: "+err.Error())
				continue
			}
			helper.ResponseChunkData(c, dto.ResponsesStreamResponse{Type: ev.Type}, string(data))
			info.SendResponseCount++
		}
	}

	helper.StreamScannerHandler(c, resp, info, func(data string, sr *helper.StreamResult) {
		var chunk dto.ChatCompletionsStreamResponse
		if err := common.UnmarshalJsonStr(data, &chunk); err != nil {
			sr.Error(err)
			return
		}
		if len(chunk.Choices) == 0 && chunk.Usage == nil {
			var probe struct {
				Error any `json:"error"`
			}
			if err := common.UnmarshalJsonStr(data, &probe); err == nil && probe.Error != nil {
				streamErr = dto.GetOpenAIError(probe.Error)
				if streamErr == nil {
					streamErr = &types.OpenAIError{Message: "upstream stream error", Type: "upstream_error"}
				}
				sr.Stop(errors.New(streamErr.Message))
				return
			}
		}
		if chunk.Usage != nil && service.ValidUsage(chunk.Usage) {
			upstreamUsage = chunk.Usage
		}
		events := t.ConsumeChunk(&chunk)
		rememberThoughtSignatures(info, t, storedSignatures)
		send(events)
	})

	if info.StreamStatus.IsScannerError() {
		return &dto.Usage{}, nil
	}
	if streamErr != nil {
		if !t.Started() {
			return nil, types.WithOpenAIError(*streamErr, http.StatusInternalServerError)
		}
		code := "upstream_error"
		if streamErr.Type != "" {
			code = streamErr.Type
		}
		send(t.Fail(code, streamErr.Message))
		return &dto.Usage{}, nil
	}
	if !t.Started() {
		return nil, types.NewOpenAIError(errors.New("empty response from upstream"), types.ErrorCodeEmptyResponse, http.StatusInternalServerError)
	}

	usage := upstreamUsage
	if usage == nil {
		usage = service.ResponseText2Usage(c, t.OutputText(), info.UpstreamModelName, info.GetEstimatePromptTokens())
	}
	_, events := t.Complete(usage)
	send(events)
	return usage, nil
}
