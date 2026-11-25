package modelcatalog

import (
	"errors"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
)

type Entry struct {
	ID                  string
	Name                string
	Description         string
	Tokenizer           string
	Modality            string
	HuggingFaceID       string
	Created             int64
	ContextLength       int
	TopContextLength    int
	MaxCompletionTokens int
	InputModalities     []string
	OutputModalities    []string
	KnowledgeCutoff     string
	SupportedParameters []string
}

type Snapshot struct {
	exact   map[string]*Entry
	loose   map[string]*Entry
	entries int
}

func (s *Snapshot) Len() int {
	if s == nil {
		return 0
	}
	return s.entries
}

type rawModel struct {
	ID              string  `json:"id"`
	Name            string  `json:"name"`
	Created         int64   `json:"created"`
	Description     string  `json:"description"`
	ContextLength   int     `json:"context_length"`
	KnowledgeCutoff *string `json:"knowledge_cutoff"`
	HuggingFaceID   string  `json:"hugging_face_id"`
	Architecture    struct {
		Modality         string   `json:"modality"`
		InputModalities  []string `json:"input_modalities"`
		OutputModalities []string `json:"output_modalities"`
		Tokenizer        string   `json:"tokenizer"`
	} `json:"architecture"`
	TopProvider struct {
		ContextLength       int  `json:"context_length"`
		MaxCompletionTokens *int `json:"max_completion_tokens"`
	} `json:"top_provider"`
	SupportedParameters []string `json:"supported_parameters"`
}

type rawResponse struct {
	Data []rawModel `json:"data"`
}

var (
	claudeVersionDotPattern = regexp.MustCompile(`(\d+)\.(\d+)`)
	dateSuffixPattern       = regexp.MustCompile(`-(\d{8}|\d{4}-\d{2}-\d{2}|latest)$`)

	current atomic.Pointer[Snapshot]
)

// NOTE: Drops the provider prefix and ":" variants; Claude dot versions become Anthropic's dash form.
func SimplifyOpenRouterID(id string) (string, bool) {
	if strings.Contains(id, ":") {
		return "", false
	}
	if idx := strings.IndexByte(id, '/'); idx >= 0 {
		id = id[idx+1:]
	}
	if strings.HasPrefix(id, "claude-") {
		id = claudeVersionDotPattern.ReplaceAllString(id, "$1-$2")
	}
	return id, true
}

func Parse(body []byte) (*Snapshot, error) {
	var resp rawResponse
	if err := common.Unmarshal(body, &resp); err != nil {
		return nil, err
	}

	snap := &Snapshot{
		exact: make(map[string]*Entry, len(resp.Data)*2),
		loose: make(map[string]*Entry, len(resp.Data)),
	}
	for i := range resp.Data {
		raw := &resp.Data[i]
		id := strings.TrimSpace(raw.ID)
		if id == "" || strings.Contains(id, ":") {
			continue
		}
		entry := &Entry{
			ID:                  id,
			Name:                raw.Name,
			Description:         raw.Description,
			Tokenizer:           raw.Architecture.Tokenizer,
			Modality:            raw.Architecture.Modality,
			HuggingFaceID:       raw.HuggingFaceID,
			Created:             raw.Created,
			ContextLength:       raw.ContextLength,
			TopContextLength:    raw.TopProvider.ContextLength,
			InputModalities:     raw.Architecture.InputModalities,
			OutputModalities:    raw.Architecture.OutputModalities,
			SupportedParameters: raw.SupportedParameters,
		}
		if entry.ContextLength <= 0 {
			entry.ContextLength = raw.TopProvider.ContextLength
		}
		if raw.TopProvider.MaxCompletionTokens != nil && *raw.TopProvider.MaxCompletionTokens > 0 {
			entry.MaxCompletionTokens = *raw.TopProvider.MaxCompletionTokens
		}
		if raw.KnowledgeCutoff != nil {
			entry.KnowledgeCutoff = strings.TrimSpace(*raw.KnowledgeCutoff)
		}
		snap.index(entry)
	}

	if snap.entries == 0 {
		return nil, errors.New("model catalog contains no usable entries")
	}
	return snap, nil
}

func (s *Snapshot) index(e *Entry) {
	s.entries++
	putFirst(s.exact, strings.ToLower(e.ID), e)
	if simple, ok := SimplifyOpenRouterID(e.ID); ok {
		simple = strings.ToLower(simple)
		putFirst(s.exact, simple, e)
		putFirst(s.loose, strings.ReplaceAll(simple, ".", "-"), e)
	}
}

func putFirst(m map[string]*Entry, key string, e *Entry) {
	if _, exists := m[key]; !exists {
		m[key] = e
	}
}

func (s *Snapshot) lookupOnce(name string) (*Entry, bool) {
	if e, ok := s.exact[name]; ok {
		return e, true
	}
	simple, ok := SimplifyOpenRouterID(name)
	if !ok {
		return nil, false
	}
	if e, ok := s.exact[simple]; ok {
		return e, true
	}
	if e, ok := s.loose[strings.ReplaceAll(simple, ".", "-")]; ok {
		return e, true
	}
	return nil, false
}

func (s *Snapshot) Lookup(modelName string) (*Entry, bool) {
	if s == nil {
		return nil, false
	}
	name := strings.ToLower(strings.TrimSpace(modelName))
	if name == "" {
		return nil, false
	}
	if e, ok := s.lookupOnce(name); ok {
		return e, true
	}
	if stripped := dateSuffixPattern.ReplaceAllString(name, ""); stripped != name && stripped != "" {
		return s.lookupOnce(stripped)
	}
	return nil, false
}

func Set(s *Snapshot) {
	current.Store(s)
}

func Clear() {
	current.Store(nil)
}

func Current() *Snapshot {
	return current.Load()
}

func Lookup(modelName string) (*Entry, bool) {
	return current.Load().Lookup(modelName)
}

func (e *Entry) ReleaseDate() string {
	if e.Created <= 0 {
		return ""
	}
	return time.Unix(e.Created, 0).UTC().Format("2006-01-02")
}

var frontendModalities = map[string]struct{}{
	"text": {}, "image": {}, "audio": {}, "video": {}, "file": {},
}

func filterModalities(in []string) []string {
	var out []string
	seen := make(map[string]struct{}, len(in))
	for _, m := range in {
		m = strings.ToLower(strings.TrimSpace(m))
		if _, ok := frontendModalities[m]; !ok {
			continue
		}
		if _, dup := seen[m]; dup {
			continue
		}
		seen[m] = struct{}{}
		out = append(out, m)
	}
	return out
}

func (e *Entry) FrontendModalities() (in, out []string) {
	return filterModalities(e.InputModalities), filterModalities(e.OutputModalities)
}

func (e *Entry) Capabilities() []string {
	params := make(map[string]struct{}, len(e.SupportedParameters))
	for _, p := range e.SupportedParameters {
		params[p] = struct{}{}
	}
	has := func(names ...string) bool {
		for _, n := range names {
			if _, ok := params[n]; ok {
				return true
			}
		}
		return false
	}

	var caps []string
	if has("tools", "tool_choice") {
		caps = append(caps, "function_calling", "tools")
	}
	if has("response_format") {
		caps = append(caps, "json_mode")
	}
	if has("structured_outputs") {
		caps = append(caps, "structured_output")
	}
	if has("reasoning", "include_reasoning") {
		caps = append(caps, "reasoning")
	}
	if has("web_search_options") {
		caps = append(caps, "web_search")
	}
	inputs, _ := e.FrontendModalities()
	for _, m := range inputs {
		if m == "image" {
			caps = append(caps, "vision")
			break
		}
	}
	return caps
}
