package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/dataplane"
	"github.com/ethosagent/meterwise/internal/ledger"
	"github.com/ethosagent/meterwise/internal/memory"
	"github.com/ethosagent/meterwise/internal/store"
)

// demoMemoryKey is the key the demo's model twin expects. It is made up: no real model key exists in a demo run.
const demoMemoryKey = "demo-memory-key"

// memoryTask is what both memory sessions are asked to do.
const memoryTask = "Fix the failing parser test"

// memoryDemo is the memory feature of a demo run: the real engine over the real store, talking to a loopback model
// twin (never a model) and wired to the ledger the way serve wires it. It is nil when -memory is off.
type memoryDemo struct {
	eng  *memory.Engine
	twin *modelTwin
	st   *store.Store
	ws   string
	once sync.Once
}

// memoryChain builds the memory hook of the demo's chain when -memory is on (observe or hint) and tells the ledger
// which sessions closed, so a finished session is distilled into a trace. memoryTenant opens the capture rule for
// the demo's tenant. It returns nil, and leaves the slot empty, when memory is off.
func memoryChain(ctx context.Context, st *store.Store, led *ledger.Hook, pricer contract.Pricer, ws string, cfg config, chain *dataplane.ChainOptions, out io.Writer) *memoryDemo {
	if cfg.memory == "" || cfg.memory == "off" {
		return nil
	}
	twin := newModelTwin(demoMemoryKey)
	privacyOff := false
	eng := memory.New(ctx, memory.Options{
		Store: st, Ledger: ledger.NewMemoryPort(st),
		Distiller: memory.NewDistiller(twin.URL, demoMemoryKey), Embedder: memory.NewEmbedder(twin.URL, demoMemoryKey),
		Model: "gpt-4o-mini", EmbedModel: "text-embedding-3-small", SelfBudget: contract.MicroUSD(1_000_000), RetentionDays: 90,
		PrivacyMode: &privacyOff,
		Price: func(model string, in, out int64) (contract.MicroUSD, bool) {
			return pricer.Cost("openai", model, contract.Usage{Input: in, Output: out}, time.Now())
		},
	})
	eng.Start()
	led.SetOnClosed(func(s ledger.ClosedSession) { eng.OnClosed(ctx, s.WorkspaceID, s.SessionID, s.Outcome, s.Calls) })
	chain.Memory = eng.Hook()
	fmt.Fprintf(out, "memory %s: model twin at %s (loopback; no model key involved), capture opened for this run's tenant\n", cfg.memory, twin.URL)
	return &memoryDemo{eng: eng, twin: twin, st: st, ws: ws}
}

// memoryTenant opens the capture rule for the demo's tenant (workspace capture on, privacy mode off): memory reads and
// writes only where capture is allowed (ruling A4), and a fresh demo workspace is private.
func memoryTenant(t tollgate.Tenant, m *memoryDemo) tollgate.Tenant {
	if m == nil {
		return t
	}
	p := tollgate.Policy{}
	if t.Policy != nil {
		p = *t.Policy
	}
	off := false
	p.Capture, p.PrivacyMode = true, &off
	t.Policy = &p
	return t
}

// finish lets the jobs the last sessions queued run (their distillations and failure records), then stops the engine
// and the twin. It is safe to call twice.
func (m *memoryDemo) finish(ctx context.Context) {
	if m == nil {
		return
	}
	m.once.Do(func() {
		_ = m.waitIdle(ctx)
		m.eng.Close()
		m.twin.Close()
	})
}

func (m *memoryDemo) stat(key string) int64 {
	n, _ := m.eng.Source().Status()[key].(int64)
	return n
}

// poll runs ok every few milliseconds until it is true, the context ends or ten seconds pass.
func poll(ctx context.Context, what string, ok func() (bool, error)) error {
	deadline := time.Now().Add(10 * time.Second)
	for {
		done, err := ok()
		if err != nil {
			return err
		}
		if done {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("memory scenario: timed out waiting for %s", what)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func (m *memoryDemo) waitIdle(ctx context.Context) error {
	return poll(ctx, "the memory jobs to finish", func() (bool, error) { return m.stat("jobs_active") == 0, nil })
}

func (m *memoryDemo) waitRecalls(ctx context.Context, n int64) error {
	return poll(ctx, "a recall to start", func() (bool, error) { return m.stat("recalls") >= n, nil })
}

func (m *memoryDemo) waitTraces(ctx context.Context, n int) error {
	return poll(ctx, "the trace to be stored", func() (bool, error) {
		l, err := m.st.ListTraces(ctx, m.ws, "", 50)
		return len(l.Items) >= n, err
	})
}

// memorySteps is the agent run both memory sessions play: the first test run fails with a compile error, the agent
// reads the file, edits it and the second run passes. The error shows on the second request, which is the moment a
// recall can name it (ruling R1), and the edit makes the finished session worth distilling.
var memorySteps = []step{
	{"call_1", "run_tests", `{"cmd":"go test ./parser"}`, "--- FAIL: TestParse\nparser/lex.go:41: undefined: lexer.Next\nFAIL\tparser"},
	{"call_2", "read_file", `{"path":"parser/lex.go"}`, "package parser\n\nfunc (l *Lexer) next() Token { return l.toks[l.pos] }"},
	{"call_3", "edit_file", `{"path":"parser/lex.go","patch":"return early when the token stream is empty"}`, "edited parser/lex.go"},
	{"call_4", "run_tests", `{"cmd":"go test ./parser"}`, "ok  \tparser\t0.012s"},
}

func memoryAgent(steps []step) ([]byte, error) {
	msgs := []obj{{"role": "system", "content": "You are a coding agent."}, {"role": "user", "content": memoryTask}}
	for _, s := range steps {
		msgs = append(msgs,
			obj{"role": "assistant", "content": nil, "tool_calls": []obj{{"id": s.id, "type": "function", "function": obj{"name": s.name, "arguments": s.args}}}},
			obj{"role": "tool", "tool_call_id": s.id, "content": s.output})
	}
	return json.Marshal(obj{"model": "gpt-4.1-mini", "stream": true, "stream_options": obj{"include_usage": true}, "messages": msgs})
}

// memoryRounds plays the memory scenario when -memory is on: session A solves the task and is closed, which distils
// a trace; session B gets the same task, and at its second request (when the same error shows) memory recalls A's
// trace and hands it to the third request as a hint (hint mode) or records the hit (observe mode). The sessions are
// named demo-<stamp>-memory-a and -memory-b. The recall runs off the request path, so the client waits for it here the
// way a human typing the next prompt would.
func memoryRounds(ctx context.Context, c *client, _ config) error {
	m := c.mem
	if m == nil {
		return nil
	}
	a, b := "demo-"+c.stamp+"-memory-a", "demo-"+c.stamp+"-memory-b"
	if err := m.waitIdle(ctx); err != nil {
		return err
	}
	if err := c.memorySession(ctx, a, nil); err != nil {
		return err
	}
	c.closeNow(ctx, a)
	if err := m.waitTraces(ctx, 1); err != nil {
		return err
	}
	return c.memorySession(ctx, b, m)
}

// memorySession sends the memory agent's requests. With m set it waits, after each of the first two requests, for the
// recall that request starts.
func (c *client) memorySession(ctx context.Context, session string, m *memoryDemo) error {
	c.sessions = append(c.sessions, session)
	for k := 0; k <= len(memorySteps); k++ {
		var r0 int64
		if m != nil && k < 2 {
			r0 = m.stat("recalls")
		}
		body, err := memoryAgent(memorySteps[:k])
		if err != nil {
			return err
		}
		if _, err := c.post(ctx, "/openai/v1/chat/completions", string(body), session, map[string]string{scenarioHeader: "openai-text"}); err != nil {
			return err
		}
		if m != nil && k < 2 {
			if err := errors.Join(m.waitRecalls(ctx, r0+1), m.waitIdle(ctx)); err != nil {
				st, _ := json.Marshal(m.eng.Source().Status())
				return fmt.Errorf("session %s request %d: %w (memory status %s)", session, k+1, err, st)
			}
		}
	}
	return nil
}

// closeNow closes one session at once (tollgate would after its quiet TTL) and notes it, so play does not close it twice.
func (c *client) closeNow(ctx context.Context, id string) {
	c.hook.OnSessionClosed(ctx, tollgate.Session{ID: id, TenantID: c.tenant})
	c.closed[id] = true
	c.closes++
}
