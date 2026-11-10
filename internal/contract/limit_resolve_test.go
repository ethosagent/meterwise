package contract_test

import (
	"testing"

	"github.com/ethosagent/meterwise/internal/contract"
)

func TestResolvePageLimit(t *testing.T) {
	tests := []struct {
		in      int
		want    int
		invalid bool
	}{
		{0, contract.DefaultPageLimit, false},
		{1, 1, false},
		{contract.MaxPageLimit, contract.MaxPageLimit, false},
		{-1, 0, true},
		{contract.MaxPageLimit + 1, 0, true},
	}
	for _, tc := range tests {
		got, err := contract.ResolvePageLimit(tc.in)
		if (err != nil) != tc.invalid || got != tc.want {
			t.Errorf("ResolvePageLimit(%d) = %d, %v; want %d invalid=%v", tc.in, got, err, tc.want, tc.invalid)
		}
	}
}
