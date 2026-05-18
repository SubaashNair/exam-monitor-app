// Package updater is the client-side update notifier. It queries GitHub's
// releases API on launch and reports whether a newer version is available,
// so the UI can prompt the user to download it manually.
//
// This package only CHECKS for updates and OPENS A BROWSER to the releases
// page. It does NOT self-replace or auto-download — those tiers were
// deliberately scoped out for v0.1.6 (see the discussion at tag time).
//
// The server-side implementation is identical and lives at
// github.com/exam-gaurd/server/updater. Keep both packages in lockstep.
package updater

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	// APIURL is the GitHub Releases API endpoint for the project. Override
	// in tests by reassigning before calling FetchLatest.
	APIURL = "https://api.github.com/repos/SubaashNair/exam-monitor-app/releases/latest"

	// ReleasesURL is the user-facing page for manual downloads.
	ReleasesURL = "https://github.com/SubaashNair/exam-monitor-app/releases/latest"

	httpTimeout = 5 * time.Second
)

// ReleaseInfo captures the subset of the GitHub API response we use.
type ReleaseInfo struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
	Body    string `json:"body"`
}

// FetchLatest queries GitHub's releases API for the latest published
// release. Returns an error on network/HTTP/decode failure; callers should
// silently swallow the error (don't bother the user with our network noise).
func FetchLatest() (*ReleaseInfo, error) {
	return fetchLatestFrom(APIURL)
}

func fetchLatestFrom(url string) (*ReleaseInfo, error) {
	client := &http.Client{Timeout: httpTimeout}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "exam-monitor-updater")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github api: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github api status %d", resp.StatusCode)
	}
	var rel ReleaseInfo
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, fmt.Errorf("decode release json: %w", err)
	}
	return &rel, nil
}

// IsNewer returns true if remote (e.g. "v0.1.6") is newer than local (e.g.
// "0.1.5"). Leading 'v' is tolerated on both sides. Non-numeric suffixes
// like "-local" or "-rc1" are considered "not a release version" and the
// function returns false conservatively — local dev builds never get
// prompted to "update" to themselves.
func IsNewer(local, remote string) bool {
	lp, lerr := parseVersion(local)
	rp, rerr := parseVersion(remote)
	if lerr != nil || rerr != nil {
		return false
	}
	for i := 0; i < 3; i++ {
		if rp[i] > lp[i] {
			return true
		}
		if rp[i] < lp[i] {
			return false
		}
	}
	return false
}

func parseVersion(s string) ([3]int, error) {
	var out [3]int
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "v")
	// Reject anything with hyphens (pre-release, '-local', '-rc1') or '+'
	// build metadata — we only compare strict release versions.
	if strings.ContainsAny(s, "-+") {
		return out, errors.New("not a release version")
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return out, errors.New("expected major.minor.patch")
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return out, fmt.Errorf("component %d: %w", i, err)
		}
		if n < 0 {
			return out, fmt.Errorf("component %d negative", i)
		}
		out[i] = n
	}
	return out, nil
}

// OpenBrowser opens url in the user's default browser, asynchronously
// (returns as soon as the helper command starts). Cross-platform.
func OpenBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	default:
		return fmt.Errorf("unsupported os: %s", runtime.GOOS)
	}
	return cmd.Start()
}
