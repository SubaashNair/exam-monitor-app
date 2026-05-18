package session

import (
	"strings"
	"testing"
	"time"
)

func TestToken_GenerateShape(t *testing.T) {
	tok := Generate(4 * time.Hour)
	got := tok.String() // display form
	if len(got) != 11 {
		t.Fatalf("display form should be 11 chars (3-3-3 + 2 hyphens), got %d in %q", len(got), got)
	}
	if got[3] != '-' || got[7] != '-' {
		t.Errorf("hyphens should be at positions 3 and 7, got %q", got)
	}
	for i := 0; i < 3; i++ {
		c := got[i]
		if !(c >= 'A' && c <= 'Z') {
			t.Errorf("first triplet position %d should be A-Z, got %c", i, c)
		}
		if c == 'O' || c == 'I' || c == 'L' {
			t.Errorf("first triplet position %d uses ambiguous letter %c", i, c)
		}
	}
	for i := 4; i < 7; i++ {
		c := got[i]
		if !(c >= 'A' && c <= 'Z') {
			t.Errorf("second triplet position %d should be A-Z, got %c", i, c)
		}
		if c == 'O' || c == 'I' || c == 'L' {
			t.Errorf("second triplet position %d uses ambiguous letter %c", i, c)
		}
	}
	for i := 8; i < 11; i++ {
		c := got[i]
		if !(c >= '0' && c <= '9') {
			t.Errorf("third triplet position %d should be 0-9, got %c", i, c)
		}
	}
}

func TestToken_CanonicalIsUppercaseAlnumOnly(t *testing.T) {
	tok := Generate(time.Hour)
	canon := tok.Canonical()
	if len(canon) != 9 {
		t.Fatalf("canonical form should be 9 chars, got %d in %q", len(canon), canon)
	}
	for _, c := range canon {
		if !((c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')) {
			t.Errorf("canonical contains non-alphanumeric: %c", c)
		}
	}
}

func TestToken_Normalise(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"hyphenated mixed case", "bio-xq7-394", "BIOXQ7394"},
		{"no hyphens", "BIOXQ7394", "BIOXQ7394"},
		{"with spaces", "Bio Xq7 394", "BIOXQ7394"},
		{"trailing whitespace", "  BIO-XQ7-394  ", "BIOXQ7394"},
		{"empty", "", ""},
		{"weird punctuation", "BIO.XQ7/394", "BIOXQ7394"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := Normalise(tt.input); got != tt.want {
				t.Errorf("Normalise(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestToken_Matches(t *testing.T) {
	tok := Generate(time.Hour)
	display := tok.String()
	canon := tok.Canonical()

	for _, input := range []string{display, canon, strings.ToLower(display), strings.ReplaceAll(display, "-", " ")} {
		if !tok.Matches(input) {
			t.Errorf("Matches(%q) returned false; want true", input)
		}
	}

	if tok.Matches("AAA-AAA-000") {
		t.Error("Matches should reject a non-matching string")
	}
}

func TestToken_Expiry(t *testing.T) {
	tok := Generate(50 * time.Millisecond)
	if tok.Expired() {
		t.Fatal("token should not be expired immediately")
	}
	time.Sleep(80 * time.Millisecond)
	if !tok.Expired() {
		t.Error("token should be expired after sleeping past expiry")
	}
}

func TestToken_Uniqueness(t *testing.T) {
	seen := make(map[string]bool, 1000)
	for i := 0; i < 1000; i++ {
		c := Generate(time.Hour).Canonical()
		if seen[c] {
			t.Fatalf("collision after %d generations: %s", i, c)
		}
		seen[c] = true
	}
}
