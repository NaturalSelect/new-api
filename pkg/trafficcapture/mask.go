package trafficcapture

import (
	"net/http"
	"net/url"
	"strings"
)

const (
	maskPrefix          = "***"
	maskKeepTailMinSize = 12
	maskKeepTailSize    = 4
)

var sensitiveHeaderNames = map[string]struct{}{
	"authorization":       {},
	"proxy-authorization": {},
	"x-api-key":           {},
	"x-goog-api-key":      {},
	"api-key":             {},
	"cookie":              {},
	"set-cookie":          {},
}

// sensitiveNameSegments are matched against whole "-" or "_" separated parts of a name, so
// "x-auth-token" is masked while rate-limit headers like "x-ratelimit-remaining-tokens" stay readable.
var sensitiveNameSegments = map[string]struct{}{
	"key":        {},
	"apikey":     {},
	"token":      {},
	"secret":     {},
	"signature":  {},
	"password":   {},
	"credential": {},
}

var sensitiveQueryParams = map[string]struct{}{
	"key":          {},
	"api_key":      {},
	"apikey":       {},
	"access_token": {},
	"token":        {},
	"signature":    {},
}

// MaskHeaders returns a copy of h where credential-bearing headers keep only a short tail,
// enough to tell apart multiple keys of the same channel.
func MaskHeaders(h http.Header) map[string][]string {
	masked := make(map[string][]string, len(h))
	for name, values := range h {
		copied := make([]string, len(values))
		sensitive := isSensitiveHeader(name)
		for i, value := range values {
			if sensitive {
				copied[i] = maskValue(value)
			} else {
				copied[i] = value
			}
		}
		masked[name] = copied
	}
	return masked
}

// MaskURL renders u with credential query parameters and any userinfo password masked.
func MaskURL(u *url.URL) string {
	if u == nil {
		return ""
	}
	masked := *u
	masked.RawQuery = maskRawQuery(u.RawQuery)
	return masked.Redacted()
}

func isSensitiveHeader(name string) bool {
	lower := strings.ToLower(name)
	if _, ok := sensitiveHeaderNames[lower]; ok {
		return true
	}
	for _, segment := range strings.FieldsFunc(lower, func(r rune) bool { return r == '-' || r == '_' }) {
		if _, ok := sensitiveNameSegments[segment]; ok {
			return true
		}
	}
	return false
}

func maskRawQuery(rawQuery string) string {
	if rawQuery == "" {
		return ""
	}
	pairs := strings.Split(rawQuery, "&")
	for i, pair := range pairs {
		rawName, rawValue, hasValue := strings.Cut(pair, "=")
		if !hasValue {
			continue
		}
		name, err := url.QueryUnescape(rawName)
		if err != nil {
			continue
		}
		if _, ok := sensitiveQueryParams[strings.ToLower(name)]; !ok {
			continue
		}
		value, err := url.QueryUnescape(rawValue)
		if err != nil {
			value = rawValue
		}
		pairs[i] = rawName + "=" + strings.ReplaceAll(url.QueryEscape(maskValue(value)), "%2A", "*")
	}
	return strings.Join(pairs, "&")
}

func maskValue(value string) string {
	if len(value) < maskKeepTailMinSize {
		return maskPrefix
	}
	return maskPrefix + value[len(value)-maskKeepTailSize:]
}
