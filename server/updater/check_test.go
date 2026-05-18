package updater

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIsNewer(t *testing.T) {
	cases := []struct {
		name          string
		local, remote string
		want          bool
	}{
		{"older patch", "0.1.5", "0.1.6", true},
		{"older minor", "0.1.5", "0.2.0", true},
		{"older major", "0.9.9", "1.0.0", true},
		{"equal", "0.1.6", "0.1.6", false},
		{"equal with v", "v0.1.6", "v0.1.6", false},
		{"mixed v prefix", "0.1.5", "v0.1.6", true},
		{"newer local", "0.2.0", "0.1.9", false},
		{"local is dev", "dev", "v0.1.6", false},
		{"local has -local suffix", "0.1.6-local", "0.1.7", false},
		{"remote is rc", "0.1.5", "v0.1.6-rc1", false},
		{"both malformed", "x.y.z", "a.b.c", false},
		{"remote has trailing whitespace", "0.1.5", "v0.1.6\n", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := IsNewer(tc.local, tc.remote)
			if got != tc.want {
				t.Errorf("IsNewer(%q, %q) = %v, want %v", tc.local, tc.remote, got, tc.want)
			}
		})
	}
}

func TestFetchLatest_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Accept"); got != "application/vnd.github+json" {
			t.Errorf("Accept header = %q, want application/vnd.github+json", got)
		}
		if got := r.Header.Get("User-Agent"); got == "" {
			t.Errorf("missing User-Agent header")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ReleaseInfo{
			TagName: "v0.1.9",
			HTMLURL: "https://example.test/releases/tag/v0.1.9",
			Body:    "release notes here",
		})
	}))
	defer srv.Close()

	rel, err := fetchLatestFrom(srv.URL)
	if err != nil {
		t.Fatalf("FetchLatest error: %v", err)
	}
	if rel.TagName != "v0.1.9" {
		t.Errorf("TagName = %q, want v0.1.9", rel.TagName)
	}
}

func TestFetchLatest_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "rate limited", http.StatusForbidden)
	}))
	defer srv.Close()

	if _, err := fetchLatestFrom(srv.URL); err == nil {
		t.Fatal("expected error on 403 response, got nil")
	}
}

func TestFetchLatest_MalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not json at all"))
	}))
	defer srv.Close()

	if _, err := fetchLatestFrom(srv.URL); err == nil {
		t.Fatal("expected decode error, got nil")
	}
}
