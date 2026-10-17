package helper

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
)

const (
	DefaultPingInterval = 10 * time.Second

	pingFrame = ": PING\n\n"
)

type StreamKeepAliveOptions struct {
	// Interval <= 0 falls back to DefaultPingInterval.
	Interval time.Duration
	// MaxDuration <= 0 means no limit.
	MaxDuration time.Duration
	// WriteLocker, when set, serializes keep-alive writes with the caller's own writes.
	WriteLocker sync.Locker
	// OnFailure is called once when the keep-alive stops because a write failed or the goroutine panicked.
	OnFailure func(err error)
}

func StreamKeepAliveEnabled(info *relaycommon.RelayInfo) bool {
	return operation_setting.GetGeneralSetting().PingIntervalEnabled && !info.DisablePing
}

func StreamKeepAliveInterval() time.Duration {
	interval := time.Duration(operation_setting.GetGeneralSetting().PingIntervalSeconds) * time.Second
	if interval <= 0 {
		return DefaultPingInterval
	}
	return interval
}

// StartStreamKeepAlive periodically writes SSE keep-alive comments until the returned stop is called.
// stop is idempotent and returns only after the goroutine has exited, so no keep-alive write
// can race with the caller's writes once it returns.
func StartStreamKeepAlive(c *gin.Context, opts StreamKeepAliveOptions) (stop func()) {
	interval := opts.Interval
	if interval <= 0 {
		interval = DefaultPingInterval
	}

	stopChan := make(chan struct{})
	exited := make(chan struct{})
	var stopOnce sync.Once

	gopool.Go(func() {
		defer close(exited)
		defer func() {
			if r := recover(); r != nil {
				logger.LogError(c, fmt.Sprintf("keep-alive goroutine panic: %v", r))
				if opts.OnFailure != nil {
					opts.OnFailure(fmt.Errorf("keep-alive panic: %v", r))
				}
			}
		}()

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		var maxDurationC <-chan time.Time
		if opts.MaxDuration > 0 {
			maxTimer := time.NewTimer(opts.MaxDuration)
			defer maxTimer.Stop()
			maxDurationC = maxTimer.C
		}

		for {
			select {
			case <-ticker.C:
				select {
				case <-stopChan:
					return
				default:
				}
				if err := writeKeepAlive(c, opts.WriteLocker); err != nil {
					if c.Request.Context().Err() != nil {
						return
					}
					logger.LogError(c, "keep-alive write error: "+err.Error())
					if opts.OnFailure != nil {
						opts.OnFailure(err)
					}
					return
				}
				logger.LogDebug(c, "keep-alive sent")
			case <-stopChan:
				return
			case <-c.Request.Context().Done():
				return
			case <-maxDurationC:
				logger.LogError(c, "keep-alive max duration reached")
				return
			}
		}
	})

	return func() {
		stopOnce.Do(func() { close(stopChan) })
		<-exited
	}
}

func writeKeepAlive(c *gin.Context, locker sync.Locker) error {
	if locker != nil {
		locker.Lock()
		defer locker.Unlock()
	}
	return PingData(c)
}

// HasWrittenPayload reports whether anything other than keep-alive comments has reached the client.
// Keep-alive comments are invisible to SSE clients, so a request that only sent those is still retryable.
func HasWrittenPayload(c *gin.Context) bool {
	keepAliveBytes := 0
	if counter, ok := common.GetContextKeyType[*atomic.Int64](c, constant.ContextKeyStreamKeepAliveBytes); ok {
		keepAliveBytes = int(counter.Load())
	}
	return c.Writer.Size() > keepAliveBytes
}

func recordKeepAliveBytes(c *gin.Context, n int) {
	counter, ok := common.GetContextKeyType[*atomic.Int64](c, constant.ContextKeyStreamKeepAliveBytes)
	if !ok {
		counter = new(atomic.Int64)
		common.SetContextKey(c, constant.ContextKeyStreamKeepAliveBytes, counter)
	}
	counter.Add(int64(n))
}
