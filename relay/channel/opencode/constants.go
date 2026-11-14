package opencode

const (
	ChannelName = "opencode"

	sessionHeader = "x-opencode-session"

	chatCompletionsPath = "/openai/v1/chat/completions"
	responsesPath       = "/openai/v1/responses"
	messagesPath        = "/anthropic/v1/messages"

	defaultAnthropicVersion = "2023-06-01"
)

// NOTE: The upstream catalog changes often, so models are fetched from its public /v1/models endpoint instead.
var ModelList = []string{}
