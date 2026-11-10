package monitors_test

import (
	"testing"

	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/monitors"
)

// A repeat of a tool call whose earlier result the trim hook folded is the model asking for it back, so
// neither duplicate_tool_call nor repeated_read fires on it; every other repeat still does.
func TestUnfoldedCallsAreNotWaste(t *testing.T) {
	sub := contract.ToolSubject("read_file", []byte(`{"path":"/a.go"}`))
	readRing := func() fakeRing {
		return fakeRing{
			{Index: 1, Tools: []string{"read_file"}, ArgsHashes: []uint64{7}, ReadHashes: []uint64{sub}},
			{Index: 2, Tools: []string{"grep"}, ArgsHashes: []uint64{12}},
			{Index: 3, Tools: []string{"read_file"}, ArgsHashes: []uint64{7}, ReadHashes: []uint64{sub}},
		}
	}
	readsAt2 := attr(func(c *contract.MonitorConfig) {
		c.RepeatedRead = contract.RepeatedReadConfig{Mode: contract.ModeObserve, MinReads: 2}
	})
	cases := []struct {
		name      string
		ring      fakeRing
		unfold    []uint64
		wantFires int
		wantSkips int64
	}{
		{"dup, hash listed", dupRing(), []uint64{parisHash}, 0, 1},
		{"dup, another hash listed", dupRing(), []uint64{99}, 1, 0},
		{"dup, empty list", dupRing(), nil, 1, 0},
		{"read, hash listed", readRing(), []uint64{7}, 0, 2}, // both detectors would have fired
		{"read, another hash listed", readRing(), []uint64{8}, 2, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newMon(monitors.Options{})
			msgs := weatherTranscript(3)
			c := before(t, tollgate.WireOpenAIChat, tc.ring, readsAt2, 4, msgs)
			if tc.unfold != nil {
				c.Meta[contract.MetaTrimUnfold] = contract.TrimUnfold{ArgsHashes: tc.unfold}
			}
			if _, err := h.Before(bg, c); err != nil {
				t.Fatal(err)
			}
			vs, _ := verdictsOf(c)
			if len(vs) != tc.wantFires {
				t.Errorf("verdicts = %d, want %d: %+v", len(vs), tc.wantFires, vs)
			}
			if got := h.Status()["unfold_skips"]; got != tc.wantSkips {
				t.Errorf("unfold_skips = %v, want %d", got, tc.wantSkips)
			}
		})
	}
}
