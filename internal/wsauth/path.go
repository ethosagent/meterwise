package wsauth

import "strings"

// IDLength is the length of a workspace public id (22 base62 characters).
const IDLength = 22

const prefix = "/w/"

// Parse splits /w/{publicId}[/rest] into the id and the rest of the path ("/" when there is none).
// ok is false when the path is not under /w/ or the id is not 22 base62 characters; the id is
// checked here so a junk id never reaches the directory.
func Parse(path string) (id, rest string, ok bool) {
	tail, found := strings.CutPrefix(path, prefix)
	if !found {
		return "", "", false
	}
	id, rest, _ = strings.Cut(tail, "/")
	if len(id) != IDLength || !base62(id) {
		return "", "", false
	}
	return id, "/" + rest, true
}

// Rest is the path after /w/{publicId}: what a provider resolver routes on. It returns "" when the
// path is not a workspace URL.
func Rest(path string) string {
	_, rest, ok := Parse(path)
	if !ok {
		return ""
	}
	return rest
}

func base62(s string) bool {
	for i := range len(s) {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'A' || c > 'Z') && (c < 'a' || c > 'z') {
			return false
		}
	}
	return true
}
