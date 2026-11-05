package typesafe

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
)

// RewriteRequestPart returns the multipart body with the "request" part
// replaced by requestJSON. The boundary, the order of the parts and every
// other part stay byte-for-byte as the client sent them.
func RewriteRequestPart(body []byte, contentType string, requestJSON []byte) ([]byte, error) {
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return nil, fmt.Errorf("invalid multipart content type: %w", err)
	}
	if len(mediaType) < len("multipart/") || mediaType[:len("multipart/")] != "multipart/" {
		return nil, errors.New("content type is not multipart")
	}
	boundary := params["boundary"]
	if boundary == "" {
		return nil, errors.New("multipart boundary is missing")
	}

	reader := multipart.NewReader(bytes.NewReader(body), boundary)
	var out bytes.Buffer
	writer := multipart.NewWriter(&out)
	if err := writer.SetBoundary(boundary); err != nil {
		return nil, err
	}

	replaced := false
	for {
		part, err := reader.NextRawPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read multipart part: %w", err)
		}
		dst, err := writer.CreatePart(part.Header)
		if err != nil {
			return nil, err
		}
		if !replaced && part.FormName() == "request" && part.FileName() == "" {
			if _, err := io.Copy(io.Discard, part); err != nil {
				return nil, err
			}
			if _, err := dst.Write(requestJSON); err != nil {
				return nil, err
			}
			replaced = true
			continue
		}
		if _, err := io.Copy(dst, part); err != nil {
			return nil, err
		}
	}
	if !replaced {
		return nil, errors.New("multipart request field is missing")
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
