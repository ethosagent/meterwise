package wsauth

// AttrTenantOK is the Tenant.Attrs key that marks a tenant whose workspace id and org id are both
// canonical lower-case uuids. Resolve checks them once per cache fill so the ledger reads the
// attribute per call instead of matching a pattern on each. Its value is "true"; absent means
// not checked or not valid, and the reader must fall back to its own check.
const AttrTenantOK = "tenant_ok"

// canonicalUUID reports whether s is a lower-case 8-4-4-4-12 hex uuid.
func canonicalUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i := range len(s) {
		switch c := s[i]; {
		case i == 8 || i == 13 || i == 18 || i == 23:
			if c != '-' {
				return false
			}
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f':
		default:
			return false
		}
	}
	return true
}
