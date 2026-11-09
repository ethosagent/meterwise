package api

import (
	"encoding/json"
	"net/http"

	"github.com/ethosagent/meterwise/internal/contract"
)

// writeJSON writes v with the given status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError writes the typed error body {code, message, details} at the
// status the registry assigns to the code.
func writeError(w http.ResponseWriter, e *contract.Error) {
	writeJSON(w, e.Status(), e.Body())
}
