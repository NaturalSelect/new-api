package operation_setting

import (
	"strings"

	"github.com/QuantumNous/new-api/setting/config"
)

type EasterEggSetting struct {
	Enabled   bool   `json:"enabled"`
	ModelName string `json:"model_name"`
}

// 默认配置
var easterEggSetting = EasterEggSetting{
	Enabled:   false,
	ModelName: "",
}

func init() {
	// 注册到全局配置管理器
	config.GlobalConfig.Register("easter_egg_setting", &easterEggSetting)
}

func GetEasterEggSetting() *EasterEggSetting {
	return &easterEggSetting
}

// IsEasterEggModel 判断给定模型名是否命中彩蛋模型配置（大小写不敏感）
func IsEasterEggModel(modelName string) bool {
	if !easterEggSetting.Enabled {
		return false
	}
	target := strings.TrimSpace(easterEggSetting.ModelName)
	if target == "" {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(modelName), target)
}
