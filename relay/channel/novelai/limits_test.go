package novelai

import (
	"bytes"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

func newTestInfo() *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "nai-diffusion-4-5-full"}}
}

func convertForTest(t *testing.T, body string) (any, error) {
	t.Helper()
	request := decodeClientImageRequest(t, body)
	return (&Adaptor{}).ConvertImageRequest(nil, newTestInfo(), *request)
}

func TestValidateSubscriptionLimitsBoundaries(t *testing.T) {
	tests := []struct {
		name    string
		width   int
		height  int
		steps   *int
		wantErr bool
	}{
		{name: "default size and steps", width: 1024, height: 1024},
		{name: "portrait inside the pixel budget", width: 832, height: 1216},
		{name: "landscape inside the pixel budget", width: 1216, height: 832},
		{name: "exactly the step limit", width: 1024, height: 1024, steps: ptrTo(maxSteps)},
		{name: "fewer steps", width: 1024, height: 1024, steps: ptrTo(1)},
		{name: "one step over", width: 1024, height: 1024, steps: ptrTo(maxSteps + 1), wantErr: true},
		{name: "one tile over the pixel budget", width: 1088, height: 1024, wantErr: true},
		{name: "large square", width: 1536, height: 1536, wantErr: true},
		{name: "tall canvas over the budget", width: 1024, height: 1472, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSubscriptionLimits(tt.width, tt.height, userOptions{Steps: tt.steps})
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestConvertImageRequestAcceptsRequestInsideLimits(t *testing.T) {
	converted, err := convertForTest(t, `{
		"model": "nai-diffusion-4-5-full",
		"prompt": "1girl",
		"size": "832x1216",
		"steps": 28
	}`)
	if err != nil {
		t.Fatalf("ConvertImageRequest: %v", err)
	}
	if _, ok := converted.(*bytes.Buffer); !ok {
		t.Fatalf("expected a multipart buffer, got %T", converted)
	}
}

func TestConvertImageRequestRejectsOversizedImage(t *testing.T) {
	if _, err := convertForTest(t, `{"model": "nai-diffusion-4-5-full", "prompt": "1girl", "size": "1536x1024"}`); err == nil {
		t.Fatal("expected an error for a size above the pixel limit")
	}
}

func TestConvertImageRequestRejectsTooManyStepsFromEveryInputPath(t *testing.T) {
	bodies := map[string]string{
		"top level":    `{"model": "nai-diffusion-4-5-full", "prompt": "p", "steps": 50}`,
		"extra_body":   `{"model": "nai-diffusion-4-5-full", "prompt": "p", "extra_body": {"steps": 50}}`,
		"extra_fields": `{"model": "nai-diffusion-4-5-full", "prompt": "p", "extra_fields": {"steps": 50}}`,
	}
	for name, body := range bodies {
		t.Run(name, func(t *testing.T) {
			if _, err := convertForTest(t, body); err == nil {
				t.Fatal("expected an error for steps above the limit")
			}
		})
	}
}

func TestConvertImageRequestLimitAppliesAfterOverrides(t *testing.T) {
	_, err := convertForTest(t, `{
		"model": "nai-diffusion-4-5-full",
		"prompt": "p",
		"steps": 10,
		"extra_body": {"steps": 20},
		"extra_fields": {"steps": 50}
	}`)
	if err == nil {
		t.Fatal("the highest priority steps value must be the one validated")
	}
}

func TestConvertImageRequestAllowsLoweredStepsOverHighTopLevel(t *testing.T) {
	_, err := convertForTest(t, `{
		"model": "nai-diffusion-4-5-full",
		"prompt": "p",
		"steps": 50,
		"extra_fields": {"steps": 20}
	}`)
	if err != nil {
		t.Fatalf("the effective steps value is inside the limit: %v", err)
	}
}
