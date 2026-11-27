package openai

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"regexp"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

const maxUpstreamTextInError = 300

var markdownImagePattern = regexp.MustCompile(`!\[[^\]]*\]\((https?://[^)\s]+)\)`)

type chatImageResponse struct {
	Created int64           `json:"created"`
	Data    []chatImageData `json:"data"`
}

type chatImageData struct {
	Url string `json:"url"`
}

// ShouldServeImageViaChat reports whether an image request must be sent upstream as
// Chat Completions and answered in OpenAI image format, because Poe serves its image
// models only through the chat endpoint.
func ShouldServeImageViaChat(info *relaycommon.RelayInfo) bool {
	return info != nil && info.ChannelMeta != nil &&
		info.ChannelType == constant.ChannelTypePoeOpenAI &&
		(info.RelayMode == relayconstant.RelayModeImagesGenerations || info.RelayMode == relayconstant.RelayModeImagesEdits)
}

func (a *Adaptor) convertImageRequestToChat(c *gin.Context, info *relaycommon.RelayInfo, request dto.ImageRequest) (any, error) {
	extraBody := make(map[string]any)
	for name, value := range service.ResolvePoeImageParams(info.UpstreamModelName, request.Size, request.Quality) {
		extraBody[name] = value
	}

	message := dto.Message{Role: "user"}
	if info.RelayMode == relayconstant.RelayModeImagesEdits {
		images, mask, err := collectEditImageURLs(c, request)
		if err != nil {
			return nil, err
		}
		if mask != "" {
			if !service.PoeModelDeclaresParam(info.UpstreamModelName, "use_mask") {
				return nil, fmt.Errorf("model %s does not support image masks", info.UpstreamModelName)
			}
			images = append(images, mask)
			extraBody["use_mask"] = true
		}
		parts := []dto.MediaContent{{Type: dto.ContentTypeText, Text: request.Prompt}}
		for _, url := range images {
			parts = append(parts, dto.MediaContent{Type: dto.ContentTypeImageURL, ImageUrl: &dto.MessageImageUrl{Url: url}})
		}
		message.SetMediaContent(parts)
	} else {
		message.SetStringContent(request.Prompt)
	}

	chatRequest := &dto.GeneralOpenAIRequest{Model: request.Model, Messages: []dto.Message{message}}
	if len(extraBody) > 0 {
		marshaled, err := common.Marshal(extraBody)
		if err != nil {
			return nil, fmt.Errorf("error marshalling extra_body: %w", err)
		}
		chatRequest.ExtraBody = marshaled
	}
	a.imageViaChat = true
	return a.ConvertOpenAIRequest(c, info, chatRequest)
}

// collectEditImageURLs returns the images to edit and the optional mask, both as URLs
// or data URLs, from either a JSON or a multipart edit request.
func collectEditImageURLs(c *gin.Context, request dto.ImageRequest) (images []string, mask string, err error) {
	if isJSONRequest(c) {
		images = append(imageReferenceURLs(request.Images), imageReferenceURLs(request.Image)...)
		if len(images) == 0 {
			return nil, "", errors.New("image is required")
		}
		if masks := imageReferenceURLs(request.Mask); len(masks) > 0 {
			mask = masks[0]
		}
		return images, mask, nil
	}

	form := c.Request.MultipartForm
	if form == nil {
		if _, err := c.MultipartForm(); err != nil {
			return nil, "", errors.New("failed to parse multipart form")
		}
		form = c.Request.MultipartForm
	}
	files, err := editImageFiles(form)
	if err != nil {
		return nil, "", err
	}
	for _, file := range files {
		dataURL, err := readImageFileAsDataURL(file)
		if err != nil {
			return nil, "", err
		}
		images = append(images, dataURL)
	}
	if maskFiles := form.File["mask"]; len(maskFiles) > 0 {
		if mask, err = readImageFileAsDataURL(maskFiles[0]); err != nil {
			return nil, "", err
		}
	}
	return images, mask, nil
}

// editImageFiles returns the uploaded images of an edit request, accepting the field
// names image, image[] and image[N].
func editImageFiles(form *multipart.Form) ([]*multipart.FileHeader, error) {
	if files := form.File["image"]; len(files) > 0 {
		return files, nil
	}
	if files := form.File["image[]"]; len(files) > 0 {
		return files, nil
	}
	fieldNames := make([]string, 0, len(form.File))
	for fieldName := range form.File {
		if strings.HasPrefix(fieldName, "image[") {
			fieldNames = append(fieldNames, fieldName)
		}
	}
	sort.Strings(fieldNames)
	var files []*multipart.FileHeader
	for _, fieldName := range fieldNames {
		files = append(files, form.File[fieldName]...)
	}
	if len(files) == 0 {
		return nil, errors.New("image is required")
	}
	return files, nil
}

func readImageFileAsDataURL(fileHeader *multipart.FileHeader) (string, error) {
	file, err := fileHeader.Open()
	if err != nil {
		return "", fmt.Errorf("failed to open image file %s: %w", fileHeader.Filename, err)
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		return "", fmt.Errorf("failed to read image file %s: %w", fileHeader.Filename, err)
	}
	return "data:" + detectImageMimeType(fileHeader.Filename) + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}

// imageReferenceURLs extracts image URLs from an image field that holds a URL string,
// an {"image_url": "..."} object, or an array of either.
func imageReferenceURLs(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var value any
	if err := common.Unmarshal(raw, &value); err != nil {
		return nil
	}
	return collectImageReferenceURLs(value)
}

func collectImageReferenceURLs(value any) []string {
	switch v := value.(type) {
	case string:
		if v != "" {
			return []string{v}
		}
	case []any:
		var urls []string
		for _, item := range v {
			urls = append(urls, collectImageReferenceURLs(item)...)
		}
		return urls
	case map[string]any:
		if url, ok := v["image_url"].(string); ok && url != "" {
			return []string{url}
		}
	}
	return nil
}

// extractMarkdownImageURLs lists the images of a chat reply written as markdown, in
// order and without repeats. Poe repeats each image as a bare URL after the markdown.
func extractMarkdownImageURLs(content string) []string {
	matches := markdownImagePattern.FindAllStringSubmatch(content, -1)
	urls := make([]string, 0, len(matches))
	seen := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		if _, duplicate := seen[match[1]]; duplicate {
			continue
		}
		seen[match[1]] = struct{}{}
		urls = append(urls, match[1])
	}
	return urls
}

func truncateRunes(text string, limit int) string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit]) + "..."
}

// OaiChatToImageHandler answers an image request from a Chat Completions upstream
// response whose message carries the generated images as markdown.
func OaiChatToImageHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, types.NewOpenAIError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}
	defer service.CloseResponseBodyGracefully(resp)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}
	logger.LogDebug(c, "upstream chat response body: %s", body)

	var chatResp dto.OpenAITextResponse
	if err := common.Unmarshal(body, &chatResp); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if oaiError := chatResp.GetOpenAIError(); oaiError != nil && oaiError.Type != "" {
		return nil, types.WithOpenAIError(*oaiError, resp.StatusCode)
	}
	if len(chatResp.Choices) == 0 {
		return nil, types.NewOpenAIError(errors.New("empty response from upstream"), types.ErrorCodeEmptyResponse, http.StatusInternalServerError)
	}

	content := chatResp.Choices[0].Message.StringContent()
	urls := extractMarkdownImageURLs(content)
	if len(urls) == 0 {
		return nil, types.NewOpenAIError(
			fmt.Errorf("upstream returned no image: %s", truncateRunes(content, maxUpstreamTextInError)),
			types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}

	imageResp := chatImageResponse{Created: common.GetTimestamp(), Data: make([]chatImageData, len(urls))}
	for i, url := range urls {
		imageResp.Data[i].Url = url
	}
	responseBody, err := common.Marshal(imageResp)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeJsonMarshalFailed, http.StatusInternalServerError)
	}
	if imageRequestWantsB64JSON(info) {
		responseBody, err = convertImageURLsToB64JSON(c, responseBody)
		if err != nil {
			return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusBadGateway)
		}
	}

	// NOTE: Per-image pricing must count the images returned, not the n the client asked for,
	// because Poe answers one chat completion with however many images the bot produced.
	if info.PriceData.UsePrice {
		info.PriceData.AddOtherRatio("n", float64(len(urls)))
	}
	usage := chatResp.Usage
	if !service.ValidUsage(&usage) {
		usage = dto.Usage{}
	}
	service.IOCopyBytesGracefully(c, resp, responseBody)
	return &usage, nil
}
