package monitors

import (
	"hash/fnv"
	"sync"
	"sync/atomic"
)

// Per-session note state: notes waiting for the session's next call (stream monitors decide after
// the response) and the keys already noted (dedup per monitor and subject). Both are bounded and
// dropped when the session closes.
const (
	sessShards  = 16
	maxPending  = 4
	maxSeenKeys = 64
)

type sessEntry struct {
	pending []string
	seen    map[string]struct{}
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
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return &s.shards[int(h.Sum32())%sessShards]
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
	e := &sessEntry{seen: map[string]struct{}{}}
	sh.m[key] = e
	return e
}

// markSeen records dedup key k for the session and reports whether it is new.
func (s *sessStore) markSeen(key, k string) bool {
	sh := s.shard(key)
	sh.mu.Lock()
	defer sh.mu.Unlock()
	e := s.entry(sh, key, true)
	if _, dup := e.seen[k]; dup {
		return false
	}
	if len(e.seen) >= maxSeenKeys {
		clear(e.seen)
	}
	e.seen[k] = struct{}{}
	return true
}

// queue holds a note for the session's next call, oldest dropped beyond maxPending.
func (s *sessStore) queue(key, text string) {
	sh := s.shard(key)
	sh.mu.Lock()
	defer sh.mu.Unlock()
	e := s.entry(sh, key, true)
	e.pending = append(e.pending, text)
	if len(e.pending) > maxPending {
		e.pending = e.pending[len(e.pending)-maxPending:]
	}
}

// take removes and returns up to n queued notes, oldest first.
func (s *sessStore) take(key string, n int) []string {
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
	out := append([]string(nil), e.pending[:n]...)
	e.pending = e.pending[n:]
	return out
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
