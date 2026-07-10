// Package auth handles token hashing and constant-time verification.
//
// The server only ever persists hashes of the webhook access token and the
// receiver identity token. Raw tokens are accepted from requests, hashed with
// the same algorithm, and compared in constant time. A token is never written
// to logs, audit rows, error responses, or the database.
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
)

// HashToken computes the stored representation of a raw token.
//
// When pepper is non-empty it is used as an HMAC-SHA256 key; otherwise a plain
// SHA-256 digest is produced. Both forms yield a lower-case hex string of
// fixed length, which keeps constant-time comparisons valid.
func HashToken(raw, pepper string) string {
	if raw == "" {
		return ""
	}
	if pepper == "" {
		sum := sha256.Sum256([]byte(raw))
		return hex.EncodeToString(sum[:])
	}
	mac := hmac.New(sha256.New, []byte(pepper))
	mac.Write([]byte(raw))
	return hex.EncodeToString(mac.Sum(nil))
}

// EqualHash reports whether two hex hash strings are equal using a
// constant-time comparison. The inputs are expected to be hashes produced by
// HashToken (same length); if lengths differ the result is always false.
func EqualHash(a, b string) bool {
	if len(a) == 0 || len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// Verify reports whether the raw token hashes to the stored hash. It always
// spends the same work regardless of the stored value length.
func Verify(raw, storedHash, pepper string) bool {
	return EqualHash(HashToken(raw, pepper), storedHash)
}

// WebhookToken is an in-memory representation of a persisted webhook token row.
type WebhookToken struct {
	TokenID  string
	Hash     string
	Enabled  bool
	Revoked  bool
}

// Checker authenticates webhook access tokens. It holds the small set of
// enabled, non-revoked token hashes in memory so every comparison is a
// constant-time hash-vs-hash check rather than a timing-sensitive lookup.
type Checker struct {
	tokens []WebhookToken
	pepper string
}

// NewChecker builds a Checker from the currently enabled, non-revoked tokens.
func NewChecker(tokens []WebhookToken, pepper string) *Checker {
	return &Checker{tokens: tokens, pepper: pepper}
}

// Replace swaps the in-memory token set. Used after bootstrap seeding.
func (c *Checker) Replace(tokens []WebhookToken) {
	c.tokens = tokens
}

// Authenticate returns the matching token for a raw bearer value, or
// ErrUnauthorized when no enabled, non-revoked token matches.
func (c *Checker) Authenticate(raw string) (*WebhookToken, error) {
	if raw == "" {
		return nil, ErrUnauthorized
	}
	candidate := HashToken(raw, c.pepper)
	var matched *WebhookToken
	for i := range c.tokens {
		t := &c.tokens[i]
		if EqualHash(candidate, t.Hash) && t.Enabled && !t.Revoked {
			// Continue iterating to keep the work roughly constant; with a
			// single bootstrap token this is effectively one comparison.
			matched = t
		}
	}
	if matched == nil {
		return nil, ErrUnauthorized
	}
	return matched, nil
}

// ErrUnauthorized is returned when no token matches.
var ErrUnauthorized = errors.New("unauthorized")
