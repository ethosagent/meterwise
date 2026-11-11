package store_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/store"
)

// seedTrim writes one workspace's trim traffic in 03-09: an acted certain call (two stages), an acted likely call on
// the Anthropic wire (not certain), an observe call, an untrimmed call, and nothing else.
func seedTrim(t *testing.T, s *store.Store, ws string) {
	t.Helper()
	at := func(i int) time.Time { return day(9).Add(time.Duration(i) * time.Hour) }
	plain := rec(ws, "t4", "s9", 4, at(4), "ok", true, mu(1_000_000))
	batch := []contract.CallRecord{
		trimRec(rec(ws, "t1", "s9", 1, at(1), "ok", false, mu(3_000_000)), contract.TrimActed, contract.ConfidenceCertain, 1000, 2_000_000, contract.StageLogWindow, contract.StageDupLines),
		trimRec(rec(ws, "t2", "s9", 2, at(2), "ok", false, mu(3_000_000)), contract.TrimActed, contract.ConfidenceLikely, 400, 400_000, contract.StageJSONTable),
		trimRec(rec(ws, "t3", "s9", 3, at(3), "ok", false, mu(3_000_000)), contract.TrimObserve, contract.ConfidenceLikely, 300, 300_000, contract.StageLogWindow),
		plain,
	}
	batch[1].Wire = "anthropic.messages"
	if _, err := s.WriteCalls(context.Background(), batch); err != nil {
		t.Fatal(err)
	}
}

func TestTrimStatsReconcileWithTheLedgerAndTheOverview(t *testing.T) {
	s, ctx := openMigrated(t), context.Background()
	ws, other := newWS(t, s, "A", "AAAAAAAAAAAAAAAAAAAAAA"), newWS(t, s, "B", "BBBBBBBBBBBBBBBBBBBBBB")
	for _, w := range []string{ws, other} { // another workspace's identical rows must never be counted
		seedTrim(t, s, w)
	}
	rg := rangeOf(8, 10)
	st, err := store.NewTrimReader(s).Stats(ctx, ws, rg)
	if err != nil {
		t.Fatal(err)
	}
	if st.Calls != 4 || st.TrimmedCalls != 2 {
		t.Errorf("calls = %d trimmed = %d, want 4 and 2", st.Calls, st.TrimmedCalls)
	}
	// Three classes, never summed: certain 2.0, likely 0.4, would-save 0.3.
	if st.SavedUsd != 2_000_000 || st.LikelySavedUsd != 400_000 || st.WouldSaveUsd != 300_000 {
		t.Errorf("money = %d / %d / %d", st.SavedUsd, st.LikelySavedUsd, st.WouldSaveUsd)
	}
	if st.TrimTokens != 1400 || st.WouldTrimTokens != 300 {
		t.Errorf("tokens = %d / %d, want 1400 / 300", st.TrimTokens, st.WouldTrimTokens)
	}
	if len(st.ByStage) != 5 {
		t.Fatalf("stages = %+v", st.ByStage)
	}
	for i, want := range contract.TrimStages() {
		if st.ByStage[i].Stage != want {
			t.Errorf("stage %d = %s, want %s", i, st.ByStage[i].Stage, want)
		}
	}
	var stageTokens int64 // acted stage rows only: the stage table adds up to the acted call tokens
	for _, r := range st.ByStage {
		stageTokens += r.Tokens
	}
	if stageTokens != st.TrimTokens {
		t.Errorf("stage tokens %d != call tokens %d", stageTokens, st.TrimTokens)
	}
	var toolTokens int64
	for _, r := range st.ByTool {
		toolTokens += r.Tokens
	}
	if toolTokens != st.TrimTokens || len(st.ByTool) != 1 || st.ByTool[0].Tool != "bash" {
		t.Errorf("tools = %+v", st.ByTool)
	}
	// The Overview's trim row and its hero count the same calls: its trim cause equals these numbers.
	o, err := s.Overview(ctx, ws, rg)
	if err != nil {
		t.Fatal(err)
	}
	trim := o.ByCause[6]
	if trim.SavedUsd != st.SavedUsd || trim.LikelySavedUsd != st.LikelySavedUsd || trim.WouldSaveUsd != st.WouldSaveUsd {
		t.Errorf("overview trim row %+v disagrees with stats %+v", trim, st)
	}
	if o.Hero.SavedUsd != st.SavedUsd || o.Hero.LikelySavedUsd != st.LikelySavedUsd || o.Hero.WouldSaveUsd != st.WouldSaveUsd {
		t.Errorf("hero %+v disagrees with stats", o.Hero)
	}
	sess, _, _, err := s.Session(ctx, ws, "s9")
	if err != nil {
		t.Fatal(err)
	}
	if sess.Saved != st.SavedUsd || sess.LikelySaved != st.LikelySavedUsd {
		t.Errorf("session %d / %d disagrees with stats", sess.Saved, sess.LikelySaved)
	}
}

func TestTrimStatsOutsideTheRangeAndBadWorkspace(t *testing.T) {
	s, ctx := openMigrated(t), context.Background()
	ws := newWS(t, s, "A", "AAAAAAAAAAAAAAAAAAAAAA")
	seedTrim(t, s, ws)
	st, err := store.NewTrimReader(s).Stats(ctx, ws, rangeOf(1, 3))
	if err != nil || st.Calls != 0 || st.SavedUsd != 0 || len(st.ByStage) != 5 || st.ByTool == nil {
		t.Errorf("empty range = %+v, %v", st, err)
	}
	if _, err := store.NewTrimReader(s).Stats(ctx, "not-a-uuid", rangeOf(8, 10)); code(err) != contract.CodeValidationFailed {
		t.Errorf("bad workspace id: %v", err)
	}
}

func TestTrimPreviewShowsSizesAndMarkersOnly(t *testing.T) {
	s, ctx := openMigrated(t), context.Background()
	ws, other := newWS(t, s, "A", "AAAAAAAAAAAAAAAAAAAAAA"), newWS(t, s, "B", "BBBBBBBBBBBBBBBBBBBBBB")
	seedTrim(t, s, ws)
	tr := store.NewTrimReader(s)
	p, err := tr.Preview(ctx, ws, "t1")
	if err != nil {
		t.Fatal(err)
	}
	if p.CallID != "t1" || p.Action == nil || *p.Action != contract.TrimActed || len(p.Stages) != 2 {
		t.Fatalf("preview = %+v", p)
	}
	if p.BodyBefore != 1800 || p.BodyAfter != 600 {
		t.Errorf("sizes = %d -> %d, want 1800 -> 600", p.BodyBefore, p.BodyAfter)
	}
	for _, v := range p.Stages {
		if v.Marker == "" || v.Tool == nil || *v.Tool != "bash" || v.BytesAfter >= v.BytesBefore || v.EstTokens == 0 {
			t.Errorf("stage = %+v", v)
		}
		if strings.ContainsAny(v.Marker, "\n") || len(v.Marker) > 60 { // a fixed template, never request text
			t.Errorf("marker %q is not a short template", v.Marker)
		}
	}
	none, err := tr.Preview(ctx, ws, "t4")
	if err != nil || none.Action != nil || len(none.Stages) != 0 || none.BodyBefore != 0 {
		t.Errorf("untrimmed call = %+v, %v", none, err)
	}
	if _, err := tr.Preview(ctx, other, "t1"); code(err) != contract.CodeNotFound { // another workspace's call
		t.Errorf("foreign call: %v", err)
	}
	if _, err := tr.Preview(ctx, ws, "missing"); code(err) != contract.CodeNotFound {
		t.Errorf("unknown call: %v", err)
	}
}
