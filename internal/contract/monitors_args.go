package contract

import (
	"bytes"
	"encoding/json"
)

// ToolArgsHash hashes a tool call by name and its arguments with whitespace removed and, for an object,
// the members in any order: the same call spelled differently hashes the same. Only the top level is
// order-insensitive; a nested object is compared as written. It allocates nothing.
func ToolArgsHash(name string, args []byte) uint64 {
	h := HashString(name)
	args = bytes.TrimSpace(args)
	if len(args) == 0 {
		return h
	}
	if args[0] != '{' {
		return mixBytes(h, args)
	}
	var sum uint64
	n := 0
	if !members(args, func(key, val []byte) bool {
		sum += mixBytes(mixBytes(fnvOffset, key), val)
		n++
		return true
	}) {
		return mixBytes(h, args)
	}
	return (h^sum)*fnvPrime ^ uint64(n)
}

// members calls fn with each top-level member of the JSON object args (key with its quotes, raw value)
// in order, until fn returns false. It reports false when args is not a well-formed object.
func members(args []byte, fn func(key, val []byte) bool) bool {
	for i := jsonSpace(args, 1); i < len(args) && args[i] != '}'; i = jsonSpace(args, i) {
		if args[i] == ',' {
			i = jsonSpace(args, i+1)
		}
		keyEnd := jsonString(args, i)
		if keyEnd < 0 {
			return false
		}
		colon := jsonSpace(args, keyEnd)
		if colon >= len(args) || args[colon] != ':' {
			return false
		}
		start := jsonSpace(args, colon+1)
		end := jsonValue(args, start)
		if end < 0 {
			return false
		}
		if !fn(args[i:keyEnd], args[start:end]) {
			return true
		}
		i = end
	}
	return true
}

// mixBytes folds b into h (FNV-1a), skipping whitespace outside strings.
func mixBytes(h uint64, b []byte) uint64 {
	in, esc := false, false
	for _, c := range b {
		switch {
		case in && esc:
			esc = false
		case in && c == '\\':
			esc = true
		case in && c == '"':
			in = false
		case !in && c == '"':
			in = true
		case !in && (c == ' ' || c == '\t' || c == '\n' || c == '\r'):
			continue
		}
		h = (h ^ uint64(c)) * fnvPrime
	}
	return h
}

func jsonSpace(b []byte, i int) int {
	for i < len(b) && (b[i] == ' ' || b[i] == '\t' || b[i] == '\n' || b[i] == '\r') {
		i++
	}
	return i
}

// jsonString returns the index past the string at b[i], or -1.
func jsonString(b []byte, i int) int {
	if i >= len(b) || b[i] != '"' {
		return -1
	}
	for i++; i < len(b); i++ {
		switch b[i] {
		case '\\':
			i++
		case '"':
			return i + 1
		}
	}
	return -1
}

// jsonValue returns the index past the value at b[i], or -1.
func jsonValue(b []byte, i int) int {
	if i >= len(b) {
		return -1
	}
	switch b[i] {
	case '"':
		return jsonString(b, i)
	case '{', '[':
		depth := 0
		for ; i < len(b); i++ {
			switch b[i] {
			case '{', '[':
				depth++
			case '}', ']':
				if depth--; depth == 0 {
					return i + 1
				}
			case '"':
				end := jsonString(b, i)
				if end < 0 {
					return -1
				}
				i = end - 1
			}
		}
		return -1
	}
	for i < len(b) && b[i] != ',' && b[i] != '}' && b[i] != ']' && b[i] != ' ' && b[i] != '\t' && b[i] != '\n' && b[i] != '\r' {
		i++
	}
	return i
}

// ToolSubject is what a tool call touches: the hash of its path argument, or of the whole call when
// it names no path. The ring's ReadHashes and WroteHashes hold subjects.
func ToolSubject(name string, args []byte) uint64 {
	if p := ToolPath(args); p != "" {
		return HashString(p)
	}
	return ToolArgsHash(name, args)
}

var pathKeys = []string{"path", "file_path", "filepath", "file", "filename", "target_file", "absolute_path"}

// ToolPath returns the file path a tool call names in its top-level arguments, or "". The first
// key of pathKeys (in that order) whose value is a non-empty string wins.
func ToolPath(args []byte) string {
	args = bytes.TrimSpace(args)
	if len(args) == 0 || args[0] != '{' {
		return ""
	}
	var found [7]string
	if !members(args, func(key, val []byte) bool {
		if k := pathKeyIndex(key[1 : len(key)-1]); k >= 0 && val[0] == '"' {
			var s string
			if json.Unmarshal(val, &s) == nil {
				found[k] = s
			}
		}
		return true
	}) {
		return ""
	}
	for _, s := range found {
		if s != "" {
			return s
		}
	}
	return ""
}

func pathKeyIndex(key []byte) int {
	for i, k := range pathKeys {
		if string(key) == k {
			return i
		}
	}
	return -1
}
