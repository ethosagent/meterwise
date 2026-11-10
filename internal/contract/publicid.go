package contract

// PublicIDLength is the length of a workspace public id: 22 base62 characters, about 131 bits
// (decision D6). It is the one definition: the generator (tenancy), the data-plane parser
// (wsauth) and the store's checks all use it.
const PublicIDLength = 22

// PublicIDAlphabet is the base62 alphabet of a public id.
const PublicIDAlphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// ValidPublicID reports whether s has the shape of a workspace public id. It checks shape only;
// whether the id names a workspace is the directory's answer.
func ValidPublicID(s string) bool {
	if len(s) != PublicIDLength {
		return false
	}
	for i := range len(s) {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'A' || c > 'Z') && (c < 'a' || c > 'z') {
			return false
		}
	}
	return true
}
