package store

import (
	"math"
	"testing"

	"github.com/ethosagent/meterwise/internal/contract"
)

func TestBasisPoints(t *testing.T) {
	const big = contract.MicroUSD(math.MaxInt64 / 2)
	tests := []struct {
		name        string
		part, whole contract.MicroUSD
		want        int // -1 = nil
	}{
		{"zero whole", 5, 0, -1},
		{"negative whole", 5, -1, -1},
		{"nothing saved", 0, 100, 0},
		{"negative part reads zero", -7, 100, 0},
		{"ten percent", 1_000_000, 10_000_000, 1000},
		{"rounds half up", 1, 3, 3333},
		{"rounds half up at .5", 1, 8, 1250},
		{"all of it", 100, 100, 10000},
		{"half of MaxInt64/2 does not overflow", big / 2, big, 5000},
		{"all of MaxInt64/2", big, big, 10000},
		{"just under all of MaxInt64/2", big - 1, big, 10000},
		{"a tenth of MaxInt64/2", big / 10, big, 1000},
		{"one micro of MaxInt64/2", 1, big, 0},
		{"part above whole is clamped", 200, 100, 10000},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := basisPoints(tc.part, tc.whole)
			switch {
			case tc.want < 0 && got != nil:
				t.Fatalf("basisPoints(%d,%d) = %d, want nil", tc.part, tc.whole, *got)
			case tc.want >= 0 && (got == nil || *got != tc.want):
				t.Fatalf("basisPoints(%d,%d) = %v, want %d", tc.part, tc.whole, got, tc.want)
			}
		})
	}
}
