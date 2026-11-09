package tenancy

import (
	"container/list"
	"sync"
	"time"

	"github.com/ethosagent/meterwise/internal/contract"
)

// bustMemory is how long a bust is remembered per workspace. A lookup is bounded by loadCeiling,
// so one that began before a bust is over long before the bust is forgotten.
const bustMemory = 2 * time.Minute

type kind uint8

const (
	kindPublic kind = iota
	kindID
)

type ckey struct {
	kind kind
	id   string
}

// centry is one cached lookup. It is immutable once stored: a hit hands out the pointer, so a hit
// allocates nothing. err is set only for a reset (past-grace) id, cached so a client still using
// the old URL does not reach Postgres on every call. view is the data plane's prepared form of ws
// (see Directory.SetView), built once per fill.
type centry struct {
	ws   contract.Workspace
	err  error
	view any
	exp  time.Time
	tok  uint64
}

type fifoTok struct {
	key ckey
	tok uint64
}

type bustMark struct {
	seq uint64
	at  time.Time
}

// dirCache is the bounded, generation-guarded cache behind Directory.
//
// Reads take the read lock only and never delete (expiry is lazy: an expired entry is a miss and is
// replaced or swept). Writes keep an insertion-order queue; at capacity the oldest entry goes, never
// the whole map. A per-workspace index makes Bust proportional to that workspace's own ids.
//
// Generations: seq counts busts, bustedAt[ws] is the seq of the workspace's latest bust and floor
// is the highest seq of a bust already forgotten. A lookup remembers seq before it reads Postgres;
// put refuses its result when the workspace was busted since, or when the memory of that bust was
// dropped (seen < floor), because the row may predate the change the bust announced. bustedAt
// holds at most one record per workspace and drops records older than bustMemory.
type dirCache struct {
	mu        sync.RWMutex
	cap       int
	m         map[ckey]*centry
	byWS      map[string]map[ckey]struct{}
	fifo      *list.List
	tok       uint64
	seq       uint64
	floor     uint64
	bustedAt  map[string]bustMark
	lastPrune time.Time
	evicted   uint64
}

func newDirCache(capacity int) *dirCache {
	return &dirCache{cap: capacity, m: map[ckey]*centry{}, byWS: map[string]map[ckey]struct{}{},
		fifo: list.New(), bustedAt: map[string]bustMark{}}
}

// get returns the live entry for k.
func (c *dirCache) get(k ckey, now time.Time) (*centry, bool) {
	c.mu.RLock()
	e, ok := c.m[k]
	c.mu.RUnlock()
	if !ok || !now.Before(e.exp) {
		return nil, false
	}
	return e, true
}

// generation is the seq a lookup records before it reads Postgres.
func (c *dirCache) generation() uint64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.seq
}

// put stores e under k unless its workspace was busted after the lookup began (seen is the
// generation then). It reports whether it stored.
func (c *dirCache) put(k ckey, e *centry, seen uint64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if seen < c.floor || c.bustedAt[e.ws.ID].seq > seen {
		return false
	}
	for len(c.m) >= c.cap && c.evictOldest() {
	}
	c.tok++
	e.tok = c.tok
	c.m[k] = e
	c.fifo.PushBack(fifoTok{key: k, tok: e.tok})
	idx := c.byWS[e.ws.ID]
	if idx == nil {
		idx = map[ckey]struct{}{}
		c.byWS[e.ws.ID] = idx
	}
	idx[k] = struct{}{}
	if c.fifo.Len() > 2*c.cap+16 {
		c.compact()
	}
	return true
}

// evictOldest drops the oldest live entry. It reports false when there is none.
func (c *dirCache) evictOldest() bool {
	for f := c.fifo.Front(); f != nil; f = c.fifo.Front() {
		t := c.fifo.Remove(f).(fifoTok)
		if e, ok := c.m[t.key]; ok && e.tok == t.tok {
			c.remove(t.key, e)
			c.evicted++
			return true
		}
	}
	return false
}

// compact drops the queue's stale tokens (entries that were replaced, busted or swept).
func (c *dirCache) compact() {
	for f := c.fifo.Front(); f != nil; {
		next := f.Next()
		if t := f.Value.(fifoTok); c.m[t.key] == nil || c.m[t.key].tok != t.tok {
			c.fifo.Remove(f)
		}
		f = next
	}
}

func (c *dirCache) remove(k ckey, e *centry) {
	delete(c.m, k)
	if idx := c.byWS[e.ws.ID]; idx != nil {
		delete(idx, k)
		if len(idx) == 0 {
			delete(c.byWS, e.ws.ID)
		}
	}
}

// bust drops every entry of the workspace (current and retired ids) and records the generation.
func (c *dirCache) bust(workspaceID string, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seq++
	c.bustedAt[workspaceID] = bustMark{seq: c.seq, at: now}
	for k := range c.byWS[workspaceID] {
		delete(c.m, k)
	}
	delete(c.byWS, workspaceID)
	if now.Sub(c.lastPrune) > bustMemory/4 {
		c.prune(now)
	}
}

// prune forgets busts older than bustMemory, raising the floor so a lookup older than a forgotten
// bust still cannot store. Callers hold the write lock.
func (c *dirCache) prune(now time.Time) {
	c.lastPrune = now
	for ws, b := range c.bustedAt {
		if now.Sub(b.at) > bustMemory {
			c.floor = max(c.floor, b.seq)
			delete(c.bustedAt, ws)
		}
	}
}

// sweep drops entries that are past their expiry or retired and past their grace, forgets old
// busts and reports how many entries it dropped.
func (c *dirCache) sweep(now time.Time) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for k, e := range c.m {
		if !now.Before(e.exp) || (e.ws.Retired && e.ws.GraceUntil != nil && !now.Before(*e.ws.GraceUntil)) {
			c.remove(k, e)
			n++
		}
	}
	c.prune(now)
	if c.fifo.Len() > c.cap {
		c.compact()
	}
	return n
}

func (c *dirCache) size() (entries int, evicted uint64) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.m), c.evicted
}

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
