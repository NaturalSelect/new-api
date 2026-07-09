package system_setting

import "github.com/QuantumNous/new-api/setting/config"

// SecureVerificationSettings 安全验证（2FA/Passkey）相关配置
type SecureVerificationSettings struct {
	// RequireForChannelKey 查看渠道密钥时是否要求安全验证（2FA/Passkey）
	RequireForChannelKey bool `json:"require_for_channel_key"`
	// RequirePasswordForChannelKey 查看渠道密钥时是否要求重新输入登录密码
	RequirePasswordForChannelKey bool `json:"require_password_for_channel_key"`
}

var secureVerificationSettings = SecureVerificationSettings{
	RequireForChannelKey:         true,  // 默认保持原有行为：需要验证
	RequirePasswordForChannelKey: false, // 默认关闭：新增的可选条件
}

func init() {
	config.GlobalConfig.Register("secure_verification", &secureVerificationSettings)
}

func GetSecureVerificationSettings() *SecureVerificationSettings {
	return &secureVerificationSettings
}
