package novelai

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
)

func decodeClientImageRequest(t *testing.T, body string) *dto.ImageRequest {
	t.Helper()
	var parsed dto.ImageRequest
	if err := common.Unmarshal([]byte(body), &parsed); err != nil {
		t.Fatalf("decode client body: %v", err)
	}
	copied, err := common.DeepCopy(&parsed)
	if err != nil {
		t.Fatalf("deep copy: %v", err)
	}
	return copied
}

func TestTopLevelExtraBodySurvivesDecodeAndDeepCopy(t *testing.T) {
	request := decodeClientImageRequest(t, `{
		"model": "nai-diffusion-4-5-full",
		"prompt": "1girl",
		"negative_prompt": "lowres, bad anatomy",
		"seed": 0,
		"steps": 20
	}`)

	opts, err := parseUserOptions(*request)
	if err != nil {
		t.Fatalf("parseUserOptions: %v", err)
	}
	upstream := buildGenerateRequest(request.Model, request.Prompt, 1024, 1024, opts, valueOr(opts.Seed, 123))

	params := upstream.Parameters
	if params.NegativePrompt != "lowres, bad anatomy" || params.V4NegativePrompt.Caption.BaseCaption != "lowres, bad anatomy" {
		t.Fatalf("negative prompt was lost: %q / %q", params.NegativePrompt, params.V4NegativePrompt.Caption.BaseCaption)
	}
	if params.Seed != 0 || params.Steps != 20 {
		t.Fatalf("seed=%d steps=%d, want 0 and 20", params.Seed, params.Steps)
	}
}

func TestUnknownExtraBodyFieldsAreIgnored(t *testing.T) {
	request := decodeClientImageRequest(t, `{
		"model": "nai-diffusion-4-5-full",
		"prompt": "1girl",
		"negative_prompt": "lowres",
		"vendor_flag": {"nested": [1, 2, 3]},
		"another": "value"
	}`)

	opts, err := parseUserOptions(*request)
	if err != nil {
		t.Fatalf("unknown extra fields must not fail the request: %v", err)
	}
	if opts.NegativePrompt == nil || *opts.NegativePrompt != "lowres" {
		t.Fatalf("known field next to unknown ones was lost: %v", opts.NegativePrompt)
	}
}

func TestNoExtraBodyKeepsDefaults(t *testing.T) {
	request := decodeClientImageRequest(t, `{"model": "nai-diffusion-4-5-full", "prompt": "1girl"}`)

	opts, err := parseUserOptions(*request)
	if err != nil {
		t.Fatalf("parseUserOptions: %v", err)
	}
	if opts != (userOptions{}) {
		t.Fatalf("expected no user options, got %+v", opts)
	}
}

func TestNestedExtraBodyFromUnflatteningSDKIsRead(t *testing.T) {
	request := decodeClientImageRequest(t, `{
		"model": "nai-diffusion-4-5-full",
		"prompt": "1girl",
		"extra_body": {"negative_prompt": "lowres", "seed": 0, "scale": 0, "steps": 20}
	}`)

	opts, err := parseUserOptions(*request)
	if err != nil {
		t.Fatalf("parseUserOptions: %v", err)
	}
	if opts.NegativePrompt == nil || *opts.NegativePrompt != "lowres" {
		t.Fatalf("negative_prompt inside extra_body was lost: %v", opts.NegativePrompt)
	}
	if opts.Seed == nil || *opts.Seed != 0 || opts.Scale == nil || *opts.Scale != 0 {
		t.Fatalf("explicit zero values inside extra_body were lost: seed=%v scale=%v", opts.Seed, opts.Scale)
	}
	if opts.Steps == nil || *opts.Steps != 20 {
		t.Fatalf("steps = %v", opts.Steps)
	}
}

func TestExtraBodyOverridesTopLevelAndExtraFieldsOverridesExtraBody(t *testing.T) {
	request := decodeClientImageRequest(t, `{
		"model": "nai-diffusion-4-5-full",
		"prompt": "1girl",
		"negative_prompt": "top",
		"steps": 10,
		"scale": 3,
		"extra_body": {"negative_prompt": "body", "steps": 20},
		"extra_fields": {"steps": 30}
	}`)

	opts, err := parseUserOptions(*request)
	if err != nil {
		t.Fatalf("parseUserOptions: %v", err)
	}
	if opts.NegativePrompt == nil || *opts.NegativePrompt != "body" {
		t.Fatalf("extra_body must override top-level, got %v", opts.NegativePrompt)
	}
	if opts.Steps == nil || *opts.Steps != 30 {
		t.Fatalf("extra_fields must override extra_body, got %v", opts.Steps)
	}
	if opts.Scale == nil || *opts.Scale != 3 {
		t.Fatalf("top-level value absent from the other layers must survive, got %v", opts.Scale)
	}
}

func TestNullExtraBodyIsIgnored(t *testing.T) {
	request := decodeClientImageRequest(t, `{
		"model": "nai-diffusion-4-5-full",
		"prompt": "1girl",
		"negative_prompt": "lowres",
		"extra_body": null
	}`)

	opts, err := parseUserOptions(*request)
	if err != nil {
		t.Fatalf("a null extra_body must not fail the request: %v", err)
	}
	if opts.NegativePrompt == nil || *opts.NegativePrompt != "lowres" {
		t.Fatalf("top-level value was lost: %v", opts.NegativePrompt)
	}
}

func TestNonObjectExtraBodyIsRejected(t *testing.T) {
	request := decodeClientImageRequest(t, `{
		"model": "nai-diffusion-4-5-full",
		"prompt": "1girl",
		"extra_body": "negative_prompt=lowres"
	}`)

	if _, err := parseUserOptions(*request); err == nil {
		t.Fatal("expected an error when extra_body is not an object")
	}
}
