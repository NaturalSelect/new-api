package opencodefree

import (
	"bytes"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
)

// NOTE: Upstream private extensions are dropped because strict clients reject unknown schema fields.
func normalizeDelta(delta map[string]any, thinking bool) map[string]any {
	if delta == nil {
		return map[string]any{}
	}
	out := copyObject(delta)
	if raw, has := out["tool_calls"]; has && raw != nil {
		if calls, ok := raw.([]any); ok {
			kept := make([]any, 0, len(calls))
			for _, call := range calls {
				if asObject(call) != nil {
					kept = append(kept, call)
				}
			}
			out["tool_calls"] = kept
		} else {
			delete(out, "tool_calls")
		}
	}
	delete(out, "name")
	delete(out, "cost")

	reasoning := ""
	if value, _ := delta["reasoning"].(string); value != "" {
		reasoning = value
	} else if value, ok := delta["reasoning_content"].(string); ok {
		reasoning = value
	}
	delete(out, "reasoning")
	delete(out, "reasoning_content")
	delete(out, "reasoning_details")
	if thinking && reasoning != "" {
		out["reasoning_content"] = reasoning
	}
	return out
}

func normalizeChunk(chunk map[string]any, model string, thinking bool) map[string]any {
	out := copyObject(chunk)
	delete(out, "cost")
	if model != "" {
		out["model"] = model
	}
	if usage, has := out["usage"]; has && usage == nil {
		delete(out, "usage")
	}
	choices, ok := out["choices"].([]any)
	if !ok {
		out["choices"] = []any{}
		return out
	}
	normalized := make([]any, 0, len(choices))
	for _, raw := range choices {
		choice := asObject(raw)
		if choice == nil {
			continue
		}
		next := copyObject(choice)
		if delta := asObject(next["delta"]); delta != nil {
			next["delta"] = normalizeDelta(delta, thinking)
		} else if next["delta"] != nil {
			next["delta"] = map[string]any{}
		}
		normalized = append(normalized, next)
	}
	out["choices"] = normalized
	return out
}

func hasContent(chunk map[string]any) bool {
	choices, _ := chunk["choices"].([]any)
	for _, raw := range choices {
		delta := asObject(asObject(raw)["delta"])
		for key := range delta {
			if key != "role" {
				return true
			}
		}
	}
	return false
}

// NOTE: The upstream labels a large share of tool-call answers as stop, which would keep clients from executing the tools.
func normalizeFinish(reason string, hasToolCalls bool) string {
	if reason == "stop" && hasToolCalls {
		return "tool_calls"
	}
	return reason
}

// NOTE: The first content-less frames are held back so an upstream failure right after them can still become an HTTP error.
type streamEncoder struct {
	plan    *requestPlan
	model   string
	started bool
	fin     map[int]bool
	toolIdx map[int]bool
	books   map[int]*toolCallBook
	pending [][]byte
}

func newStreamEncoder(plan *requestPlan, model string) *streamEncoder {
	return &streamEncoder{
		plan:    plan,
		model:   model,
		fin:     map[int]bool{},
		toolIdx: map[int]bool{},
		books:   map[int]*toolCallBook{},
	}
}

func (s *streamEncoder) encode(frame map[string]any) ([]byte, error) {
	normalized := normalizeChunk(frame, s.model, s.plan.thinking)
	choices, _ := normalized["choices"].([]any)
	usable := make([]any, 0, len(choices))
	for _, raw := range choices {
		choice := asObject(raw)
		if choice != nil && !s.fin[choiceIndex(choice)] {
			usable = append(usable, choice)
		}
	}
	if len(usable) == 0 && !usagePresent(normalized["usage"]) {
		return nil, nil
	}
	for _, raw := range usable {
		choice := raw.(map[string]any)
		index := choiceIndex(choice)
		if delta := asObject(choice["delta"]); delta != nil {
			if calls, ok := delta["tool_calls"].([]any); ok && len(calls) > 0 {
				s.toolIdx[index] = true
				book := s.books[index]
				if book == nil {
					book = newToolCallBook()
					s.books[index] = book
				}
				delta["tool_calls"] = normalizeStreamToolCalls(calls, book)
			}
		}
		if reason, _ := choice["finish_reason"].(string); reason != "" {
			s.fin[index] = true
		}
	}
	// NOTE: The finish rewrite runs after the loop so tool-call deltas in this very chunk count for their own choice.
	for _, raw := range usable {
		choice := raw.(map[string]any)
		if reason, _ := choice["finish_reason"].(string); reason != "" {
			choice["finish_reason"] = normalizeFinish(reason, s.toolIdx[choiceIndex(choice)])
		}
	}
	out := copyObject(normalized)
	out["choices"] = usable
	raw, err := common.Marshal(out)
	if err != nil {
		return nil, err
	}
	encoded := append(append([]byte("data: "), raw...), '\n', '\n')
	if !hasContent(out) && !s.started && len(s.pending) < maxPendingFrames {
		s.pending = append(s.pending, encoded)
		return nil, nil
	}
	s.started = true
	if len(s.pending) > 0 {
		encoded = append(bytes.Join(s.pending, nil), encoded...)
		s.pending = nil
	}
	return encoded, nil
}

func (s *streamEncoder) flush() []byte {
	out := bytes.Join(s.pending, nil)
	s.pending = nil
	s.started = true
	return out
}

type choiceAccumulator struct {
	role      string
	content   strings.Builder
	reasoning strings.Builder
	finish    string
	extra     map[string]any
	calls     *toolCallBook
}

// NOTE: Non-stream clients get a single JSON body built from the upstream stream, because the free tier always streams.
type jsonAggregator struct {
	plan    *requestPlan
	model   string
	choices map[int]*choiceAccumulator
	id      string
	created any
	usage   map[string]any
}

func aggregateResponse(parser *frameParser, plan *requestPlan, model string) ([]byte, error) {
	a := &jsonAggregator{plan: plan, model: model, choices: map[int]*choiceAccumulator{}}
	for {
		frame, err := parser.next()
		if err != nil {
			return nil, err
		}
		if frame == nil {
			break
		}
		a.consume(frame)
	}
	if err := parser.completed(); err != nil {
		return nil, err
	}
	return common.Marshal(a.result())
}

func (a *jsonAggregator) consume(frame map[string]any) {
	normalized := normalizeChunk(frame, a.model, a.plan.thinking)
	if id, _ := normalized["id"].(string); id != "" && a.id == "" {
		a.id = id
	}
	if a.created == nil {
		a.created = normalized["created"]
	}
	if usage := normalized["usage"]; usagePresent(usage) {
		a.usage = usage.(map[string]any)
	}
	choices, _ := normalized["choices"].([]any)
	for _, raw := range choices {
		choice := asObject(raw)
		if choice == nil {
			continue
		}
		index := choiceIndex(choice)
		acc := a.choices[index]
		if acc != nil && acc.finish != "" {
			continue
		}
		if acc == nil {
			acc = &choiceAccumulator{role: "assistant", extra: map[string]any{}, calls: newToolCallBook()}
			a.choices[index] = acc
		}
		delta := asObject(choice["delta"])
		if role, _ := delta["role"].(string); role != "" {
			acc.role = role
		}
		if content, ok := delta["content"].(string); ok {
			acc.content.WriteString(content)
		}
		if reasoning, ok := delta["reasoning_content"].(string); ok {
			acc.reasoning.WriteString(reasoning)
		}
		mergePassthrough(acc.extra, delta)
		calls, _ := delta["tool_calls"].([]any)
		for _, callRaw := range calls {
			call := asObject(callRaw)
			if call == nil {
				continue
			}
			target := toolCallTarget(acc.calls, call)
			if id := callID(call); id != "" {
				target.id = id
				acc.calls.callsById[id] = target
			}
			if fn := asObject(call["function"]); fn != nil {
				if name, _ := fn["name"].(string); name != "" {
					target.name = name
				}
				if args, ok := fn["arguments"].(string); ok {
					target.arguments += args
				}
			}
		}
		if reason, _ := choice["finish_reason"].(string); reason != "" {
			acc.finish = reason
		}
	}
}

func (a *jsonAggregator) result() map[string]any {
	indices := make([]int, 0, len(a.choices))
	for index := range a.choices {
		indices = append(indices, index)
	}
	sort.Ints(indices)
	choices := make([]any, 0, len(indices))
	for _, index := range indices {
		acc := a.choices[index]
		message := map[string]any{"role": acc.role, "content": nil}
		if content := acc.content.String(); content != "" {
			message["content"] = content
		}
		if reasoning := acc.reasoning.String(); reasoning != "" {
			message["reasoning_content"] = reasoning
		}
		if len(acc.calls.calls) > 0 {
			toolCalls := make([]any, 0, len(acc.calls.calls))
			for _, call := range orderedToolCalls(acc.calls) {
				toolCalls = append(toolCalls, map[string]any{
					"id":   call.id,
					"type": "function",
					"function": map[string]any{
						"name":      call.name,
						"arguments": call.arguments,
					},
				})
			}
			message["tool_calls"] = toolCalls
		}
		for key, value := range acc.extra {
			message[key] = value
		}
		finish := acc.finish
		if finish == "" {
			finish = "stop"
		}
		choices = append(choices, map[string]any{
			"index":         index,
			"message":       message,
			"finish_reason": normalizeFinish(finish, len(acc.calls.calls) > 0),
		})
	}
	id := a.id
	if id == "" {
		id = "chatcmpl-" + common.GetUUID()
	}
	created := a.created
	if created == nil {
		created = time.Now().Unix()
	}
	result := map[string]any{
		"id":      id,
		"object":  "chat.completion",
		"created": created,
		"model":   a.model,
		"choices": choices,
	}
	if len(a.usage) > 0 {
		result["usage"] = a.usage
	}
	return result
}

// NOTE: Non-stream aggregation must carry every delta field the stream path passes through, or the two modes diverge.
func mergePassthrough(extra map[string]any, delta map[string]any) {
	for key, value := range delta {
		switch key {
		case "role", "content", "reasoning_content", "tool_calls":
			continue
		}
		if value != nil {
			extra[key] = mergeValue(extra[key], value)
		}
	}
}

// NOTE: String increments concatenate and objects merge recursively, because the upstream splits
// function_call name and arguments across frames.
func mergeValue(existing, incoming any) any {
	switch left := existing.(type) {
	case string:
		if right, ok := incoming.(string); ok {
			return left + right
		}
	case map[string]any:
		if right, ok := incoming.(map[string]any); ok {
			merged := copyObject(left)
			for key, value := range right {
				if value != nil {
					merged[key] = mergeValue(left[key], value)
				}
			}
			return merged
		}
	}
	return incoming
}

// NOTE: Closing the replacement body must also close the upstream body, otherwise a client that
// disconnects leaves the producer goroutine blocked on an idle upstream read.
type pipeBody struct {
	*io.PipeReader
	parser *frameParser
}

func (b *pipeBody) Close() error {
	b.parser.close()
	return b.PipeReader.Close()
}

// NOTE: The first frames are read synchronously so a failure before any content becomes a regular HTTP error
// and can still be retried on another channel. Once content flows, failures break the pipe instead.
func newStreamBody(upstream io.ReadCloser, plan *requestPlan, model string) (io.ReadCloser, error) {
	parser := newFrameParser(upstream, plan)
	encoder := newStreamEncoder(plan, model)
	var head []byte
	ended := false
	for !encoder.started {
		frame, err := parser.next()
		if err != nil {
			parser.close()
			return nil, err
		}
		if frame == nil {
			ended = true
			break
		}
		out, err := encoder.encode(frame)
		if err != nil {
			parser.close()
			return nil, err
		}
		head = append(head, out...)
	}
	if ended {
		defer parser.close()
		if err := parser.completed(); err != nil {
			return nil, err
		}
		head = append(head, encoder.flush()...)
		head = append(head, "data: [DONE]\n\n"...)
		return io.NopCloser(bytes.NewReader(head)), nil
	}

	reader, writer := io.Pipe()
	go func() {
		defer parser.close()
		if _, err := writer.Write(head); err != nil {
			return
		}
		for {
			frame, err := parser.next()
			if err != nil {
				_ = writer.CloseWithError(err)
				return
			}
			if frame == nil {
				break
			}
			out, err := encoder.encode(frame)
			if err != nil {
				_ = writer.CloseWithError(err)
				return
			}
			if len(out) > 0 {
				if _, err := writer.Write(out); err != nil {
					return
				}
			}
		}
		if err := parser.completed(); err != nil {
			_ = writer.CloseWithError(err)
			return
		}
		tail := append(encoder.flush(), "data: [DONE]\n\n"...)
		_, _ = writer.Write(tail)
		_ = writer.Close()
	}()
	return &pipeBody{PipeReader: reader, parser: parser}, nil
}
