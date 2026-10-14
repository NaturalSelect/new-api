package novelai

const (
	ChannelName = "novelai"

	generateImagePath = "/ai/generate-image-stream"
	extraBodyKey      = "extra_body"
	defaultSize       = "1024x1024"
	sizeStep          = 64

	defaultScale   = 5.0
	defaultSteps   = 28
	defaultSampler = "k_dpmpp_2s_ancestral"

	defaultNoiseSchedule = "karras"
)

var ModelList = []string{
	"nai-diffusion-4-5-full",
	"nai-diffusion-4-5-curated",
}
