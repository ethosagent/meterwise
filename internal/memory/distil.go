package memory

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ethosagent/meterwise/internal/contract"
)

// PromptVersion is stored with every trace (distil_prompt_v); bump it when the prompt below changes.
const PromptVersion = 1

// Summary is what the distiller is shown about a finished session: the ring-summary kind of facts, never the raw
// transcript. Every string has been through Redact and the first user message is at most 200 characters.
type Summary struct {
	Task     string // first user message, at most 200 characters
	Calls    int
	Tools    []string
	Files    []string
	Errors   []string
	LastText string // the last assistant text, capped
	Repo     string
}

// Trace is what the distiller returns, strictly validated.
type Trace struct {
	TaskSummary  string
	Observations []string
	Approach     string
	Fix          string
	Signature    contract.TraceSignature
	Confidence   float32
}

// Distiller turns a session summary into a trace with one model call.
type Distiller interface {
	Distil(ctx context.Context, model string, s Summary) (Trace, Usage, error)
}

type httpDistiller struct{ c *modelClient }

// NewDistiller returns the distiller for an OpenAI-compatible API root and key.
func NewDistiller(base, key string) Distiller { return httpDistiller{newModelClient(base, key)} }

const distilSystem = `You turn a finished coding-agent session into one reusable memory. Answer with a single JSON object and nothing else:
{"task_summary": string (<=200 chars), "observations": [string] (<=5, each <=160 chars), "approach": string (<=200 chars, what worked), "fix": string (<=200 chars, the concrete change), "signature": {"repo": string or null, "files": [string], "errors": [string], "tools": [string]}, "confidence": number between 0 and 1}.
Use only the facts given. Never include secrets, keys or tokens.`

type chatRequest struct {
	Model          string        `json:"model"`
	Messages       []chatMessage `json:"messages"`
	ResponseFormat struct {
		Type string `json:"type"`
	} `json:"response_format"`
	MaxTokens   int     `json:"max_tokens"`
	Temperature float64 `json:"temperature"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int64 `json:"prompt_tokens"`
		CompletionTokens int64 `json:"completion_tokens"`
	} `json:"usage"`
}

type distilJSON struct {
	TaskSummary  string   `json:"task_summary"`
	Observations []string `json:"observations"`
	Approach     string   `json:"approach"`
	Fix          string   `json:"fix"`
	Signature    struct {
		Repo   *string  `json:"repo"`
		Files  []string `json:"files"`
		Errors []string `json:"errors"`
		Tools  []string `json:"tools"`
	} `json:"signature"`
	Confidence *float64 `json:"confidence"`
}

func (d httpDistiller) Distil(ctx context.Context, model string, s Summary) (Trace, Usage, error) {
	req := chatRequest{Model: model, MaxTokens: 600, Messages: []chatMessage{{Role: "system", Content: distilSystem}, {Role: "user", Content: summaryText(s)}}}
	req.ResponseFormat.Type = "json_object"
	var out chatResponse
	if err := d.c.post(ctx, "/chat/completions", req, &out); err != nil {
		return Trace{}, Usage{}, err
	}
	u := Usage{Input: out.Usage.PromptTokens, Output: out.Usage.CompletionTokens}
	if len(out.Choices) == 0 {
		return Trace{}, u, fmt.Errorf("%w: no answer", errModel)
	}
	t, err := ParseTrace(out.Choices[0].Message.Content)
	return t, u, err
}

// summaryText is the user message of the distil call.
func summaryText(s Summary) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Task (first user message): %s\nCalls: %d\n", s.Task, s.Calls)
	for _, f := range []struct {
		name string
		v    []string
	}{{"Tools used", s.Tools}, {"Files touched", s.Files}, {"Errors seen", s.Errors}} {
		if len(f.v) > 0 {
			fmt.Fprintf(&b, "%s: %s\n", f.name, strings.Join(f.v, " | "))
		}
	}
	if s.Repo != "" {
		fmt.Fprintf(&b, "Repo: %s\n", s.Repo)
	}
	if s.LastText != "" {
		fmt.Fprintf(&b, "Final assistant message: %s\n", s.LastText)
	}
	return b.String()
}

// ParseTrace reads the distiller's JSON strictly (an unknown field, a missing field or trailing text is an error) and
// then clamps lengths and runs the redaction pass: what comes out is what may be stored.
func ParseTrace(raw string) (Trace, error) {
	dec := json.NewDecoder(bytes.NewReader([]byte(strings.TrimSpace(raw))))
	dec.DisallowUnknownFields()
	var j distilJSON
	if err := dec.Decode(&j); err != nil || dec.More() {
		return Trace{}, fmt.Errorf("%w: the distil answer is not the expected JSON", errModel)
	}
	if strings.TrimSpace(j.TaskSummary) == "" || strings.TrimSpace(j.Approach) == "" || strings.TrimSpace(j.Fix) == "" || j.Confidence == nil || *j.Confidence < 0 || *j.Confidence > 1 {
		return Trace{}, fmt.Errorf("%w: the distil answer is missing a field or out of range", errModel)
	}
	t := Trace{
		TaskSummary: clampText(j.TaskSummary, 300), Approach: clampText(j.Approach, 300), Fix: clampText(j.Fix, 300),
		Observations: clampList(j.Observations, 5, 200), Confidence: float32(*j.Confidence),
	}
	t.Signature = contract.TraceSignature{
		Files: clampList(j.Signature.Files, 10, 200), Errors: clampList(j.Signature.Errors, 5, 200), Tools: clampList(j.Signature.Tools, 20, 64),
	}
	if j.Signature.Repo != nil && strings.TrimSpace(*j.Signature.Repo) != "" {
		t.Signature.Repo = new(clampText(*j.Signature.Repo, 200))
	}
	return t, nil
}

// clampText redacts and shortens s on a character boundary.
func clampText(s string, n int) string {
	return contract.TruncateUTF8(strings.TrimSpace(Redact(strings.ToValidUTF8(s, ""))), n)
}

func clampList(v []string, maxItems, maxLen int) []string {
	out := make([]string, 0, min(len(v), maxItems))
	for _, s := range v {
		if s = clampText(s, maxLen); s != "" && len(out) < maxItems {
			out = append(out, s)
		}
	}
	return out
}

// clean redacts and clamps every text of the trace. ParseTrace already did; the engine runs it again before a store so a
// Distiller of any origin cannot put a credential or an unbounded string into a row.
func (t Trace) clean() Trace {
	t.TaskSummary, t.Approach, t.Fix = clampText(t.TaskSummary, 300), clampText(t.Approach, 300), clampText(t.Fix, 300)
	t.Observations = clampList(t.Observations, 5, 200)
	t.Signature = contract.TraceSignature{
		Files: clampList(t.Signature.Files, 10, 200), Errors: clampList(t.Signature.Errors, 5, 200), Tools: clampList(t.Signature.Tools, 20, 64),
	}
	if t.Signature.Repo != nil {
		t.Signature.Repo = new(clampText(*t.Signature.Repo, 200))
	}
	t.Confidence = max(0, min(1, t.Confidence))
	return t
}
