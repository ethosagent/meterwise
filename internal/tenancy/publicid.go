package tenancy

import (
	"crypto/rand"
	"errors"
	"io"
)

// PublicIDLength is the length of a workspace public id: 22 base62 characters,
// about 131 bits (decision D6).
const PublicIDLength = 22

const base62 = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// maxUnbiased is the largest multiple of 62 that fits in a byte: bytes at or
// above it are redrawn, so every character is equally likely.
const maxUnbiased = 256 - 256%62

// NewPublicID draws a public id from r (crypto/rand in production).
func NewPublicID(r io.Reader) (string, error) {
	if r == nil {
		r = rand.Reader
	}
	out := make([]byte, 0, PublicIDLength)
	buf := make([]byte, 2*PublicIDLength)
	for len(out) < PublicIDLength {
		if _, err := io.ReadFull(r, buf); err != nil {
			return "", errors.New("tenancy: cannot read random bytes")
		}
		for _, b := range buf {
			if b < maxUnbiased && len(out) < PublicIDLength {
				out = append(out, base62[int(b)%62])
			}
		}
	}
	return string(out), nil
}
