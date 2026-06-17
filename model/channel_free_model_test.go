package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChannelIsFreeModel(t *testing.T) {
	setting := `{"free_models":["gpt-4o"," gemini-2.5-pro-thinking-* ",""]}`
	channel := &Channel{Setting: &setting}

	require.True(t, channel.IsFreeModel("gpt-4o"))
	require.True(t, channel.IsFreeModel("gemini-2.5-pro-thinking-1024"))
	require.True(t, channel.IsFreeModel("gemini-2.5-pro-thinking-*"))
	require.False(t, channel.IsFreeModel("gpt-4o-mini"))
	require.False(t, channel.IsFreeModel(""))
}

func TestChannelIsFreeModelDoesNotBroadenExactEntries(t *testing.T) {
	setting := `{"free_models":["gemini-2.5-pro-thinking-1024"]}`
	channel := &Channel{Setting: &setting}

	require.True(t, channel.IsFreeModel("gemini-2.5-pro-thinking-1024"))
	require.False(t, channel.IsFreeModel("gemini-2.5-pro-thinking-2048"))
}

func TestSelectFreeModelChannelOnlyUsesFreeSubset(t *testing.T) {
	paidSetting := `{"free_models":["paid-model"]}`
	freeSetting := `{"free_models":["free-model"]}`
	channels := []*Channel{
		{Id: 1, Setting: &paidSetting},
		{Id: 2, Setting: &freeSetting},
	}

	selected := selectFreeModelChannel("free-model", channels)
	require.NotNil(t, selected)
	require.Equal(t, 2, selected.Id)
	require.Nil(t, selectFreeModelChannel("missing-model", channels))
}
