package service

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/cachex"

	"github.com/samber/hot"
)

const (
	poeThoughtSignatureCacheNamespace = "new-api:poe_thought_signature:v1"
	poeThoughtSignatureTTL            = 24 * time.Hour
	poeThoughtSignatureMaxEntries     = 20_000
)

var (
	poeThoughtSignatureCache     *cachex.HybridCache[string]
	poeThoughtSignatureCacheOnce sync.Once
)

func getPoeThoughtSignatureCache() *cachex.HybridCache[string] {
	poeThoughtSignatureCacheOnce.Do(func() {
		poeThoughtSignatureCache = cachex.NewHybridCache[string](cachex.HybridCacheConfig[string]{
			Namespace: cachex.Namespace(poeThoughtSignatureCacheNamespace),
			Redis:     common.RDB,
			RedisEnabled: func() bool {
				return common.RedisEnabled && common.RDB != nil
			},
			RedisCodec: cachex.StringCodec{},
			Memory: func() *hot.HotCache[string, string] {
				return hot.NewHotCache[string, string](hot.LRU, poeThoughtSignatureMaxEntries).
					WithTTL(poeThoughtSignatureTTL).
					WithJanitor().
					Build()
			},
		})
	})
	return poeThoughtSignatureCache
}

// NOTE: The call id is client supplied, so it is hashed to keep keys short and uniform.
func poeThoughtSignatureKey(userID int, model, callID string) string {
	sum := sha256.Sum256([]byte(callID))
	return strconv.Itoa(userID) + ":" + strings.ToLower(strings.TrimSpace(model)) + ":" + hex.EncodeToString(sum[:16])
}

// StorePoeThoughtSignature remembers the Gemini thought signature Poe returned for a tool
// call. A Responses client cannot echo it back, so it is restored from here on the next turn.
func StorePoeThoughtSignature(userID int, model, callID, signature string) {
	if callID == "" || signature == "" {
		return
	}
	if err := getPoeThoughtSignatureCache().SetWithTTL(poeThoughtSignatureKey(userID, model, callID), signature, poeThoughtSignatureTTL); err != nil {
		common.SysError("failed to store poe thought signature: " + err.Error())
	}
}

// LoadPoeThoughtSignature returns the stored signature for a tool call, or "" on a miss.
func LoadPoeThoughtSignature(userID int, model, callID string) string {
	if callID == "" {
		return ""
	}
	signature, found, err := getPoeThoughtSignatureCache().Get(poeThoughtSignatureKey(userID, model, callID))
	if err != nil {
		common.SysError("failed to load poe thought signature: " + err.Error())
		return ""
	}
	if !found {
		return ""
	}
	return signature
}
