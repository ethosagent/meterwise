package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ethosagent/meterwise/internal/identity/clerkmock"
)

const (
	defaultURL        = "http://127.0.0.1:8081/api/webhooks/clerk"
	envWebhookSigning = "CLERK_WEBHOOK_SECRET"
	httpTimeout       = 10 * time.Second
)

func main() {
	os.Exit(run(os.Args[1:], os.Getenv, os.Stdout, os.Stderr))
}

// run posts the chosen fixtures and returns the exit code: 0 when every
// delivery was acknowledged with 204, 1 otherwise, 2 for a usage error.
func run(args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("clerk-replay", flag.ContinueOnError)
	fs.SetOutput(stderr)
	url := fs.String("url", defaultURL, "webhook endpoint")
	fixture := fs.String("fixture", "all", "fixture name ("+strings.Join(clerkmock.FixtureNames(), ", ")+") or all")
	secret := fs.String("secret", "", "Svix signing secret (default $"+envWebhookSigning+")")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *secret == "" {
		*secret = getenv(envWebhookSigning)
	}
	if *secret == "" {
		fmt.Fprintf(stderr, "clerk-replay: set %s or pass -secret\n", envWebhookSigning)
		return 2
	}
	names := clerkmock.FixtureNames()
	if *fixture != "all" {
		names = []string{*fixture}
	}
	client := &http.Client{Timeout: httpTimeout}
	code := 0
	for _, name := range names {
		body, err := clerkmock.Fixture(name)
		if err != nil {
			fmt.Fprintf(stderr, "clerk-replay: no fixture %q\n", name)
			return 2
		}
		var raw [8]byte
		_, _ = rand.Read(raw[:])
		h := clerkmock.SignWebhook(*secret, "msg_replay_"+hex.EncodeToString(raw[:]), time.Now(), body)
		status, err := post(client, *url, h, body)
		if err != nil || status != http.StatusNoContent {
			fmt.Fprintf(stdout, "%-14s FAILED (status %d)\n", name, status)
			code = 1
			continue
		}
		fmt.Fprintf(stdout, "%-14s ok (204)\n", name)
	}
	return code
}

func post(c *http.Client, url string, h http.Header, body []byte) (int, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header = h
	resp, err := c.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode, nil
}
