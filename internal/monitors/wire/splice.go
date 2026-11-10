package wire

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

// Errors returned by the editors. A caller treats any of them as "cannot edit this body".
var (
	ErrUnsupportedWire = errors.New("wire: unsupported wire format")
	ErrNoSplice        = errors.New("wire: no place to splice the note")
	ErrNoTurn          = errors.New("wire: no assistant turn to replay")
)

// span is a half-open byte range [start, end) of a JSON value.
type span struct{ start, end int }

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }

// skipSpace returns the first index at or after i that is not JSON whitespace.
func skipSpace(b []byte, i int) int {
	for i < len(b) && isSpace(b[i]) {
		i++
	}
	return i
}

// skipValue returns the index just past the JSON value that starts at b[i] (no leading space).
// The body was already parsed by tollgate, so this only walks it: no allocation, one pass.
func skipValue(b []byte, i int) (int, error) {
	if i >= len(b) {
		return 0, fmt.Errorf("%w: truncated", ErrNoSplice)
	}
	switch b[i] {
	case '"':
		return skipString(b, i)
	case '{', '[':
		return skipNested(b, i)
	}
	for i < len(b) && b[i] != ',' && b[i] != '}' && b[i] != ']' && !isSpace(b[i]) {
		i++
	}
	return i, nil
}

// skipString returns the index just past the string that opens at b[i].
func skipString(b []byte, i int) (int, error) {
	if i >= len(b) || b[i] != '"' {
		return 0, fmt.Errorf("%w: expected a string", ErrNoSplice)
	}
	for i++; i < len(b); i++ {
		switch b[i] {
		case '\\':
			i++
		case '"':
			return i + 1, nil
		}
	}
	return 0, fmt.Errorf("%w: unterminated string", ErrNoSplice)
}

func skipNested(b []byte, i int) (int, error) {
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
			end, err := skipString(b, i)
			if err != nil {
				return 0, err
			}
			i = end - 1
		}
	}
	return 0, fmt.Errorf("%w: unterminated value", ErrNoSplice)
}

// objectFields returns the byte span of each top-level value of the JSON object in b.
func objectFields(b []byte) (map[string]span, error) {
	i := skipSpace(b, 0)
	if i >= len(b) || b[i] != '{' {
		return nil, fmt.Errorf("%w: not an object", ErrNoSplice)
	}
	out := map[string]span{}
	for i = skipSpace(b, i+1); i < len(b) && b[i] != '}'; i = skipSpace(b, i) {
		if b[i] == ',' {
			i = skipSpace(b, i+1)
		}
		keyEnd, err := skipString(b, i)
		if err != nil {
			return nil, err
		}
		var key string
		if json.Unmarshal(b[i:keyEnd], &key) != nil {
			return nil, fmt.Errorf("%w: bad key", ErrNoSplice)
		}
		start := skipSpace(b, keyEnd)
		if start >= len(b) || b[start] != ':' {
			return nil, fmt.Errorf("%w: missing colon", ErrNoSplice)
		}
		start = skipSpace(b, start+1)
		end, err := skipValue(b, start)
		if err != nil {
			return nil, err
		}
		out[key] = span{start, end}
		i = end
	}
	return out, nil
}

// arrayElems returns the byte span of each element of the JSON array in b.
func arrayElems(b []byte) ([]span, error) {
	i := skipSpace(b, 0)
	if i >= len(b) || b[i] != '[' {
		return nil, fmt.Errorf("%w: not an array", ErrNoSplice)
	}
	var out []span
	for i = skipSpace(b, i+1); i < len(b) && b[i] != ']'; i = skipSpace(b, i) {
		if b[i] == ',' {
			i = skipSpace(b, i+1)
		}
		end, err := skipValue(b, i)
		if err != nil {
			return nil, err
		}
		out = append(out, span{i, end})
		i = end
	}
	return out, nil
}

// splice returns body with ins inserted at offset at; the rest is copied unchanged.
func splice(body []byte, at int, ins string) []byte {
	out := make([]byte, 0, len(body)+len(ins))
	out = append(out, body[:at]...)
	out = append(out, ins...)
	return append(out, body[at:]...)
}

// appendToArray inserts elem as the new last element of the array that arr spans in body.
func appendToArray(body []byte, arr span, elem string) ([]byte, error) {
	if arr.end-arr.start < 2 || body[arr.start] != '[' || body[arr.end-1] != ']' {
		return nil, fmt.Errorf("%w: not an array", ErrNoSplice)
	}
	p := arr.end - 1
	for p > arr.start+1 && isSpace(body[p-1]) {
		p--
	}
	if body[p-1] == '[' {
		return splice(body, p, elem), nil
	}
	return splice(body, p, ","+elem), nil
}

// jsonString encodes s as a JSON string without HTML escaping.
func jsonString(s string) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	return string(bytes.TrimRight(buf.Bytes(), "\n"))
}
