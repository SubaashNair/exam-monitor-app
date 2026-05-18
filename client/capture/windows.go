//go:build windows

package capture

import (
	"fmt"
	"image"
	"os"

	"github.com/kbinani/screenshot"
)

// New constructs the Windows Capturer. Uses kbinani DXGI path with explicit
// multi-monitor support. We also detect RDP / multi-session environments
// where DXGI commonly returns black frames; in that case we log but still
// attempt capture (some setups work, others don't — instructor will see the
// black tile and follow up).
func New() (Capturer, error) {
	n := screenshot.NumActiveDisplays()
	if n == 0 {
		return nil, ErrNoDisplay
	}
	monitor := monitorFromEnv(n)
	return &windowsCapturer{
		monitor:   monitor,
		displays:  n,
		rdpActive: isRemoteSession(),
	}, nil
}

type windowsCapturer struct {
	monitor   int
	displays  int
	rdpActive bool
}

func (c *windowsCapturer) Capture() (image.Image, error) {
	bounds := screenshot.GetDisplayBounds(c.monitor)
	img, err := screenshot.CaptureRect(bounds)
	if err != nil {
		return nil, fmt.Errorf("windows capture: %w", err)
	}
	return img, nil
}

func (c *windowsCapturer) Close() error { return nil }

func (c *windowsCapturer) Diagnostics() string {
	rdp := ""
	if c.rdpActive {
		rdp = " rdp=true"
	}
	return fmt.Sprintf("backend=windows monitor=%d/%d%s", c.monitor, c.displays, rdp)
}

// monitorFromEnv reads the user's preferred monitor index from the
// EXAM_MONITOR_DISPLAY env var, falling back to 0. Clamped to the available
// range. This is the minimum-viable multi-monitor knob; a proper UI picker
// can be added later by writing the same env-equivalent into persistence.
func monitorFromEnv(displays int) int {
	raw := os.Getenv("EXAM_MONITOR_DISPLAY")
	if raw == "" {
		return 0
	}
	var n int
	if _, err := fmt.Sscanf(raw, "%d", &n); err != nil || n < 0 {
		return 0
	}
	if n >= displays {
		return displays - 1
	}
	return n
}

// isRemoteSession returns true if the process is running inside an RDP /
// Remote Desktop session. Detected via SESSIONNAME env var which Terminal
// Services sets to "RDP-Tcp#..." for remote logons.
func isRemoteSession() bool {
	name := os.Getenv("SESSIONNAME")
	return len(name) >= 4 && name[:4] == "RDP-"
}
