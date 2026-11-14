package api_test

import (
	"context"
	"testing"
	"time"

	"github.com/ethosagent/meterwise/internal/api"
	"github.com/ethosagent/meterwise/internal/contract"
)

// hitMemory is a memory port that also lists a session's hits, the optional read the session detail uses.
type hitMemory struct {
	*fakeMemory
	asked []string
}

func (h *hitMemory) SessionHits(_ context.Context, ws, session string) ([]contract.MemoryHitView, error) {
	h.asked = append(h.asked, ws+"/"+session)
	return []contract.MemoryHitView{{ID: "5", CallID: "a2", TraceID: "7", SimilarityBp: 9100, Mode: contract.MemoryModeHint, Injected: true, Hint: "Note: read the lexer first."}}, nil
}

func TestSessionDetailCarriesTheMemoryHitsOfThatSession(t *testing.T) {
	hm := &hitMemory{fakeMemory: &fakeMemory{}}
	s := newStackWith(t, func(d *api.Deps) { d.Memory = hm })
	admin := s.admin(t, "user_admin")
	_, ws := s.defaultWorkspace(t, admin.tok)
	s.seedCalls(t, callRec(ws, "a1", "sess-a", 1, time.Now().UTC().Add(-time.Hour), microp(1000)))
	d := decodeAs[contract.SessionDetail](t, s.must(t, admin.tok, "GET", "/api/sessions/sess-a?workspaceId="+ws, "", 200))
	if len(d.MemoryHits) != 1 || d.MemoryHits[0].TraceID != "7" || !d.MemoryHits[0].Injected || d.MemoryHits[0].Hint == "" {
		t.Fatalf("memory hits = %+v", d.MemoryHits)
	}
	if len(hm.asked) != 1 || hm.asked[0] != ws+"/sess-a" {
		t.Errorf("the port was asked for %v, want this workspace's session only", hm.asked)
	}
}

func TestSessionDetailHasNoMemoryHitsWithoutThePortOrWithAnOlderOne(t *testing.T) {
	for name, edit := range map[string]func(*api.Deps){
		"no port":    nil,
		"older port": func(d *api.Deps) { d.Memory = &fakeMemory{} },
	} {
		t.Run(name, func(t *testing.T) {
			s := newStackWith(t, edit)
			admin := s.admin(t, "user_admin")
			_, ws := s.defaultWorkspace(t, admin.tok)
			s.seedCalls(t, callRec(ws, "a1", "sess-a", 1, time.Now().UTC().Add(-time.Hour), microp(1000)))
			d := decodeAs[contract.SessionDetail](t, s.must(t, admin.tok, "GET", "/api/sessions/sess-a?workspaceId="+ws, "", 200))
			if len(d.MemoryHits) != 0 {
				t.Fatalf("memory hits = %+v", d.MemoryHits)
			}
		})
	}
}
