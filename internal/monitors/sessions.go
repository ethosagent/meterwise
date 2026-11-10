package monitors

import (
	"sync"
	"sync/atomic"

	"github.com/ethosagent/meterwise/internal/contract"
)

// Per-session note state: notes waiting for the session's next call (stream monitors decide after the
// response), and the dedup keys of what was noted or blocked (one per monitor and subject). A key is
// reserved while its note is queued or being delivered, and used up once delivered (S2, K1, K2): a
// reservation is released when the delivery fails, and the key then fires again. Both are bounded and
// dropped when the session closes.
const (
	sessShards  = 16
	maxPending  = 4
	maxSeenKeys = 64
)

// queuedNote is a stream note waiting for the session's next call, with the key it reserves.
type queuedNote struct{ key, text string }

type sessEntry struct {
	pending []queuedNote
	keys    map[string]bool // dedup key -> used up (true) or reserved (false)
}

type sessShard struct {
	mu sync.Mutex
	m  map[string]*sessEntry
}

type sessStore struct {
	shards  [sessShards]sessShard
	perCap  int
	evicted atomic.Int64
}

func newSessStore(maxSessions int) *sessStore {
	s := &sessStore{perCap: max(1, maxSessions/sessShards)}
	for i := range s.shards {
		s.shards[i].m = map[string]*sessEntry{}
	}
	return s
}

func (s *sessStore) shard(key string) *sessShard {
	return &s.shards[contract.HashString(key)%sessShards]
}

// entry returns the session's entry, creating it (and dropping an arbitrary one when the shard is
// full) when create is set. The shard lock must be held.
func (s *sessStore) entry(sh *sessShard, key string, create bool) *sessEntry {
	if e := sh.m[key]; e != nil || !create {
		return e
	}
	if len(sh.m) >= s.perCap {
		for k := range sh.m {
			delete(sh.m, k)
			s.evicted.Add(1)
			break
		}
	}
	e := &sessEntry{keys: map[string]bool{}}
	sh.m[key] = e
	return e
}

// reserve takes dedup key k for the session unless it is already reserved or used up, and reports whether
// it is now the caller's. The caller commits it once its note or block is delivered, or releases it.
func (s *sessStore) reserve(key, k string) bool {
	sh := s.shard(key)
	sh.mu.Lock()
	defer sh.mu.Unlock()
	return s.reserveLocked(s.entry(sh, key, true), k)
}

func (s *sessStore) reserveLocked(e *sessEntry, k string) bool {
	if _, taken := e.keys[k]; taken {
		return false
	}
	if len(e.keys) >= maxSeenKeys {
		for kk, used := range e.keys { // forget what was used up; reservations belong to calls in flight
			if used {
				delete(e.keys, kk)
			}
		}
	}
	e.keys[k] = false
	return true
}

// commit uses dedup key k up: its note or block was delivered.
func (s *sessStore) commit(key, k string) {
	sh := s.shard(key)
	sh.mu.Lock()
	defer sh.mu.Unlock()
	s.entry(sh, key, true).keys[k] = true
}

// release gives back a reserved dedup key whose note or block was not delivered.
func (s *sessStore) release(key, k string) {
	sh := s.shard(key)
	sh.mu.Lock()
	defer sh.mu.Unlock()
	if e := s.entry(sh, key, false); e != nil && !e.keys[k] {
		delete(e.keys, k)
	}
}

// queue holds a stream note for the session's next call and reserves its key, unless the key is taken
// (reported false). Beyond maxPending the oldest note is dropped and its key released.
func (s *sessStore) queue(key, k, text string) bool {
	sh := s.shard(key)
	sh.mu.Lock()
	defer sh.mu.Unlock()
	e := s.entry(sh, key, true)
	if !s.reserveLocked(e, k) {
		return false
	}
	e.pending = append(e.pending, queuedNote{k, text})
	e.trim()
	return true
}

func (e *sessEntry) trim() {
	for len(e.pending) > maxPending {
		delete(e.keys, e.pending[0].key)
		e.pending = e.pending[1:]
	}
}

// take removes up to n queued notes, oldest first. Their keys stay reserved: the caller commits them once
// delivered, or restores the notes.
func (s *sessStore) take(key string, n int) []queuedNote {
	if n <= 0 {
		return nil
	}
	sh := s.shard(key)
	sh.mu.Lock()
	defer sh.mu.Unlock()
	e := s.entry(sh, key, false)
	if e == nil || len(e.pending) == 0 {
		return nil
	}
	n = min(n, len(e.pending))
	out := append([]queuedNote(nil), e.pending[:n]...)
	e.pending = e.pending[n:]
	return out
}

// restore puts notes that were taken but not delivered back at the front of the queue.
func (s *sessStore) restore(key string, notes []queuedNote) {
	if len(notes) == 0 {
		return
	}
	sh := s.shard(key)
	sh.mu.Lock()
	defer sh.mu.Unlock()
	e := s.entry(sh, key, true)
	e.pending = append(append([]queuedNote(nil), notes...), e.pending...)
	e.trim()
}

func (s *sessStore) remove(key string) {
	sh := s.shard(key)
	sh.mu.Lock()
	delete(sh.m, key)
	sh.mu.Unlock()
}

func (s *sessStore) size() int {
	n := 0
	for i := range s.shards {
		s.shards[i].mu.Lock()
		n += len(s.shards[i].m)
		s.shards[i].mu.Unlock()
	}
	return n
}
