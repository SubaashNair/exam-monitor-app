// Package updater is the server-side update notifier. See the matching
// client/updater package for the design notes — both implementations are
// kept identical so a single bug fix or API change touches both lockstep.
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
	APIURL      = "https://api.github.com/repos/SubaashNair/exam-monitor-app/releases/latest"
	ReleasesURL = "https://github.com/SubaashNair/exam-monitor-app/releases/latest"

	httpTimeout = 5 * time.Second
)

type ReleaseInfo struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
	Body    string `json:"body"`
}

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
