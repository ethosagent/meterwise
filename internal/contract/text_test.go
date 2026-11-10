package contract_test

import (
	"testing"
	"unicode/utf8"

	"github.com/ethosagent/meterwise/internal/contract"
)

func TestTruncateUTF8(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		n    int
		want string
	}{
		{"fits", "abc", 3, "abc"},
		{"ascii cut", "abcdef", 4, "abcd"},
		{"empty", "", 5, ""},
		{"zero", "abc", 0, ""},
		{"negative", "abc", -1, ""},
		{"cut on a boundary", "aé", 3, "aé"},
		{"cut inside a 2-byte rune", "aéb", 2, "a"},
		{"cut inside a 3-byte rune", "a€b", 3, "a"},
		{"cut inside a 4-byte rune", "a😀b", 4, "a"},
		{"only a rune that does not fit", "€", 2, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := contract.TruncateUTF8(tc.in, tc.n)
			if got != tc.want || !utf8.ValidString(got) {
				t.Errorf("TruncateUTF8(%q, %d) = %q, want %q", tc.in, tc.n, got, tc.want)
			}
		})
	}
}

func TestHashOfIsHashIDOfTheRenderedHash(t *testing.T) {
	for _, h := range []uint64{0, 1, 12345, 1<<64 - 1, contract.HashString("get_weather")} {
		if got, want := contract.HashOf("ws-1", h), contract.HashID("ws-1", contract.HashValue(h)); got != want {
			t.Errorf("HashOf(%d) = %s, want %s", h, got, want)
		}
	}
}
