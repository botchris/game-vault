// Package passwordhash implements the password Hasher port with bcrypt.
package passwordhash

import "golang.org/x/crypto/bcrypt"

// Bcrypt implements auth.Hasher.
type Bcrypt struct{ Cost int }

// New returns a Bcrypt hasher with bcrypt's default cost.
func New() Bcrypt { return Bcrypt{Cost: bcrypt.DefaultCost} }

// Hash returns the bcrypt hash of the password.
func (b Bcrypt) Hash(password string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(password), b.Cost)
	return string(h), err
}

// Verify reports whether the password matches the bcrypt hash.
func (b Bcrypt) Verify(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}
