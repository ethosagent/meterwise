package wire_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/wire"
)

// MC2: AppendNotes scans and copies once, and its bytes are those of adding the notes one by one.
func TestAppendNotesEqualsAppendingOneByOne(t *testing.T) {
	notes := []string{"[proxy] one <&>", "[proxy] two \"q\"", "[proxy] three\n"}
	bodies := map[string]struct {
		w    tollgate.WireFormat
		body string
	}{
		"openai":                   {tollgate.WireOpenAIChat, `{"model":"m","messages":[{"role":"user","content":"hi"} ],"stream":true}`},
		"openai empty":             {tollgate.WireOpenAIChat, `{"messages":[ ]}`},
		"anthropic string content": {tollgate.WireAnthropicMessages, `{"system":"S","messages":[{"role":"user","content":"hi"},{"role":"assistant","content":"yo"},{"role":"user","content":"again"}]}`},
		"anthropic block content":  {tollgate.WireAnthropicMessages, `{"messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"r"}] }],"max_tokens":9}`},
	}
	for name, tc := range bodies {
		t.Run(name, func(t *testing.T) {
			want := []byte(tc.body)
			for _, n := range notes {
				var err error
				if want, err = wire.AppendNotes(tc.w, want, []string{n}); err != nil {
					t.Fatal(err)
				}
			}
			for k := 1; k <= len(notes); k++ {
				seq := []byte(tc.body)
				for _, n := range notes[:k] {
					seq, _ = wire.AppendNotes(tc.w, seq, []string{n})
				}
				got, err := wire.AppendNotes(tc.w, []byte(tc.body), notes[:k])
				if err != nil || !bytes.Equal(got, seq) {
					t.Errorf("%d notes: got %s (%v)\nwant %s", k, got, err, seq)
				}
			}
			if got, _ := wire.AppendNotes(tc.w, []byte(tc.body), notes); !bytes.Equal(got, want) {
				t.Errorf("got %s\nwant %s", got, want)
			}
		})
	}
}

func TestAppendNotesWithNoNotesLeavesTheBodyAndFailsAsOneNoteDoes(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":"hi"}]}`)
	if got, err := wire.AppendNotes(tollgate.WireOpenAIChat, body, nil); err != nil || !bytes.Equal(got, body) {
		t.Errorf("no notes: %s %v", got, err)
	}
	if _, err := wire.AppendNotes(tollgate.WireAnthropicMessages, []byte(`{"messages":[{"role":"assistant","content":"x"}]}`), []string{"a", "b"}); !errors.Is(err, wire.ErrNoSplice) {
		t.Errorf("err = %v, want ErrNoSplice", err)
	}
}
