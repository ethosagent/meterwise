package memory

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

// callTimeout bounds one model call; the whole client carries it too.
const callTimeout = 10 * time.Second

// maxResponse caps what is read from a model response.
const maxResponse = 1 << 20

// modelClient is the only code in the host that sends our own model key anywhere (ADR 0033, archtest H12). It has its
// own http.Client with a 10 s timeout and an explicit Transport; the key is set per request and never logged, never
// put in an error, never returned.
type modelClient struct {
	base string
	key  string
	hc   *http.Client
}

func newModelClient(base, key string) *modelClient {
	tr := &http.Transport{
		DialContext:           (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: callTimeout,
		MaxIdleConns:          8,
		MaxIdleConnsPerHost:   4,
		IdleConnTimeout:       60 * time.Second,
		ForceAttemptHTTP2:     true,
	}
	return &modelClient{base: base, key: key, hc: &http.Client{Timeout: callTimeout, Transport: tr}}
}

// errModel is a model call that failed. Its text never carries the key or a response body.
var errModel = errors.New("memory: model call failed")

// post sends body as JSON to base+path and decodes the JSON answer into out. A non-2xx status is an error that names the
// status only.
func (c *modelClient) post(ctx context.Context, path string, body, out any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("%w: request is not JSON", errModel)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+path, bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("%w: bad URL", errModel)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.key)
	resp, err := c.hc.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("%w: unreachable", errModel)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse))
	if err != nil {
		return fmt.Errorf("%w: cannot read the answer", errModel)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("%w: status %d", errModel, resp.StatusCode)
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("%w: answer is not JSON", errModel)
	}
	return nil
}

// Usage is the tokens one model call used.
type Usage struct{ Input, Output int64 }
