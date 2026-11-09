package archtest

import (
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
)

// roleOwners may compare roles: contract defines them, access decides with them (the evaluator
// and the escalation guard), store keeps the last-admin invariant in its transactions.
var roleOwners = []string{"internal/contract", "internal/access", "internal/store"}

// inlineRole matches the shapes of an authorization decision made in place: a comparison of a
// `.Role` field or of RoleAdmin/RoleMember, a switch on `.Role`, a case on a role constant, a
// comparison with the string "admin"/"member", or the removed contract.Allows.
var inlineRole = regexp.MustCompile(
	`\.Role\s*[!=]=` +
		`|[!=]=\s*(?:contract\.)?Role(?:Admin|Member)\b` +
		`|\bRole(?:Admin|Member)\s*[!=]=` +
		`|\bcase\s+(?:contract\.)?Role(?:Admin|Member)\b` +
		`|\bswitch\s+[\w.()]*\.Role\b` +
		`|[!=]=\s*"(?:admin|member)"|"(?:admin|member)"\s*[!=]=` +
		`|\bcontract\.Allows\(`)

// scanInlineRoleChecks applies H11: outside the role owners, non-test Go code never decides
// access by looking at a role. Every route names a permission in the contract table and the one
// gateway middleware asks internal/access; handlers and services call access (IsAdmin,
// MayAssignRole) or take the decision as an argument. Comments are ignored.
func scanInlineRoleChecks(files map[string]string) []Violation {
	var out []Violation
	for rel, body := range files {
		if path.Ext(rel) != ".go" || strings.HasSuffix(rel, "_test.go") || inTestdata(rel) ||
			strings.HasPrefix(rel, "internal/archtest/") || underDirs(rel, roleOwners) {
			continue
		}
		if m := inlineRole.FindString(goComments.ReplaceAllString(body, " ")); m != "" {
			out = append(out, Violation{"H11", fmt.Sprintf("%s checks a role inline (%q); name a permission in the contract route table and let the gateway and internal/access decide", rel, strings.Join(strings.Fields(m), " "))})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Msg < out[j].Msg })
	return out
}
