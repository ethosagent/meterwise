package ledger_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/ethosagent/meterwise/internal/ledger"
)

func TestClientFamily(t *testing.T) {
	cases := []struct {
		name string
		h    http.Header
		want string
	}{
		{"opencode as in the ledger fixture", http.Header{"User-Agent": {"opencode/1.2 (linux)"}}, "opencode"},
		{"demo-traffic as in cmd/demo-traffic", http.Header{"User-Agent": {"demo-traffic/1"}}, "demo-traffic"},
		{"lower-cased", http.Header{"User-Agent": {"OpenCode/1.2"}}, "opencode"},
		{"no version", http.Header{"User-Agent": {"curl"}}, "curl"},
		{"leading space", http.Header{"User-Agent": {"  tool/9"}}, "tool"},
		{"absent", http.Header{}, ""},
		{"empty", http.Header{"User-Agent": {""}}, ""},
		{"only a version", http.Header{"User-Agent": {"/1.0"}}, ""},
		{"control and non-ASCII bytes dropped", http.Header{"User-Agent": {"to\x00olé/1"}}, "tool"},
		{"capped at 64 bytes", http.Header{"User-Agent": {strings.Repeat("a", 200) + "/1"}}, strings.Repeat("a", 64)},
		{"OpenAI Python SDK by UA", http.Header{"User-Agent": {"OpenAI/Python 1.30.1"}}, "openai-python"},
		{"OpenAI Python SDK by stainless lang", http.Header{"User-Agent": {"OpenAI/Python 1.30.1"}, "X-Stainless-Lang": {"python"}, "X-Stainless-Package-Version": {"1.30.1"}}, "openai-python"},
		{"OpenAI node SDK", http.Header{"User-Agent": {"OpenAI/JS 4.52.0"}, "X-Stainless-Lang": {"js"}, "X-Stainless-Package-Version": {"4.52.0"}}, "openai-node"},
		{"Anthropic node SDK", http.Header{"User-Agent": {"Anthropic/JS 0.24.3"}, "X-Stainless-Lang": {"js"}, "X-Stainless-Package-Version": {"0.24.3"}}, "anthropic-node"},
		{"Anthropic Python SDK", http.Header{"User-Agent": {"Anthropic/Python 0.28.0"}, "X-Stainless-Lang": {"python"}}, "anthropic-python"},
		{"SDK token without a known language stays generic", http.Header{"User-Agent": {"OpenAI/Go 1.0"}, "X-Stainless-Lang": {"go"}}, "openai"},
		{"a literal unknown is NULL, not a client", http.Header{"User-Agent": {"Unknown/1.0"}}, ""},
		{"credential headers are never read", http.Header{"Authorization": {"Bearer sk-secret"}, "X-Api-Key": {"sk-ant-secret"}}, ""},
		{"credentials never leak next to a UA", http.Header{"User-Agent": {"opencode/1"}, "Authorization": {"Bearer sk-secret"}, "X-Api-Key": {"k"}}, "opencode"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ledger.ClientFamily(c.h); got != c.want {
				t.Errorf("ClientFamily = %q, want %q", got, c.want)
			}
		})
	}
}
