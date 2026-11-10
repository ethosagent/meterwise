package history_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/history"
)

// MC7: a turn's shingles are stored as a sorted set, so the monitors compare them by a merge walk, and a
// rebuilt entry stores them the same way as a live one.
func TestRingShinglesAreASortedSet(t *testing.T) {
	loop := strings.Repeat("one two three four five six seven eight nine ten ", 12) // many repeated windows
	h := history.New(history.Options{})
	run(t, h, newCall("ws", "s", 1), 1, 1, text(loop))
	live := ringOf(t, run(t, h, newCall("ws", "s", 2), 1, 1))
	if len(live) != 1 || len(live[0].Shingles) == 0 {
		t.Fatalf("ring %+v", live)
	}
	rebuilt := ringOf(t, mustBefore(t, history.New(history.Options{}), newCall("ws", "r", 2, asst(loop), tollgate.Message{Role: tollgate.RoleUser, Text: "go"})))
	for name, sh := range map[string][]uint64{"live": live[0].Shingles, "rebuilt": rebuilt[0].Shingles} {
		if !slices.IsSorted(sh) || len(slices.Compact(slices.Clone(sh))) != len(sh) {
			t.Errorf("%s shingles are not a sorted set: %v", name, sh)
		}
	}
	if !slices.Equal(live[0].Shingles, rebuilt[0].Shingles) {
		t.Error("a rebuilt entry must store the shingles the live one does")
	}
}
