package common

import (
	"strings"

	"github.com/QuantumNous/new-api/constant"
)

// IsOpenCodeSystemOneModel reports whether Zen serves the model on its System One endpoint instead of a chat API.
func IsOpenCodeSystemOneModel(modelName string) bool {
	return strings.HasPrefix(modelName, "jev") || strings.Contains(modelName, "-decision")
}

// GetEndpointTypesByChannelType 获取渠道最优先端点类型（所有的渠道都支持 OpenAI 端点）
func GetEndpointTypesByChannelType(channelType int, modelName string) []constant.EndpointType {
	var endpointTypes []constant.EndpointType
	switch channelType {
	case constant.ChannelTypeJina:
		endpointTypes = []constant.EndpointType{constant.EndpointTypeJinaRerank}
	//case constant.ChannelTypeMidjourney, constant.ChannelTypeMidjourneyPlus:
	//	endpointTypes = []constant.EndpointType{constant.EndpointTypeMidjourney}
	//case constant.ChannelTypeSunoAPI:
	//	endpointTypes = []constant.EndpointType{constant.EndpointTypeSuno}
	//case constant.ChannelTypeKling:
	//	endpointTypes = []constant.EndpointType{constant.EndpointTypeKling}
	//case constant.ChannelTypeJimeng:
	//	endpointTypes = []constant.EndpointType{constant.EndpointTypeJimeng}
	case constant.ChannelTypeAws:
		fallthrough
	case constant.ChannelTypeAnthropic:
		fallthrough
	case constant.ChannelTypePoeAnthropic:
		endpointTypes = []constant.EndpointType{constant.EndpointTypeAnthropic, constant.EndpointTypeOpenAI}
	case constant.ChannelTypeVertexAi:
		fallthrough
	case constant.ChannelTypeGemini:
		if channelType == constant.ChannelTypeGemini {
			// NOTE: Only Google's own endpoint serves Decisions, so Vertex AI channels keep the chat endpoints.
			if strings.HasPrefix(modelName, "gemini-decision") {
				endpointTypes = []constant.EndpointType{constant.EndpointTypeGeminiDecision}
				break
			}
			if strings.HasPrefix(modelName, "jev") {
				endpointTypes = []constant.EndpointType{constant.EndpointTypeTypeSafe}
				break
			}
		}
		endpointTypes = []constant.EndpointType{constant.EndpointTypeGemini, constant.EndpointTypeOpenAI}
		// NOTE: Only the Gemini adaptor converts Responses requests, the Vertex AI adaptor does not.
		if channelType == constant.ChannelTypeGemini {
			endpointTypes = append(endpointTypes, constant.EndpointTypeOpenAIResponse)
		}
	case constant.ChannelTypeOpenRouter: // OpenRouter 只支持 OpenAI 端点
		endpointTypes = []constant.EndpointType{constant.EndpointTypeOpenAI}
	case constant.ChannelTypeXai:
		endpointTypes = []constant.EndpointType{constant.EndpointTypeOpenAI, constant.EndpointTypeOpenAIResponse}
	case constant.ChannelTypeSora:
		endpointTypes = []constant.EndpointType{constant.EndpointTypeOpenAIVideo}
	case constant.ChannelTypeTypeSafe:
		endpointTypes = []constant.EndpointType{constant.EndpointTypeTypeSafe}
	case constant.ChannelTypeOpenCodeGo, constant.ChannelTypeOpenCodeZen:
		// NOTE: Only Zen serves System One models, and they have no chat endpoint.
		if channelType == constant.ChannelTypeOpenCodeZen && IsOpenCodeSystemOneModel(modelName) {
			endpointTypes = []constant.EndpointType{constant.EndpointTypeTypeSafe}
			break
		}
		endpointTypes = []constant.EndpointType{constant.EndpointTypeOpenAI, constant.EndpointTypeOpenAIResponse, constant.EndpointTypeAnthropic}
	default:
		if IsOpenAIResponseOnlyModel(modelName) {
			endpointTypes = []constant.EndpointType{constant.EndpointTypeOpenAIResponse}
		} else {
			endpointTypes = []constant.EndpointType{constant.EndpointTypeOpenAI}
		}
	}
	if IsImageGenerationModel(modelName) {
		// add to first
		endpointTypes = append([]constant.EndpointType{constant.EndpointTypeImageGeneration}, endpointTypes...)
	}
	return endpointTypes
}
