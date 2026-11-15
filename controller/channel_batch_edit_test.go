package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func intRef(v int) *int       { return &v }
func strRef(v string) *string { return &v }

func TestValidateStatusCodeMapping(t *testing.T) {
	assert.NoError(t, validateStatusCodeMapping(""))
	assert.NoError(t, validateStatusCodeMapping(`{"400":"500","429":503}`))

	for _, bad := range []string{
		`not json`,
		`[]`,
		`{"abc":"500"}`,
		`{"99":"500"}`,
		`{"600":"500"}`,
		`{"400":"99"}`,
		`{"400":"x"}`,
		`{"400":1.5}`,
		`{"400":true}`,
	} {
		assert.Error(t, validateStatusCodeMapping(bad), bad)
	}
}

func TestBuildChannelBatchEditParams_Validation(t *testing.T) {
	cases := []struct {
		name string
		req  ChannelBatchEdit
	}{
		{"unknown models mode", ChannelBatchEdit{ModelsMode: "bogus", Models: []string{"a"}}},
		{"empty models for append", ChannelBatchEdit{ModelsMode: "append"}},
		{"unknown ignored mode", ChannelBatchEdit{IgnoredModelsMode: "bogus"}},
		{"empty ignored list for append", ChannelBatchEdit{IgnoredModelsMode: "append"}},
		{"blank group", ChannelBatchEdit{Group: strRef("  ")}},
		{"auto ban out of range", ChannelBatchEdit{AutoBan: intRef(2)}},
		{"optimistic out of range", ChannelBatchEdit{AutoBanOptimistic: intRef(-1)}},
		{"unban by balance out of range", ChannelBatchEdit{AutoUnbanByBalance: intRef(3)}},
		{"retry too high", ChannelBatchEdit{RetryOn429: intRef(51)}},
		{"retry negative", ChannelBatchEdit{RetryOn429: intRef(-1)}},
		{"disguise too high", ChannelBatchEdit{ClaudeCodeDisguiseMode: intRef(8)}},
		{"disguise negative", ChannelBatchEdit{ClaudeCodeDisguiseMode: intRef(-1)}},
		{"bad status code mapping", ChannelBatchEdit{StatusCodeMapping: strRef(`{"700":"500"}`)}},
		{"bad param override", ChannelBatchEdit{ParamOverride: strRef(`{`)}},
		{"bad header override", ChannelBatchEdit{HeaderOverride: strRef(`{`)}},
		{"bad model mapping", ChannelBatchEdit{ModelMapping: strRef(`{`)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := tc.req
			_, err := buildChannelBatchEditParams(&req)
			assert.Error(t, err)
		})
	}
}

func TestBuildChannelBatchEditParams_AcceptsBoundariesAndZeroValues(t *testing.T) {
	req := ChannelBatchEdit{
		ModelsMode:             "replace",
		Models:                 []string{" a ", "a", "b", ""},
		IgnoredModelsMode:      "replace",
		IgnoredModels:          nil,
		AutoBan:                intRef(0),
		AutoBanOptimistic:      intRef(1),
		RetryOn429:             intRef(0),
		ClaudeCodeDisguiseMode: intRef(7),
		StatusCodeMapping:      strRef("  "),
		Proxy:                  strRef("  http://p  "),
		SystemPrompt:           strRef("  keep spaces  "),
		ParamOverride:          strRef(""),
	}
	params, err := buildChannelBatchEditParams(&req)
	require.NoError(t, err)

	assert.Equal(t, []string{"a", "b"}, params.Models)
	assert.Equal(t, model.BatchModelsModeReplace, params.IgnoredModelsMode)
	assert.Empty(t, params.IgnoredModels)
	require.NotNil(t, params.AutoBan)
	assert.Equal(t, 0, *params.AutoBan)
	require.NotNil(t, params.RetryOn429)
	assert.Equal(t, 0, *params.RetryOn429)
	require.NotNil(t, params.StatusCodeMapping)
	assert.Equal(t, "", *params.StatusCodeMapping)
	require.NotNil(t, params.Proxy)
	assert.Equal(t, "http://p", *params.Proxy)
	require.NotNil(t, params.SystemPrompt)
	assert.Equal(t, "  keep spaces  ", *params.SystemPrompt)
	require.NotNil(t, params.ParamOverride)
	assert.Equal(t, "", *params.ParamOverride)
}
