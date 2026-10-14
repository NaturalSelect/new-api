package novelai

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"

	"github.com/ugorji/go/codec"
)

const (
	finalEventType = "final"
	maxZipImageLen = 64 << 20
	errorHeadLen   = 64
)

var (
	pngMagic  = []byte("\x89PNG\r\n\x1a\n")
	jpegMagic = []byte("\xff\xd8\xff")
	zipMagic  = []byte("PK")

	msgpackHandle = newMsgpackHandle()
)

func newMsgpackHandle() *codec.MsgpackHandle {
	handle := &codec.MsgpackHandle{}
	handle.MapType = reflect.TypeOf(map[string]any(nil))
	handle.WriteExt = true
	return handle
}

// extractImage returns the generated image from a generate-image-stream response body.
func extractImage(raw []byte) ([]byte, error) {
	if isImageBytes(raw) {
		return raw, nil
	}
	if bytes.HasPrefix(raw, zipMagic) {
		if img, ok := firstPNGInZip(raw); ok {
			return img, nil
		}
	}

	events, decodeErr := decodeEvents(raw)
	if img := largestImage(finalEvents(events)); img != nil {
		return img, nil
	}
	if decodeErr != nil {
		return nil, fmt.Errorf("novelai adaptor: decode response stream failed after %d events: %w", len(events), decodeErr)
	}
	if img := largestImage(events); img != nil {
		return img, nil
	}
	return nil, fmt.Errorf("novelai adaptor: no image in response (events=%d, head=%q)", len(events), head(raw))
}

func decodeEvents(raw []byte) ([]any, error) {
	events, framed, err := decodeFramedEvents(raw)
	if framed {
		return events, err
	}
	return decodeBareEvents(raw)
}

// decodeFramedEvents reads 4-byte big-endian length prefixed msgpack frames.
// framed is false when the first frame is not a valid frame, so the caller can try another layout.
func decodeFramedEvents(raw []byte) (events []any, framed bool, err error) {
	pos := 0
	for pos < len(raw) {
		remaining := len(raw) - pos
		if remaining < 4 {
			return events, len(events) > 0, errors.New("truncated frame header")
		}
		frameLen := int(binary.BigEndian.Uint32(raw[pos : pos+4]))
		if frameLen == 0 || frameLen > remaining-4 {
			if len(events) == 0 {
				return nil, false, nil
			}
			return events, true, fmt.Errorf("truncated frame at offset %d", pos)
		}
		event, decodeErr := decodeSingleObject(raw[pos+4 : pos+4+frameLen])
		if decodeErr != nil {
			if len(events) == 0 {
				return nil, false, nil
			}
			return events, true, fmt.Errorf("frame at offset %d: %w", pos, decodeErr)
		}
		events = append(events, event)
		pos += 4 + frameLen
	}
	return events, true, nil
}

func decodeBareEvents(raw []byte) ([]any, error) {
	decoder := codec.NewDecoder(bytes.NewReader(raw), msgpackHandle)
	var events []any
	for {
		var event any
		if err := decoder.Decode(&event); err != nil {
			if errors.Is(err, io.EOF) {
				return events, nil
			}
			return events, err
		}
		events = append(events, event)
	}
}

func decodeSingleObject(frame []byte) (any, error) {
	var event any
	if err := codec.NewDecoderBytes(frame, msgpackHandle).Decode(&event); err != nil {
		return nil, err
	}
	return event, nil
}

func finalEvents(events []any) []any {
	var finals []any
	for _, event := range events {
		fields, ok := event.(map[string]any)
		if !ok {
			continue
		}
		if eventType, _ := fields["event_type"].(string); eventType == finalEventType {
			finals = append(finals, event)
		}
	}
	return finals
}

func largestImage(events []any) []byte {
	var best []byte
	for _, event := range events {
		for _, img := range collectImages(event, nil) {
			if len(img) > len(best) {
				best = img
			}
		}
	}
	return best
}

func collectImages(node any, found [][]byte) [][]byte {
	switch value := node.(type) {
	case []byte:
		if img, ok := imageFromBytes(value); ok {
			return append(found, img)
		}
	case string:
		if img, ok := imageFromBase64([]byte(value)); ok {
			return append(found, img)
		}
	case map[string]any:
		for _, child := range value {
			found = collectImages(child, found)
		}
	case []any:
		for _, child := range value {
			found = collectImages(child, found)
		}
	}
	return found
}

func imageFromBytes(data []byte) ([]byte, bool) {
	if isImageBytes(data) {
		return data, true
	}
	return imageFromBase64(data)
}

func imageFromBase64(encoded []byte) ([]byte, bool) {
	decoded, err := base64.StdEncoding.DecodeString(string(encoded))
	if err != nil || !isImageBytes(decoded) {
		return nil, false
	}
	return decoded, true
}

func isImageBytes(data []byte) bool {
	return bytes.HasPrefix(data, pngMagic) || bytes.HasPrefix(data, jpegMagic)
}

func firstPNGInZip(raw []byte) ([]byte, bool) {
	archive, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, false
	}
	for _, file := range archive.File {
		if !strings.HasSuffix(strings.ToLower(file.Name), ".png") {
			continue
		}
		reader, err := file.Open()
		if err != nil {
			continue
		}
		data, err := io.ReadAll(io.LimitReader(reader, maxZipImageLen))
		_ = reader.Close()
		if err == nil && len(data) > 0 {
			return data, true
		}
	}
	return nil, false
}

func head(raw []byte) []byte {
	if len(raw) > errorHeadLen {
		return raw[:errorHeadLen]
	}
	return raw
}
