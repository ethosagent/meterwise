package main

import (
	"github.com/ethosagent/meterwise/internal/api"
	"github.com/ethosagent/meterwise/internal/store"
)

// trimFeature builds the control-plane port for trim stats and previews over the ledger's calls and trim_events. The
// hook itself lives in internal/dataplane/feature_trim.go.
func trimFeature(rt *instance) api.TrimReader { return store.NewTrimReader(rt.db) }
