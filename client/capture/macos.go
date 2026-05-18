//go:build darwin

package capture

import (
	"fmt"
	"image"

	"github.com/kbinani/screenshot"
)

// New constructs the macOS Capturer. On macOS the only fragility is the TCC
// (Screen Recording) permission: without it, CGDisplayCreateImage returns a
// fully-black frame and gives no error. We detect that on the first non-error
// frame and convert it to ErrPermissionDenied.
func New() (Capturer, error) {
	if screenshot.NumActiveDisplays() == 0 {
		return nil, ErrNoDisplay
	}
	return &macCapturer{}, nil
}

type macCapturer struct {
	firstFrameChecked bool
	permissionDenied  bool
}

func (c *macCapturer) Capture() (image.Image, error) {
	if c.permissionDenied {
		return nil, ErrPermissionDenied
	}
	bounds := screenshot.GetDisplayBounds(0)
	img, err := screenshot.CaptureRect(bounds)
	if err != nil {
		return nil, fmt.Errorf("macos capture: %w", err)
	}
	if !c.firstFrameChecked {
		c.firstFrameChecked = true
		if isAllBlack(img) {
			c.permissionDenied = true
			return nil, ErrPermissionDenied
		}
	}
	return img, nil
}

func (c *macCapturer) Close() error { return nil }

func (c *macCapturer) Diagnostics() string {
	n := screenshot.NumActiveDisplays()
	state := "ok"
	if c.permissionDenied {
		state = "permission-denied"
	}
	return fmt.Sprintf("backend=macos displays=%d state=%s", n, state)
}
