package opencodefree

import (
	"fmt"
	"maps"
	"math"
	"sort"
	"strconv"
	"strings"
)

func asObject(value any) map[string]any {
	object, _ := value.(map[string]any)
	return object
}

func copyObject(object map[string]any) map[string]any {
	out := make(map[string]any, len(object))
	maps.Copy(out, object)
	return out
}

func asInt(value any) (int, bool) {
	number, ok := value.(float64)
	if !ok || number != math.Trunc(number) {
		return 0, false
	}
	return int(number), true
}

func usagePresent(value any) bool {
	usage, ok := value.(map[string]any)
	return ok && len(usage) > 0
}

func choiceIndex(choice map[string]any) int {
	index, ok := asInt(choice["index"])
	if !ok || index < 0 {
		return 0
	}
	return index
}

// NOTE: One book exists per choice. It tracks tool call identity across frames for name restoration,
// downstream index assignment and non-stream argument accumulation.
type toolCallBook struct {
	calls         map[string]*callIdentity // slot key: decimal index or "auto:N"
	callsById     map[string]*callIdentity
	unindexed     map[int]*callIdentity
	current       *callIdentity
	nextOrder     int
	autoKeys      int
	legacyTool    *callIdentity
	streamIndexes map[int]bool
	nextIndex     int
}

type callIdentity struct {
	id        string
	name      string
	arguments string
	order     int
	slot      string

	streamIndexAssigned bool
	streamIndex         int

	upstreamName string
	pendingName  bool
	nameRestored bool
}

func newToolCallBook() *toolCallBook {
	return &toolCallBook{
		calls:         map[string]*callIdentity{},
		callsById:     map[string]*callIdentity{},
		unindexed:     map[int]*callIdentity{},
		streamIndexes: map[int]bool{},
	}
}

func callID(call map[string]any) string {
	id, _ := call["id"].(string)
	return id
}

// NOTE: Parallel upstream calls often omit index, so identity is resolved by index, then id, then
// creation order. An index-less fragment with neither id nor name continues the current call.
func toolCallTarget(book *toolCallBook, call map[string]any) *callIdentity {
	identified := book.callsById[callID(call)]
	if rawIndex, hasIndex := asInt(call["index"]); hasIndex {
		key := strconv.Itoa(rawIndex)
		target := book.calls[key]
		if target == nil && identified != nil && identified.slot != "" {
			target = identified
		}
		if target == nil {
			if candidate := book.unindexed[rawIndex]; candidate != nil && (callID(call) == "" || candidate.id == "") {
				target = candidate
			}
		}
		if target == nil {
			target = newToolCall(book)
		}
		if target.slot != key {
			bindCallIndex(book, target, rawIndex)
		}
		book.current = target
		return target
	}
	if identified != nil {
		book.current = identified
		return identified
	}
	name := ""
	if fn := asObject(call["function"]); fn != nil {
		name, _ = fn["name"].(string)
	}
	if callID(call) == "" && name == "" && book.current != nil {
		return book.current
	}
	target := newToolCall(book)
	target.slot = fmt.Sprintf("auto:%d", book.autoKeys)
	book.autoKeys++
	book.calls[target.slot] = target
	book.unindexed[target.order] = target
	book.current = target
	return target
}

func newToolCall(book *toolCallBook) *callIdentity {
	target := &callIdentity{order: book.nextOrder}
	book.nextOrder++
	return target
}

func bindCallIndex(book *toolCallBook, target *callIdentity, index int) {
	if strings.HasPrefix(target.slot, "auto:") {
		delete(book.calls, target.slot)
		delete(book.unindexed, target.order)
	}
	target.slot = strconv.Itoa(index)
	book.calls[target.slot] = target
}

func (book *toolCallBook) ensureLegacy() *callIdentity {
	if book.legacyTool == nil {
		book.legacyTool = &callIdentity{}
	}
	return book.legacyTool
}

func (book *toolCallBook) checkToolNamesComplete() error {
	for _, target := range book.calls {
		if target.pendingName {
			return &streamError{message: "Incomplete upstream tool name"}
		}
	}
	if book.legacyTool != nil && book.legacyTool.pendingName {
		return &streamError{message: "Incomplete upstream tool name"}
	}
	return nil
}

// NOTE: Indexed streams are ordered by index, mixed index-less streams keep arrival order.
func orderedToolCalls(book *toolCallBook) []*callIdentity {
	entries := make([]*callIdentity, 0, len(book.calls))
	numeric := make(map[*callIdentity]int, len(book.calls))
	allNumeric := true
	for key, target := range book.calls {
		entries = append(entries, target)
		value, err := strconv.Atoi(key)
		if err != nil {
			allNumeric = false
		}
		numeric[target] = value
	}
	sort.Slice(entries, func(i, j int) bool {
		if allNumeric {
			return numeric[entries[i]] < numeric[entries[j]]
		}
		return entries[i].order < entries[j].order
	})
	return entries
}

// NOTE: Each fragment's downstream index is assigned once and never changed. Standard indexes are kept,
// colliding or missing ones get a free slot so the client never merges two calls into one.
func normalizeStreamToolCalls(calls []any, book *toolCallBook) []any {
	out := make([]any, 0, len(calls))
	for _, raw := range calls {
		call := asObject(raw)
		if call == nil {
			out = append(out, raw)
			continue
		}
		target := toolCallTarget(book, call)
		if id := callID(call); id != "" {
			target.id = id
			book.callsById[id] = target
		}
		if !target.streamIndexAssigned {
			index, has := asInt(call["index"])
			if has && index >= 0 && !book.streamIndexes[index] {
				target.streamIndex = index
			} else {
				target.streamIndex = book.nextIndex
			}
			book.streamIndexes[target.streamIndex] = true
			for book.streamIndexes[book.nextIndex] {
				book.nextIndex++
			}
			target.streamIndexAssigned = true
		}
		rewrite := copyObject(call)
		rewrite["index"] = target.streamIndex
		out = append(out, rewrite)
	}
	return out
}

func choiceHasToolAction(choice map[string]any) bool {
	if delta := asObject(choice["delta"]); delta != nil {
		if calls, ok := delta["tool_calls"].([]any); ok && len(calls) > 0 {
			return true
		}
		if asObject(delta["function_call"]) != nil {
			return true
		}
	}
	reason, _ := choice["finish_reason"].(string)
	return reason == "tool_calls" || reason == "function_call"
}

// NOTE: Tool names arrive split across frames, so the alias decision is deferred until the accumulated
// name stops being a prefix of a longer candidate. A call to the reserved gate tool fails the whole response.
func (p *frameParser) restoreChoiceToolNames(choice map[string]any, index int) error {
	delta := asObject(choice["delta"])
	if delta == nil {
		return nil
	}
	calls, _ := delta["tool_calls"].([]any)
	legacy := asObject(delta["function_call"])
	if len(calls) == 0 && legacy == nil {
		return nil
	}
	book := p.names[index]
	if book == nil {
		book = newToolCallBook()
		p.names[index] = book
	}
	if len(calls) > 0 {
		restored := make([]any, 0, len(calls))
		for _, raw := range calls {
			call := asObject(raw)
			if call == nil {
				restored = append(restored, raw)
				continue
			}
			target := toolCallTarget(book, call)
			if id := callID(call); id != "" {
				target.id = id
				book.callsById[id] = target
			}
			fn := asObject(call["function"])
			if fn == nil {
				restored = append(restored, call)
				continue
			}
			next := copyObject(call)
			restoredFn, err := p.restoreFunctionName(fn, target)
			if err != nil {
				return err
			}
			next["function"] = restoredFn
			restored = append(restored, next)
		}
		delta["tool_calls"] = restored
	}
	if legacy != nil {
		restoredFn, err := p.restoreFunctionName(legacy, book.ensureLegacy())
		if err != nil {
			return err
		}
		delta["function_call"] = restoredFn
	}
	if reason, _ := choice["finish_reason"].(string); reason != "" {
		return book.checkToolNamesComplete()
	}
	return nil
}

func (p *frameParser) restoreFunctionName(fn map[string]any, target *callIdentity) (map[string]any, error) {
	fragment, _ := fn["name"].(string)
	if fragment == "" && !target.pendingName {
		return fn, nil
	}
	if fragment != "" && target.upstreamName != fragment {
		target.upstreamName += fragment
	}
	name := target.upstreamName
	next := copyObject(fn)
	candidates := p.plan.candidates()
	// NOTE: A full tool name can also be a prefix of another alias (oc2cpa_read vs oc2cpa_read_1),
	// so the decision waits until arguments exist.
	if !argsPresent(fn) {
		for _, candidate := range candidates {
			if candidate != name && strings.HasPrefix(candidate, name) {
				delete(next, "name")
				target.pendingName = true
				return next, nil
			}
		}
	}
	for _, reserved := range p.plan.reservedNames {
		if name == reserved {
			return next, &streamError{message: "Upstream called reserved tool: " + name}
		}
	}
	if restored, found := p.plan.aliases[name]; found {
		if target.nameRestored {
			delete(next, "name")
		} else {
			next["name"] = restored
		}
		target.nameRestored = true
		target.pendingName = false
		return next, nil
	}
	known := false
	for _, client := range p.plan.clientNames {
		if client == name {
			known = true
			break
		}
	}
	if !known {
		for _, candidate := range candidates {
			if strings.HasPrefix(candidate, name) {
				delete(next, "name")
				target.pendingName = true
				return next, nil
			}
		}
	}
	if target.pendingName {
		next["name"] = name
	}
	target.pendingName = false
	return next, nil
}

func argsPresent(fn map[string]any) bool {
	switch value := fn["arguments"].(type) {
	case nil:
		return false
	case string:
		return value != ""
	case float64:
		return value != 0
	}
	return true
}
