package store

import (
	"encoding/json"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/ethosagent/meterwise/internal/contract"
)

// UnreadableToolCalls is how many stored tool_calls values could not be read since Open.
func (s *Store) UnreadableToolCalls() int64 { return s.badTools.n.Load() }

// SetLogger sets the logger the store reports data problems to (the app logger). Until it is set they
// are counted but not logged.
func (s *Store) SetLogger(l *slog.Logger) { s.log.Store(l) }

func (s *Store) logger() *slog.Logger {
	if l := s.log.Load(); l != nil {
		return l
	}
	return slog.New(slog.DiscardHandler)
}

// badData counts the stored JSON values that did not decode and logs only the first.
type badData struct {
	n    atomic.Int64
	once sync.Once
}

// lenient decodes raw into a T. A value that does not decode reads as zero, is counted every time and is
// logged the first time (msg with the row's id), so a corrupt row leaves a trace and never fails a read.
func lenient[T any](s *Store, bad *badData, raw []byte, zero T, msg, idKey string, id any) T {
	out := zero
	if err := json.Unmarshal(raw, &out); err != nil {
		bad.n.Add(1)
		bad.once.Do(func() { s.logger().Warn(msg, slog.Any(idKey, id)) })
		return zero
	}
	return out
}

// toolNames decodes a calls.tool_calls value. One that is not a JSON array of names reads as no tool calls.
func (s *Store) toolNames(callID string, raw []byte) []string {
	tools := lenient(s, &s.badTools, raw, []string{}, "store: calls.tool_calls is not a JSON array of names; read as none (counted, logged once)", "call_id", callID)
	if tools == nil {
		tools = []string{}
	}
	return tools
}

// UnexpectedStateErrors is how many 55000 errors with an unrecognised message the store has seen
// (process-wide); the ledger reports it on /statusz.
func (*Store) UnexpectedStateErrors() int64 { return UnexpectedStateErrors() }

// UnreadableEvidence is how many stored monitor_events.evidence values could not be read since Open.
func (s *Store) UnreadableEvidence() int64 { return s.badEvidence.n.Load() }

// evidenceOf decodes a monitor_events.evidence value. One that does not decode reads as empty evidence (the
// event stays visible).
func (s *Store) evidenceOf(eventID int64, raw []byte) contract.MonitorEvidence {
	return lenient(s, &s.badEvidence, raw, contract.MonitorEvidence{}, "store: monitor_events.evidence does not decode; read as empty (counted, logged once)", "event_id", eventID)
}
