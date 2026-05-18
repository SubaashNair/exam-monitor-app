//go:build linux

package capture

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/png" // needed for tool-shellout PNG output decoding
	"os"
	"os/exec"
	"time"

	"github.com/kbinani/screenshot"
)

// New picks an X11 or Wayland backend at runtime. Wayland uses tool shell-out
// (grim → gnome-screenshot → spectacle); X11 uses the kbinani/screenshot XShm
// path.
func New() (Capturer, error) {
	sessionType := os.Getenv("XDG_SESSION_TYPE")
	backend, toolPath, err := PickLinuxBackend(sessionType, exec.LookPath)
	if err != nil {
		return nil, err
	}
	switch backend {
	case BackendX11:
		if screenshot.NumActiveDisplays() == 0 {
			return nil, ErrNoDisplay
		}
		return &linuxX11Capturer{}, nil
	case BackendWaylandGrim, BackendWaylandGnomeScreenshot, BackendWaylandSpectacle:
		return &linuxWaylandCapturer{backend: backend, toolPath: toolPath}, nil
	default:
		return nil, fmt.Errorf("capture: no backend for session %q", sessionType)
	}
}

// linuxX11Capturer uses the kbinani XShm path. Identical to the macOS path
// (kbinani handles both via build tags), so no fancy logic here.
type linuxX11Capturer struct{}

func (c *linuxX11Capturer) Capture() (image.Image, error) {
	bounds := screenshot.GetDisplayBounds(0)
	img, err := screenshot.CaptureRect(bounds)
	if err != nil {
		return nil, fmt.Errorf("linux x11 capture: %w", err)
	}
	return img, nil
}

func (c *linuxX11Capturer) Close() error { return nil }

func (c *linuxX11Capturer) Diagnostics() string {
	return fmt.Sprintf("backend=linux-x11 displays=%d", screenshot.NumActiveDisplays())
}

// linuxWaylandCapturer shells out once per frame to a screenshot tool whose
// stdout we pipe back through stdlib image decoding. ~30-60ms per frame on a
// typical laptop; safely under the 500ms capture cadence.
type linuxWaylandCapturer struct {
	backend  Backend
	toolPath string
}

func (c *linuxWaylandCapturer) Capture() (image.Image, error) {
	// Cap per-frame at 2s to avoid stalling the capture loop on a hung tool.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	args := c.commandArgs()
	cmd := exec.CommandContext(ctx, c.toolPath, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%s: %w (stderr=%q)", c.backend, err, stderr.String())
	}
	img, _, err := image.Decode(&stdout)
	if err != nil {
		return nil, fmt.Errorf("%s: decode: %w", c.backend, err)
	}
	return img, nil
}

func (c *linuxWaylandCapturer) commandArgs() []string {
	switch c.backend {
	case BackendWaylandGrim:
		// `grim -` writes PNG to stdout by default.
		return []string{"-"}
	case BackendWaylandGnomeScreenshot:
		// gnome-screenshot: write to /dev/stdout, type=png implicit.
		return []string{"--file=/dev/stdout"}
	case BackendWaylandSpectacle:
		// spectacle -bn = background, no-notify; -o = output file. /dev/stdout works on Linux.
		return []string{"-bn", "-o", "/dev/stdout"}
	default:
		return nil
	}
}

func (c *linuxWaylandCapturer) Close() error { return nil }

func (c *linuxWaylandCapturer) Diagnostics() string {
	return fmt.Sprintf("backend=%s tool=%s", c.backend, c.toolPath)
}
