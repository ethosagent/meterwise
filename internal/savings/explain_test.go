package savings_test

import (
	"strings"
	"testing"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/savings"
)

func TestExplain(t *testing.T) {
	rr, dup, np := contract.MonitorRepeatedRead, contract.MonitorDuplicateToolCall, contract.MonitorNoProgress
	cases := []struct {
		name    string
		in      []contract.RepeatTarget
		n       int
		path    string // want Path of the first suggestion ("" = nil)
		hash    string // want PathHash of the first suggestion ("" = nil)
		contain string
	}{
		{"none", nil, 0, "", "", ""},
		{"a stored path is named", []contract.RepeatTarget{{Monitor: rr, Tool: "read", Path: "src/a.go", PathHash: "deadbeefcafe", Count: 4}}, 1, "src/a.go", "", "read re-read src/a.go (monitor fired 4 times)"},
		{"no stored path: tool and the 8-hex hash prefix", []contract.RepeatTarget{{Monitor: rr, Tool: "read", PathHash: "DEADBEEFCAFE0123", Count: 3}}, 1, "", "deadbeef", "(hash deadbeef; monitor fired 3 times)"},
		{"neither path nor hash: the tool only", []contract.RepeatTarget{{Monitor: dup, Tool: "grep", Count: 2}}, 1, "", "", "grep repeated the same call (monitor fired 2 times)"},
		{"duplicate with a hash", []contract.RepeatTarget{{Monitor: dup, Tool: "bash", PathHash: "0123456789", Count: 5}}, 1, "", "01234567", "(hash 01234567; monitor fired 5 times)"},
		{"fewer than two repeats earns nothing", []contract.RepeatTarget{{Monitor: rr, Tool: "read", Path: "a", Count: 1}}, 0, "", "", ""},
		{"a monitor that does not repeat work earns nothing", []contract.RepeatTarget{{Monitor: np, Tool: "read", Path: "a", Count: 9}}, 0, "", "", ""},
		{"a nameless tool earns nothing", []contract.RepeatTarget{{Monitor: rr, Tool: " \x00 ", Count: 9}}, 0, "", "", ""},
		{"control characters never reach the text", []contract.RepeatTarget{{Monitor: rr, Tool: "re\nad", Path: "a\x1b[31m/b", Count: 2}}, 1, "a[31m/b", "", "read re-read a[31m/b (monitor fired 2 times)"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := savings.Explain(c.in)
			if got == nil || len(got) != c.n {
				t.Fatalf("got %+v, want %d", got, c.n)
			}
			if c.n == 0 {
				return
			}
			g := got[0]
			if (g.Path == nil) != (c.path == "") || (g.Path != nil && *g.Path != c.path) || (g.PathHash == nil) != (c.hash == "") || (g.PathHash != nil && *g.PathHash != c.hash) {
				t.Errorf("path/hash = %v / %v, want %q / %q", g.Path, g.PathHash, c.path, c.hash)
			}
			if !strings.Contains(g.Text, c.contain) {
				t.Errorf("text %q lacks %q", g.Text, c.contain)
			}
		})
	}
}

func TestExplainNeverNamesAPathUnlessStored(t *testing.T) {
	for _, s := range savings.Explain([]contract.RepeatTarget{
		{Monitor: contract.MonitorRepeatedRead, Tool: "read", PathHash: "abcdef0123456789", Count: 7},
		{Monitor: contract.MonitorDuplicateToolCall, Tool: "bash", PathHash: "1111111122222222", Count: 3},
	}) {
		if s.Path != nil || strings.Contains(s.Text, "abcdef0123456789") || strings.Contains(s.Text, "1111111122222222") {
			t.Errorf("suggestion leaks a path or the full hash: %+v", s)
		}
	}
}

func TestExplainOrderAndCap(t *testing.T) {
	var in []contract.RepeatTarget
	for i := range 8 {
		in = append(in, contract.RepeatTarget{Monitor: contract.MonitorRepeatedRead, Tool: "read", Path: string(rune('a' + i)), Count: int64(2 + i%3)})
	}
	got := savings.Explain(in)
	if len(got) != 5 {
		t.Fatalf("len = %d, want the cap 5", len(got))
	}
	for i := 1; i < len(got); i++ {
		if got[i-1].Count < got[i].Count {
			t.Fatalf("not most-repeated-first: %+v", got)
		}
	}
	again := savings.Explain(in)
	for i := range got {
		if got[i].Text != again[i].Text {
			t.Fatal("order is not stable")
		}
	}
	if savings.Primary(nil) != nil || *savings.Primary(got) != got[0].Text {
		t.Error("Primary is the first suggestion's text, nil for none")
	}
}
