package service

import "testing"

const poeModelsSample = `{"data":[
{"id":"gemini-3.1-pro","parameters":[
  {"name":"thinking_level","schema":{"enum":["low","high"]},"default_value":"high"},
  {"name":"web_search","schema":{"type":"boolean"}}]},
{"id":"gemini-3-flash","parameters":[
  {"name":"thinking_level","schema":{"enum":["minimal","low","high"]}}]},
{"id":"gpt-5.2","parameters":[
  {"name":"reasoning_effort","schema":{"enum":["none","low","medium","high","xhigh"]}},
  {"name":"verbosity","schema":{"enum":["low","medium","high"]}}]},
{"id":"seed-2.0-pro","parameters":[
  {"name":"reasoning_effort","schema":{"enum":["low","medium","high"]}},
  {"name":"video_fps","schema":{"type":"number","minimum":0.2,"maximum":5}},
  {"name":"odd_numeric_enum","schema":{"enum":[1,2,3]}}]},
{"id":"gemma-4-31b","parameters":[]},
{"id":"manus","parameters":null}
]}`

func loadPoeModelsSample(t *testing.T) {
	t.Helper()
	params, err := parsePoeModelParams([]byte(poeModelsSample))
	if err != nil {
		t.Fatalf("parsePoeModelParams returned error: %v", err)
	}
	SetPoeModelParams(params)
	t.Cleanup(func() { SetPoeModelParams(nil) })
}

func TestParsePoeModelParamsToleratesNonStringEnumAndNullParameters(t *testing.T) {
	params, err := parsePoeModelParams([]byte(poeModelsSample))
	if err != nil {
		t.Fatalf("parsePoeModelParams returned error: %v", err)
	}
	if len(params) != 6 {
		t.Fatalf("models parsed = %d, want 6", len(params))
	}
	if enum, ok := params["seed-2.0-pro"]["odd_numeric_enum"]; !ok || len(enum) != 0 {
		t.Fatalf("numeric enum = %v (declared=%v), want declared with no string values", enum, ok)
	}
	if _, ok := params["seed-2.0-pro"]["video_fps"]; !ok {
		t.Fatalf("parameter without enum should still be recorded as declared")
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
	SetPoeModelParams(nil)
	if _, _, ok := ResolvePoeReasoningParam("gemini-3.1-pro", "high"); ok {
		t.Fatalf("empty cache must not resolve any parameter")
	}
}
