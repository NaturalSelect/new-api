package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"

	"github.com/stretchr/testify/require"
)

const ocgoTestUsageBody = `{"usage":{"rolling":{"status":"ok","percent":0,"resetsAt":"2026-11-17T18:43:39.592Z"},"weekly":{"status":"ok","percent":33,"resetsAt":"2026-11-23T00:00:00.000Z"},"monthly":{"status":"ok","percent":16,"resetsAt":"2026-12-16T02:09:13.000Z"}}}`

func newOCGOUpstream(t *testing.T, goodKey string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/inference/go/v1/usage" || r.Header.Get("Authorization") != "Bearer "+goodKey {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(ocgoTestUsageBody))
	}))
	t.Cleanup(server.Close)
	return server
}

func resetOCGOState(t *testing.T) {
	t.Helper()
	clean := func() {
		model.DB.Exec("DELETE FROM channels")
		ocgoCacheMutex.Lock()
		ocgoCacheUsage = []OCGOUsageItem{}
		ocgoCacheUpdatedAt = 0
		ocgoCacheMutex.Unlock()
	}
	clean()
	t.Cleanup(clean)
}

func createOCGOChannel(t *testing.T, ch *model.Channel) {
	t.Helper()
	require.NoError(t, model.DB.Create(ch).Error)
}

func TestFetchOpenCodeGoUsageSendsKeyAndReturnsBody(t *testing.T) {
	var gotPath, gotAuth, gotUserAgent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotUserAgent = r.Header.Get("User-Agent")
		_, _ = w.Write([]byte(ocgoTestUsageBody))
	}))
	defer server.Close()

	status, body, err := FetchOpenCodeGoUsage(context.Background(), server.Client(), server.URL+"/inference/go/", " oc_sk_test ")

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, status)
	require.JSONEq(t, ocgoTestUsageBody, string(body))
	require.Equal(t, "/inference/go/v1/usage", gotPath)
	require.Equal(t, "Bearer oc_sk_test", gotAuth)
	require.Equal(t, openCodeGoUsageUserAgent, gotUserAgent)
}

func TestFetchOpenCodeGoUsageRejectsMissingInput(t *testing.T) {
	_, _, err := FetchOpenCodeGoUsage(context.Background(), nil, "https://example.com", "key")
	require.Error(t, err)
	_, _, err = FetchOpenCodeGoUsage(context.Background(), http.DefaultClient, " ", "key")
	require.Error(t, err)
	_, _, err = FetchOpenCodeGoUsage(context.Background(), http.DefaultClient, "https://example.com", " ")
	require.Error(t, err)
}

func TestParseOpenCodeGoUsage(t *testing.T) {
	usage, err := ParseOpenCodeGoUsage([]byte(ocgoTestUsageBody))
	require.NoError(t, err)
	require.Equal(t, 33.0, usage.Weekly.Percent)
	require.Equal(t, "ok", usage.Rolling.Status)
	require.Equal(t, "2026-12-16T02:09:13.000Z", usage.Monthly.ResetsAt)

	usage, err = ParseOpenCodeGoUsage([]byte(`{"usage":{"weekly":{"status":"ok","percent":5,"resetsAt":"x"}}}`))
	require.NoError(t, err)
	require.Nil(t, usage.Rolling)
	require.Nil(t, usage.Monthly)

	_, err = ParseOpenCodeGoUsage([]byte(`{"error":"nope"}`))
	require.Error(t, err)
	_, err = ParseOpenCodeGoUsage([]byte(`not json`))
	require.Error(t, err)
}

func TestBuildOCGOTargets(t *testing.T) {
	custom := "https://mirror.example.com/inference/go"
	channels := []*model.Channel{
		{Id: 1, Name: "single", Type: constant.ChannelTypeOpenCodeGo, Key: " key-a \n", Setting: ptr(`{"proxy":"http://127.0.0.1:7890"}`)},
		{Id: 2, Name: "blank", Type: constant.ChannelTypeOpenCodeGo, Key: "  "},
		{
			Id: 3, Name: "multi", Type: constant.ChannelTypeOpenCodeGo, Key: "k0\nk1\n\nk3", BaseURL: &custom,
			ChannelInfo: model.ChannelInfo{IsMultiKey: true, MultiKeyStatusList: map[int]int{1: common.ChannelStatusManuallyDisabled}},
		},
	}

	targets := buildOCGOTargets(channels)

	require.Len(t, targets, 3)
	require.Equal(t, ocgoTarget{
		id: "1", channelID: 1, name: "single", key: "key-a",
		baseURL: constant.ChannelBaseURLs[constant.ChannelTypeOpenCodeGo], proxy: "http://127.0.0.1:7890",
	}, targets[0])
	require.Equal(t, ocgoTarget{id: "3-0", channelID: 3, name: "multi #1", key: "k0", baseURL: custom}, targets[1])
	require.Equal(t, ocgoTarget{id: "3-3", channelID: 3, name: "multi #4", key: "k3", baseURL: custom}, targets[2])
}

func TestRunOCGOSyncOnceCollectsEnabledKeysAndReportsFailures(t *testing.T) {
	resetOCGOState(t)
	upstream := newOCGOUpstream(t, "good-key")
	base := upstream.URL + "/inference/go"

	createOCGOChannel(t, &model.Channel{Name: "good", Type: constant.ChannelTypeOpenCodeGo, Key: "good-key", Status: common.ChannelStatusEnabled, BaseURL: &base})
	createOCGOChannel(t, &model.Channel{Name: "bad", Type: constant.ChannelTypeOpenCodeGo, Key: "bad-key", Status: common.ChannelStatusEnabled, BaseURL: &base})
	createOCGOChannel(t, &model.Channel{Name: "disabled", Type: constant.ChannelTypeOpenCodeGo, Key: "good-key", Status: common.ChannelStatusManuallyDisabled, BaseURL: &base})
	createOCGOChannel(t, &model.Channel{Name: "other type", Type: constant.ChannelTypeOpenAI, Key: "good-key", Status: common.ChannelStatusEnabled, BaseURL: &base})

	usage, updatedAt, err := RefreshOCGOUsage()

	require.NoError(t, err)
	require.NotZero(t, updatedAt)
	require.Len(t, usage, 2)
	require.Equal(t, "good", usage[0].Name)
	require.Empty(t, usage[0].Error)
	require.Equal(t, 0.0, usage[0].Rolling.Percent)
	require.Equal(t, 33.0, usage[0].Weekly.Percent)
	require.Equal(t, "2026-12-16T02:09:13.000Z", usage[0].Monthly.ResetAt)
	require.Equal(t, "bad", usage[1].Name)
	require.Equal(t, "upstream status 401", usage[1].Error)
	require.Nil(t, usage[1].Weekly)

	cached, cachedAt := GetOCGOUsage()
	require.Equal(t, usage, cached)
	require.Equal(t, updatedAt, cachedAt)
}

func TestRunOCGOSyncOnceSendsRequestsThroughChannelProxy(t *testing.T) {
	resetOCGOState(t)
	var mu sync.Mutex
	var proxied []string
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		proxied = append(proxied, r.URL.String()+"|"+r.Header.Get("Authorization"))
		mu.Unlock()
		_, _ = w.Write([]byte(ocgoTestUsageBody))
	}))
	defer proxyServer.Close()

	// NOTE: The host does not resolve, so the call can only succeed by going through the proxy.
	unreachable := "http://ocgo-upstream.invalid/inference/go"
	setting := `{"proxy":"` + proxyServer.URL + `"}`
	createOCGOChannel(t, &model.Channel{Name: "proxied", Type: constant.ChannelTypeOpenCodeGo, Key: "proxy-key", Status: common.ChannelStatusEnabled, BaseURL: &unreachable, Setting: &setting})

	usage, _, err := RefreshOCGOUsage()

	require.NoError(t, err)
	require.Len(t, usage, 1)
	require.Empty(t, usage[0].Error)
	require.Equal(t, 33.0, usage[0].Weekly.Percent)
	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, []string{"http://ocgo-upstream.invalid/inference/go/v1/usage|Bearer proxy-key"}, proxied)
}

func TestRunOCGOSyncOnceReportsInvalidProxyWithoutLeakingIt(t *testing.T) {
	resetOCGOState(t)
	base := "http://127.0.0.1:1/inference/go"
	setting := `{"proxy":"http://user:secret@bad host:1"}`
	createOCGOChannel(t, &model.Channel{Name: "badproxy", Type: constant.ChannelTypeOpenCodeGo, Key: "k", Status: common.ChannelStatusEnabled, BaseURL: &base, Setting: &setting})

	usage, _, err := RefreshOCGOUsage()

	require.NoError(t, err)
	require.Len(t, usage, 1)
	require.Equal(t, "invalid proxy setting", usage[0].Error)
	require.NotContains(t, usage[0].Error, "secret")
}

func TestRunOCGOSyncOnceWithoutChannelsClearsSnapshot(t *testing.T) {
	resetOCGOState(t)
	ocgoCacheMutex.Lock()
	ocgoCacheUsage = []OCGOUsageItem{{ID: "stale"}}
	ocgoCacheMutex.Unlock()

	usage, updatedAt, err := RefreshOCGOUsage()

	require.NoError(t, err)
	require.NotNil(t, usage)
	require.Empty(t, usage)
	require.NotZero(t, updatedAt)
}

func ptr(s string) *string { return &s }
