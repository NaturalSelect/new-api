package novelai

type userOptions struct {
	NegativePrompt *string  `json:"negative_prompt,omitempty"`
	Scale          *float64 `json:"scale,omitempty"`
	Steps          *int     `json:"steps,omitempty"`
	Sampler        *string  `json:"sampler,omitempty"`
	Seed           *uint32  `json:"seed,omitempty"`
	CfgRescale     *float64 `json:"cfg_rescale,omitempty"`
	NoiseSchedule  *string  `json:"noise_schedule,omitempty"`
}

type generateRequest struct {
	Input      string     `json:"input"`
	Model      string     `json:"model"`
	Action     string     `json:"action"`
	Parameters parameters `json:"parameters"`
}

type parameters struct {
	ParamsVersion                      int              `json:"params_version"`
	Width                              int              `json:"width"`
	Height                             int              `json:"height"`
	Scale                              float64          `json:"scale"`
	Sampler                            string           `json:"sampler"`
	Steps                              int              `json:"steps"`
	NSamples                           int              `json:"n_samples"`
	UcPreset                           int              `json:"ucPreset"`
	QualityToggle                      bool             `json:"qualityToggle"`
	AutoSmea                           bool             `json:"autoSmea"`
	DynamicThresholding                bool             `json:"dynamic_thresholding"`
	ControlnetStrength                 int              `json:"controlnet_strength"`
	Legacy                             bool             `json:"legacy"`
	AddOriginalImage                   bool             `json:"add_original_image"`
	CfgRescale                         float64          `json:"cfg_rescale"`
	NoiseSchedule                      string           `json:"noise_schedule"`
	LegacyV3Extend                     bool             `json:"legacy_v3_extend"`
	SkipCfgAboveSigma                  *float64         `json:"skip_cfg_above_sigma"`
	UseCoords                          bool             `json:"use_coords"`
	LegacyUc                           bool             `json:"legacy_uc"`
	NormalizeReferenceStrengthMultiple bool             `json:"normalize_reference_strength_multiple"`
	InpaintImg2ImgStrength             int              `json:"inpaintImg2ImgStrength"`
	Seed                               uint32           `json:"seed"`
	CharacterPrompts                   []any            `json:"characterPrompts"`
	V4Prompt                           v4Prompt         `json:"v4_prompt"`
	V4NegativePrompt                   v4NegativePrompt `json:"v4_negative_prompt"`
	NegativePrompt                     string           `json:"negative_prompt"`
	DeliberateEulerAncestralBug        bool             `json:"deliberate_euler_ancestral_bug"`
	PreferBrownian                     bool             `json:"prefer_brownian"`
	ImageFormat                        string           `json:"image_format"`
	Stream                             string           `json:"stream"`

	UcPresetID      *string `json:"ucPresetId,omitempty"`
	QualityPresetID *string `json:"qualityPresetId,omitempty"`
	StraightAlpha   *bool   `json:"straight_alpha,omitempty"`
	TagHintQt       *int    `json:"tag_hint_qt,omitempty"`
	TagHintUcPreset *int    `json:"tag_hint_uc_preset,omitempty"`
}

type caption struct {
	BaseCaption  string `json:"base_caption"`
	CharCaptions []any  `json:"char_captions"`
}

type v4Prompt struct {
	Caption   caption `json:"caption"`
	UseCoords bool    `json:"use_coords"`
	UseOrder  bool    `json:"use_order"`
}

type v4NegativePrompt struct {
	Caption  caption `json:"caption"`
	LegacyUc bool    `json:"legacy_uc"`
}
