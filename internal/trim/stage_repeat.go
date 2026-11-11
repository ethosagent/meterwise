package trim

import "github.com/ethosagent/meterwise/internal/contract"

// minRepeatBytes is the smallest result worth replacing by a marker.
const minRepeatBytes = 80

// repeatCollapse replaces a tool result that is byte-identical to an earlier result of the same tool in the request
// (an agent re-reading a file) by a one-line marker. The first copy stays whole. Identity is by content hash, so the
// stage needs nothing but Unit.Seen; the near-identical case (98 % similar, plan 11 section 3.1) is left for a later version.
type repeatCollapse struct{}

func (repeatCollapse) Name() contract.TrimStage { return contract.StageRepeatCollapse }

// Apply returns the marker "[unchanged since step N — see above]" when the result repeats message N.
func (repeatCollapse) Apply(u Unit, text string) (string, bool) {
	if u.Seen <= 0 || len(text) < minRepeatBytes {
		return "", false
	}
	return repeatMarker(u.Seen), true
}

func repeatMarker(step int) string { return "[unchanged since step " + itoa(step) + " — see above]" }
