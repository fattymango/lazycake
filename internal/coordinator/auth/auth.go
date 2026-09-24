// Package auth hashes bearer tokens the same way on write (CreateToken) and
// read (Authenticate), so the database never stores a token in the clear.
package auth

import "crypto/sha256"

// Hash returns the token's storage/lookup key.
func Hash(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}
