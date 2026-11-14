package memory

import (
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/contract"
)

// Per-session summary bounds. The summary is what the distiller is shown, so it is small by construction.
const (
	lastHintCall     = 3            // a hint is never given after this call, unless a new error started the recall (then the next call)
	firstRecallCall  = 1            // the first-call recall starts on this call
	lastErrorRecall  = lastHintCall // an error-retry recall may start up to this call
	retryRecallBelow = lastHintCall // a finished empty recall may be re-armed while the session has fewer calls than this
	maxTaskChars     = 200
	maxLastChars     = 300
	maxToolNames     = 20
	maxSummaryFiles  = 10
	maxErrorLines    = 5
	maxErrorChars    = 160
	maxToolArgs      = 64
	idleTTL          = time.Hour
	sweepEvery       = 256
)

// candidate is a recalled trace waiting for the next Before: the hint is already rendered, so Before does no work.
type candidate struct {
	traceID int64
	simBp   int
	hint    string
	at      int  // the session's call count when the recall started
	errMove bool // the recall was started by a new error (a retry moment), not by the first call
}

type recallStage int

const (
	recallNone recallStage = iota
	recallPending
	recallDone
)

// sess is memory's own summary of one session. History evicts its ring when a session closes, so the facts the
// distiller needs are kept here, bounded, and dropped when the ledger reports the close (or after an idle hour).
type sess struct {
	ws, id  string
	seen    time.Time
	scanned int // messages already read
	calls   int
	capture bool
	cfg     contract.MemoryConfig
	sum     Summary
	wrote   bool
	recall  recallStage
	cand    *candidate
	used    map[int64]struct{}
	hints   int
}

type sessions struct {
	mu     sync.Mutex
	m      map[string]*sess
	limit  int
	n      int
	evicts int64
}

func newSessions(limit int) *sessions { return &sessions{m: map[string]*sess{}, limit: limit} }

func skey(ws, id string) string { return ws + "\x00" + id }

// get returns the session's state, creating it; the caller holds s.mu.
func (s *sessions) get(ws, id string, now time.Time) *sess {
	k := skey(ws, id)
	st := s.m[k]
	if st == nil {
		if len(s.m) >= s.limit {
			s.evictLocked(now)
		}
		st = &sess{ws: ws, id: id, used: map[int64]struct{}{}}
		s.m[k] = st
	}
	st.seen = now
	if s.n++; s.n%sweepEvery == 0 {
		s.sweepLocked(now)
	}
	return st
}

// sweepLocked drops sessions idle longer than idleTTL.
func (s *sessions) sweepLocked(now time.Time) {
	for k, st := range s.m {
		if now.Sub(st.seen) > idleTTL {
			delete(s.m, k)
			s.evicts++
		}
	}
}

// evictLocked makes room: idle sessions first, else the least recently seen one.
func (s *sessions) evictLocked(now time.Time) {
	s.sweepLocked(now)
	if len(s.m) < s.limit {
		return
	}
	var oldest string
	var at time.Time
	for k, st := range s.m {
		if oldest == "" || st.seen.Before(at) {
			oldest, at = k, st.seen
		}
	}
	delete(s.m, oldest)
	s.evicts++
}

// take removes and returns the session's state (nil when there is none).
func (s *sessions) take(ws, id string) *sess {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := skey(ws, id)
	st := s.m[k]
	delete(s.m, k)
	return st
}

func (s *sessions) size() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.m)
}

// dropWorkspace forgets every recalled candidate of the workspace (Forget must stop their hints at once).
func (s *sessions) dropWorkspace(ws string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, st := range s.m {
		if st.ws == ws {
			st.cand = nil
		}
	}
}

// next hands Before the session's recalled candidate when the session may still take a hint, and marks it used.
// It reads and updates a few fields under the lock: no I/O.
func (s *sessions) next(ws, id string, maxHints, calls int) *candidate {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.m[skey(ws, id)]
	if st == nil || st.cand == nil || st.hints >= maxHints {
		return nil
	}
	c := st.cand
	st.cand = nil
	if calls > lastHintCall && (!c.errMove || calls > c.at+1) {
		return nil // a slow recall: the moment has passed (plan 09 3.2), the hint is dropped, never late
	}
	st.hints++
	st.used[c.traceID] = struct{}{}
	return c
}

// observe folds the messages the call added into the session summary and says whether a recall should start now:
// after the first call, or at an error-retry moment (a new error seen on call 2 or 3) while none has run.
func (s *sessions) observe(c *tollgate.Call, cfg contract.MemoryConfig, now time.Time) (startRecall bool, snap recallInput) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.get(c.Tenant.ID, c.Session.ID, now)
	st.cfg, st.capture = cfg, true
	st.calls = max(st.calls+1, c.Session.Calls)
	errs := len(st.sum.Errors)
	st.read(c.Messages)
	newError := len(st.sum.Errors) > errs
	if st.recall == recallNone && (st.calls <= firstRecallCall || (st.calls <= lastErrorRecall && newError)) {
		st.recall = recallPending
		return true, recallInput{ws: st.ws, id: st.id, cfg: cfg, text: embedText(st.sum), used: usedOf(st), at: st.calls, errMove: st.calls > firstRecallCall && newError}
	}
	return false, recallInput{}
}

func usedOf(st *sess) map[int64]struct{} {
	return maps.Clone(st.used)
}

// setCandidate stores the recall's result and ends the pending stage.
func (s *sessions) setCandidate(ws, id string, c *candidate) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.m[skey(ws, id)]
	if st == nil {
		return
	}
	st.recall = recallDone
	if c != nil {
		if _, used := st.used[c.traceID]; !used {
			st.cand = c
		}
	}
}

// retryRecall lets a later retry moment run a recall again after one that found nothing (never while one is pending).
func (s *sessions) retryRecall(ws, id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if st := s.m[skey(ws, id)]; st != nil && st.recall == recallDone && st.cand == nil && st.calls < retryRecallBelow {
		st.recall = recallNone
	}
}

// read folds new messages into the summary: the first user message, tool names, files, errors from tool results.
func (st *sess) read(msgs []tollgate.Message) {
	if len(msgs) < st.scanned {
		st.scanned = 0
	}
	for _, m := range msgs[st.scanned:] {
		switch m.Role {
		case tollgate.RoleUser:
			if st.sum.Task == "" {
				st.sum.Task = clampText(m.Text, maxTaskChars)
			}
		case tollgate.RoleAssistant:
			if t := strings.TrimSpace(m.Text); t != "" {
				st.sum.LastText = clampText(t, maxLastChars)
			}
			for _, tc := range m.ToolCalls {
				st.tool(tc)
			}
		case tollgate.RoleTool:
			st.errorOf(m.Text)
		}
	}
	st.scanned = len(msgs)
}

func (st *sess) tool(tc tollgate.ToolCall) {
	name := contract.TruncateUTF8(strings.TrimSpace(tc.Name), maxToolArgs)
	if name == "" {
		return
	}
	if !slices.Contains(st.sum.Tools, name) && len(st.sum.Tools) < maxToolNames {
		st.sum.Tools = append(st.sum.Tools, name)
	}
	if isWriteTool(name) {
		st.wrote = true
	}
	if p := contract.ToolPath([]byte(tc.Arguments)); p != "" && len(st.sum.Files) < maxSummaryFiles {
		if p = clampText(p, 200); p != "" && !slices.Contains(st.sum.Files, p) {
			st.sum.Files = append(st.sum.Files, p)
		}
	}
}

// errorWords mark a tool result line as an error (lower case).
var errorWords = []string{"error", "failed", "exception", "panic:", "traceback", "cannot ", "undefined:", "not found"}

func (st *sess) errorOf(text string) {
	if len(st.sum.Errors) >= maxErrorLines {
		return
	}
	if len(text) > 4096 {
		text = text[:4096]
	}
	for _, line := range strings.Split(text, "\n") {
		l := strings.ToLower(line)
		for _, w := range errorWords {
			if strings.Contains(l, w) {
				if e := clampText(line, maxErrorChars); e != "" && !slices.Contains(st.sum.Errors, e) {
					st.sum.Errors = append(st.sum.Errors, e)
				}
				return
			}
		}
	}
}

// isWriteTool reports whether a tool name changes files or runs commands, by the vocabulary monitors and history use
// (contract.ClassifyTool): shell and git tools count, a name like replacement_info does not.
func isWriteTool(name string) bool { return contract.ClassifyTool(name) == contract.ToolWrite }

// embedText is what both capture and recall embed, so a task and its remembered solution live in the same space: the
// first user message (at most 200 characters) and the error lines.
func embedText(s Summary) string {
	if len(s.Errors) == 0 {
		return s.Task
	}
	return s.Task + "\n" + strings.Join(s.Errors, "\n")
}
