package main

import "strings"

// slugify converts a human-readable string into a URL/filename-safe slug.
// Only ASCII letters and digits are kept; spaces, hyphens, and underscores
// become a single hyphen. The result is lower-cased, trimmed of leading/trailing
// hyphens, and capped at 32 characters.
func slugify(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	prevHyphen := true
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevHyphen = false
		case (r == ' ' || r == '-' || r == '_') && !prevHyphen:
			b.WriteRune('-')
			prevHyphen = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 32 {
		out = out[:32]
	}
	if out == "" {
		out = "untitled"
	}
	return out
}
