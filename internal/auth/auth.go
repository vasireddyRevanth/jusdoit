// Package auth holds password hashing and session token helpers.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"

	"golang.org/x/crypto/bcrypt"
)

const bcryptCost = 12

// HashPassword returns a bcrypt hash of pw.
func HashPassword(pw string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), bcryptCost)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// CheckPassword reports whether pw matches hash.
func CheckPassword(hash, pw string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil
}

// NewToken returns a raw session token and the hash stored in the database.
func NewToken() (raw, hash string, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return "", "", err
	}
	raw = base64.RawURLEncoding.EncodeToString(b)
	return raw, HashToken(raw), nil
}

// HashToken returns the hex SHA-256 of a raw token.
func HashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// dummyHash is compared against when a username does not exist, so login
// timing does not reveal whether an account is registered.
var dummyHash, _ = HashPassword("dummy-password-for-timing")

// WastePasswordTime performs a bcrypt comparison that always fails.
func WastePasswordTime(pw string) bool {
	_ = bcrypt.CompareHashAndPassword([]byte(dummyHash), []byte(pw))
	return false
}
