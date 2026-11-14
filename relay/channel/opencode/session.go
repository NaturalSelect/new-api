package opencode

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
)

// NOTE: These are the session headers of other coding agents, which the upstream also recognizes as conversation identity.
var clientSessionHeaders = []string{
	"Session_id",
	"X-Session-Id",
	"X-Session-Affinity",
	"X-Claude-Code-Session-Id",
}

// NOTE: The upstream refuses requests without this header and uses it to route a conversation and reuse its prompt cache,
// so the value must stay the same for every request of one conversation.
func resolveSessionID(c *gin.Context, info *relaycommon.RelayInfo) string {
	if c != nil && c.Request != nil {
		if id := strings.TrimSpace(c.Request.Header.Get(sessionHeader)); id != "" {
			return id
		}
	}
	return deriveSessionID(info.UserId, conversationKey(c, info))
}

func conversationKey(c *gin.Context, info *relaycommon.RelayInfo) string {
	if info.UpstreamPromptCacheKey != "" {
		return "cache:" + info.UpstreamPromptCacheKey
	}
	if info.UpstreamMetadataUserID != "" {
		return "metadata:" + info.UpstreamMetadataUserID
	}
	if c != nil && c.Request != nil {
		for _, name := range clientSessionHeaders {
			if value := strings.TrimSpace(c.Request.Header.Get(name)); value != "" {
				return "header:" + value
			}
		}
	}
	return fmt.Sprintf("token:%d:%s", info.TokenId, info.UpstreamModelName)
}

// NOTE: The id is derived instead of stored so it needs no cleanup, and it is scoped by user so unrelated users never share a session.
func deriveSessionID(userID int, key string) string {
	sum := sha256.Sum256([]byte(strconv.Itoa(userID) + "\x00" + key))
	return "ses_" + hex.EncodeToString(sum[:13])
}
