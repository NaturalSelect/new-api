package openai

import (
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
	"golang.org/x/sync/errgroup"
)

func imageRequestWantsB64JSON(info *relaycommon.RelayInfo) bool {
	if info == nil {
		return false
	}
	imageReq, ok := info.Request.(*dto.ImageRequest)
	return ok && strings.EqualFold(imageReq.ResponseFormat, "b64_json")
}

// convertImageURLsToB64JSON replaces every upstream image URL in an OpenAI image
// response with the downloaded image as b64_json, leaving other fields untouched.
func convertImageURLsToB64JSON(c *gin.Context, body []byte) ([]byte, error) {
	items := gjson.GetBytes(body, "data")
	if !items.IsArray() {
		return body, nil
	}

	type download struct {
		index int
		url   string
	}
	var downloads []download
	for i, item := range items.Array() {
		url := strings.TrimSpace(item.Get("url").String())
		if url == "" || item.Get("b64_json").String() != "" {
			continue
		}
		downloads = append(downloads, download{index: i, url: url})
	}
	if len(downloads) == 0 {
		return body, nil
	}

	results := make([]string, len(downloads))
	var g errgroup.Group
	for i, d := range downloads {
		g.Go(func() error {
			_, b64, err := service.GetImageFromUrl(d.url)
			if err != nil {
				// NOTE: Log the detail only; the error text carries the upstream URL and must not reach the client.
				logger.LogError(c, fmt.Sprintf("openai_image_url_to_b64_failed: index=%d err=%s", d.index, err.Error()))
				return fmt.Errorf("failed to download image %d", d.index)
			}
			results[i] = b64
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}

	for i, d := range downloads {
		path := fmt.Sprintf("data.%d", d.index)
		var err error
		if body, err = sjson.SetBytes(body, path+".b64_json", results[i]); err != nil {
			return nil, errors.New("failed to rewrite image response")
		}
		if body, err = sjson.DeleteBytes(body, path+".url"); err != nil {
			return nil, errors.New("failed to rewrite image response")
		}
	}
	return body, nil
}
