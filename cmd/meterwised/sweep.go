package main

import (
	"context"
	"time"
)

// sweepEvery is how often the workspace directory drops cache entries that are expired or retired
// past their grace. It is memory hygiene: a retired id's entry already expires with its grace.
const sweepEvery = 60 * time.Second

// sweepDirectory runs until ctx ends.
func (rt *instance) sweepDirectory(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			if n := rt.svc.Directory().SweepRetired(now); n > 0 {
				rt.log.Debug("directory swept", "dropped", n)
			}
		}
	}
}
