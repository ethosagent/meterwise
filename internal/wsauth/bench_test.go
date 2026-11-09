package wsauth_test

import (
	"context"
	"testing"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/wsauth"
)

// warmDir answers every lookup from a view built once, as tenancy.Directory does on a cache hit.
type warmDir struct{ view any }

func (w warmDir) CachedView(string) (any, bool, error)          { return w.view, true, nil }
func (w warmDir) LoadView(context.Context, string) (any, error) { return w.view, nil }

// BenchmarkAuthenticateHit is the hot path: a cached workspace with settings and two networks.
// Before the carry (D1, D2) the same request cost 590 ns, 809 B and 11 allocs.
func BenchmarkAuthenticateHit(b *testing.B) {
	ws, _ := (&dir{}).ByPublicID(context.Background(), cidrID)
	ws.Settings = map[string]any{"allowedProviders": []any{"openai"}, "maxConcurrent": float64(4)}
	a := wsauth.New(warmDir{wsauth.Resolve(ws)}, 0)
	r := authReq("/w/"+cidrID+"/v1/chat/completions", "203.0.113.77:1")
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := a.Authenticate(ctx, r); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkAuthenticateMiss builds the view and the lookup deadline on every call.
func BenchmarkAuthenticateMiss(b *testing.B) {
	a := wsauth.New(wsauth.Uncached(func(context.Context, string) (contract.Workspace, error) {
		return (&dir{}).ByPublicID(context.Background(), cidrID)
	}), 0)
	r := authReq("/w/"+cidrID+"/v1/chat/completions", "203.0.113.77:1")
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := a.Authenticate(ctx, r); err != nil {
			b.Fatal(err)
		}
	}
}
