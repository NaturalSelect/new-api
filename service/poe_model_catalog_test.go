package service

import "testing"

const poeModelsSample = `{"data":[
{"id":"gemini-3.1-pro","supported_endpoints":["/v1/responses","/v1/chat/completions"],"parameters":[
  {"name":"thinking_level","schema":{"enum":["low","high"]},"default_value":"high"},
  {"name":"web_search","schema":{"type":"boolean"}}]},
{"id":"gemini-3-flash","supported_endpoints":["/v1/responses","/v1/chat/completions"],"parameters":[
  {"name":"thinking_level","schema":{"enum":["minimal","low","high"]}}]},
{"id":"gemini-3.8-flash","supported_endpoints":["/v1/chat/completions"],"parameters":[
  {"name":"thinking_level","schema":{"enum":["low","medium","high"]}}]},
{"id":"gemini-4-hydrogen","supported_endpoints":[],"parameters":[
  {"name":"thinking_level","schema":{"enum":["low","medium","high","xhigh"]}}]},
{"id":"Nano-Banana-2-Lite","supported_endpoints":["/V1/Responses"],"parameters":[]},
{"id":"gpt-5.2","supported_endpoints":["/v1/responses","/v1/chat/completions"],"parameters":[
  {"name":"reasoning_effort","schema":{"enum":["none","low","medium","high","xhigh"]}},
  {"name":"verbosity","schema":{"enum":["low","medium","high"]}}]},
{"id":"seed-2.0-pro","parameters":[
  {"name":"reasoning_effort","schema":{"enum":["low","medium","high"]}},
  {"name":"video_fps","schema":{"type":"number","minimum":0.2,"maximum":5}},
  {"name":"odd_numeric_enum","schema":{"enum":[1,2,3]}}]},
{"id":"gemma-4-31b","supported_endpoints":["/v1/responses","/v1/chat/completions"],"parameters":[]},
{"id":"manus","parameters":null}
]}`

func loadPoeModelsSample(t *testing.T) {
	t.Helper()
	catalog, err := parsePoeModelCatalog([]byte(poeModelsSample))
	if err != nil {
		t.Fatalf("parsePoeModelCatalog returned error: %v", err)
	}
	SetPoeModelCatalog(catalog)
	t.Cleanup(func() { SetPoeModelCatalog(nil) })
}

func TestParsePoeModelCatalogToleratesNonStringEnumAndNullParameters(t *testing.T) {
	catalog, err := parsePoeModelCatalog([]byte(poeModelsSample))
	if err != nil {
		t.Fatalf("parsePoeModelCatalog returned error: %v", err)
	}
	if len(catalog) != 9 {
		t.Fatalf("models parsed = %d, want 9", len(catalog))
	}
	if enum, ok := catalog["seed-2.0-pro"].Params["odd_numeric_enum"]; !ok || len(enum) != 0 {
		t.Fatalf("numeric enum = %v (declared=%v), want declared with no string values", enum, ok)
	}
	if _, ok := catalog["seed-2.0-pro"].Params["video_fps"]; !ok {
		t.Fatalf("parameter without enum should still be recorded as declared")
	}
	if got := catalog["nano-banana-2-lite"].Endpoints; len(got) != 1 || got[0] != "/v1/responses" {
		t.Fatalf("endpoints = %v, want lowercased /v1/responses under lowercased id", got)
	}
}

func TestResolvePoeReasoningParam(t *testing.T) {
	loadPoeModelsSample(t)

	tests := []struct {
		name      string
		model     string
		effort    string
		wantName  string
		wantValue string
		wantOK    bool
	}{
		{"gemini renames to thinking_level", "gemini-3.1-pro", "high", "thinking_level", "high", true},
		{"model name is case-insensitive", "Gemini-3.1-Pro", "low", "thinking_level", "low", true},
		{"tie between low and high prefers higher", "gemini-3.1-pro", "medium", "thinking_level", "high", true},
		{"none clamps up to lowest level", "gemini-3.1-pro", "none", "thinking_level", "low", true},
		{"xhigh clamps down to high", "gemini-3.1-pro", "xhigh", "thinking_level", "high", true},
		{"value present in enum is kept", "gemini-3-flash", "minimal", "thinking_level", "minimal", true},
		{"native reasoning_effort is kept", "gpt-5.2", "xhigh", "reasoning_effort", "xhigh", true},
		{"minimal on gpt never becomes none", "gpt-5.2", "minimal", "reasoning_effort", "low", true},
		{"reasoning_effort preferred and clamped", "seed-2.0-pro", "xhigh", "reasoning_effort", "high", true},
		{"unknown effort passes through", "gemini-3.1-pro", "turbo", "thinking_level", "turbo", true},
		{"model without reasoning param", "gemma-4-31b", "high", "", "", false},
		{"model with null parameters", "manus", "high", "", "", false},
		{"unknown model", "some-community-bot", "high", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, value, ok := ResolvePoeReasoningParam(tt.model, tt.effort)
			if ok != tt.wantOK || name != tt.wantName || value != tt.wantValue {
				t.Fatalf("ResolvePoeReasoningParam(%q, %q) = (%q, %q, %v), want (%q, %q, %v)",
					tt.model, tt.effort, name, value, ok, tt.wantName, tt.wantValue, tt.wantOK)
			}
		})
	}
}

func TestResolvePoeReasoningParamWithEmptyCache(t *testing.T) {
	SetPoeModelCatalog(nil)
	if _, _, ok := ResolvePoeReasoningParam("gemini-3.1-pro", "high"); ok {
		t.Fatalf("empty cache must not resolve any parameter")
	}
}

func TestShouldPoeResponsesUseChat(t *testing.T) {
	loadPoeModelsSample(t)

	tests := []struct {
		name  string
		model string
		want  bool
	}{
		{"endpoint list has responses", "gemini-3.1-pro", false},
		{"lookup is case-insensitive", "GPT-5.2", false},
		{"chat-only model falls back", "gemini-3.8-flash", true},
		{"empty endpoint list falls back", "gemini-4-hydrogen", true},
		{"missing endpoint field falls back", "seed-2.0-pro", true},
		{"responses-only model is not touched", "nano-banana-2-lite", false},
		{"unknown model is not touched", "some-community-bot", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ShouldPoeResponsesUseChat(tt.model); got != tt.want {
				t.Fatalf("ShouldPoeResponsesUseChat(%q) = %v, want %v", tt.model, got, tt.want)
			}
		})
	}
}

func TestShouldPoeResponsesUseChatWithEmptyCache(t *testing.T) {
	SetPoeModelCatalog(nil)
	if ShouldPoeResponsesUseChat("gemini-4-hydrogen") {
		t.Fatalf("empty cache must keep the client's endpoint")
	}
}
