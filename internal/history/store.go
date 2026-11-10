package history

import (
	"cmp"
	"container/list"
	"hash/fnv"
	"slices"
	"sync"

	"github.com/ethosagent/meterwise/internal/contract"
)

// RingDepth is how many calls a session's ring keeps; ringShingles is how many of the newest keep their shingles.
const (
	RingDepth    = 50
	ringShingles = 3
)

// ring is one session's state: its entries keyed and ordered by call index (ascending, at most RingDepth,
// immutable once published: every change copies) and top, the highest call index whose After has run,
// finished or failed. Catch-up fills only indexes above both.
type ring struct {
	calls []contract.CallSummary
	top   int
}

// newest is the highest index the ring knows of: its newest entry or top.
func (r ring) newest() int {
	if n := len(r.calls); n > 0 {
		return max(r.calls[n-1].Index, r.top)
	}
	return r.top
}

type entry struct {
	key string
	r   ring
}

type shard struct {
	mu  sync.Mutex
	m   map[string]*list.Element
	lru *list.List // front = most recent
	cap int
}

// store is a sharded, bounded LRU of session rings.
type store struct {
	shards  []*shard
	evicted func()
}

func newStore(maxSessions, shards int, evicted func()) *store {
	s := &store{evicted: evicted}
	per := max(1, (maxSessions+shards-1)/shards)
	for i := 0; i < shards; i++ {
		s.shards = append(s.shards, &shard{m: map[string]*list.Element{}, lru: list.New(), cap: per})
	}
	return s
}

func (s *store) shard(key string) *shard {
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return s.shards[int(h.Sum32())%len(s.shards)]
}

// get returns the session's ring and whether it exists, and marks it recently used.
func (s *store) get(key string) (ring, bool) {
	sh := s.shard(key)
	sh.mu.Lock()
	defer sh.mu.Unlock()
	el, ok := sh.m[key]
	if !ok {
		return ring{}, false
	}
	sh.lru.MoveToFront(el)
	return el.Value.(*entry).r, true
}

// update replaces the session's ring with fn(current) and returns it, creating it (and evicting the least
// recent session when the shard is full) as needed. fn must not mutate its argument's slice.
func (s *store) update(key string, fn func(cur ring) ring) ring {
	sh := s.shard(key)
	sh.mu.Lock()
	defer sh.mu.Unlock()
	if el, ok := sh.m[key]; ok {
		e := el.Value.(*entry)
		e.r = fn(e.r)
		sh.lru.MoveToFront(el)
		return e.r
	}
	e := &entry{key: key, r: fn(ring{})}
	sh.m[key] = sh.lru.PushFront(e)
	for sh.lru.Len() > sh.cap {
		last := sh.lru.Back()
		sh.lru.Remove(last)
		delete(sh.m, last.Value.(*entry).key)
		s.evicted()
	}
	return e.r
}

func (s *store) remove(key string) {
	sh := s.shard(key)
	sh.mu.Lock()
	defer sh.mu.Unlock()
	if el, ok := sh.m[key]; ok {
		sh.lru.Remove(el)
		delete(sh.m, key)
	}
}

func (s *store) size() int {
	n := 0
	for _, sh := range s.shards {
		sh.mu.Lock()
		n += sh.lru.Len()
		sh.mu.Unlock()
	}
	return n
}

// put returns a new ring slice: cur with each of add at its call index (ascending order, one entry per
// index), at most RingDepth entries and only indexes within RingDepth of the newest, only the newest
// ringShingles keeping their shingles. cur is not modified.
//
// fill marks add as read from a request's messages (catch-up or rebuild): it never overwrites an entry
// with content, only an empty one (a call answered without a stream, such as a block), keeping that
// entry's token counts. Otherwise add comes from the call's own After and replaces the entry, except that
// an After with no content (no stream) keeps the content a catch-up already found.
func put(cur []contract.CallSummary, add []contract.CallSummary, fill bool) []contract.CallSummary {
	out := make([]contract.CallSummary, len(cur), len(cur)+len(add))
	copy(out, cur)
	n := len(out)
	for _, s := range add {
		if i, found := slices.BinarySearchFunc(out[:n], s.Index, byIndex); found {
			out[i] = merge(out[i], s, fill)
			continue
		}
		out = append(out, s)
	}
	if len(out) > n {
		slices.SortStableFunc(out, func(a, b contract.CallSummary) int { return cmp.Compare(a.Index, b.Index) })
		out = slices.CompactFunc(out, func(a, b contract.CallSummary) bool { return a.Index == b.Index })
	}
	lo := 0
	for lo < len(out) && (len(out)-lo > RingDepth || out[lo].Index <= out[len(out)-1].Index-RingDepth) {
		lo++
	}
	out = out[lo:]
	for j := 0; j < len(out)-ringShingles; j++ {
		out[j].Shingles = nil
	}
	return out
}

func byIndex(e contract.CallSummary, idx int) int { return cmp.Compare(e.Index, idx) }

func merge(old, s contract.CallSummary, fill bool) contract.CallSummary {
	switch {
	case fill && !emptyTurn(old):
		return old
	case fill:
		s.Input, s.Output, s.BodyBytes = old.Input, old.Output, old.BodyBytes
		return s
	case emptyTurn(s) && !emptyTurn(old):
		old.Input, old.Output, old.BodyBytes = s.Input, s.Output, s.BodyBytes
		return old
	}
	return s
}

func emptyTurn(s contract.CallSummary) bool { return len(s.Tools) == 0 && s.TextHash == 0 }

// ringView is the immutable snapshot a call's Meta carries.
type ringView []contract.CallSummary

// Calls returns the snapshot, newest last. Callers must not modify it.
func (v ringView) Calls() []contract.CallSummary { return v }
