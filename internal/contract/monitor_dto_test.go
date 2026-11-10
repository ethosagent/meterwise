package contract_test

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/ethosagent/meterwise/internal/contract"
)

// TestMonitorStatsRowHasNoSummedSavingsField: the stats row carries the saved (certain), likely and
// would-save amounts each in its own field, tokens and USD alike. No field name covers more than one
// class, so no field can hold certain and likely added together (non-negotiable 5).
func TestMonitorStatsRowHasNoSummedSavingsField(t *testing.T) {
	b, err := json.Marshal(contract.MonitorStatsRowView{})
	if err != nil {
		t.Fatal(err)
	}
	var row map[string]any
	if err := json.Unmarshal(b, &row); err != nil {
		t.Fatal(err)
	}
	var savings []string
	for k := range row {
		lk := strings.ToLower(k)
		if strings.Contains(lk, "sav") || strings.Contains(lk, "est") {
			savings = append(savings, k)
		}
	}
	slices.Sort(savings)
	want := []string{"likelySavedTokens", "likelySavedUsd", "savedTokens", "savedUsd", "wouldSaveTokens", "wouldSaveUsd"}
	if !slices.Equal(savings, want) {
		t.Errorf("savings fields = %v, want exactly %v (one class per field)", savings, want)
	}
}
