package tenancy

import (
	"sync"
	"time"
)

// negCache remembers ids that named no workspace, briefly and in bounded number, so a client
// hammering a wrong URL does not reach Postgres on every call. Public ids are 131 random bits, so
// an id that is unknown now becomes valid only by an astronomically unlikely draw: it needs no
// bust, only the short TTL.
type negCache struct {
	mu  sync.RWMutex
	cap int
	ttl time.Duration
	m   map[string]negEntry
}

type negEntry struct {
	exp time.Time
	err error
}

func newNegCache(capacity int, ttl time.Duration) *negCache {
	return &negCache{cap: capacity, ttl: ttl, m: map[string]negEntry{}}
}

func (n *negCache) get(id string, now time.Time) (negEntry, bool) {
	n.mu.RLock()
	e, ok := n.m[id]
	n.mu.RUnlock()
	if !ok || !now.Before(e.exp) {
		return negEntry{}, false
	}
	return e, true
}

// put remembers id. At capacity it drops the expired entries, then the one expiring first.
func (n *negCache) put(id string, err error, now time.Time) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if _, ok := n.m[id]; !ok && len(n.m) >= n.cap {
		oldest, oldestExp := "", time.Time{}
		for k, e := range n.m {
			if !now.Before(e.exp) {
				delete(n.m, k)
				continue
			}
			if oldest == "" || e.exp.Before(oldestExp) {
				oldest, oldestExp = k, e.exp
			}
		}
		if len(n.m) >= n.cap {
			delete(n.m, oldest)
		}
	}
	n.m[id] = negEntry{exp: now.Add(n.ttl), err: err}
}

func (n *negCache) sweep(now time.Time) int {
	n.mu.Lock()
	defer n.mu.Unlock()
	dropped := 0
	for k, e := range n.m {
		if !now.Before(e.exp) {
			delete(n.m, k)
			dropped++
		}
	}
	return dropped
}

func (n *negCache) size() int {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return len(n.m)
}
