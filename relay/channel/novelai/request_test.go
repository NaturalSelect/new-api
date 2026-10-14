package novelai

import (
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
)

func TestParseSize(t *testing.T) {
	tests := []struct {
		name       string
		size       string
		wantWidth  int
		wantHeight int
		wantErr    bool
	}{
		{name: "empty uses default", size: "", wantWidth: 1024, wantHeight: 1024},
		{name: "portrait", size: "832x1216", wantWidth: 832, wantHeight: 1216},
		{name: "uppercase separator", size: "1216X832", wantWidth: 1216, wantHeight: 832},
		{name: "not a multiple of 64", size: "1000x1000", wantErr: true},
		{name: "missing height", size: "1024", wantErr: true},
		{name: "non numeric", size: "axb", wantErr: true},
		{name: "zero", size: "0x0", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			width, height, err := parseSize(tt.size)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if width != tt.wantWidth || height != tt.wantHeight {
				t.Fatalf("got %dx%d, want %dx%d", width, height, tt.wantWidth, tt.wantHeight)
			}
		})
	}
}

func TestBuildGenerateRequestDefaults(t *testing.T) {
	req := buildGenerateRequest("nai-diffusion-4-5-full", "1girl", 832, 1216, userOptions{}, 42)

	if req.Input != "1girl" || req.Action != "generate" || req.Model != "nai-diffusion-4-5-full" {
		t.Fatalf("unexpected envelope: %+v", req)
	}
	params := req.Parameters
	if params.ParamsVersion != 3 {
		t.Fatalf("params_version = %d, want 3", params.ParamsVersion)
	}
	if params.Scale != defaultScale || params.Steps != defaultSteps || params.Sampler != defaultSampler {
		t.Fatalf("defaults not applied: %+v", params)
	}
	if params.Seed != 42 || params.Width != 832 || params.Height != 1216 {
		t.Fatalf("unexpected size or seed: %+v", params)
	}
	if params.V4Prompt.Caption.BaseCaption != "1girl" {
		t.Fatalf("v4 prompt caption = %q", params.V4Prompt.Caption.BaseCaption)
	}
	if params.UcPresetID != nil || params.StraightAlpha != nil {
		t.Fatal("v5 only fields must be absent for a non-v5 model")
	}
}

func TestBuildGenerateRequestKeepsExplicitZeroValues(t *testing.T) {
	zeroFloat := 0.0
	zeroSeed := uint32(0)
	zeroSteps := 0
	opts := userOptions{CfgRescale: &zeroFloat, Scale: &zeroFloat, Seed: &zeroSeed, Steps: &zeroSteps}

	req := buildGenerateRequest("nai-diffusion-4-5-full", "p", 64, 64, opts, *opts.Seed)

	if req.Parameters.Scale != 0 || req.Parameters.CfgRescale != 0 || req.Parameters.Steps != 0 {
		t.Fatalf("explicit zero values were replaced by defaults: %+v", req.Parameters)
	}
}

func TestBuildGenerateRequestAddsV5Fields(t *testing.T) {
	req := buildGenerateRequest("nai-diffusion-5-full", "p", 1024, 1024, userOptions{}, 1)

	params := req.Parameters
	if params.ParamsVersion != 4 {
		t.Fatalf("params_version = %d, want 4", params.ParamsVersion)
	}
	if params.UcPresetID == nil || *params.UcPresetID != "heavy" {
		t.Fatalf("ucPresetId = %v", params.UcPresetID)
	}
	if params.QualityPresetID == nil || *params.QualityPresetID != "standard" {
		t.Fatalf("qualityPresetId = %v", params.QualityPresetID)
	}
	if params.StraightAlpha == nil || !*params.StraightAlpha {
		t.Fatalf("straight_alpha = %v", params.StraightAlpha)
	}
	if params.TagHintQt == nil || *params.TagHintQt != 1 || params.TagHintUcPreset == nil || *params.TagHintUcPreset != 2 {
		t.Fatalf("tag hints = %v %v", params.TagHintQt, params.TagHintUcPreset)
	}
}

func TestBuildGenerateRequestSerializesNullSigmaAndOmitsV5Fields(t *testing.T) {
	req := buildGenerateRequest("nai-diffusion-4-5-full", "p", 64, 64, userOptions{}, 1)

	raw, err := common.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded struct {
		Parameters map[string]any `json:"parameters"`
	}
	if err := common.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if value, ok := decoded.Parameters["skip_cfg_above_sigma"]; !ok || value != nil {
		t.Fatalf("skip_cfg_above_sigma must be sent as null, got %v (present=%v)", value, ok)
	}
	if _, ok := decoded.Parameters["ucPresetId"]; ok {
		t.Fatal("ucPresetId must be omitted for a non-v5 model")
	}
}

func TestParseUserOptionsReadsExtraAndExtraFields(t *testing.T) {
	seed := uint32(0)
	req := dto.ImageRequest{
		Extra: map[string]json.RawMessage{
			"negative_prompt": []byte(`"lowres"`),
			"seed":            []byte(`7`),
			"unrelated":       []byte(`true`),
		},
		ExtraFields: []byte(`{"seed":0,"steps":20}`),
	}

	opts, err := parseUserOptions(req)
	if err != nil {
		t.Fatalf("parseUserOptions: %v", err)
	}
	if opts.NegativePrompt == nil || *opts.NegativePrompt != "lowres" {
		t.Fatalf("negative_prompt = %v", opts.NegativePrompt)
	}
	if opts.Seed == nil || *opts.Seed != seed {
		t.Fatalf("extra_fields must override extra and keep an explicit zero seed, got %v", opts.Seed)
	}
	if opts.Steps == nil || *opts.Steps != 20 {
		t.Fatalf("steps = %v", opts.Steps)
	}
	if opts.Scale != nil {
		t.Fatalf("scale must stay nil when not provided, got %v", *opts.Scale)
	}
}

func TestParseUserOptionsRejectsWrongTypes(t *testing.T) {
	req := dto.ImageRequest{Extra: map[string]json.RawMessage{"steps": []byte(`"many"`)}}

	if _, err := parseUserOptions(req); err == nil {
		t.Fatal("expected an error for a non numeric steps value")
	}
}

func TestBuildMultipartBodyRoundTrip(t *testing.T) {
	payload := buildGenerateRequest("nai-diffusion-4-5-full", "1girl", 832, 1216, userOptions{}, 9)

	body, contentType, err := buildMultipartBody(payload)
	if err != nil {
		t.Fatalf("buildMultipartBody: %v", err)
	}

	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType != "multipart/form-data" {
		t.Fatalf("content type = %q (%v)", contentType, err)
	}
	reader := multipart.NewReader(body, params["boundary"])
	part, err := reader.NextPart()
	if err != nil {
		t.Fatalf("next part: %v", err)
	}
	if part.FormName() != "request" || part.FileName() != "blob" {
		t.Fatalf("part name=%q filename=%q", part.FormName(), part.FileName())
	}
	if got := part.Header.Get("Content-Type"); got != "application/json" {
		t.Fatalf("part content type = %q", got)
	}
	raw, err := io.ReadAll(part)
	if err != nil {
		t.Fatalf("read part: %v", err)
	}
	var decoded generateRequest
	if err := common.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decode part: %v", err)
	}
	if decoded.Input != "1girl" || decoded.Parameters.Seed != 9 || decoded.Parameters.Width != 832 {
		t.Fatalf("round trip mismatch: %+v", decoded)
	}
}
