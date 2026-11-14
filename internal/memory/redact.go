package memory

import "regexp"

// secretRes are the shapes of a credential that must never be stored or sent to a model: provider keys (sk-, sk-ant-),
// bearer tokens, cloud and VCS tokens, and key=value pairs whose key names a secret.
var secretRes = []*regexp.Regexp{
	regexp.MustCompile(`sk-ant-[A-Za-z0-9_\-]{6,}`),
	regexp.MustCompile(`sk-[A-Za-z0-9_\-]{8,}`),
	regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/\-]{6,}=*`),
	regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`),
	regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{20,}`),
	regexp.MustCompile(`\bxox[abprs]-[A-Za-z0-9\-]{10,}`),
	regexp.MustCompile(`(?i)(?:api[_-]?key|secret|token|passwd|password|authorization)[\w-]*\s*[:=]\s*["']?[^\s"',;]{6,}`),
}

const redacted = "[redacted]"

// Redact replaces every credential-shaped substring of s with "[redacted]".
func Redact(s string) string {
	for _, re := range secretRes {
		s = re.ReplaceAllString(s, redacted)
	}
	return s
}
