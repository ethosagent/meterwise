package dataplane

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/ethosagent/meterwise/internal/contract"
)

// namer is the optional Name a StatusSource may have; without it the source is "source<i>".
type namer interface{ Name() string }

// AdminHandler is tollgate's admin handler with /statusz extended: the JSON tollgate writes gets a
// "sources" object with one entry per registered StatusSource (ledger queue depth, drops, last
// flush error, directory cache, admission). /metrics and everything else pass through. Bind it to
// the private listener only.
//
// Assumption: tollgate's /statusz writes one JSON object (true of v0.1.0). Only the body and the
// status code are carried over; inner headers are not. Should a future tollgate answer something
// else (text, an array), the raw body is embedded as a string under "tollgate_raw" with tollgate's
// status code, so the sources still render and nothing becomes a 500.
func AdminHandler(admin http.Handler, sources []contract.StatusSource) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/", admin)
	mux.HandleFunc("GET /statusz", func(w http.ResponseWriter, r *http.Request) {
		rec := httptest.NewRecorder()
		admin.ServeHTTP(rec, r)
		doc := map[string]any{}
		if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil || doc == nil {
			doc = map[string]any{"tollgate_raw": rec.Body.String()}
		}
		doc["sources"] = collect(sources)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(rec.Code)
		_ = json.NewEncoder(w).Encode(doc)
	})
	return mux
}

func collect(sources []contract.StatusSource) map[string]any {
	out := make(map[string]any, len(sources))
	for i, s := range sources {
		name := fmt.Sprintf("source%d", i)
		if n, ok := s.(namer); ok && n.Name() != "" {
			name = n.Name()
		}
		if _, dup := out[name]; dup {
			name = fmt.Sprintf("%s#%d", name, i) // never overwrite another source's entry
		}
		out[name] = s.Status()
	}
	return out
}
