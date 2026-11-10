package main

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/ledger"
	"github.com/ethosagent/meterwise/internal/savings"
	"github.com/ethosagent/meterwise/internal/store"
	"github.com/ethosagent/meterwise/internal/wsauth"
)

// memWriter is the ledger's store, in memory: the whole demo pipeline (history, monitors, ledger) runs
// against it with no database.
type memWriter struct {
	mu    sync.Mutex
	calls []contract.CallRecord
}

func (w *memWriter) WriteCalls(_ context.Context, recs []contract.CallRecord) (store.WriteResult, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.calls = append(w.calls, recs...)
	return store.WriteResult{Inserted: len(recs)}, nil
}
func (*memWriter) CloseSessions(context.Context, []contract.SessionClose) (int, error) { return 0, nil }
func (*memWriter) UnreadableToolCalls() int64                                          { return 0 }
func (*memWriter) UnreadableEvidence() int64                                           { return 0 }
func (*memWriter) UnexpectedStateErrors() int64                                        { return 0 }

func (w *memWriter) records() []contract.CallRecord {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]contract.CallRecord(nil), w.calls...)
}

// testWS is the one workspace of the offline runs; its org is the organization the ledger accepts.
const testWS = "11111111-1111-4111-8111-111111111111"

var testOrg = store.OrgID()

var epoch = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

// playOffline runs rounds of the demo in the given monitor mode and returns what the ledger would write.
func playOffline(t *testing.T, mode contract.MonitorMode, rounds int) (result, []contract.CallRecord) {
	t.Helper()
	cfg := modeConfig(mode)
	tenant := wsauth.Resolve(contract.Workspace{ID: testWS, OrgID: testOrg, Settings: map[string]any{"monitors": cfg}}).(*wsauth.Resolved).Tenant
	w := &memWriter{}
	pricer := savings.NewTablePricer([]savings.PriceRow{
		{Provider: "openai", Model: "gpt-4.1-mini", InputPerM: 400_000, OutputPerM: 1_600_000, EffectiveFrom: epoch},
		{Provider: "anthropic", Model: "claude-sonnet-4-5", InputPerM: 3_000_000, OutputPerM: 15_000_000, EffectiveFrom: epoch},
	})
	privacy := true
	hook, err := ledger.New(context.Background(), ledger.Options{Writer: w, Pricer: pricer, Estimator: savings.Estimate, PrivacyMode: &privacy, FlushInterval: 10 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	res, err := play(context.Background(), tenant, hook, rounds, "t")
	if err != nil {
		t.Fatal(err)
	}
	return res, w.records()
}

func countVerdicts(recs []contract.CallRecord) map[contract.MonitorName]map[contract.MonitorMode]int {
	out := map[contract.MonitorName]map[contract.MonitorMode]int{}
	for _, r := range recs {
		for _, v := range r.Verdicts {
			if out[v.Monitor] == nil {
				out[v.Monitor] = map[contract.MonitorMode]int{}
			}
			out[v.Monitor][v.Action]++
		}
	}
	return out
}

func TestEveryLoopKindFiresItsMonitorOnBothWiresStreamedOrNot(t *testing.T) {
	// 4 rounds: kind k of round r uses variant (r+k)%4, so each kind meets each wire and stream mode once.
	_, recs := playOffline(t, contract.ModeNote, 4)
	got := countVerdicts(recs)
	for _, m := range []contract.MonitorName{contract.MonitorDuplicateToolCall, contract.MonitorRepeatedRead, contract.MonitorNoProgress} {
		if n := got[m][contract.ModeNote]; n < 4 {
			t.Errorf("%s: %d note events over 4 variants, want at least 4 (all: %v)", m, n, got)
		}
	}
	for _, r := range recs {
		if r.Blocked {
			t.Fatalf("note mode blocked call %s", r.ID)
		}
	}
}

func TestBlockModeStopsTheLoopsSavesMoneyAndCallsTheUpstreamLess(t *testing.T) {
	obs, _ := playOffline(t, contract.ModeObserve, 4)
	blk, recs := playOffline(t, contract.ModeBlock, 4)
	if blk.upstreamHits >= obs.upstreamHits || blk.upstreamHits >= blk.clientCalls {
		t.Fatalf("block: upstream saw %d of %d client calls, observe: %d of %d", blk.upstreamHits, blk.clientCalls, obs.upstreamHits, obs.clientCalls)
	}
	if obs.upstreamHits != obs.clientCalls {
		t.Fatalf("observe must reach the upstream every time: %d of %d", obs.upstreamHits, obs.clientCalls)
	}
	var blocked int
	var certain contract.MicroUSD
	for _, r := range recs {
		if r.Blocked {
			blocked++
		}
		for _, v := range r.Verdicts {
			if v.Action == contract.ModeBlock && v.Confidence == contract.ConfidenceCertain {
				certain += v.EstUSD
			}
		}
	}
	if blocked == 0 || certain <= 0 {
		t.Fatalf("block mode: %d blocked calls, certain saved %v", blocked, certain)
	}
	if blk.clientCalls >= obs.clientCalls {
		t.Fatalf("a client that is blocked must stop: %d calls in block mode vs %d in observe", blk.clientCalls, obs.clientCalls)
	}
}

func TestObserveRecordsEventsButNeverTouchesACallOrASavingsNumber(t *testing.T) {
	_, recs := playOffline(t, contract.ModeObserve, 4)
	var events int
	for _, r := range recs {
		if r.Blocked || r.NotesInjected != 0 {
			t.Fatalf("observe touched call %s", r.ID)
		}
		for _, v := range r.Verdicts {
			events++
			if v.Action != contract.ModeObserve {
				t.Fatalf("observe verdict %+v", v)
			}
		}
	}
	if events == 0 {
		t.Fatal("observe recorded no monitor event")
	}
}

func TestEverySessionEndsInATextAnswer(t *testing.T) {
	for _, mode := range []contract.MonitorMode{contract.ModeObserve, contract.ModeNote, contract.ModeBlock} {
		res, recs := playOffline(t, mode, 4)
		last := map[string]contract.CallRecord{}
		for _, r := range recs {
			if prev, ok := last[r.SessionID]; !ok || r.CallIndex > prev.CallIndex {
				last[r.SessionID] = r
			}
		}
		if len(last) != len(res.sessions) {
			t.Fatalf("%s: %d sessions in the ledger, %d played", mode, len(last), len(res.sessions))
		}
		for id, r := range last {
			if r.Status != 200 || len(r.ToolCalls) > 0 {
				t.Errorf("%s: session %s ends on status %d, tool calls %v: want a text answer", mode, id, r.Status, r.ToolCalls)
			}
		}
	}
}

func TestTheMonitorsPatchAppliedToTheDefaultsIsTheModeConfigTheOfflineRunsUse(t *testing.T) {
	for _, mode := range []contract.MonitorMode{contract.ModeObserve, contract.ModeNote, contract.ModeBlock} {
		got := modePatch(mode).Apply(contract.DefaultMonitorConfig())
		if got != modeConfig(mode) {
			t.Fatalf("%s: patch gives %+v, modeConfig %+v", mode, got, modeConfig(mode))
		}
		if err := got.Validate(); err != nil {
			t.Fatalf("%s: %v", mode, err)
		}
	}
	if m, err := parseMonitors(""); err != nil || m != contract.ModeNote {
		t.Fatalf("default = %q (%v), want note", m, err)
	}
	if _, err := parseMonitors("loud"); err == nil {
		t.Fatal("loud accepted")
	}
}
