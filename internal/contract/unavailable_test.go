package contract_test

import (
	"testing"
	"time"

	"github.com/ethosagent/meterwise/internal/contract"
)

func TestUnavailable(t *testing.T) {
	tests := []struct {
		what, so, want string
	}{
		{"the ledger", "", "the ledger is not configured"},
		{"Clerk", "no invitation email is sent", "Clerk is not configured, so no invitation email is sent"},
	}
	for _, tc := range tests {
		e := contract.Unavailable(tc.what, tc.so)
		if e.Code != contract.CodeNotAvailable || e.Message != tc.want {
			t.Errorf("Unavailable(%q, %q) = %s %q, want %q", tc.what, tc.so, e.Code, e.Message, tc.want)
		}
	}
}

func TestUTCDayStart(t *testing.T) {
	ist := time.FixedZone("IST", 5*3600+1800)
	tests := []struct {
		name string
		in   time.Time
		want time.Time
	}{
		{"midday UTC", time.Date(2026, 3, 4, 12, 30, 1, 5, time.UTC), time.Date(2026, 3, 4, 0, 0, 0, 0, time.UTC)},
		{"already midnight", time.Date(2026, 3, 4, 0, 0, 0, 0, time.UTC), time.Date(2026, 3, 4, 0, 0, 0, 0, time.UTC)},
		{"zone ahead of UTC is the UTC day", time.Date(2026, 3, 4, 2, 0, 0, 0, ist), time.Date(2026, 3, 3, 0, 0, 0, 0, time.UTC)},
	}
	for _, tc := range tests {
		if got := contract.UTCDayStart(tc.in); !got.Equal(tc.want) || got.Location() != time.UTC {
			t.Errorf("%s: UTCDayStart(%v) = %v, want %v", tc.name, tc.in, got, tc.want)
		}
	}
}
