package openai

import (
	"net/http"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
)

const codexDisguiseOriginator = "codex_cli_rs"

// ApplyCodexDisguiseHeaders injects Codex CLI headers when the channel setting is enabled.
//
// When enabled, User-Agent is always overwritten with the Codex CLI UA. The
// `originator` header is set only if absent, so explicit header overrides or
// caller-supplied values are preserved.
func ApplyCodexDisguiseHeaders(c *gin.Context, req *http.Header, info *relaycommon.RelayInfo) {
	if info == nil || !info.ChannelOtherSettings.CodexDisguise {
		return
	}
	req.Set("User-Agent", dto.CodexDisguiseUserAgent)
	if req.Get("originator") == "" {
		req.Set("originator", codexDisguiseOriginator)
	}
}
