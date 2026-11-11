package trim

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// jsonLen is the length internal/wire's encoder writes, for every kind of byte.
func TestJSONLenMatchesTheEncoder(t *testing.T) {
	cases := []string{
		"", "plain", `quote " and \ slash`, "tab\tnl\ncr\rbs\bff\f", "ctl \x01\x1f del \x7f", "<html> & </html>",
		"é ü 日本語 🎉", "line sep   para  ", "bad \xff\xfe utf8 \xc3", strings.Repeat("a\"b", 100),
	}
	for _, s := range cases {
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		_ = enc.Encode(s)
		if want := buf.Len() - 1; jsonLen(s) != want {
			t.Errorf("jsonLen(%q) = %d, the encoder writes %d", s, jsonLen(s), want)
		}
	}
}

func TestSameTemplateIgnoresDigitRuns(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"id=17 ok", "id=2048 ok", true}, {"id=17 ok", "id=17 no", false}, {"a1b", "a1", false}, {"", "", true}, {"x", "", false},
		{"[3/300] CC a_3.c", "[299/300] CC a_299.c", true},
	} {
		if got := sameTemplate(c.a, c.b); got != c.want {
			t.Errorf("sameTemplate(%q, %q) = %v", c.a, c.b, got)
		}
	}
}

func TestMarkerLenMatchesTheMarker(t *testing.T) {
	for _, n := range []int{1, 4, 9, 10, 99, 100, 12345} {
		for _, e := range []int{0, 1, 9, 10, 250} {
			if got, want := markerLen(n, e), len(foldMarker(n, e)); got != want {
				t.Errorf("markerLen(%d, %d) = %d, want %d", n, e, got, want)
			}
		}
	}
}
