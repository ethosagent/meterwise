package tenancy

import (
	"fmt"
	"testing"
	"time"

	"github.com/ethosagent/meterwise/internal/contract"
)

var t0 = time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)

func ent(ws string, exp time.Time) *centry {
	return &centry{ws: contract.Workspace{ID: ws}, exp: exp}
}

func TestCacheEvictsTheOldestAtCapacityAndNeverWipes(t *testing.T) {
	c := newDirCache(3)
	for i := range 5 {
		k := ckey{kindPublic, fmt.Sprint("p", i)}
		if !c.put(k, ent(fmt.Sprint("w", i), t0.Add(time.Hour)), 0) {
			t.Fatalf("put %d refused", i)
		}
	}
	n, evicted := c.size()
	if n != 3 || evicted != 2 {
		t.Fatalf("size %d evicted %d, want 3 and 2", n, evicted)
	}
	for i, want := range []bool{false, false, true, true, true} {
		if _, ok := c.get(ckey{kindPublic, fmt.Sprint("p", i)}, t0); ok != want {
			t.Errorf("p%d present = %v, want %v (the oldest go first, the newest stay)", i, ok, want)
		}
	}
}

func TestCacheBustDropsOnlyThatWorkspacesEntries(t *testing.T) {
	c := newDirCache(10)
	exp := t0.Add(time.Hour)
	c.put(ckey{kindPublic, "a-cur"}, ent("A", exp), 0)
	c.put(ckey{kindPublic, "a-old"}, ent("A", exp), 0)
	c.put(ckey{kindID, "A"}, ent("A", exp), 0)
	c.put(ckey{kindPublic, "b-cur"}, ent("B", exp), 0)
	c.bust("A", t0)
	for _, k := range []ckey{{kindPublic, "a-cur"}, {kindPublic, "a-old"}, {kindID, "A"}} {
		if _, ok := c.get(k, t0); ok {
			t.Errorf("%v survived the bust", k)
		}
	}
	if _, ok := c.get(ckey{kindPublic, "b-cur"}, t0); !ok {
		t.Fatal("another workspace's entry was dropped")
	}
	if _, ok := c.byWS["A"]; ok {
		t.Fatal("the per-workspace index kept a dead workspace")
	}
}

func TestCacheRefusesALookupThatBeganBeforeABust(t *testing.T) {
	c := newDirCache(10)
	seen := c.generation()
	c.bust("A", t0)
	if c.put(ckey{kindPublic, "p"}, ent("A", t0.Add(time.Hour)), seen) {
		t.Fatal("a row read before the bust was cached")
	}
	if !c.put(ckey{kindPublic, "p"}, ent("A", t0.Add(time.Hour)), c.generation()) {
		t.Fatal("a row read after the bust was refused")
	}
	if !c.put(ckey{kindPublic, "q"}, ent("B", t0.Add(time.Hour)), seen) {
		t.Fatal("a bust of A blocked a lookup of B")
	}
}

func TestBustMemoryIsBoundedAndForgettingIsSafe(t *testing.T) {
	c := newDirCache(10)
	early := c.generation()
	for i := range 1000 {
		c.bust(fmt.Sprint("w", i), t0)
	}
	if len(c.bustedAt) != 1000 {
		t.Fatalf("one record per workspace expected, got %d", len(c.bustedAt))
	}
	later := t0.Add(2 * bustMemory)
	c.bust("fresh", later)
	if len(c.bustedAt) != 1 {
		t.Fatalf("old busts must be forgotten, %d remain", len(c.bustedAt))
	}
	if c.put(ckey{kindPublic, "p"}, ent("w1", later.Add(time.Hour)), early) {
		t.Fatal("a lookup older than a forgotten bust must still be refused (the floor)")
	}
}

func TestQueueStaysBoundedUnderReplacementChurn(t *testing.T) {
	c := newDirCache(4)
	k := ckey{kindPublic, "p"}
	for range 1000 {
		c.put(k, ent("A", t0.Add(time.Hour)), 0)
	}
	if c.fifo.Len() > 2*c.cap+17 {
		t.Fatalf("queue grew to %d for one live entry", c.fifo.Len())
	}
	if n, _ := c.size(); n != 1 {
		t.Fatalf("size %d", n)
	}
}

func TestSweepDropsExpiredAndRetiredPastGrace(t *testing.T) {
	c := newDirCache(10)
	grace := t0.Add(time.Minute)
	c.put(ckey{kindPublic, "live"}, ent("A", t0.Add(time.Hour)), 0)
	c.put(ckey{kindPublic, "expired"}, ent("B", t0.Add(time.Second)), 0)
	c.put(ckey{kindPublic, "retired"}, &centry{ws: contract.Workspace{ID: "C", Retired: true, GraceUntil: &grace}, exp: t0.Add(time.Hour)}, 0)
	if n := c.sweep(t0.Add(2 * time.Minute)); n != 2 {
		t.Fatalf("swept %d, want 2", n)
	}
	if _, ok := c.get(ckey{kindPublic, "live"}, t0.Add(2*time.Minute)); !ok {
		t.Fatal("a live entry was swept")
	}
}
