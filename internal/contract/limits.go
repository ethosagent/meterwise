package contract

// Limits shared by the API, the store and the web client. cmd/gen emits them into
// web/src/contract/generated.ts (Limits), so a number lives here and nowhere else.
const (
	// DefaultGraceMinutes is how long the previous proxy URL keeps working after a reset when the
	// request does not say.
	DefaultGraceMinutes = 10
	// MaxGraceMinutes is the longest grace window a reset may ask for (24 hours).
	MaxGraceMinutes = 24 * 60
	// MaxAllowedCIDRs is how many source networks a workspace may list.
	MaxAllowedCIDRs = 32
	// MaxAuditLimit is the largest page the audit log serves: the one page bound of every paged list.
	MaxAuditLimit = MaxPageLimit
)

// Limit is one named number of the contract; Name is its TypeScript constant.
type Limit struct {
	Name  string
	Value int
}

// Limits lists every limit in a fixed order.
func Limits() []Limit {
	return []Limit{
		{"GRACE_MINUTES_DEFAULT", DefaultGraceMinutes},
		{"GRACE_MINUTES_MAX", MaxGraceMinutes},
		{"ALLOWED_CIDRS_MAX", MaxAllowedCIDRs},
		{"AUDIT_LIMIT_MAX", MaxAuditLimit},
	}
}
