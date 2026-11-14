package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"time"

	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/ledger"
)

// result is what one play of the demo did.
type result struct {
	clientCalls  int      // requests the clients sent to the proxy
	upstreamHits int      // requests that reached the fake upstream (a blocked call never does)
	sessions     []string // every session id played
}

// play runs the demo for one tenant: a fake upstream, the proxy in front of it, rounds of sessions, then
// it closes the sessions and flushes the ledger hook (which it takes over: it is closed on return).
func play(ctx context.Context, tenant tollgate.Tenant, hook *ledger.Hook, rounds int, stamp string, f features) (result, error) {
	up := newUpstream()
	defer up.Close()
	p, srv, err := startProxy(ctx, up.URL, tenant, hook, f.chain)
	if err != nil {
		return result{}, err
	}
	defer srv.Close()
	c := &client{srv: srv, hook: hook, stamp: stamp, tenant: tenant.ID, mem: f.mem, closed: map[string]bool{}}
	if err := c.replay(ctx, rounds); err != nil {
		return result{}, err
	}
	for _, extra := range []func(context.Context, *client, config) error{trimRounds, routerRounds, memoryRounds} {
		if err := extra(ctx, c, f.cfg); err != nil {
			return result{}, err
		}
	}
	srv.Close()
	for _, id := range c.sessions {
		// tollgate closes a session after its quiet TTL (minutes); the demo is finished, so close them now.
		if !c.closed[id] {
			hook.OnSessionClosed(ctx, tollgate.Session{ID: id, TenantID: tenant.ID})
		}
	}
	if err := p.Close(ctx); err != nil {
		return result{}, err
	}
	if err := hook.Close(ctx); err != nil {
		return result{}, err
	}
	f.mem.finish(ctx) // the sessions closed above queue their distillations or failure records
	return result{clientCalls: c.sent, upstreamHits: int(up.hits.Load()), sessions: c.sessions}, nil
}

// client sends the demo's requests and, after each, waits until the ledger has seen the call: an agent
// that outruns the ledger would send its next request before the monitors know the last answer's usage.
type client struct {
	srv      *httptest.Server
	hook     *ledger.Hook
	stamp    string
	tenant   string          // the workspace id, for closing a session early
	mem      *memoryDemo     // nil unless -memory is on
	closed   map[string]bool // sessions already closed by the scenario that played them
	sent     int
	closes   int // session closes queued early: the ledger counts them in "enqueued" too
	sessions []string
}

func sessionID(stamp string, round int) string { return fmt.Sprintf("demo-%s-%d", stamp, round) }

// replay sends the script once per round as one session, then one looping agent of each kind in a session of
// its own, so each round shows every loop shape and four rounds show every wire and stream mode.
func (c *client) replay(ctx context.Context, rounds int) error {
	for r := range rounds {
		if err := c.script(ctx, sessionID(c.stamp, r)); err != nil {
			return err
		}
		for k, kind := range loopKinds {
			if err := c.loop(ctx, kind, r, k, fmt.Sprintf("%s-%s", sessionID(c.stamp, r), kind.name)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *client) script(ctx context.Context, session string) error {
	c.sessions = append(c.sessions, session)
	for _, name := range script {
		sc, ok := scenarioByName(name)
		if !ok {
			return fmt.Errorf("demo-traffic: unknown scenario %q", name)
		}
		if _, err := c.post(ctx, sc.path, sc.request, session, map[string]string{scenarioHeader: sc.name}); err != nil {
			return err
		}
	}
	return nil
}

// loop is one looping agent. It asks, runs the tool the reply names (the same one again, which is the bug
// being demonstrated) and asks again, until the reply is not a tool call: the upstream's text answer after
// loopTurns tool calls, or the monitors' block answer. A client that is blocked stops there.
func (c *client) loop(ctx context.Context, kind loopKind, round, idx int, session string) error {
	c.sessions = append(c.sessions, session)
	w, stream := variantOf(round, idx)
	for turn := 0; turn <= loopTurns; turn++ {
		body, err := kind.body(w, stream, turn)
		if err != nil {
			return err
		}
		hdr := map[string]string{scenarioHeader: loopScenarioName(kind.name, w, stream, turn)}
		if turn == loopTurns {
			hdr = map[string]string{scenarioHeader: w.text(stream)}
		}
		reply, err := c.post(ctx, w.path, string(body), session, hdr)
		if err != nil {
			return err
		}
		if !asksForATool(reply) {
			return nil
		}
	}
	return nil
}

// post sends one request and returns the reply body.
func (c *client) post(ctx context.Context, path, body, session string, hdr map[string]string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.srv.URL+path, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(weightHeader, strconv.Itoa(c.sent%9))
	req.Header.Set(sessionHeader, session)
	req.Header.Set("User-Agent", "demo-traffic/1")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := c.srv.Client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", hdr[scenarioHeader], err)
	}
	defer resp.Body.Close()
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	c.sent++
	c.waitLedger()
	return bytes.Clone(got), nil
}

// waitLedger returns once the ledger has seen every call sent so far (After runs just behind the
// response), or after ten seconds.
func (c *client) waitLedger() {
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(time.Millisecond) {
		if n, _ := c.hook.Status()["enqueued"].(int64); n >= int64(c.sent+c.closes) {
			return
		}
	}
}
