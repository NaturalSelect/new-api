package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/pkg/modelcatalog"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/stretchr/testify/require"
)

func openRouterCatalogSampleBody(t *testing.T) []byte {
	t.Helper()
	body, err := os.ReadFile("../pkg/modelcatalog/testdata/openrouter_models_sample.json")
	require.NoError(t, err)
	return body
}

func TestFetchOpenRouterCatalogParsesResponse(t *testing.T) {
	var gotAccept string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAccept = r.Header.Get("Accept")
		_, _ = w.Write(openRouterCatalogSampleBody(t))
	}))
	defer server.Close()

	snap, err := fetchOpenRouterCatalog(context.Background(), server.Client(), server.URL)

	require.NoError(t, err)
	require.Equal(t, "application/json", gotAccept)
	require.Equal(t, 4, snap.Len())
	entry, ok := snap.Lookup("claude-fable-5.2")
	require.True(t, ok)
	require.Equal(t, 1_000_000, entry.ContextLength)
}

func TestFetchOpenRouterCatalogRejectsBadResponses(t *testing.T) {
	tests := map[string]http.HandlerFunc{
		"non-200 status": func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "boom", http.StatusBadGateway)
		},
		"empty model list": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"data":[]}`))
		},
		"malformed body": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`<html>not json</html>`))
		},
	}
	for name, handler := range tests {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(handler)
			defer server.Close()

			snap, err := fetchOpenRouterCatalog(context.Background(), server.Client(), server.URL)

			require.Error(t, err)
			require.Nil(t, snap)
		})
	}
}

func TestFetchOpenRouterCatalogRejectsNonHTTPURL(t *testing.T) {
	for _, u := range []string{"file:///etc/passwd", "ftp://example.com/models", "not a url", ""} {
		snap, err := fetchOpenRouterCatalog(context.Background(), http.DefaultClient, u)
		require.Error(t, err, u)
		require.Nil(t, snap)
	}
}

func TestOpenRouterCatalogSyncStep(t *testing.T) {
	t.Cleanup(modelcatalog.Clear)
	modelcatalog.Clear()

	var calls atomic.Int32
	var failing atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if failing.Load() {
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write(openRouterCatalogSampleBody(t))
	}))
	defer server.Close()

	cfg := operation_setting.OpenRouterCatalogSetting{
		Enabled:         true,
		RefreshInterval: 60,
		ModelsURL:       server.URL,
	}
	var state openRouterCatalogSyncState
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)

	runOpenRouterCatalogSyncStep(&state, cfg, now)
	require.EqualValues(t, 1, calls.Load())
	require.NotNil(t, modelcatalog.Current())
	require.Equal(t, now.Add(time.Hour), state.nextRefresh)

	runOpenRouterCatalogSyncStep(&state, cfg, now.Add(30*time.Minute))
	require.EqualValues(t, 1, calls.Load(), "no refetch before the interval elapses")

	runOpenRouterCatalogSyncStep(&state, cfg, now.Add(time.Hour))
	require.EqualValues(t, 2, calls.Load(), "refetch once the interval elapses")

	changed := cfg
	changed.ModelsURL = server.URL + "/other"
	runOpenRouterCatalogSyncStep(&state, changed, now.Add(time.Hour+time.Minute))
	require.EqualValues(t, 3, calls.Load(), "refetch immediately when the source changes")

	failing.Store(true)
	failAt := now.Add(3 * time.Hour)
	runOpenRouterCatalogSyncStep(&state, changed, failAt)
	require.EqualValues(t, 4, calls.Load())
	require.NotNil(t, modelcatalog.Current(), "previous catalog is kept after a failed refresh")
	require.Equal(t, failAt.Add(openRouterCatalogRetryDelay), state.nextRefresh)

	runOpenRouterCatalogSyncStep(&state, changed, failAt.Add(time.Minute))
	require.EqualValues(t, 4, calls.Load(), "failed refresh backs off before retrying")

	disabled := changed
	disabled.Enabled = false
	runOpenRouterCatalogSyncStep(&state, disabled, failAt.Add(2*time.Minute))
	require.Nil(t, modelcatalog.Current(), "disabling clears the catalog")
	require.Equal(t, openRouterCatalogSyncState{}, state)
	require.EqualValues(t, 4, calls.Load())
}

func TestOpenRouterCatalogSyncStepFailureWithoutCatalogRetriesLater(t *testing.T) {
	t.Cleanup(modelcatalog.Clear)
	modelcatalog.Clear()

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "down", http.StatusInternalServerError)
	}))
	defer server.Close()

	cfg := operation_setting.OpenRouterCatalogSetting{Enabled: true, RefreshInterval: 60, ModelsURL: server.URL}
	var state openRouterCatalogSyncState
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)

	runOpenRouterCatalogSyncStep(&state, cfg, now)
	runOpenRouterCatalogSyncStep(&state, cfg, now.Add(time.Minute))

	require.EqualValues(t, 1, calls.Load())
	require.Nil(t, modelcatalog.Current())
}
