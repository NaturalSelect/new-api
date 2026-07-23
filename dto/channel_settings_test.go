package dto

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func ptr(v int) *int { return &v }

// TestEffectiveClaudeCodeDisguiseMode verifies the three-branch fallback logic:
//   - mode pointer set (even to 0) → use it directly, never fall back to legacy bool
//   - mode pointer nil + legacy bool true → full disguise
//   - mode pointer nil + legacy bool false (or nil receiver) → no disguise
func TestEffectiveClaudeCodeDisguiseMode(t *testing.T) {
	t.Run("nil receiver returns 0", func(t *testing.T) {
		var s *ChannelOtherSettings
		assert.Equal(t, 0, s.EffectiveClaudeCodeDisguiseMode())
	})

	t.Run("mode pointer nil, legacy false → 0", func(t *testing.T) {
		s := &ChannelOtherSettings{}
		assert.Equal(t, 0, s.EffectiveClaudeCodeDisguiseMode())
	})

	t.Run("mode pointer nil, legacy true → ClaudeDisguiseFull (backwards compat)", func(t *testing.T) {
		s := &ChannelOtherSettings{ClaudeCodeDisguise: true}
		assert.Equal(t, ClaudeDisguiseFull, s.EffectiveClaudeCodeDisguiseMode())
	})

	t.Run("mode pointer 7 (full) → 7 regardless of legacy bool", func(t *testing.T) {
		s := &ChannelOtherSettings{
			ClaudeCodeDisguise:     true,
			ClaudeCodeDisguiseMode: ptr(ClaudeDisguiseFull),
		}
		assert.Equal(t, ClaudeDisguiseFull, s.EffectiveClaudeCodeDisguiseMode())
	})

	t.Run("mode pointer 1 (UA only) → 1", func(t *testing.T) {
		s := &ChannelOtherSettings{
			ClaudeCodeDisguiseMode: ptr(ClaudeDisguiseUA),
		}
		assert.Equal(t, ClaudeDisguiseUA, s.EffectiveClaudeCodeDisguiseMode())
	})

	t.Run("mode pointer 0 (user explicitly disabled) → 0, NOT falling back to legacy true", func(t *testing.T) {
		// This is the critical case: a channel that previously had ClaudeCodeDisguise=true,
		// but the user opened the new UI and unchecked every dimension. The new UI writes
		// ClaudeCodeDisguiseMode=ptr(0). Without pointer semantics, 0 == unset and the
		// old bool would still return ClaudeDisguiseFull — that was the bug.
		s := &ChannelOtherSettings{
			ClaudeCodeDisguise:     true, // legacy field still present from old data
			ClaudeCodeDisguiseMode: ptr(0),
		}
		assert.Equal(t, 0, s.EffectiveClaudeCodeDisguiseMode(),
			"explicit mode=0 must disable disguise even when legacy bool is true")
	})

	t.Run("mode pointer 6 (Header+SystemPrompt, no UA)", func(t *testing.T) {
		s := &ChannelOtherSettings{
			ClaudeCodeDisguiseMode: ptr(ClaudeDisguiseHeader | ClaudeDisguiseSystemPrompt),
		}
		assert.Equal(t, 6, s.EffectiveClaudeCodeDisguiseMode())
		mode := s.EffectiveClaudeCodeDisguiseMode()
		assert.Zero(t, mode&ClaudeDisguiseUA, "UA bit must be off")
		assert.NotZero(t, mode&ClaudeDisguiseHeader, "Header bit must be on")
		assert.NotZero(t, mode&ClaudeDisguiseSystemPrompt, "SystemPrompt bit must be on")
	})
}
