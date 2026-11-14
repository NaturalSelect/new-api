package common

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"

	"github.com/stretchr/testify/require"
)

func TestGetEndpointTypesByChannelTypeGeminiSpecialModels(t *testing.T) {
	tests := []struct {
		name        string
		channelType int
		modelName   string
		want        []constant.EndpointType
	}{
		{name: "gemini decision model", channelType: constant.ChannelTypeGemini, modelName: "gemini-decision-1", want: []constant.EndpointType{constant.EndpointTypeGeminiDecision}},
		{name: "system one model on a gemini channel", channelType: constant.ChannelTypeGemini, modelName: "jev-latest", want: []constant.EndpointType{constant.EndpointTypeTypeSafe}},
		{name: "chat model on a gemini channel", channelType: constant.ChannelTypeGemini, modelName: "gemini-2.0-flash", want: []constant.EndpointType{constant.EndpointTypeGemini, constant.EndpointTypeOpenAI}},
		{name: "vertex keeps the chat endpoints", channelType: constant.ChannelTypeVertexAi, modelName: "gemini-decision-1", want: []constant.EndpointType{constant.EndpointTypeGemini, constant.EndpointTypeOpenAI}},
		{name: "typesafe channel", channelType: constant.ChannelTypeTypeSafe, modelName: "jev-latest", want: []constant.EndpointType{constant.EndpointTypeTypeSafe}},
		{name: "opencode go serves all three families", channelType: constant.ChannelTypeOpenCodeGo, modelName: "glm-5.3", want: []constant.EndpointType{constant.EndpointTypeOpenAI, constant.EndpointTypeOpenAIResponse, constant.EndpointTypeAnthropic}},
		{name: "opencode zen serves all three families", channelType: constant.ChannelTypeOpenCodeZen, modelName: "claude-sonnet-5-5", want: []constant.EndpointType{constant.EndpointTypeOpenAI, constant.EndpointTypeOpenAIResponse, constant.EndpointTypeAnthropic}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, GetEndpointTypesByChannelType(tt.channelType, tt.modelName))
		})
	}
}
