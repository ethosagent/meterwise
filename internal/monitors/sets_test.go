package monitors

import (
	"math/rand"
	"slices"
	"testing"
)

// MC7: the set operations walk sorted slices; they must agree with the map-based definitions they replace,
// including on rings that were not stored sorted (a fixture, an older entry).

func refJaccard[K comparable](a, b []K) int {
	sa, sb := map[K]struct{}{}, map[K]struct{}{}
	for _, v := range a {
		sa[v] = struct{}{}
	}
	for _, v := range b {
		sb[v] = struct{}{}
	}
	if len(sa) == 0 && len(sb) == 0 {
		return 100
	}
	inter := 0
	for k := range sa {
		if _, ok := sb[k]; ok {
			inter++
		}
	}
	return inter * 100 / (len(sa) + len(sb) - inter)
}

func randHashes(rng *rand.Rand, n, span int) []uint64 {
	out := make([]uint64, n)
	for i := range out {
		out[i] = uint64(rng.Intn(span))
	}
	return out
}

func TestJaccardByMergeWalkEqualsTheMapDefinition(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	for i := 0; i < 2000; i++ {
		a, b := randHashes(rng, rng.Intn(40), 60), randHashes(rng, rng.Intn(40), 60) // unsorted, with repeats
		if got, want := jaccardPct(sortedSet(a), sortedSet(b)), refJaccard(a, b); got != want {
			t.Fatalf("jaccard(%v, %v) = %d, want %d", a, b, got, want)
		}
	}
}

func TestSortedSetKeepsASortedSetAndFixesTheRest(t *testing.T) {
	sorted := []uint64{1, 4, 9}
	if got := sortedSet(sorted); &got[0] != &sorted[0] {
		t.Error("a stored set must be used as it is, without a copy")
	}
	in := []uint64{9, 1, 4, 1}
	if got := sortedSet(in); !slices.Equal(got, []uint64{1, 4, 9}) || !slices.Equal(in, []uint64{9, 1, 4, 1}) {
		t.Errorf("sortedSet(%v) = %v (input must be left alone)", in, got)
	}
}

func TestJaccardAtLeastEqualsComparingThePercentage(t *testing.T) {
	rng := rand.New(rand.NewSource(5))
	words := []string{"read", "file", "path", "src", "main", "go", "test", "limit", "50", "grep", "x"}
	pick := func() []string {
		var out []string
		for _, w := range words {
			if rng.Intn(3) == 0 {
				out = append(out, w)
			}
		}
		return out // sorted order is not needed for the claim below: sort it
	}
	for i := 0; i < 3000; i++ {
		a, b := pick(), pick()
		slices.Sort(a)
		slices.Sort(b)
		pct := 1 + rng.Intn(100)
		if got, want := jaccardAtLeast(a, b, pct), jaccardPct(a, b) >= pct; got != want {
			t.Fatalf("jaccardAtLeast(%v, %v, %d) = %v, want %v", a, b, pct, got, want)
		}
	}
}

func TestSetRunBySearchEqualsTheMapDefinition(t *testing.T) {
	rng := rand.New(rand.NewSource(9))
	for i := 0; i < 1000; i++ {
		win, set := randHashes(rng, rng.Intn(60), 30), randHashes(rng, rng.Intn(25), 30)
		m := map[uint64]struct{}{}
		for _, v := range set {
			m[v] = struct{}{}
		}
		wantBest, wantAt, run := 0, 0, 0
		for j, w := range win {
			if _, ok := m[w]; !ok {
				run = 0
				continue
			}
			run++
			if run > wantBest {
				wantBest, wantAt = run, j-run+1
			}
		}
		if b, a := setRun(win, sortedSet(set)); b != wantBest || a != wantAt {
			t.Fatalf("setRun(%v, %v) = %d,%d want %d,%d", win, set, b, a, wantBest, wantAt)
		}
	}
}
