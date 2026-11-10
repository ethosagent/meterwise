package history

import (
	"container/list"
	"hash/fnv"
	"sync"

	"github.com/ethosagent/meterwise/internal/contract"
)

// RingDepth is how many calls a session's ring keeps; ringShingles is how many of the newest keep their shingles.
const (
	RingDepth    = 50
	ringShingles = 3
)

type entry struct {
	key   string
	calls []contract.CallSummary // immutable once published: appends copy
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

// get returns the session's current ring (nil when none) and marks it recently used.
func (s *store) get(key string) []contract.CallSummary {
	sh := s.shard(key)
	sh.mu.Lock()
	defer sh.mu.Unlock()
	el, ok := sh.m[key]
	if !ok {
		return nil
	}
	sh.lru.MoveToFront(el)
	return el.Value.(*entry).calls
}

// update replaces the session's ring with fn(current), creating it (and evicting the least recent
// session when the shard is full) as needed. fn must not mutate its argument.
func (s *store) update(key string, fn func(cur []contract.CallSummary) []contract.CallSummary) {
	sh := s.shard(key)
	sh.mu.Lock()
	defer sh.mu.Unlock()
	if el, ok := sh.m[key]; ok {
		e := el.Value.(*entry)
		e.calls = fn(e.calls)
		sh.lru.MoveToFront(el)
		return
	}
	sh.m[key] = sh.lru.PushFront(&entry{key: key, calls: fn(nil)})
	for sh.lru.Len() > sh.cap {
		last := sh.lru.Back()
		sh.lru.Remove(last)
		delete(sh.m, last.Value.(*entry).key)
		s.evicted()
	}
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

// appendCall returns a new ring: cur plus c, at most RingDepth long, only the newest ringShingles
// keeping their shingles. cur is not modified.
func appendCall(cur []contract.CallSummary, c contract.CallSummary) []contract.CallSummary {
	if n := len(cur); n > 0 && cur[n-1].Index == c.Index {
		cur = cur[:n-1] // the entry for this call was caught up from the next request: this one is the real one
	}
	keep := cur
	if len(keep) >= RingDepth {
		keep = keep[len(keep)-RingDepth+1:]
	}
	out := make([]contract.CallSummary, 0, len(keep)+1)
	out = append(out, keep...)
	out = append(out, c)
	for i := 0; i < len(out)-ringShingles; i++ {
		out[i].Shingles = nil
	}
	return out
}

// ringView is the immutable snapshot a call's Meta carries.
type ringView []contract.CallSummary

// Calls returns the snapshot, newest last. Callers must not modify it.
func (v ringView) Calls() []contract.CallSummary { return v }
