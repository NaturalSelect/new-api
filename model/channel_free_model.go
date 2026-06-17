package model

import (
	"fmt"
	"math/rand"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

func (channel *Channel) GetFreeModels() []string {
	if channel == nil || channel.Setting == nil || strings.TrimSpace(*channel.Setting) == "" {
		return nil
	}

	setting := dto.ChannelSettings{}
	if err := common.Unmarshal([]byte(*channel.Setting), &setting); err != nil {
		common.SysLog(fmt.Sprintf("failed to unmarshal setting free_models: channel_id=%d, error=%v", channel.Id, err))
		return nil
	}
	return setting.FreeModels
}

func (channel *Channel) IsFreeModel(modelName string) bool {
	return isFreeModelName(modelName, channel.GetFreeModels())
}

func isFreeModelName(modelName string, freeModels []string) bool {
	modelName = strings.TrimSpace(modelName)
	if modelName == "" || len(freeModels) == 0 {
		return false
	}

	normalizedModelName := ratio_setting.FormatMatchingModelName(modelName)
	for _, freeModel := range freeModels {
		freeModel = strings.TrimSpace(freeModel)
		if freeModel == "" {
			continue
		}
		if freeModel == modelName || freeModel == normalizedModelName {
			return true
		}
	}
	return false
}

func selectFreeModelChannel(modelName string, channels []*Channel) *Channel {
	freeChannels := make([]*Channel, 0, len(channels))
	for _, channel := range channels {
		if channel.IsFreeModel(modelName) {
			freeChannels = append(freeChannels, channel)
		}
	}
	if len(freeChannels) == 0 {
		return nil
	}
	return freeChannels[rand.Intn(len(freeChannels))]
}
