package main

import "github.com/ethosagent/meterwise/internal/api"

// trimFeature builds the control-plane port for trim stats and previews. Step 0 ships no trimming, so it returns nil
// and the routes answer not_available; the trim owner replaces this file wholesale.
func trimFeature(*instance) api.TrimReader { return nil }
