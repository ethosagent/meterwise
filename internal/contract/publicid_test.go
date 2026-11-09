package contract_test

import (
	"strings"
	"testing"

	"github.com/ethosagent/meterwise/internal/contract"
)

func TestValidPublicID(t *testing.T) {
	for _, tc := range []struct {
		name, id string
		want     bool
	}{
		{"22 base62", "Aa0Bb1Cc2Dd3Ee4Ff5Gg6H", true},
		{"all digits", strings.Repeat("7", contract.PublicIDLength), true},
		{"empty", "", false},
		{"short", "Aa0Bb1", false},
		{"long", "Aa0Bb1Cc2Dd3Ee4Ff5Gg6Hx", false},
		{"dash", "Aa0Bb1Cc2Dd3Ee4Ff5Gg6-", false},
		{"slash", "Aa0Bb1Cc2Dd3Ee4Ff5Gg/H", false},
		{"non-ascii", "Aa0Bb1Cc2Dd3Ee4Ff5Gg6é", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := contract.ValidPublicID(tc.id); got != tc.want {
				t.Fatalf("ValidPublicID(%q) = %v, want %v", tc.id, got, tc.want)
			}
		})
	}
	if contract.PublicIDLength != 22 || len(contract.PublicIDAlphabet) != 62 {
		t.Fatalf("shape constants drifted: %d, %d", contract.PublicIDLength, len(contract.PublicIDAlphabet))
	}
}
