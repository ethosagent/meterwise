package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"strings"
)

// trimFixtures are the tool outputs the trim scenarios replay: a 300-line build log with three errors, a package.json
// that is read twice and a git diff. They are made up for the demo.
//
//go:embed testdata/trim/*
var trimFixtures embed.FS

func trimFixture(name string) string {
	b, err := trimFixtures.ReadFile("testdata/trim/" + name)
	if err != nil {
		panic("demo-traffic: missing trim fixture " + name) // a build-time embed, not input
	}
	return string(b)
}

// step is one tool call of an agent and what the tool printed.
type step struct{ id, name, args, output string }

// trimRounds plays the trim scenarios after the base rounds when a trim mode is on: one OpenAI session that reads a
// build log, runs `cat package.json` twice and shows a git diff, and one Anthropic session with a cache_control
// breakpoint on its system prompt. Each request carries the whole history, as a real agent's does, so trimming
// shows from the second request on. The sessions are named demo-<stamp>-trim-openai and -trim-anthropic.
func trimRounds(ctx context.Context, c *client, cfg config) error {
	if cfg.trim == "" || cfg.trim == "off" {
		return nil
	}
	openai := []step{
		{"call_1", "run_shell", `{"cmd":"make all"}`, trimFixture("build.log")},
		{"call_2", "run_shell", `{"cmd":"cat package.json"}`, trimFixture("package.json")},
		{"call_3", "run_shell", `{"cmd":"cat package.json"}`, trimFixture("package.json")},
		{"call_4", "run_shell", `{"cmd":"git diff"}`, trimFixture("git.diff")},
	}
	anth := []step{
		{"toolu_1", "run_shell", `{"cmd":"make all"}`, trimFixture("build.log")},
		{"toolu_2", "run_shell", `{"cmd":"git diff"}`, trimFixture("git.diff")},
	}
	if err := c.agent(ctx, "demo-"+c.stamp+"-trim-openai", "/openai/v1/chat/completions", "openai-text", openai, openAIAgent); err != nil {
		return err
	}
	return c.agent(ctx, "demo-"+c.stamp+"-trim-anthropic", "/anthropic/v1/messages", "anthropic-text", anth, anthropicAgent)
}

// agent sends len(steps)+1 requests: request k carries the first k-1 steps, so each tool result reaches the proxy
// as new in one request and as history in every later one.
func (c *client) agent(ctx context.Context, session, path, scenario string, steps []step, build func([]step) ([]byte, error)) error {
	c.sessions = append(c.sessions, session)
	for k := 0; k <= len(steps); k++ {
		body, err := build(steps[:k])
		if err != nil {
			return err
		}
		if _, err := c.post(ctx, path, string(body), session, map[string]string{scenarioHeader: scenario}); err != nil {
			return err
		}
	}
	return nil
}

type obj = map[string]any

func openAIAgent(steps []step) ([]byte, error) {
	msgs := []obj{{"role": "system", "content": "You are a build agent."}, {"role": "user", "content": "Build the service and tell me what broke."}}
	for _, s := range steps {
		msgs = append(msgs,
			obj{"role": "assistant", "content": nil, "tool_calls": []obj{{"id": s.id, "type": "function", "function": obj{"name": s.name, "arguments": s.args}}}},
			obj{"role": "tool", "tool_call_id": s.id, "content": s.output})
	}
	return json.Marshal(obj{"model": "gpt-4.1-mini", "stream": true, "stream_options": obj{"include_usage": true}, "messages": msgs})
}

func anthropicAgent(steps []step) ([]byte, error) {
	msgs := []obj{{"role": "user", "content": "Build the service and tell me what broke."}}
	for _, s := range steps {
		var in any
		if err := json.NewDecoder(strings.NewReader(s.args)).Decode(&in); err != nil {
			return nil, fmt.Errorf("step %s: %w", s.id, err)
		}
		msgs = append(msgs,
			obj{"role": "assistant", "content": []obj{{"type": "tool_use", "id": s.id, "name": s.name, "input": in}}},
			obj{"role": "user", "content": []obj{{"type": "tool_result", "tool_use_id": s.id, "content": s.output}}})
	}
	system := []obj{{"type": "text", "text": "You are a build agent.", "cache_control": obj{"type": "ephemeral"}}}
	return json.Marshal(obj{"model": "claude-sonnet-4-5", "max_tokens": 256, "stream": true, "system": system, "messages": msgs})
}
