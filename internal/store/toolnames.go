package store

import (
	"encoding/json"
	"log/slog"
)

// UnreadableToolCalls is how many stored tool_calls values could not be read since Open.
func (s *Store) UnreadableToolCalls() int64 { return s.badTools.Load() }

// SetLogger sets the logger the store reports data problems to (the app logger). Until it is set they
// are counted but not logged.
func (s *Store) SetLogger(l *slog.Logger) { s.log.Store(l) }

func (s *Store) logger() *slog.Logger {
	if l := s.log.Load(); l != nil {
		return l
	}
	return slog.New(slog.DiscardHandler)
}

// toolNames decodes a calls.tool_calls value. One that is not a JSON array of names reads as no tool
// calls, is counted every time and is logged the first time, so a corrupt row leaves a trace.
func (s *Store) toolNames(callID string, raw []byte) []string {
	tools := []string{}
	if err := json.Unmarshal(raw, &tools); err != nil {
		s.badTools.Add(1)
		s.badToolsOnce.Do(func() {
			s.logger().Warn("store: calls.tool_calls is not a JSON array of names; read as none (counted, logged once)",
				slog.String("call_id", callID))
		})
		return []string{}
	}
	if tools == nil {
		tools = []string{}
	}
	return tools
}

// UnexpectedStateErrors is how many 55000 errors with an unrecognised message the store has seen
// (process-wide); the ledger reports it on /statusz.
func (*Store) UnexpectedStateErrors() int64 { return UnexpectedStateErrors() }
