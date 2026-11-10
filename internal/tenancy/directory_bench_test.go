package tenancy_test

import (
	"context"
	"testing"
)

func benchDir(b *testing.B) (lookup func() error, bust func()) {
	b.Helper()
	dir, svc, _, _ := newDir(b)
	ctx := context.Background()
	ws, err := svc.CreateWorkspace(ctx, principal(), "Bench")
	if err != nil {
		b.Fatal(err)
	}
	if _, err := dir.ByPublicID(ctx, ws.PublicID); err != nil {
		b.Fatal(err)
	}
	return func() error { _, err := dir.ByPublicID(ctx, ws.PublicID); return err }, func() { dir.Bust(ws.ID) }
}

// BenchmarkDirectoryHit is a warm lookup: the cache read the data plane does per request.
func BenchmarkDirectoryHit(b *testing.B) {
	lookup, _ := benchDir(b)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if err := lookup(); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkDirectoryHitParallel is the same read from every core at once (lock contention).
func BenchmarkDirectoryHitParallel(b *testing.B) {
	lookup, _ := benchDir(b)
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if err := lookup(); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkDirectoryMiss busts the entry before every lookup, so each one reads Postgres.
func BenchmarkDirectoryMiss(b *testing.B) {
	lookup, bust := benchDir(b)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		bust()
		if err := lookup(); err != nil {
			b.Fatal(err)
		}
	}
}
