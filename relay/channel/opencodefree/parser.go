package opencodefree

import (
	"bufio"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
)

type streamError struct{ message string }

func (e *streamError) Error() string { return e.message }

var errIdleTimeout = &streamError{message: "Upstream stream idle timeout"}

// NOTE: Closing the body from a timer is the only way to interrupt a blocked read, so the timer is
// re-armed on every successful read and the read error is rewritten when the timer caused it.
type idleReader struct {
	body     io.ReadCloser
	timer    *time.Timer
	trailing atomic.Bool
	timedOut atomic.Bool
}

func newIdleReader(body io.ReadCloser) *idleReader {
	r := &idleReader{body: body}
	r.timer = time.AfterFunc(firstEventTimeout, func() {
		r.timedOut.Store(true)
		_ = r.body.Close()
	})
	return r
}

func (r *idleReader) Read(p []byte) (int, error) {
	n, err := r.body.Read(p)
	if n > 0 && !r.trailing.Load() {
		r.timer.Reset(bodyIdleTimeout)
	}
	if err != nil && err != io.EOF && r.timedOut.Load() {
		return n, errIdleTimeout
	}
	return n, err
}

// NOTE: After [DONE] only a short window is left for a late usage frame, and it is never extended.
func (r *idleReader) enterTrailing() {
	r.trailing.Store(true)
	r.timer.Reset(trailingUsageWindow)
}

func (r *idleReader) Close() error {
	r.timer.Stop()
	return r.body.Close()
}

// NOTE: The parser validates the upstream stream and restores client tool names, so both the streaming
// and the aggregating consumer only ever see frames that are safe to hand to the client.
type frameParser struct {
	plan        *requestPlan
	reader      *idleReader
	lines       *bufio.Reader
	data        []string
	eventType   string
	formatKnown bool
	sawDone     bool
	sawUsage    bool
	done        bool
	choices     map[int]bool
	finished    map[int]bool
	names       map[int]*toolCallBook
}

func newFrameParser(body io.ReadCloser, plan *requestPlan) *frameParser {
	reader := newIdleReader(body)
	return &frameParser{
		plan:     plan,
		reader:   reader,
		lines:    bufio.NewReader(reader),
		choices:  map[int]bool{},
		finished: map[int]bool{},
		names:    map[int]*toolCallBook{},
	}
}

func (p *frameParser) close() {
	_ = p.reader.Close()
}

// NOTE: A nil frame with a nil error means the stream is over.
func (p *frameParser) next() (map[string]any, error) {
	for !p.done {
		line, readErr := p.lines.ReadString('\n')
		eof := errors.Is(readErr, io.EOF)
		if readErr != nil && !eof {
			if p.sawDone {
				p.done = true
				break
			}
			if streamErr, ok := readErr.(*streamError); ok {
				return nil, streamErr
			}
			return nil, &streamError{message: "Failed to read upstream stream: " + readErr.Error()}
		}
		if !p.formatKnown {
			if lead := strings.TrimSpace(line); lead != "" {
				p.formatKnown = true
				// NOTE: The free tier can answer HTTP 200 with a plain JSON error body instead of an event stream.
				if strings.HasPrefix(lead, "{") {
					return nil, p.rawBodyError(line)
				}
			}
		}
		var payload, kind string
		if line != "" {
			payload, kind = p.readLine(strings.TrimRight(line, "\r\n"))
		}
		if eof {
			if payload == "" {
				payload, kind = p.takeEvent()
			}
			p.done = true
		}
		if payload == "" {
			continue
		}
		frame, err := p.handle(payload, kind)
		if err != nil {
			return nil, err
		}
		if frame != nil {
			return frame, nil
		}
	}
	return nil, nil
}

func (p *frameParser) readLine(line string) (string, string) {
	if line == "" {
		return p.takeEvent()
	}
	if strings.HasPrefix(line, ":") {
		return "", ""
	}
	field, value, _ := strings.Cut(line, ":")
	value = strings.TrimPrefix(value, " ")
	switch field {
	case "data":
		p.data = append(p.data, value)
	case "event":
		p.eventType = value
	}
	return "", ""
}

func (p *frameParser) takeEvent() (string, string) {
	payload := strings.TrimSpace(strings.Join(p.data, "\n"))
	kind := p.eventType
	p.data = nil
	p.eventType = ""
	return payload, kind
}

func (p *frameParser) handle(payload, kind string) (map[string]any, error) {
	if payload == "[DONE]" {
		if p.sawDone {
			return nil, nil
		}
		for _, book := range p.names {
			if err := book.checkToolNamesComplete(); err != nil {
				return nil, err
			}
		}
		p.sawDone = true
		if p.sawUsage {
			p.done = true
		} else {
			p.reader.enterTrailing()
		}
		return nil, nil
	}
	var root map[string]any
	if err := common.UnmarshalJsonStr(payload, &root); err != nil || root == nil {
		if p.sawDone {
			return nil, nil
		}
		return nil, &streamError{message: "Invalid SSE payload from upstream"}
	}
	if message, failed := upstreamError(root, kind == "error"); failed {
		if p.sawDone {
			return nil, nil
		}
		return nil, &streamError{message: message}
	}
	if p.sawDone {
		// NOTE: The trailing tail can only add usage; it must never invalidate an answer that already completed.
		if usagePresent(root["usage"]) {
			pruned := copyObject(root)
			pruned["choices"] = []any{}
			p.sawUsage = true
			p.done = true
			return pruned, nil
		}
		return nil, nil
	}
	choices, _ := root["choices"].([]any)
	for _, raw := range choices {
		choice := asObject(raw)
		if choice == nil {
			continue
		}
		// NOTE: Explicit tool_choice none is rejected upstream, so disabled tools are enforced here.
		if p.plan.toolsDisabled && choiceHasToolAction(choice) {
			return nil, &streamError{message: "Upstream returned a tool call when tools are disabled"}
		}
		index := choiceIndex(choice)
		if !p.finished[index] {
			if err := p.restoreChoiceToolNames(choice, index); err != nil {
				return nil, err
			}
		}
		p.choices[index] = true
		if reason, _ := choice["finish_reason"].(string); reason != "" {
			p.finished[index] = true
		}
	}
	if usagePresent(root["usage"]) {
		p.sawUsage = true
	}
	if p.sawUsage && len(p.choices) >= p.plan.expectedChoices && p.allFinished() {
		p.done = true
	}
	return root, nil
}

func (p *frameParser) allFinished() bool {
	for index := range p.choices {
		if !p.finished[index] {
			return false
		}
	}
	return true
}

// NOTE: A stream that ends without any choice, or without [DONE] while some choice has no finish reason, is truncated.
func (p *frameParser) completed() error {
	if len(p.choices) == 0 {
		return &streamError{message: "Empty response from upstream"}
	}
	if !p.sawDone && !p.allFinished() {
		return &streamError{message: "Incomplete response from upstream"}
	}
	return nil
}

func (p *frameParser) rawBodyError(first string) error {
	rest, _ := io.ReadAll(io.LimitReader(p.lines, errorBodyMaxBytes))
	var parsed map[string]any
	if err := common.UnmarshalJsonStr(first+string(rest), &parsed); err == nil {
		if message, failed := upstreamError(parsed, true); failed {
			return &streamError{message: message}
		}
	}
	return &streamError{message: "Upstream returned an unexpected response body"}
}

// NOTE: Unforced parsing only treats payloads carrying an error field or type "error" as failures,
// so ordinary chat frames never match.
func upstreamError(root map[string]any, forced bool) (string, bool) {
	errValue := root["error"]
	if !forced && errValue == nil {
		if flag, _ := root["type"].(string); flag != "error" {
			return "", false
		}
	}
	message := ""
	switch value := errValue.(type) {
	case string:
		message = value
	case map[string]any:
		message, _ = value["message"].(string)
	}
	if message == "" {
		message, _ = root["message"].(string)
	}
	if message == "" {
		message = "Upstream request failed"
	}
	return message, true
}
