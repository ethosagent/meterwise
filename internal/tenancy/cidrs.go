package tenancy

import (
	"errors"
	"net/netip"
	"strconv"

	"github.com/ethosagent/meterwise/internal/contract"
)

// NormalizeCIDRs validates and canonicalises a source-network list: every entry must be an IPv4 or
// IPv6 CIDR (a bare address becomes /32 or /128), is masked to its network, and duplicates are
// dropped. Every bad entry is listed (422), never just the first.
func NormalizeCIDRs(in []string) ([]string, error) {
	if len(in) > contract.MaxAllowedCIDRs {
		return nil, contract.NewError(contract.CodeValidationFailed, "request validation failed",
			"cidrs: at most "+strconv.Itoa(contract.MaxAllowedCIDRs)+" entries")
	}
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	var problems []string
	for i, raw := range in {
		p, err := parseCIDR(raw)
		if err != nil {
			problems = append(problems, "cidrs["+strconv.Itoa(i)+"]: not an IPv4 or IPv6 address or CIDR")
			continue
		}
		s := p.String()
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	if len(problems) > 0 {
		return nil, contract.NewError(contract.CodeValidationFailed, "request validation failed", problems...)
	}
	return out, nil
}

var errZone = errors.New("IPv6 zone")

func parseCIDR(raw string) (netip.Prefix, error) {
	if a, err := netip.ParseAddr(raw); err == nil {
		if a.Zone() != "" { // a zone names a local interface, never a client network: refuse, do not drop it
			return netip.Prefix{}, errZone
		}
		a = a.Unmap()
		return netip.PrefixFrom(a, a.BitLen()), nil
	}
	p, err := netip.ParsePrefix(raw)
	if err != nil {
		return netip.Prefix{}, err
	}
	return p.Masked(), nil
}
