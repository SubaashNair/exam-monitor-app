package main

import (
	"fmt"
	"hash/fnv"
	"os/exec"
	"runtime"
	"strings"
)

// RoomNameToPort maps a room name (case-insensitive, trimmed) to a stable
// TCP/UDP port number. Named presets get fixed well-known ports; arbitrary
// strings are hashed deterministically into the 12000–61999 range.
//
// IMPORTANT: the server's util.go MUST contain an identical implementation.
// Both sides convert the room name to a port independently, and they must
// arrive at the same number or the student's client won't reach the server.
func RoomNameToPort(name string) int {
	n := strings.ToLower(strings.TrimSpace(name))
	switch n {
	case "alpha":
		return 12001
	case "bravo":
		return 12002
	case "charlie":
		return 12003
	case "delta":
		return 12004
	case "":
		return 12000
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(n))
	return 12000 + int(h.Sum32()%50000) // 12000–61999
}

// CopyToClipboard writes text to the system clipboard. Returns an error if
// no supported clipboard utility is available.
func CopyToClipboard(text string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("pbcopy")
	case "windows":
		cmd = exec.Command("cmd", "/c", "clip")
	case "linux":
		if _, err := exec.LookPath("wl-copy"); err == nil {
			cmd = exec.Command("wl-copy")
		} else if _, err := exec.LookPath("xclip"); err == nil {
			cmd = exec.Command("xclip", "-selection", "clipboard")
		} else {
			return fmt.Errorf("install wl-copy or xclip to enable clipboard")
		}
	default:
		return fmt.Errorf("clipboard not supported on %s", runtime.GOOS)
	}
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}
