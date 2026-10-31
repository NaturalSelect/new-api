package opencodefree

import (
	"net/http"
	"strings"
	"time"
)

const (
	ChannelName = "opencode"

	// NOTE: Channel creation requires a non-empty key, but the free tier ignores it entirely.
	PlaceholderKey = "public"

	chatCompletionsPath = "/v1/chat/completions"

	// NOTE: The free tier rejects every other token, so the channel key is never sent upstream.
	upstreamToken = "public"
	// NOTE: The upstream validates the OpenCode client identity, so this mirrors the OpenCode CLI user agent.
	upstreamUserAgent = "opencode/latest/2.0.29/cli"

	// NOTE: The free tier only accepts requests that carry this built-in tool by name and schema shape.
	gateToolName        = "read"
	gateToolDescription = "Reserved for the host runtime; do not call or select this function. Use tools explicitly supplied by the user instead."

	aliasPrefix       = "oc2cpa_"
	maxToolNameLength = 64

	defaultMaxTokens = 32000

	firstEventTimeout = 30 * time.Second
	bodyIdleTimeout   = 120 * time.Second
	// NOTE: The upstream sometimes sends the usage frame right after [DONE].
	trailingUsageWindow = time.Second
	// NOTE: Content-less frames are held back so an upstream error right after them can still become an HTTP error.
	maxPendingFrames  = 8
	errorBodyMaxBytes = 64 * 1024
)

var ModelList = []string{
	"big-pickle",
	"mimo-v2.6-flash-free",
	"deepseek-v4.1-flash-free",
	"glm-5.3-flash-free",
	"beam-free",
	"flint-40b-free",
	"nemotron-3.5-super-vl-free",
	"nemotron-3.5-lightning-free",
}

// NOTE: The free tier serves big-pickle and every model whose id ends with -free.
func IsAllowedModel(id string) bool {
	return id == "big-pickle" || strings.HasSuffix(id, "-free")
}

func FetchModelsHeaders() http.Header {
	header := http.Header{}
	header.Set("Accept", "application/json")
	header.Set("Authorization", "Bearer "+upstreamToken)
	header.Set("User-Agent", upstreamUserAgent)
	return header
}
