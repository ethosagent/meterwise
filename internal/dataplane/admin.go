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
func AdminHandler(admin http.Handler, sources []contract.StatusSource) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/", admin)
	mux.HandleFunc("GET /statusz", func(w http.ResponseWriter, r *http.Request) {
		rec := httptest.NewRecorder()
		admin.ServeHTTP(rec, r)
		doc := map[string]any{}
		if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
			http.Error(w, "statusz unavailable", http.StatusInternalServerError)
			return
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
		out[name] = s.Status()
	}
	return out
}
