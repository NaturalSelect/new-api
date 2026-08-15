package relay

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

const nailongASCIIArt = `@@@@@@@@@@%%%%%%%%@@@@@@@@@@@@@@@@@@@@@@@@@@@@
@@@@@@@@%%%###%%%%%%%@@@@@@@@@@@@@@@@@@@@@@@@@
@@@@@@@########*#####%%%%@@@@@@@@@@@@@@@@@@@@@
@@@@@@#*****+=-=+****###%%%%%%@@@@@@@@@@@@@@@@
@@@@@@#==****+*==+++****####%%%%@@@@@@@@@@@@@@
@@@@@@@@*+=+=-:  ++++******###%%%%@@@@@@@@@@@@
@@@@@@@@%*-     :+++++++****####%%%%@@@@@@@@@@
@@@@@@@#***=::::=+=+++++******######%@@@@@@@@@
@@@@@@#*++**++=====+++++++++*****####%@@@@@@@@
@@@@@%**+==*+*+++++++++++++-+******####@@@@@@@
@@@@@*+++=-*************+++=-=++*****###@@@@@@
@@@@%*++=::######*#######***=-=++*****###@@@@@
@@@@@##%#+=*###########*****####***+***###@@@@
@@@@@%*###+=****##******+**#****++++****##%@@@
@@@@@@%****==--*##=-==++***++++===+*******%@@@
@@@@@@@@%*=--:-====---=+++=====+++++******%@@@
@@@@@@@@@@%=:::....::::-==++++++++++++***#@@@@
@@@@@@@@@@@@#=:::-=-:-==+++++++++++++*++#@@@@@
%%%%%%%%%%%%@%++++++++++++++====+++****#%%%%%%
###%#########*+============--==+++++*+*#######
*************===--::---=+=======++++++********
+*********+*==-----::-=******========+********
++++++++++++=---::::=++++++++=---=====++++++++
`

// EasterEggHelper 拦截命中彩蛋模型的请求，按客户端请求格式本地构造一次固定回复，
// 不请求任何上游渠道，也不产生计费副作用。
func EasterEggHelper(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	switch info.RelayFormat {
	case types.RelayFormatOpenAI, types.RelayFormatClaude, types.RelayFormatGemini:
		if info.IsStream {
			return easterEggChatStream(c, info)
		}
		return easterEggChatNonStream(c, info)
	case types.RelayFormatOpenAIResponses:
		if info.IsStream {
			return easterEggResponsesStream(c, info)
		}
		return easterEggResponsesNonStream(c, info)
	default:
		return types.NewError(fmt.Errorf("easter egg model does not support relay format %q", info.RelayFormat), types.ErrorCodeModelNotFound)
	}
}

// buildOpenAIResponse 构造一份 OpenAI 形态的完整回复，Claude/Gemini 均由此转换而来。
func buildOpenAIResponse(c *gin.Context, info *relaycommon.RelayInfo, createdAt int64) *dto.OpenAITextResponse {
	message := dto.Message{Role: "assistant"}
	message.SetStringContent(nailongASCIIArt)
	return &dto.OpenAITextResponse{
		Id:      helper.GetResponseID(c),
		Model:   info.OriginModelName,
		Object:  "chat.completion",
		Created: createdAt,
		Choices: []dto.OpenAITextResponseChoice{
			{
				Index:        0,
				Message:      message,
				FinishReason: "stop",
			},
		},
	}
}

func easterEggChatNonStream(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	resp := buildOpenAIResponse(c, info, time.Now().Unix())

	switch info.RelayFormat {
	case types.RelayFormatClaude:
		c.JSON(http.StatusOK, service.ResponseOpenAI2Claude(resp, info))
	case types.RelayFormatGemini:
		c.JSON(http.StatusOK, service.ResponseOpenAI2Gemini(resp, info))
	default:
		c.JSON(http.StatusOK, resp)
	}
	return nil
}

// easterEggChatStream 按 OpenAI 流式分片顺序（起始空分片 -> 内容分片 -> stop 分片 ->
// usage 分片）依次交给 openai.HandleStreamFormat / HandleFinalResponse，由其内部按
// info.RelayFormat 自动转换成 Claude / Gemini 的流式事件。三条必须遵守的顺序约束：
//  1. 起始空分片必须是第一次 HandleStreamFormat 调用，Claude 的 message_start 由
//     info.SendResponseCount == 1 触发，顺序错了客户端收不到 message_start。
//  2. 带 finish_reason 的 stop 分片必须先经过一次 HandleStreamFormat：Gemini 转换器靠它
//     才能产出 finishReason=STOP 的最终帧（真实上游的流里这个分片恰好也能带 usage，而
//     StreamResponseOpenAI2Claude 在 usage 非空时会直接走闭合路径，因此这也是 Claude
//     流合法的收尾方式）。
//  3. 最后再额外发一个"choices 为空、仅携带 usage"的分片交给 HandleFinalResponse 兜底，
//     保证 Claude 流在任一顺序下都一定发出 message_stop。
func easterEggChatStream(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	helper.SetEventStreamHeaders(c)

	id := helper.GetResponseID(c)
	createdAt := time.Now().Unix()
	model := info.OriginModelName

	startResp := helper.GenerateStartEmptyResponse(id, createdAt, model, nil)
	startData, _ := common.Marshal(startResp)
	_ = openai.HandleStreamFormat(c, info, string(startData), false, false)

	contentResp := &dto.ChatCompletionsStreamResponse{
		Id:      id,
		Object:  "chat.completion.chunk",
		Created: createdAt,
		Model:   model,
		Choices: []dto.ChatCompletionsStreamResponseChoice{
			{Index: 0, Delta: dto.ChatCompletionsStreamResponseChoiceDelta{Role: "assistant"}},
		},
	}
	contentResp.Choices[0].Delta.SetContentString(nailongASCIIArt)
	contentData, _ := common.Marshal(contentResp)
	_ = openai.HandleStreamFormat(c, info, string(contentData), false, false)

	usage := dto.Usage{}
	stopResp := helper.GenerateStopResponse(id, createdAt, model, "stop")
	stopResp.Usage = &usage
	stopData, _ := common.Marshal(stopResp)
	_ = openai.HandleStreamFormat(c, info, string(stopData), false, false)

	usageResp := helper.GenerateFinalUsageResponse(id, createdAt, model, usage)
	usageData, _ := common.Marshal(usageResp)
	openai.HandleFinalResponse(c, info, string(usageData), id, createdAt, model, "", &usage, true)
	return nil
}

func easterEggResponsesNonStream(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	c.JSON(http.StatusOK, buildResponsesResponse(c, info, "completed", time.Now().Unix()))
	return nil
}

func buildResponsesResponse(c *gin.Context, info *relaycommon.RelayInfo, status string, createdAt int64) *dto.OpenAIResponsesResponse {
	itemId := fmt.Sprintf("msg_%s", helper.GetResponseID(c))
	return &dto.OpenAIResponsesResponse{
		ID:        fmt.Sprintf("resp_%s", helper.GetResponseID(c)),
		Object:    "response",
		CreatedAt: int(createdAt),
		Status:    json.RawMessage(`"` + status + `"`),
		Model:     info.OriginModelName,
		Output: []dto.ResponsesOutput{
			{
				Type:   "message",
				ID:     itemId,
				Status: status,
				Role:   "assistant",
				Content: []dto.ResponsesOutputContent{
					{Type: "output_text", Text: nailongASCIIArt},
				},
			},
		},
		Usage: &dto.Usage{},
	}
}

// easterEggResponsesStream 按 Responses API 最小可用事件集顺序发送：
// response.created -> response.output_item.added -> response.output_text.delta ->
// response.output_item.done -> response.completed。
func easterEggResponsesStream(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	helper.SetEventStreamHeaders(c)

	createdAt := time.Now().Unix()
	itemId := fmt.Sprintf("msg_%s", helper.GetResponseID(c))
	zero := 0

	sendEvent := func(resp dto.ResponsesStreamResponse) {
		data, _ := common.Marshal(resp)
		helper.ResponseChunkData(c, resp, string(data))
	}

	inProgress := buildResponsesResponse(c, info, "in_progress", createdAt)
	inProgress.Output = []dto.ResponsesOutput{}
	sendEvent(dto.ResponsesStreamResponse{Type: "response.created", Response: inProgress})

	addedItem := &dto.ResponsesOutput{
		Type:    "message",
		ID:      itemId,
		Status:  "in_progress",
		Role:    "assistant",
		Content: []dto.ResponsesOutputContent{},
	}
	sendEvent(dto.ResponsesStreamResponse{
		Type:        "response.output_item.added",
		OutputIndex: &zero,
		Item:        addedItem,
	})

	sendEvent(dto.ResponsesStreamResponse{
		Type:         "response.output_text.delta",
		Delta:        nailongASCIIArt,
		ItemID:       itemId,
		OutputIndex:  &zero,
		ContentIndex: &zero,
	})

	doneItem := &dto.ResponsesOutput{
		Type:   "message",
		ID:     itemId,
		Status: "completed",
		Role:   "assistant",
		Content: []dto.ResponsesOutputContent{
			{Type: "output_text", Text: nailongASCIIArt},
		},
	}
	sendEvent(dto.ResponsesStreamResponse{
		Type:        "response.output_item.done",
		OutputIndex: &zero,
		Item:        doneItem,
	})

	completed := buildResponsesResponse(c, info, "completed", createdAt)
	sendEvent(dto.ResponsesStreamResponse{Type: "response.completed", Response: completed})
	return nil
}
