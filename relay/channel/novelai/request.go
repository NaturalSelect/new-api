package novelai

import (
	"bytes"
	"fmt"
	"mime/multipart"
	"net/textproto"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
)

// parseUserOptions merges, from lowest to highest priority: top-level fields,
// a literal "extra_body" object that some SDKs forward without flattening, and "extra_fields".
func parseUserOptions(req dto.ImageRequest) (userOptions, error) {
	var opts userOptions
	if len(req.Extra) > 0 {
		raw, err := common.Marshal(req.Extra)
		if err != nil {
			return opts, fmt.Errorf("novelai adaptor: encode extra params failed: %w", err)
		}
		if err := common.Unmarshal(raw, &opts); err != nil {
			return opts, fmt.Errorf("novelai adaptor: invalid extra params: %w", err)
		}
	}
	if raw, ok := req.Extra[extraBodyKey]; ok {
		if err := common.Unmarshal(raw, &opts); err != nil {
			return opts, fmt.Errorf("novelai adaptor: invalid %s: %w", extraBodyKey, err)
		}
	}
	if len(req.ExtraFields) > 0 {
		if err := common.Unmarshal(req.ExtraFields, &opts); err != nil {
			return opts, fmt.Errorf("novelai adaptor: invalid extra_fields: %w", err)
		}
	}
	return opts, nil
}

func parseSize(size string) (int, int, error) {
	size = strings.TrimSpace(size)
	if size == "" {
		size = defaultSize
	}
	parts := strings.Split(strings.ToLower(size), "x")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("novelai adaptor: size %q must look like 1024x1024", size)
	}
	width, errWidth := strconv.Atoi(strings.TrimSpace(parts[0]))
	height, errHeight := strconv.Atoi(strings.TrimSpace(parts[1]))
	if errWidth != nil || errHeight != nil {
		return 0, 0, fmt.Errorf("novelai adaptor: size %q must look like 1024x1024", size)
	}
	if width <= 0 || height <= 0 || width%sizeStep != 0 || height%sizeStep != 0 {
		return 0, 0, fmt.Errorf("novelai adaptor: size %q must be positive multiples of %d", size, sizeStep)
	}
	return width, height, nil
}

func validateSubscriptionLimits(width, height int, opts userOptions) error {
	if width*height > maxPixels {
		return fmt.Errorf("novelai adaptor: size %dx%d exceeds the %d pixel limit", width, height, maxPixels)
	}
	if steps := valueOr(opts.Steps, defaultSteps); steps > maxSteps {
		return fmt.Errorf("novelai adaptor: steps %d exceeds the limit of %d", steps, maxSteps)
	}
	return nil
}

func isV5Model(model string) bool {
	return strings.Contains(model, "diffusion-5")
}

func buildGenerateRequest(model, prompt string, width, height int, opts userOptions, seed uint32) generateRequest {
	negative := valueOr(opts.NegativePrompt, "")
	v5 := isV5Model(model)

	params := parameters{
		ParamsVersion:                      3,
		Width:                              width,
		Height:                             height,
		Scale:                              valueOr(opts.Scale, defaultScale),
		Sampler:                            valueOr(opts.Sampler, defaultSampler),
		Steps:                              valueOr(opts.Steps, defaultSteps),
		NSamples:                           1,
		QualityToggle:                      true,
		ControlnetStrength:                 1,
		AddOriginalImage:                   true,
		CfgRescale:                         valueOr(opts.CfgRescale, 0),
		NoiseSchedule:                      valueOr(opts.NoiseSchedule, defaultNoiseSchedule),
		NormalizeReferenceStrengthMultiple: true,
		InpaintImg2ImgStrength:             1,
		Seed:                               seed,
		CharacterPrompts:                   []any{},
		V4Prompt: v4Prompt{
			Caption:  caption{BaseCaption: prompt, CharCaptions: []any{}},
			UseOrder: true,
		},
		V4NegativePrompt: v4NegativePrompt{
			Caption: caption{BaseCaption: negative, CharCaptions: []any{}},
		},
		NegativePrompt: negative,
		PreferBrownian: true,
		ImageFormat:    "png",
		Stream:         "msgpack",
	}

	if v5 {
		params.ParamsVersion = 4
		params.UcPresetID = ptrTo("heavy")
		params.QualityPresetID = ptrTo("standard")
		params.StraightAlpha = ptrTo(true)
		params.TagHintQt = ptrTo(1)
		params.TagHintUcPreset = ptrTo(2)
	}

	return generateRequest{
		Input:      prompt,
		Model:      model,
		Action:     "generate",
		Parameters: params,
	}
}

func buildMultipartBody(payload generateRequest) (*bytes.Buffer, string, error) {
	jsonBytes, err := common.Marshal(payload)
	if err != nil {
		return nil, "", fmt.Errorf("novelai adaptor: encode request failed: %w", err)
	}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", `form-data; name="request"; filename="blob"`)
	header.Set("Content-Type", "application/json")
	part, err := writer.CreatePart(header)
	if err != nil {
		return nil, "", fmt.Errorf("novelai adaptor: create multipart part failed: %w", err)
	}
	if _, err := part.Write(jsonBytes); err != nil {
		return nil, "", fmt.Errorf("novelai adaptor: write multipart part failed: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, "", fmt.Errorf("novelai adaptor: close multipart writer failed: %w", err)
	}
	return body, writer.FormDataContentType(), nil
}

func valueOr[T any](ptr *T, fallback T) T {
	if ptr == nil {
		return fallback
	}
	return *ptr
}

func ptrTo[T any](value T) *T {
	return &value
}
