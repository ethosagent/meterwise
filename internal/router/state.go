package router

import (
	"hash/fnv"
	"sync"
	"time"

	"github.com/ethosagent/meterwise/internal/contract"
)

// escalateCalls is how many calls stay on the strong model after a monitor fired on a routed call.
const escalateCalls = 2

// sessionState is what the router remembers about one session between calls, in memory only: the verdict
// collection of the last routed call (read at the next Before, when the monitors have had their say) and how many
// more calls are forced strong.
type sessionState struct {
	pending *contract.CallVerdicts
	forced  int
}

type sessionShard struct {
	mu sync.Mutex
	m  map[string]*sessionState
}

const shardCount = 32

// sessions is the bounded per-session memory, sharded so concurrent calls rarely meet on one lock. A full shard
// drops an arbitrary entry (counted by the caller) rather than grow: losing one escalation is safe, it only means
// that session's next call may be routed again.
type sessions struct {
	shards [shardCount]sessionShard
	perCap int
}

func newSessions(limit int) *sessions {
	s := &sessions{perCap: max(limit/shardCount, 1)}
	for i := range s.shards {
		s.shards[i].m = map[string]*sessionState{}
	}
	return s
}

func (s *sessions) shard(key string) *sessionShard {
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return &s.shards[h.Sum32()%shardCount]
}

// use runs f on the session's state under its shard lock, creating the state. It reports whether an entry was
// evicted to make room.
func (s *sessions) use(key string, f func(*sessionState)) (evicted bool) {
	sh := s.shard(key)
	sh.mu.Lock()
	defer sh.mu.Unlock()
	st := sh.m[key]
	if st == nil {
		if len(sh.m) >= s.perCap {
			for k := range sh.m {
				delete(sh.m, k)
				evicted = true
				break
			}
		}
		st = &sessionState{}
		sh.m[key] = st
	}
	f(st)
	return evicted
}

func (s *sessions) drop(key string) {
	sh := s.shard(key)
	sh.mu.Lock()
	delete(sh.m, key)
	sh.mu.Unlock()
}

func (s *sessions) size() int {
	n := 0
	for i := range s.shards {
		s.shards[i].mu.Lock()
		n += len(s.shards[i].m)
		s.shards[i].mu.Unlock()
	}
	return n
}

// pairKey names one route of one workspace.
type pairKey struct{ tenant, provider, from, to string }

// unavailable remembers, in memory, the pairs an upstream refused (400, 403 or 404 on a routed call) until a
// deadline. It is bounded: when full, expired marks go first, and a mark that still has no room is not stored.
type unavailable struct {
	mu  sync.RWMutex
	m   map[pairKey]time.Time
	cap int
}

func newUnavailable(limit int) *unavailable {
	return &unavailable{m: map[pairKey]time.Time{}, cap: limit}
}

func (u *unavailable) blocked(k pairKey, now time.Time) bool {
	u.mu.RLock()
	until, ok := u.m[k]
	u.mu.RUnlock()
	return ok && now.Before(until)
}

// mark records k until the given time and reports whether it was stored.
func (u *unavailable) mark(k pairKey, now, until time.Time) bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	if _, ok := u.m[k]; !ok && len(u.m) >= u.cap {
		for old, t := range u.m {
			if !now.Before(t) {
				delete(u.m, old)
			}
		}
		if len(u.m) >= u.cap {
			return false
		}
	}
	u.m[k] = until
	return true
}

func (u *unavailable) size(now time.Time) int {
	u.mu.RLock()
	defer u.mu.RUnlock()
	n := 0
	for _, t := range u.m {
		if now.Before(t) {
			n++
		}
	}
	return n
}
