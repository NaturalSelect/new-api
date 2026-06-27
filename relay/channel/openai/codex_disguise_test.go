package openai

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func makeCodexRelayInfo(disguise bool) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelOtherSettings: dto.ChannelOtherSettings{
				CodexDisguise: disguise,
			},
		},
	}
}

func TestApplyCodexDisguiseHeaders_Disabled(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	req := http.Header{}
	req.Set("User-Agent", "original-agent")
	info := makeCodexRelayInfo(false)

	ApplyCodexDisguiseHeaders(c, &req, info)

	assert.Equal(t, "original-agent", req.Get("User-Agent"))
	assert.Equal(t, "", req.Get("originator"))
}

func TestApplyCodexDisguiseHeaders_Enabled(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)

	t.Run("no existing originator", func(t *testing.T) {
		req := http.Header{}
		info := makeCodexRelayInfo(true)
		ApplyCodexDisguiseHeaders(c, &req, info)

		assert.Equal(t, codexDisguiseUserAgent, req.Get("User-Agent"))
		assert.Equal(t, codexDisguiseOriginator, req.Get("originator"))
	})
}

func TestApplyCodexDisguiseHeaders_EnabledExistingOriginator(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	req := http.Header{}
	req.Set("originator", "custom")
	info := makeCodexRelayInfo(true)

	ApplyCodexDisguiseHeaders(c, &req, info)

	assert.Equal(t, codexDisguiseUserAgent, req.Get("User-Agent"))
	assert.Equal(t, "custom", req.Get("originator"))
}

func TestApplyCodexDisguiseHeaders_EnabledExistingUA(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	req := http.Header{}
	req.Set("User-Agent", "my-client/1.0")
	info := makeCodexRelayInfo(true)

	ApplyCodexDisguiseHeaders(c, &req, info)

	assert.Equal(t, codexDisguiseUserAgent, req.Get("User-Agent"))
}

func TestApplyCodexDisguiseHeaders_NilInfo(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	req := http.Header{}
	req.Set("User-Agent", "original")

	ApplyCodexDisguiseHeaders(c, &req, nil)

	assert.Equal(t, "original", req.Get("User-Agent"))
	assert.Equal(t, "", req.Get("originator"))
}
