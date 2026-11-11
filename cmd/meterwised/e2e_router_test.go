package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/dataplane"
)

// Plan 10 end to end: the real serve wiring (router tables loaded from the seeded database and priced from the
// ledger's own snapshot, router hook in the chain, ledger, control plane) in front of a loopback upstream that
// answers every chat call with 1000 input and 100 output tokens, echoes nothing it was not sent, and refuses
// gpt-4o-mini with a 404 (the target a workspace's key cannot use).

type routerUpstream struct {
	*httptest.Server
	mu     sync.Mutex
	models []string
}

func newRouterUpstream(t *testing.T) *routerUpstream {
	u := &routerUpstream{}
	u.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var doc struct {
			Model string `json:"model"`
		}
		_ = json.Unmarshal(b, &doc)
		u.mu.Lock()
		u.models = append(u.models, doc.Model)
		u.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if doc.Model == "gpt-4o-mini" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"message":"The model does not exist or you do not have access to it.","type":"invalid_request_error"}}`))
			return
		}
		fmt.Fprintf(w, `{"id":"x","object":"chat.completion","model":%q,"choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1000,"completion_tokens":100,"total_tokens":1100}}`, doc.Model)
	}))
	t.Cleanup(u.Close)
	return u
}

func (u *routerUpstream) seen() []string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]string(nil), u.models...)
}

// readerBody is call n of an agent that read a different file in each earlier turn (tool-free request, tool-using history).
func readerBody(model string, n int) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, `{"model":%q,"stream":false,"messages":[{"role":"system","content":"You are a test agent."},{"role":"user","content":"fix it"}`, model)
	for i := 1; i < n; i++ {
		fmt.Fprintf(&sb, `,{"role":"assistant","content":null,"tool_calls":[{"id":"c%d","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"f%d.go\"}"}}]},{"role":"tool","tool_call_id":"c%d","content":"package f%d"}`, i, i, i, i)
	}
	sb.WriteString(`]}`)
	return sb.String()
}

type routerRig struct {
	t  *testing.T
	p  *plane
	up *routerUpstream
}

func bootRouter(t *testing.T) *routerRig {
	t.Helper()
	up := newRouterUpstream(t)
	base, _ := url.Parse(up.URL)
	return &routerRig{t: t, p: bootPlane(t, dataplane.Deps{Resolver: bothWires{base}}, nil), up: up}
}

func (r *routerRig) api(method, path, body string) (int, []byte) {
	r.t.Helper()
	req, _ := http.NewRequestWithContext(context.Background(), method, r.p.api+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		r.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

func (r *routerRig) patch(body string) {
	r.t.Helper()
	if code, b := r.api(http.MethodPatch, "/api/workspaces/"+r.p.ws.ID+"/router", body); code != 200 {
		r.t.Fatalf("PATCH router %s: %d %s", body, code, b)
	}
}

// run sends calls 1..n of one session and waits for the ledger after each (a client that outruns After gets a
// load-dependent ring); it returns the client's status codes.
func (r *routerRig) run(session, model string, n int) []int {
	r.t.Helper()
	var codes []int
	for i := 1; i <= n; i++ {
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, r.p.proxy+"/w/"+r.p.ws.PublicID+"/openai/v1/chat/completions", strings.NewReader(readerBody(model, i)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer sk-caller-own-key")
		req.Header.Set("x-meterwise-session", session)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			r.t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		codes = append(codes, resp.StatusCode)
		r.waitDetail(session, i)
	}
	return codes
}

func (r *routerRig) waitDetail(session string, n int) contract.SessionDetail {
	r.t.Helper()
	var d contract.SessionDetail
	for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		code, b := r.api(http.MethodGet, "/api/sessions/"+url.PathEscape(session)+"?workspaceId="+r.p.ws.ID, "")
		if code == 200 && json.Unmarshal(b, &d) == nil && len(d.Calls) >= n {
			return d
		}
	}
	r.t.Fatalf("session %s: %d of %d calls recorded", session, len(d.Calls), n)
	return d
}

func (r *routerRig) get(path string, into any) []byte {
	r.t.Helper()
	code, b := r.api(http.MethodGet, "/api/workspaces/"+r.p.ws.ID+path, "")
	if code != 200 {
		r.t.Fatalf("GET %s: %d %s", path, code, b)
	}
	if err := json.Unmarshal(b, into); err != nil {
		r.t.Fatal(err)
	}
	return b
}

// A routed call's saving is the price delta at its real usage: gpt-4.1 at 1000 in / 100 out costs 2800 micro-USD,
// gpt-4.1-mini 560, so each routed call saves 2240. It is certain (never likely), it lands on the call, the session
// and the Overview headline and the router cause row, and all of them agree with the router stats.
func TestRoutedCallsSaveThePriceDeltaAndReconcileAcrossTheReads(t *testing.T) {
	r := bootRouter(t)
	r.patch(`{"mode":"conservative"}`)
	if codes := r.run("route-a", "gpt-4.1-2025-04-14", 4); fmt.Sprint(codes) != "[200 200 200 200]" {
		t.Fatalf("statuses %v", codes)
	}
	if got, want := fmt.Sprint(r.up.seen()), "[gpt-4.1-2025-04-14 gpt-4.1-2025-04-14 gpt-4.1-mini gpt-4.1-mini]"; got != want {
		t.Fatalf("the upstream saw %s, want %s (the target is sent exactly as mapped)", got, want)
	}
	d := r.waitDetail("route-a", 4)
	const delta = contract.MicroUSD(2240)
	for i, c := range d.Calls {
		routed := i >= 2
		if routed != (c.RouterAction != nil && *c.RouterAction == string(contract.RouteRouted)) {
			t.Fatalf("call %d router action = %v", i+1, c.RouterAction)
		}
		if routed && (c.SavedUsdRouter != delta || c.RoutedFrom == nil || *c.RoutedFrom != "gpt-4.1-2025-04-14" || c.RoutedTo == nil || *c.RoutedTo != "gpt-4.1-mini" || c.RouterScoreBp == nil || *c.RouterScoreBp != 2500) {
			t.Errorf("call %d: %+v", i+1, c)
		}
		if !routed && (c.SavedUsdRouter != 0 || c.RouterAction != nil) {
			t.Errorf("held call %d recorded a route: %+v", i+1, c)
		}
	}
	if d.Session.SavedUsd != 2*delta || d.Session.LikelySavedUsd != 0 {
		t.Errorf("session saved %v likely %v, want %v and 0", d.Session.SavedUsd, d.Session.LikelySavedUsd, 2*delta)
	}
	var ov contract.SavingsOverview
	raw := r.get("/savings/overview?range=7d", &ov)
	if ov.Hero.SavedUsd != 2*delta || ov.Hero.LikelySavedUsd != 0 {
		t.Errorf("hero saved %v likely %v: %s", ov.Hero.SavedUsd, ov.Hero.LikelySavedUsd, raw)
	}
	var routerCause *contract.CauseView
	for i := range ov.ByCause {
		if ov.ByCause[i].Cause == contract.CauseRouter {
			routerCause = &ov.ByCause[i]
		}
	}
	if routerCause == nil || routerCause.SavedUsd != 2*delta || routerCause.LikelySavedUsd != 0 || routerCause.Fires != 2 {
		t.Errorf("router cause row = %+v", routerCause)
	}
	var st contract.RouterStats
	r.get("/router/stats?range=7d", &st)
	if st.Mode != contract.RouterModeConservative || st.RoutedCalls != 2 || st.Calls != 4 || st.SavedUsd != 2*delta || st.Unavailable != 0 ||
		len(st.ByPair) != 1 || st.ByPair[0].From != "gpt-4.1" || st.ByPair[0].To != "gpt-4.1-mini" || st.ByPair[0].Calls != 2 {
		t.Errorf("router stats = %+v", st)
	}
}

// A pair whose target has no price is never routed, even when the workspace's own map names it.
func TestAnUnpricedTargetIsNeverRouted(t *testing.T) {
	r := bootRouter(t)
	r.patch(`{"mode":"conservative","map":[{"provider":"openai","from":"gpt-4.1","to":"gpt-0-unpriced"}]}`)
	r.run("unpriced", "gpt-4.1", 4)
	if got := fmt.Sprint(r.up.seen()); got != "[gpt-4.1 gpt-4.1 gpt-4.1 gpt-4.1]" {
		t.Errorf("the upstream saw %s", got)
	}
	var st contract.RouterStats
	r.get("/router/stats?range=7d", &st)
	if st.RoutedCalls != 0 || st.SavedUsd != 0 {
		t.Errorf("stats = %+v", st)
	}
}

// Observe records what routing would have saved and moves neither savings number.
func TestObserveRecordsWouldSaveAndNothingElse(t *testing.T) {
	r := bootRouter(t)
	r.patch(`{"mode":"observe"}`)
	r.run("observe", "gpt-4.1", 4)
	if got := fmt.Sprint(r.up.seen()); got != "[gpt-4.1 gpt-4.1 gpt-4.1 gpt-4.1]" {
		t.Errorf("observe rewrote the model: %s", got)
	}
	var st contract.RouterStats
	r.get("/router/stats?range=7d", &st)
	if st.ObservedCalls != 2 || st.WouldSaveUsd != 2*2240 || st.SavedUsd != 0 || st.RoutedCalls != 0 {
		t.Errorf("stats = %+v", st)
	}
	var ov contract.SavingsOverview
	r.get("/savings/overview?range=7d", &ov)
	if ov.Hero.SavedUsd != 0 || ov.Hero.LikelySavedUsd != 0 || ov.Hero.WouldSaveUsd != 2*2240 {
		t.Errorf("hero = %+v", ov.Hero)
	}
}

// The upstream refuses the routed target (404): the client sees that one error, the pair is marked unavailable, the
// next call goes to the model it asked for, and the stats count the refusal (ruling A1).
func TestARefusedTargetIsCountedAndTheNextCallStaysOnTheStrongModel(t *testing.T) {
	r := bootRouter(t)
	r.patch(`{"mode":"conservative"}`)
	codes := r.run("refused", "gpt-4o", 5)
	if fmt.Sprint(codes) != "[200 200 404 200 200]" {
		t.Fatalf("client statuses %v", codes)
	}
	if got := fmt.Sprint(r.up.seen()); got != "[gpt-4o gpt-4o gpt-4o-mini gpt-4o gpt-4o]" {
		t.Errorf("the upstream saw %s", got)
	}
	var st contract.RouterStats
	r.get("/router/stats?range=7d", &st)
	if st.Unavailable != 1 || st.RoutedCalls != 1 || st.SavedUsd != 0 {
		t.Errorf("stats = %+v: the refused call is routed, unavailable, and saved nothing", st)
	}
	marked := int64(-1)
	for _, src := range r.p.rt.sources {
		if v, ok := src.Status()["unavailable_marked"].(int64); ok {
			marked = v
		}
	}
	if marked != 1 {
		t.Errorf("the router's /statusz counters report %d pairs marked unavailable, want 1", marked)
	}
}
