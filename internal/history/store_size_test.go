package history

import (
	"math/rand"
	"testing"

	"github.com/ethosagent/meterwise/internal/contract"
)

// MC6: a ring's byte charge is kept as a delta by with; it must always equal the charge of its entries.
func TestRingSizeDeltaEqualsTheEntriesCharge(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	entry := func(idx int) contract.CallSummary {
		s := contract.CallSummary{Index: idx, Tools: []string{"grep", "read_file"[:1+rng.Intn(9)]}, ArgsHashes: make([]uint64, rng.Intn(3)),
			Shingles: make([]uint64, rng.Intn(contract.MaxRingShingles))}
		if rng.Intn(4) == 0 {
			s.Tools, s.ArgsHashes = nil, nil // an empty turn
		}
		return s
	}
	var r ring
	for step := 0; step < 3000; step++ {
		fill := rng.Intn(3) == 0
		var add []contract.CallSummary
		switch rng.Intn(4) {
		case 0: // the next call
			add = []contract.CallSummary{entry(r.newest() + 1)}
		case 1: // a replacement of an entry already there
			if n := len(r.calls); n > 0 {
				add = []contract.CallSummary{entry(r.calls[rng.Intn(n)].Index)}
			}
		case 2: // several, out of order, one index twice
			base := r.newest() + 1
			add = []contract.CallSummary{entry(base + 2), entry(base), entry(base + 2)}
		default: // far ahead: older entries fall out of the ring
			add = []contract.CallSummary{entry(r.newest() + 1 + rng.Intn(80))}
		}
		r = r.with(add, fill)
		var want int64
		for _, c := range r.calls {
			want += entryBytes(c)
		}
		if r.size != want || len(r.calls) > RingDepth {
			t.Fatalf("step %d: size %d, entries charge %d, %d entries", step, r.size, want, len(r.calls))
		}
	}
}
