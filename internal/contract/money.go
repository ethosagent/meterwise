package contract

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// MicroUSD is money as an integer count of millionths of a US dollar.
// Money is never a float in Go (ruling R12); the API sends decimal strings.
type MicroUSD int64

const microPerUSD = 1_000_000

// Decimal renders the amount as a fixed six-place decimal string, for example
// 1500 -> "0.001500" and -2500000 -> "-2.500000". It is exact for every int64,
// including math.MinInt64 (whose plain negation overflows int64).
func (m MicroUSD) Decimal() string {
	q, r := int64(m)/microPerUSD, int64(m)%microPerUSD
	if m < 0 {
		// q and r are both <= 0 and |q| < 2^63/1e6, |r| < 1e6: negating them cannot overflow
		return fmt.Sprintf("-%d.%06d", -q, -r)
	}
	return fmt.Sprintf("%d.%06d", q, r)
}

// ParseMicroUSD reads a decimal string with at most six fraction digits and a
// value that fits int64 (so "-9223372036854.775808" parses, one micro more does not).
func ParseMicroUSD(s string) (MicroUSD, error) {
	orig := s
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	whole, frac, _ := strings.Cut(s, ".")
	if whole == "" || len(frac) > 6 || strings.ContainsAny(whole+frac, "+-eE _ ") {
		return 0, fmt.Errorf("contract: %q is not a decimal with at most 6 fraction digits", orig)
	}
	frac += strings.Repeat("0", 6-len(frac))
	w, err := strconv.ParseInt(whole, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("contract: parse whole part: %w", err)
	}
	f, err := strconv.ParseInt(frac, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("contract: parse fraction: %w", err)
	}
	if neg {
		nw := -w
		if nw < math.MinInt64/microPerUSD {
			return 0, fmt.Errorf("contract: %q is out of range", orig)
		}
		v := nw * microPerUSD
		if v < math.MinInt64+f {
			return 0, fmt.Errorf("contract: %q is out of range", orig)
		}
		return MicroUSD(v - f), nil
	}
	if w > math.MaxInt64/microPerUSD {
		return 0, fmt.Errorf("contract: %q is out of range", orig)
	}
	v := w * microPerUSD
	if v > math.MaxInt64-f {
		return 0, fmt.Errorf("contract: %q is out of range", orig)
	}
	return MicroUSD(v + f), nil
}

// MarshalJSON sends money as a decimal string, never a JSON number (ruling R12).
func (m MicroUSD) MarshalJSON() ([]byte, error) {
	b, err := json.Marshal(m.Decimal())
	if err != nil {
		return nil, fmt.Errorf("contract: marshal money: %w", err)
	}
	return b, nil
}

// UnmarshalJSON reads the decimal-string form; a bare number is rejected.
func (m *MicroUSD) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("contract: money must be a decimal string: %w", err)
	}
	v, err := ParseMicroUSD(s)
	if err != nil {
		return err
	}
	*m = v
	return nil
}
