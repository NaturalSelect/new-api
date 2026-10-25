package trafficcapture

import (
	"sync"

	"github.com/QuantumNous/new-api/common"

	"github.com/klauspost/compress/zstd"
)

// maxDecodedBytes bounds decompression so a corrupted payload cannot allocate without limit.
const maxDecodedBytes = 512 << 20

var (
	codecOnce sync.Once
	encoder   *zstd.Encoder
	decoder   *zstd.Decoder
	codecErr  error
)

func initCodec() error {
	codecOnce.Do(func() {
		encoder, codecErr = zstd.NewWriter(nil)
		if codecErr != nil {
			return
		}
		decoder, codecErr = zstd.NewReader(nil, zstd.WithDecoderMaxMemory(maxDecodedBytes))
	})
	return codecErr
}

// Encode serializes rec and compresses it. rawSize is the JSON size before compression.
func Encode(rec Record) (payload []byte, rawSize int64, err error) {
	if err = initCodec(); err != nil {
		return nil, 0, err
	}
	raw, err := common.Marshal(rec)
	if err != nil {
		return nil, 0, err
	}
	return encoder.EncodeAll(raw, make([]byte, 0, len(raw)/4)), int64(len(raw)), nil
}

func Decode(payload []byte) (Record, error) {
	var rec Record
	if err := initCodec(); err != nil {
		return rec, err
	}
	raw, err := decoder.DecodeAll(payload, nil)
	if err != nil {
		return rec, err
	}
	err = common.Unmarshal(raw, &rec)
	return rec, err
}
