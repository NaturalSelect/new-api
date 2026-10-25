package trafficcapture

const (
	RecordVersion  = 1
	EncodingText   = "text"
	EncodingBase64 = "base64"
)

// Record is the full picture of one relayed request: what the client sent and received,
// plus every attempt made against an upstream provider.
type Record struct {
	Version     int               `json:"version"`
	RequestId   string            `json:"request_id"`
	NodeName    string            `json:"node_name"`
	StartedAtMs int64             `json:"started_at_ms"`
	DurationMs  int64             `json:"duration_ms"`
	UserId      int               `json:"user_id"`
	TokenId     int               `json:"token_id"`
	ModelName   string            `json:"model_name"`
	Group       string            `json:"group"`
	ClientIp    string            `json:"client_ip"`
	Failed      bool              `json:"failed"`
	Client      Exchange          `json:"client"`
	Upstream    []UpstreamAttempt `json:"upstream"`
}

type Exchange struct {
	Method          string              `json:"method"`
	Url             string              `json:"url"`
	RequestHeaders  map[string][]string `json:"request_headers"`
	RequestBody     CapturedBody        `json:"request_body"`
	Status          int                 `json:"status"`
	ResponseHeaders map[string][]string `json:"response_headers"`
	ResponseBody    CapturedBody        `json:"response_body"`
}

type UpstreamAttempt struct {
	RetryIndex  int    `json:"retry_index"`
	ChannelId   int    `json:"channel_id"`
	ChannelType int    `json:"channel_type"`
	Error       string `json:"error"`
	Exchange
}

// CapturedBody keeps at most the configured number of bytes of a body. Size is always the
// full original length, so Truncated bodies still report how much was dropped.
type CapturedBody struct {
	Encoding    string `json:"encoding"`
	Data        string `json:"data"`
	Size        int64  `json:"size"`
	Truncated   bool   `json:"truncated"`
	FirstByteMs int64  `json:"first_byte_ms"`
	LastByteMs  int64  `json:"last_byte_ms"`
}
