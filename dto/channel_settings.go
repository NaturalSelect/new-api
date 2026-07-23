package dto

// MaxRetryOn429 渠道 429 原地重试的最大次数
const MaxRetryOn429 = 50

type ChannelSettings struct {
	ForceFormat            bool     `json:"force_format,omitempty"`
	ThinkingToContent      bool     `json:"thinking_to_content,omitempty"`
	Proxy                  string   `json:"proxy"`
	PassThroughBodyEnabled bool     `json:"pass_through_body_enabled,omitempty"`
	SystemPrompt           string   `json:"system_prompt,omitempty"`
	SystemPromptOverride   bool     `json:"system_prompt_override,omitempty"`
	FreeModels             []string `json:"free_models,omitempty"`
	RetryOn429             int      `json:"retry_on_429,omitempty"`
}

type VertexKeyType string

const (
	VertexKeyTypeJSON   VertexKeyType = "json"
	VertexKeyTypeAPIKey VertexKeyType = "api_key"
)

type AwsKeyType string

const (
	AwsKeyTypeAKSK   AwsKeyType = "ak_sk" // 默认
	AwsKeyTypeApiKey AwsKeyType = "api_key"
)

// Claude Code 伪装努力程度 bitmask 位定义。
// 每一位独立控制伪装的一个维度，可任意组合。
const (
	ClaudeDisguiseUA           = 1 << 0 // 仅伪装 User-Agent
	ClaudeDisguiseHeader       = 1 << 1 // 仅伪装其余 Header（X-App、anthropic-beta）
	ClaudeDisguiseSystemPrompt = 1 << 2 // 仅伪装 System Prompt / body（system 注入、user system 迁移、metadata.user_id 规范化）
	ClaudeDisguiseFull         = ClaudeDisguiseUA | ClaudeDisguiseHeader | ClaudeDisguiseSystemPrompt // 完全伪装
)

// Claude Code / Codex CLI 伪装写入的固定 UA 与 Header 值。
// 定义在 dto 包，供 relay/channel/claude、relay/channel/openai（写入伪装值）与
// relay/channel（渠道亲和 header 覆盖后的保护逻辑）三方共享，避免相互 import 造成循环依赖。
const (
	ClaudeCodeDisguiseUserAgent = "claude-cli/2.1.50"
	ClaudeCodeDisguiseXApp      = "claude-code"
	CodexDisguiseUserAgent      = "codex_cli_rs/0.42.0"
)

type ChannelOtherSettings struct {
	AzureResponsesVersion                 string        `json:"azure_responses_version,omitempty"`
	VertexKeyType                         VertexKeyType `json:"vertex_key_type,omitempty"` // "json" or "api_key"
	OpenRouterEnterprise                  *bool         `json:"openrouter_enterprise,omitempty"`
	ClaudeBetaQuery                       bool          `json:"claude_beta_query,omitempty"`         // Claude 渠道是否强制追加 ?beta=true
	ClaudeCodeDisguise                    bool          `json:"claude_code_disguise,omitempty"`      // Deprecated: 已被 ClaudeCodeDisguiseMode 取代，仅用于旧数据兼容（true 等价于 ClaudeDisguiseFull）
	ClaudeCodeDisguiseMode                *int          `json:"claude_code_disguise_mode,omitempty"` // NOTE: Claude Code 伪装努力程度 bitmask，见 ClaudeDisguise* 常量。使用指针以区分"未设置"（nil，沿用 ClaudeCodeDisguise 兼容判断）与"显式设为 0"（用户主动关闭，不再回退到旧字段）
	CodexDisguise                         bool          `json:"codex_disguise,omitempty"`            // NOTE: 是否将请求伪装成 Codex CLI (codex_cli_rs)
	AutoCacheControl                      bool          `json:"auto_cache_control,omitempty"`        // NOTE: 是否自动注入提示缓存控制（Claude cache_control / OpenAI prompt_cache_retention）
	AllowServiceTier                      bool          `json:"allow_service_tier,omitempty"`        // 是否允许 service_tier 透传（默认过滤以避免额外计费）
	AllowInferenceGeo                     bool          `json:"allow_inference_geo,omitempty"`       // 是否允许 inference_geo 透传（仅 Claude，默认过滤以满足数据驻留合规
	AllowSpeed                            bool          `json:"allow_speed,omitempty"`               // 是否允许 speed 透传（仅 Claude，默认过滤以避免意外切换推理速度模式）
	AllowSafetyIdentifier                 bool          `json:"allow_safety_identifier,omitempty"`   // 是否允许 safety_identifier 透传（默认过滤以保护用户隐私）
	DisableStore                          bool          `json:"disable_store,omitempty"`             // 是否禁用 store 透传（默认允许透传，禁用后可能导致 Codex 无法使用）
	AllowIncludeObfuscation               bool          `json:"allow_include_obfuscation,omitempty"` // 是否允许 stream_options.include_obfuscation 透传（默认过滤以避免关闭流混淆保护）
	AwsKeyType                            AwsKeyType    `json:"aws_key_type,omitempty"`
	UpstreamModelUpdateCheckEnabled       bool          `json:"upstream_model_update_check_enabled,omitempty"`        // 是否检测上游模型更新
	UpstreamModelUpdateAutoSyncEnabled    bool          `json:"upstream_model_update_auto_sync_enabled,omitempty"`    // 是否自动同步上游模型更新
	UpstreamModelUpdateLastCheckTime      int64         `json:"upstream_model_update_last_check_time,omitempty"`      // 上次检测时间
	UpstreamModelUpdateLastDetectedModels []string      `json:"upstream_model_update_last_detected_models,omitempty"` // 上次检测到的可加入模型
	UpstreamModelUpdateLastRemovedModels  []string      `json:"upstream_model_update_last_removed_models,omitempty"`  // 上次检测到的可删除模型
	UpstreamModelUpdateIgnoredModels      []string      `json:"upstream_model_update_ignored_models,omitempty"`       // 手动忽略的模型
}

func (s *ChannelOtherSettings) IsOpenRouterEnterprise() bool {
	if s == nil || s.OpenRouterEnterprise == nil {
		return false
	}
	return *s.OpenRouterEnterprise
}

// EffectiveClaudeCodeDisguiseMode returns the effective Claude Code disguise
// bitmask, providing backward compatibility with the legacy ClaudeCodeDisguise
// bool field.
//
// ClaudeCodeDisguiseMode is a pointer so "unset" (nil — legacy channel that has
// never been saved through the new mode-aware form) and "explicitly zero" (user
// unchecked every dimension and saved) are distinguishable: a plain int/bool
// zero value is indistinguishable from a field absent in JSON, which would
// otherwise make it impossible to ever turn disguise fully off on a channel
// that still carries the legacy ClaudeCodeDisguise=true.
//   - mode == nil: fall back to the legacy bool (true => ClaudeDisguiseFull)
//   - mode != nil: use *mode as-is (0 means the user explicitly disabled disguise)
func (s *ChannelOtherSettings) EffectiveClaudeCodeDisguiseMode() int {
	if s == nil {
		return 0
	}
	if s.ClaudeCodeDisguiseMode != nil {
		return *s.ClaudeCodeDisguiseMode
	}
	if s.ClaudeCodeDisguise {
		return ClaudeDisguiseFull
	}
	return 0
}
