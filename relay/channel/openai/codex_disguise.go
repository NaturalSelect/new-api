package openai

import (
	"net/http"

	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
)

const (
	codexDisguiseUserAgent   = "codex_cli_rs/0.42.0"
	codexDisguiseOriginator = "codex_cli_rs"
)

// ApplyCodexDisguiseHeaders injects Codex CLI headers when the channel setting is enabled.
//
// When enabled, User-Agent is always overwritten with the Codex CLI UA. The
// `originator` header is set only if absent, so explicit header overrides or
// caller-supplied values are preserved.
func ApplyCodexDisguiseHeaders(c *gin.Context, req *http.Header, info *relaycommon.RelayInfo) {
	if info == nil || !info.ChannelOtherSettings.CodexDisguise {
		return
	}
	req.Set("User-Agent", codexDisguiseUserAgent)
	if req.Get("originator") == "" {
		req.Set("originator", codexDisguiseOriginator)
	}
}
