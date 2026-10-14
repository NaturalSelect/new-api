package novelai

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"testing"

	"github.com/ugorji/go/codec"
)

var (
	previewPNG = append(append([]byte{}, pngMagic...), []byte("preview")...)
	finalPNG   = append(append([]byte{}, pngMagic...), []byte("final-image-data-longer")...)
)

func encodeMsgpack(t *testing.T, value any) []byte {
	t.Helper()
	var buf []byte
	if err := codec.NewEncoderBytes(&buf, msgpackHandle).Encode(value); err != nil {
		t.Fatalf("encode msgpack: %v", err)
	}
	return buf
}

func frame(payload []byte) []byte {
	prefix := make([]byte, 4)
	binary.BigEndian.PutUint32(prefix, uint32(len(payload)))
	return append(prefix, payload...)
}

func TestExtractImagePrefersFinalEventOverLargerPreview(t *testing.T) {
	bigPreview := append(append([]byte{}, pngMagic...), bytes.Repeat([]byte("p"), 1024)...)
	stream := append(
		frame(encodeMsgpack(t, map[string]any{"event_type": "intermediate", "image": bigPreview})),
		frame(encodeMsgpack(t, map[string]any{"event_type": "final", "image": finalPNG}))...,
	)

	got, err := extractImage(stream)
	if err != nil {
		t.Fatalf("extractImage: %v", err)
	}
	if !bytes.Equal(got, finalPNG) {
		t.Fatalf("expected the final image, got %d bytes", len(got))
	}
}

func TestExtractImageFallsBackToLargestWithoutFinalEvent(t *testing.T) {
	stream := append(
		frame(encodeMsgpack(t, map[string]any{"image": previewPNG})),
		frame(encodeMsgpack(t, map[string]any{"image": finalPNG}))...,
	)

	got, err := extractImage(stream)
	if err != nil {
		t.Fatalf("extractImage: %v", err)
	}
	if !bytes.Equal(got, finalPNG) {
		t.Fatalf("expected the largest image, got %d bytes", len(got))
	}
}

func TestExtractImageReadsBase64StringAndNestedValues(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString(finalPNG)
	stream := frame(encodeMsgpack(t, map[string]any{
		"event_type": "final",
		"data":       []any{map[string]any{"image": encoded}},
	}))

	got, err := extractImage(stream)
	if err != nil {
		t.Fatalf("extractImage: %v", err)
	}
	if !bytes.Equal(got, finalPNG) {
		t.Fatalf("expected the base64 image, got %d bytes", len(got))
	}
}

func TestExtractImageReadsBareMsgpackStream(t *testing.T) {
	stream := append(
		encodeMsgpack(t, map[string]any{"event_type": "intermediate", "image": previewPNG}),
		encodeMsgpack(t, map[string]any{"event_type": "final", "image": finalPNG})...,
	)

	got, err := extractImage(stream)
	if err != nil {
		t.Fatalf("extractImage: %v", err)
	}
	if !bytes.Equal(got, finalPNG) {
		t.Fatalf("expected the final image, got %d bytes", len(got))
	}
}

func TestExtractImageAcceptsRawPNG(t *testing.T) {
	got, err := extractImage(finalPNG)
	if err != nil {
		t.Fatalf("extractImage: %v", err)
	}
	if !bytes.Equal(got, finalPNG) {
		t.Fatal("expected the raw image back")
	}
}

func TestExtractImageAcceptsZipArchive(t *testing.T) {
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	entry, err := writer.Create("image_0.png")
	if err != nil {
		t.Fatalf("create zip entry: %v", err)
	}
	if _, err := entry.Write(finalPNG); err != nil {
		t.Fatalf("write zip entry: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}

	got, err := extractImage(archive.Bytes())
	if err != nil {
		t.Fatalf("extractImage: %v", err)
	}
	if !bytes.Equal(got, finalPNG) {
		t.Fatal("expected the zipped image back")
	}
}

func TestExtractImageReportsTruncatedStream(t *testing.T) {
	complete := frame(encodeMsgpack(t, map[string]any{"event_type": "intermediate", "image": previewPNG}))
	second := frame(encodeMsgpack(t, map[string]any{"event_type": "final", "image": finalPNG}))
	stream := append(complete, second[:len(second)-5]...)

	if _, err := extractImage(stream); err == nil {
		t.Fatal("expected an error for a truncated stream")
	}
}

func TestExtractImageReportsMissingImage(t *testing.T) {
	stream := frame(encodeMsgpack(t, map[string]any{"event_type": "final", "message": "no image here"}))

	if _, err := extractImage(stream); err == nil {
		t.Fatal("expected an error when no image is present")
	}
}

func TestExtractImageHandlesEmptyBody(t *testing.T) {
	if _, err := extractImage(nil); err == nil {
		t.Fatal("expected an error for an empty body")
	}
}
