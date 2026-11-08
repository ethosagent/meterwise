package contract

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// MicroUSD is money as an integer count of millionths of a US dollar.
// Money is never a float in Go (ruling R12); the API sends decimal strings.
type MicroUSD int64

const microPerUSD = 1_000_000

// Decimal renders the amount as a fixed six-place decimal string, for example
// 1500 -> "0.001500" and -2500000 -> "-2.500000".
func (m MicroUSD) Decimal() string {
	neg := m < 0
	v := int64(m)
	if neg {
		v = -v
	}
	s := fmt.Sprintf("%d.%06d", v/microPerUSD, v%microPerUSD)
	if neg {
		return "-" + s
	}
	return s
}

// ParseMicroUSD reads a decimal string with at most six fraction digits.
func ParseMicroUSD(s string) (MicroUSD, error) {
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	whole, frac, _ := strings.Cut(s, ".")
	if whole == "" || len(frac) > 6 || strings.ContainsAny(whole+frac, "+-eE ") {
		return 0, fmt.Errorf("contract: %q is not a decimal with at most 6 fraction digits", s)
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
	v := w*microPerUSD + f
	if neg {
		v = -v
	}
	return MicroUSD(v), nil
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
