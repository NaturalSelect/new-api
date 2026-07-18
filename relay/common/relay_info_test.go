package common

import (
	"testing"

	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestRelayInfoGetFinalRequestRelayFormatPrefersExplicitFinal(t *testing.T) {
	info := &RelayInfo{
		RelayFormat:             types.RelayFormatOpenAI,
		RequestConversionChain:  []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatClaude},
		FinalRequestRelayFormat: types.RelayFormatOpenAIResponses,
	}

	require.Equal(t, types.RelayFormat(types.RelayFormatOpenAIResponses), info.GetFinalRequestRelayFormat())
}

func TestRelayInfoGetFinalRequestRelayFormatFallsBackToConversionChain(t *testing.T) {
	info := &RelayInfo{
		RelayFormat:            types.RelayFormatOpenAI,
		RequestConversionChain: []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatClaude},
	}

	require.Equal(t, types.RelayFormat(types.RelayFormatClaude), info.GetFinalRequestRelayFormat())
}

func TestRelayInfoGetFinalRequestRelayFormatFallsBackToRelayFormat(t *testing.T) {
	info := &RelayInfo{
		RelayFormat: types.RelayFormatGemini,
	}

	require.Equal(t, types.RelayFormat(types.RelayFormatGemini), info.GetFinalRequestRelayFormat())
}

func TestRelayInfoGetFinalRequestRelayFormatNilReceiver(t *testing.T) {
	var info *RelayInfo
	require.Equal(t, types.RelayFormat(""), info.GetFinalRequestRelayFormat())
}

func TestExtractUpstreamIdentityBothPresent(t *testing.T) {
	info := &RelayInfo{}
	ExtractUpstreamIdentity([]byte(`{"prompt_cache_key":"abc","metadata":{"user_id":"xyz"}}`), info)

	require.Equal(t, "abc", info.UpstreamPromptCacheKey)
	require.Equal(t, "xyz", info.UpstreamMetadataUserID)
}

func TestExtractUpstreamIdentityOnlyPromptCacheKey(t *testing.T) {
	info := &RelayInfo{}
	ExtractUpstreamIdentity([]byte(`{"prompt_cache_key":"abc"}`), info)

	require.Equal(t, "abc", info.UpstreamPromptCacheKey)
	require.Equal(t, "", info.UpstreamMetadataUserID)
}

func TestExtractUpstreamIdentityEmptyBody(t *testing.T) {
	info := &RelayInfo{}
	ExtractUpstreamIdentity([]byte(`{}`), info)

	require.Equal(t, "", info.UpstreamPromptCacheKey)
	require.Equal(t, "", info.UpstreamMetadataUserID)
}

func TestExtractUpstreamIdentityMetadataNotObject(t *testing.T) {
	info := &RelayInfo{}
	ExtractUpstreamIdentity([]byte(`{"metadata":"not-an-object"}`), info)

	require.Equal(t, "", info.UpstreamPromptCacheKey)
	require.Equal(t, "", info.UpstreamMetadataUserID)
}

func TestExtractUpstreamIdentityNilInfoOrEmptyBody(t *testing.T) {
	require.NotPanics(t, func() {
		ExtractUpstreamIdentity([]byte(`{"prompt_cache_key":"abc"}`), nil)
	})

	info := &RelayInfo{}
	ExtractUpstreamIdentity(nil, info)
	require.Equal(t, "", info.UpstreamPromptCacheKey)
}
