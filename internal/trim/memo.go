package trim

import (
	"container/list"
	"slices"
	"sync"
	"sync/atomic"
)

// memoKey names a region decision: the hash of the tool name and the region's bytes, and which copy of that content
// this is in the request (0 for the first, 1 for an identical repeat, ...). A repeat is decided differently from the
// first copy (repeat_collapse), and the pair is stable as a conversation grows.
type memoKey struct {
	h uint64
	k int
}

// decision is what the hook decided for one region and what it sends for it from then on.
type decision struct {
	tool     string
	applied  bool   // the region is sent as out (acted mode)
	out      string // the replacement text, only when applied
	saved    int    // bytes of the JSON string the decision saves (0: unchanged)
	stages   []StageOutcome
	args     uint64 // ToolArgsHash of the tool call that produced the region
	callID   string // and that call's id
	hasArgs  bool
	unfolded bool // the agent asked for this result again, so it is sent whole from now on
}

func (d decision) size() int { return 96 + len(d.out) + 24*len(d.stages) }

// Per-session memo limits. A session over either is forgotten whole and counted as evicted; its regions are then
// decided again by the config of the day.
const (
	memoMaxEntries = 4096
	memoMaxBytes   = 4 << 20
	memoMaxFolded  = 1024
)

// sessMemo is one session's decisions, the tool calls whose result was folded, and those that were asked for again.
type sessMemo struct {
	mu       sync.Mutex
	entries  map[memoKey]decision
	folded   map[uint64][]memoKey
	unfolded map[uint64]bool
	bytes    int
	closed   bool          // dropped by the store: nothing more is kept
	total    *atomic.Int64 // the store's byte total
}

func newSessMemo(total *atomic.Int64) *sessMemo {
	return &sessMemo{entries: map[memoKey]decision{}, folded: map[uint64][]memoKey{}, unfolded: map[uint64]bool{}, total: total}
}

func (m *sessMemo) get(k memoKey) (decision, bool) {
	m.mu.Lock()
	d, ok := m.entries[k]
	m.mu.Unlock()
	return d, ok
}

// put stores d unless another call stored one first, and returns the decision in force (the first writer wins, so a
// race never changes what a region is sent as) with the number of entries dropped to make room.
func (m *sessMemo) put(k memoKey, d decision) (decision, int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if cur, ok := m.entries[k]; ok {
		return cur, 0
	}
	if m.closed {
		return d, 0
	}
	dropped := 0
	if len(m.entries) >= memoMaxEntries || m.bytes+d.size() > memoMaxBytes || len(m.folded) >= memoMaxFolded {
		dropped = m.clearLocked()
	}
	m.entries[k] = d
	m.bytes += d.size()
	m.total.Add(int64(d.size()))
	if d.applied && d.hasArgs {
		m.folded[d.args] = append(m.folded[d.args], k)
	}
	return d, dropped
}

func (m *sessMemo) clearLocked() int {
	n := len(m.entries)
	m.total.Add(-int64(m.bytes))
	m.entries, m.folded, m.unfolded, m.bytes = map[memoKey]decision{}, map[uint64][]memoKey{}, map[uint64]bool{}, 0
	return n
}

func (m *sessMemo) isUnfolded(args uint64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.unfolded[args]
}

// unfold marks the tool call as asked for again and turns every folded result of it into "send whole", except the
// results of the calls in newest: those are this request's own newest calls, not earlier ones (a retry of the request
// is not a repeat). It reports whether anything was turned back, which happens once per tool call.
func (m *sessMemo) unfold(args uint64, newest []string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.unfolded[args] {
		return false
	}
	var earlier []memoKey
	for _, k := range m.folded[args] {
		if !slices.Contains(newest, m.entries[k].callID) {
			earlier = append(earlier, k)
		}
	}
	if len(earlier) == 0 {
		return false
	}
	m.unfolded[args] = true
	for _, k := range earlier {
		d := m.entries[k]
		m.bytes -= d.size()
		m.total.Add(-int64(d.size()))
		d.applied, d.out, d.saved, d.stages, d.unfolded = false, "", 0, nil, true
		m.entries[k] = d
		m.bytes += d.size()
		m.total.Add(int64(d.size()))
	}
	delete(m.folded, args)
	return true
}

// memoStore holds the session memos, least recently used last, bounded by session count and total bytes.
type memoStore struct {
	mu       sync.Mutex
	maxSess  int
	maxBytes int64
	lru      *list.List
	idx      map[string]*list.Element
	total    atomic.Int64
	evicted  atomic.Int64
}

type memoSlot struct {
	key string
	m   *sessMemo
}

func newMemoStore(maxSess int, maxBytes int64) *memoStore {
	return &memoStore{maxSess: maxSess, maxBytes: maxBytes, lru: list.New(), idx: map[string]*list.Element{}}
}

// get returns the session's memo, creating it, and drops the least recently used sessions beyond the limits.
func (s *memoStore) get(key string) *sessMemo {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.idx[key]; ok {
		s.lru.MoveToFront(e)
		return e.Value.(*memoSlot).m
	}
	m := newSessMemo(&s.total)
	s.idx[key] = s.lru.PushFront(&memoSlot{key, m})
	for s.lru.Len() > 1 && (s.lru.Len() > s.maxSess || s.total.Load() > s.maxBytes) {
		last := s.lru.Back()
		s.dropLocked(last)
	}
	return m
}

func (s *memoStore) dropLocked(e *list.Element) {
	slot := e.Value.(*memoSlot)
	s.lru.Remove(e)
	delete(s.idx, slot.key)
	slot.m.mu.Lock()
	s.evicted.Add(int64(slot.m.clearLocked()))
	slot.m.closed = true
	slot.m.mu.Unlock()
}

func (s *memoStore) remove(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.idx[key]; ok {
		slot := e.Value.(*memoSlot)
		s.lru.Remove(e)
		delete(s.idx, key)
		slot.m.mu.Lock()
		slot.m.clearLocked()
		slot.m.closed = true
		slot.m.mu.Unlock()
	}
}

func (s *memoStore) sessions() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lru.Len()
}
