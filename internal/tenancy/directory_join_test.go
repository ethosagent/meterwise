package tenancy

import (
	"testing"
	"time"

	"github.com/ethosagent/meterwise/internal/contract"
)

// The late goroutine of the singleflight race, ordered with no timing at all: it checked the cache and
// missed, then the in-flight read finished (filled the cache, left the flights map), and only then did
// it reach join. join must answer from the cache it now holds under fmu, not start a second read.
func TestJoinAfterTheFlightFinishedAnswersFromTheCacheNotANewRead(t *testing.T) {
	d := NewDirectory(nil, func() time.Time { return t0 }, nil)
	k := ckey{kindPublic, "AAAAAAAAAAAAAAAAAAAAAA"}

	// the first caller's flight: begins, reads, fills the cache, leaves the map
	first, leader := d.join(k)
	if !leader {
		t.Fatal("the first caller must lead")
	}
	first.e, first.err = d.fill(k, contract.Workspace{ID: "w1", PublicID: k.id}, nil, first.seen)
	d.fmu.Lock()
	delete(d.flights, k)
	d.fmu.Unlock()
	close(first.done)

	// the late goroutine, which missed the cache before the fill
	late, leader := d.join(k)
	if leader {
		t.Fatal("a caller that arrives after the read finished started a second Postgres read")
	}
	select {
	case <-late.done:
	default:
		t.Fatal("the answer from the cache must be ready at once")
	}
	if late.err != nil || late.e == nil || late.e.ws.ID != "w1" {
		t.Fatalf("late answer = %+v, %v", late.e, late.err)
	}

	// the same for an id that was found to be unknown (negative cache)
	nk := ckey{kindPublic, "ZZZZZZZZZZZZZZZZZZZZZZ"}
	nf, _ := d.join(nk)
	nf.e, nf.err = d.fill(nk, contract.Workspace{}, contract.NewError(contract.CodeNotFound, "not found"), nf.seen)
	d.fmu.Lock()
	delete(d.flights, nk)
	d.fmu.Unlock()
	close(nf.done)
	if lf, leader := d.join(nk); leader || lf.err == nil {
		t.Fatalf("unknown id: leader=%v err=%v", leader, lf.err)
	}

	// a bust drops the entry and moves the generation: the next caller leads a fresh read
	d.Bust("w1")
	if _, leader := d.join(k); !leader {
		t.Fatal("after a bust the next caller must read Postgres again")
	}
}
