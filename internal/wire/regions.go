package wire

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/ethosagent/tollgate"
)

// ErrBadEdit is returned by ReplaceRegions for edits that are out of order, overlap, leave the body, or do not
// cover a JSON string.
var ErrBadEdit = errors.New("wire: bad region edit")

// Region is one JSON string of a request that a feature may rewrite: a tool result's text, or a tool's
// description. Start and End are the byte span of the string in the body, quotes included; Text is the decoded
// string (empty from the *Spans functions, see Decode). Msg is the index of the message (or of the tool for
// ToolDescriptions) in its array. Tool is the tool's name, resolved from the assistant turn that called it.
//
// Protected means the string is in a system or developer message. Cached means it sits at or before the last
// cache_control breakpoint (anthropic): the trim feature does not skip such a region (ruling A5: it protects the
// system prompt and the tool definitions, and keeps every decision sticky so the cached prefix is byte-stable), but a
// caller that wants the stricter rule has the flag.
type Region struct {
	Msg        int
	Start, End int
	ToolCallID string
	Tool       string
	Text       string
	Protected  bool
	Cached     bool

	blk int // index of the content block in its message (anthropic), for Cached
}

// Edit replaces the string Start..End with Text, which ReplaceRegions encodes as a JSON string.
type Edit struct {
	Start, End int
	Text       string
}

// Decode returns the region's string, decoded from body.
func (r Region) Decode(body []byte) string {
	if r.Start < 0 || r.End > len(body) || r.End-r.Start < 2 {
		return ""
	}
	raw := body[r.Start:r.End]
	if s, ok := unquote(raw); ok {
		return s
	}
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return ""
	}
	return s
}

// unquote decodes a JSON string without the general decoder's overhead: one allocation, no validation beyond what
// the escapes need. It reports false for anything it does not handle (the caller then uses encoding/json).
func unquote(raw []byte) (string, bool) {
	if raw[0] != '"' || raw[len(raw)-1] != '"' {
		return "", false
	}
	in := raw[1 : len(raw)-1]
	if bytes.IndexByte(in, '\\') < 0 {
		return string(in), true
	}
	var b strings.Builder
	b.Grow(len(in))
	for len(in) > 0 {
		j := bytes.IndexByte(in, '\\')
		if j < 0 {
			b.Write(in)
			break
		}
		b.Write(in[:j])
		if j+1 >= len(in) {
			return "", false
		}
		switch in[j+1] {
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case 'r':
			b.WriteByte('\r')
		case 'b':
			b.WriteByte('\b')
		case 'f':
			b.WriteByte('\f')
		case '"', '\\', '/':
			b.WriteByte(in[j+1])
		default:
			return "", false // \uXXXX and anything unusual: leave it to encoding/json
		}
		in = in[j+2:]
	}
	return b.String(), true
}

// ToolResults returns every tool result string of the request in order, decoded. openai.chat: the content of
// role "tool" messages (a string, or each text part of an array). anthropic.messages: the content of tool_result
// blocks (a string, or each text block), except results flagged is_error. Any other wire is ErrUnsupportedWire.
func ToolResults(w tollgate.WireFormat, body []byte) ([]Region, error) {
	rs, err := ToolResultSpans(w, body)
	for i := range rs {
		rs[i].Text = rs[i].Decode(body)
	}
	return rs, err
}

// ToolResultSpans is ToolResults without decoding the strings: the hot path hashes the span's bytes and decodes
// only a region it has not decided before.
func ToolResultSpans(w tollgate.WireFormat, body []byte) ([]Region, error) {
	f, err := topFields(body, "messages")
	if err != nil {
		return nil, err
	}
	msgs, ok := f["messages"]
	if !ok {
		return nil, fmt.Errorf("%w: no messages", ErrNoSplice)
	}
	switch w {
	case tollgate.WireOpenAIChat:
		return openaiResults(body, msgs)
	case tollgate.WireAnthropicMessages:
		return anthropicResults(body, msgs)
	}
	return nil, fmt.Errorf("%w: %q", ErrUnsupportedWire, w)
}

// ToolDescriptions returns each tool definition's description string, decoded. A tool with no string description
// has no region.
func ToolDescriptions(w tollgate.WireFormat, body []byte) ([]Region, error) {
	if w != tollgate.WireOpenAIChat && w != tollgate.WireAnthropicMessages {
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedWire, w)
	}
	f, err := topFields(body, "tools")
	if err != nil {
		return nil, err
	}
	tools, ok := f["tools"]
	if !ok || body[tools.start] != '[' {
		return nil, nil
	}
	var out []Region
	err = eachElem(body, tools, func(i int, t span) error {
		if body[t.start] != '{' {
			return nil
		}
		holder := t
		if w == tollgate.WireOpenAIChat {
			holder = field(body, t, "function")
			if holder.end == 0 || body[holder.start] != '{' {
				return nil
			}
		}
		d := field(body, holder, "description")
		if d.end == 0 || body[d.start] != '"' {
			return nil
		}
		r := Region{Msg: i, Start: d.start, End: d.end}
		if n := field(body, holder, "name"); n.end != 0 && body[n.start] == '"' {
			r.Tool = Region{Start: n.start, End: n.end}.Decode(body)
		}
		r.Text = r.Decode(body)
		out = append(out, r)
		return nil
	})
	return out, err
}

// ReplaceRegions returns body with each edit's string replaced, copying every other byte unchanged. The edits must
// be in ascending order, not overlap, and each cover a JSON string; otherwise ErrBadEdit and nothing is returned.
func ReplaceRegions(body []byte, edits []Edit) ([]byte, error) {
	if len(edits) == 0 {
		return body, nil
	}
	enc := make([]string, len(edits))
	size, prev := len(body), 0
	for i, e := range edits {
		if e.Start < prev || e.End <= e.Start || e.End > len(body) || body[e.Start] != '"' || body[e.End-1] != '"' {
			return nil, fmt.Errorf("%w: edit %d", ErrBadEdit, i)
		}
		enc[i] = jsonString(e.Text)
		size += len(enc[i]) - (e.End - e.Start)
		prev = e.End
	}
	out := make([]byte, 0, size)
	at := 0
	for i, e := range edits {
		out = append(out, body[at:e.Start]...)
		out = append(out, enc[i]...)
		at = e.End
	}
	return append(out, body[at:]...), nil
}

// --- scanning -------------------------------------------------------------------------------------------------

// strEnd returns the index just past the string that opens at b[i], jumping from quote to quote.
func strEnd(b []byte, i int) (int, error) {
	for j := i + 1; j < len(b); {
		k := bytes.IndexByte(b[j:], '"')
		if k < 0 {
			break
		}
		q := j + k
		n := 0
		for p := q - 1; p > i && b[p] == '\\'; p-- {
			n++
		}
		if n%2 == 0 {
			return q + 1, nil
		}
		j = q + 1
	}
	return 0, fmt.Errorf("%w: unterminated string", ErrNoSplice)
}

// valEnd returns the index just past the JSON value that starts at b[i].
func valEnd(b []byte, i int) (int, error) {
	if i >= len(b) {
		return 0, fmt.Errorf("%w: truncated", ErrNoSplice)
	}
	switch b[i] {
	case '"':
		return strEnd(b, i)
	case '{', '[':
		depth := 0
		for ; i < len(b); i++ {
			switch b[i] {
			case '{', '[':
				depth++
			case '}', ']':
				depth--
				if depth == 0 {
					return i + 1, nil
				}
			case '"':
				e, err := strEnd(b, i)
				if err != nil {
					return 0, err
				}
				i = e - 1
			}
		}
		return 0, fmt.Errorf("%w: unterminated value", ErrNoSplice)
	}
	for i < len(b) && b[i] != ',' && b[i] != '}' && b[i] != ']' && !isSpace(b[i]) {
		i++
	}
	return i, nil
}

// eachField calls fn for each member of the object that spans o: the raw key (no quotes) and the value's span.
func eachField(b []byte, o span, fn func(key []byte, v span) error) error {
	i := skipSpace(b, o.start)
	if i >= o.end || b[i] != '{' {
		return fmt.Errorf("%w: not an object", ErrNoSplice)
	}
	for i = skipSpace(b, i+1); i < o.end && b[i] != '}'; i = skipSpace(b, i) {
		if b[i] == ',' {
			i = skipSpace(b, i+1)
		}
		ke, err := strEnd(b, i)
		if err != nil {
			return err
		}
		vs := skipSpace(b, ke)
		if vs >= o.end || b[vs] != ':' {
			return fmt.Errorf("%w: missing colon", ErrNoSplice)
		}
		vs = skipSpace(b, vs+1)
		ve, err := valEnd(b, vs)
		if err != nil {
			return err
		}
		if err := fn(b[i+1:ke-1], span{vs, ve}); err != nil {
			return err
		}
		i = ve
	}
	return nil
}

// eachElem calls fn for each element of the array that spans a.
func eachElem(b []byte, a span, fn func(i int, v span) error) error {
	i := skipSpace(b, a.start)
	if i >= a.end || b[i] != '[' {
		return fmt.Errorf("%w: not an array", ErrNoSplice)
	}
	n := 0
	for i = skipSpace(b, i+1); i < a.end && b[i] != ']'; i = skipSpace(b, i) {
		if b[i] == ',' {
			i = skipSpace(b, i+1)
		}
		e, err := valEnd(b, i)
		if err != nil {
			return err
		}
		if err := fn(n, span{i, e}); err != nil {
			return err
		}
		n++
		i = e
	}
	return nil
}

// field returns the span of key in the object o; a zero span (end 0) when absent.
func field(b []byte, o span, key string) span {
	var out span
	_ = eachField(b, o, func(k []byte, v span) error {
		if string(k) == key {
			out = v
			return errStop
		}
		return nil
	})
	return out
}

var errStop = errors.New("stop")

// topFields returns the spans of the named members of the body object.
func topFields(body []byte, keys ...string) (map[string]span, error) {
	out := make(map[string]span, len(keys))
	err := eachField(body, span{0, len(body)}, func(k []byte, v span) error {
		for _, want := range keys {
			if string(k) == want {
				out[want] = v
			}
		}
		return nil
	})
	return out, err
}

// --- openai.chat ------------------------------------------------------------------------------------------------

// callName is an assistant tool call seen so far: the spans of its id and name.
type callName struct{ id, name span }

func (c callName) idIs(b []byte, id span) bool {
	return bytes.Equal(b[c.id.start:c.id.end], b[id.start:id.end])
}

// toolOf is the name of the tool call whose id is id, read from the calls seen so far; empty when there is none.
func toolOf(body []byte, calls []callName, id span) string {
	name := ""
	for _, c := range calls {
		if c.idIs(body, id) {
			name = Region{Start: c.name.start, End: c.name.end}.Decode(body)
		}
	}
	return name
}

// oaScan walks the messages of an openai.chat body.
type oaScan struct {
	body  []byte
	out   []Region
	calls []callName
}

func openaiResults(body []byte, msgs span) ([]Region, error) {
	s := &oaScan{body: body}
	err := eachElem(body, msgs, s.message)
	return s.out, err
}

func (s *oaScan) message(i int, m span) error {
	body := s.body
	if body[m.start] != '{' {
		return nil
	}
	var role, content, callID, toolCalls span
	if err := eachField(body, m, func(k []byte, v span) error {
		switch string(k) {
		case "role":
			role = v
		case "content":
			content = v
		case "tool_call_id":
			callID = v
		case "tool_calls":
			toolCalls = v
		}
		return nil
	}); err != nil {
		return err
	}
	if err := s.noteCalls(toolCalls); err != nil {
		return err
	}
	r := string(body[role.start:role.end])
	if content.end == 0 || (r != `"tool"` && r != `"system"` && r != `"developer"`) {
		return nil
	}
	if r != `"tool"` {
		return nil // system text is never a tool result
	}
	base := Region{Msg: i}
	if callID.end != 0 && body[callID.start] == '"' {
		base.ToolCallID = Region{Start: callID.start, End: callID.end}.Decode(body)
		base.Tool = toolOf(body, s.calls, callID)
	}
	return textStrings(body, content, base, &s.out)
}

// noteCalls records the id and name spans of the tool calls of an assistant message.
func (s *oaScan) noteCalls(toolCalls span) error {
	if toolCalls.end == 0 || s.body[toolCalls.start] != '[' {
		return nil
	}
	return eachElem(s.body, toolCalls, func(_ int, tc span) error {
		id := field(s.body, tc, "id")
		fn := field(s.body, tc, "function")
		if id.end != 0 && fn.end != 0 && s.body[fn.start] == '{' {
			if n := field(s.body, fn, "name"); n.end != 0 {
				s.calls = append(s.calls, callName{id, n})
			}
		}
		return nil
	})
}

// textStrings appends a region for the string at v, or for each "text" string of the text parts of the array at v.
func textStrings(body []byte, v span, base Region, out *[]Region) error {
	switch body[v.start] {
	case '"':
		base.Start, base.End = v.start, v.end
		*out = append(*out, base)
	case '[':
		return eachElem(body, v, func(j int, part span) error {
			if body[part.start] != '{' {
				return nil
			}
			var typ, text span
			if err := eachField(body, part, func(k []byte, pv span) error {
				switch string(k) {
				case "type":
					typ = pv
				case "text":
					text = pv
				}
				return nil
			}); err != nil {
				return err
			}
			if text.end != 0 && body[text.start] == '"' && (typ.end == 0 || string(body[typ.start:typ.end]) == `"text"`) {
				r := base
				r.Start, r.End, r.blk = text.start, text.end, j
				*out = append(*out, r)
			}
			return nil
		})
	}
	return nil
}

// --- anthropic.messages ---------------------------------------------------------------------------------------

// anScan walks the messages of an anthropic.messages body, remembering the last cache_control breakpoint.
type anScan struct {
	body         []byte
	out          []Region
	calls        []callName
	bpMsg, bpBlk int
}

func anthropicResults(body []byte, msgs span) ([]Region, error) {
	s := &anScan{body: body, bpMsg: -1, bpBlk: -1}
	if err := eachElem(body, msgs, s.message); err != nil {
		return nil, err
	}
	for k := range s.out {
		r := &s.out[k]
		r.Cached = s.bpMsg >= 0 && (r.Msg < s.bpMsg || (r.Msg == s.bpMsg && r.blk <= s.bpBlk))
	}
	return s.out, nil
}

func (s *anScan) message(i int, m span) error {
	content := field(s.body, m, "content")
	if content.end == 0 || s.body[content.start] != '[' {
		return nil
	}
	return eachElem(s.body, content, func(j int, blk span) error { return s.block(i, j, blk) })
}

// anBlock is the members of a content block the scan reads.
type anBlock struct {
	typ, id, name, tuid, inner, isErr span
	cached                            bool
}

func (s *anScan) block(i, j int, blk span) error {
	body := s.body
	if body[blk.start] != '{' {
		return nil
	}
	var f anBlock
	if err := eachField(body, blk, func(k []byte, v span) error {
		switch string(k) {
		case "type":
			f.typ = v
		case "id":
			f.id = v
		case "name":
			f.name = v
		case "tool_use_id":
			f.tuid = v
		case "content":
			f.inner = v
		case "is_error":
			f.isErr = v
		case "cache_control":
			f.cached = string(body[v.start:v.end]) != "null"
		}
		return nil
	}); err != nil {
		return err
	}
	if f.cached {
		s.bpMsg, s.bpBlk = i, j
	}
	switch string(body[f.typ.start:f.typ.end]) {
	case `"tool_use"`:
		if f.id.end != 0 && f.name.end != 0 {
			s.calls = append(s.calls, callName{f.id, f.name})
		}
	case `"tool_result"`:
		return s.result(i, j, f)
	}
	return nil
}

// result adds the regions of a tool_result block (an is_error one is skipped) and notes a breakpoint inside it.
func (s *anScan) result(i, j int, f anBlock) error {
	body := s.body
	if f.inner.end == 0 || (f.isErr.end != 0 && string(body[f.isErr.start:f.isErr.end]) == "true") {
		return nil
	}
	base := Region{Msg: i, blk: j}
	if f.tuid.end != 0 && body[f.tuid.start] == '"' {
		base.ToolCallID = Region{Start: f.tuid.start, End: f.tuid.end}.Decode(body)
		base.Tool = toolOf(body, s.calls, f.tuid)
	}
	before := len(s.out)
	if err := textStrings(body, f.inner, base, &s.out); err != nil {
		return err
	}
	for k := before; k < len(s.out); k++ {
		s.out[k].blk = j
	}
	if body[f.inner.start] == '[' && innerCached(body, f.inner) {
		s.bpMsg, s.bpBlk = i, j
	}
	return nil
}

// innerCached reports whether any block of a tool_result's content array carries a cache_control marker.
func innerCached(body []byte, inner span) bool {
	found := false
	_ = eachElem(body, inner, func(_ int, b span) error {
		if body[b.start] == '{' {
			if c := field(body, b, "cache_control"); c.end != 0 && string(body[c.start:c.end]) != "null" {
				found = true
			}
		}
		return nil
	})
	return found
}
