// Package session models a single exam session: its state machine, token,
// and lifecycle metadata. The token is a low-friction join secret; not a
// cryptographic credential — see spec §6.6.
package session

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"
	"time"
)

// tokenLetters is the uppercase alphabet minus O, I, L (ambiguous glyphs).
const tokenLetters = "ABCDEFGHJKMNPQRSTUVWXYZ"

// Token is the join secret a teacher gives students. Stored canonical
// (uppercase, no hyphens); displayed with hyphens; matched after Normalise.
//
// The zero value is not a valid Token; use Generate to obtain one.
type Token struct {
	canonical string
	expiresAt time.Time
}

// Generate creates a new random token valid for the given duration.
func Generate(validFor time.Duration) Token {
	var b strings.Builder
	b.Grow(9)
	for i := 0; i < 6; i++ {
		b.WriteByte(tokenLetters[randIndex(len(tokenLetters))])
	}
	for i := 0; i < 3; i++ {
		b.WriteByte(byte('0' + randIndex(10)))
	}
	return Token{
		canonical: b.String(),
		expiresAt: time.Now().UTC().Add(validFor),
	}
}

func randIndex(n int) int {
	x, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		// crypto/rand failure indicates a misconfigured system that cannot
		// generate secure randomness. Panic rather than silently degrade
		// to a time-derived value (the previous behaviour was a security
		// regression hidden behind a "rare" comment).
		panic(fmt.Errorf("session: crypto/rand failure: %w", err))
	}
	return int(x.Int64())
}

// String returns the display form: XXX-XXX-NNN.
func (t Token) String() string {
	if len(t.canonical) != 9 {
		return ""
	}
	return fmt.Sprintf("%s-%s-%s", t.canonical[0:3], t.canonical[3:6], t.canonical[6:9])
}

// Canonical returns the storage form: 9 chars, uppercase, alphanumeric.
func (t Token) Canonical() string { return t.canonical }

// Matches compares the input (after Normalise) to this token's canonical
// form.
func (t Token) Matches(input string) bool {
	return t.canonical != "" && Normalise(input) == t.canonical
}

// Expired returns true if the token's validity window has passed.
func (t Token) Expired() bool {
	return !time.Now().UTC().Before(t.expiresAt)
}

// ExpiresAt returns the absolute expiry instant.
func (t Token) ExpiresAt() time.Time { return t.expiresAt }

// Normalise strips whitespace and non-alphanumerics, then uppercases.
func Normalise(input string) string {
	var b strings.Builder
	b.Grow(len(input))
	for _, r := range input {
		switch {
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= 'a' && r <= 'z':
			b.WriteRune(r - 32)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		}
	}
	return b.String()
}
