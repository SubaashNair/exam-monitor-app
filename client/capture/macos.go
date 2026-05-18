//go:build darwin

package capture

import (
	"fmt"
	"image"
	"image/draw"

	"github.com/kbinani/screenshot"
)

// New constructs the macOS Capturer. On macOS the only fragility is the TCC
// (Screen Recording) permission: without it, CGDisplayCreateImage returns a
// fully-black frame and gives no error. We detect that on the first non-error
// frame and convert it to ErrPermissionDenied.
//
// Multi-display: all active displays are composited into a single image at
// their physical layout positions, so a student can't hide content by
// dragging it to a secondary monitor.
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

	n := screenshot.NumActiveDisplays()
	if n == 0 {
		return nil, ErrNoDisplay
	}

	// Single-display fast path (matches pre-v0.1.8 behaviour exactly).
	if n == 1 {
		img, err := screenshot.CaptureRect(screenshot.GetDisplayBounds(0))
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

	// Multi-display: compute the union of all display bounds (macOS allows
	// negative origins, e.g. a monitor to the left of the primary), then
	// composite each display at its offset within the union.
	union := screenshot.GetDisplayBounds(0)
	for i := 1; i < n; i++ {
		union = union.Union(screenshot.GetDisplayBounds(i))
	}
	canvas := image.NewRGBA(image.Rect(0, 0, union.Dx(), union.Dy()))

	var firstSub image.Image
	for i := 0; i < n; i++ {
		b := screenshot.GetDisplayBounds(i)
		sub, err := screenshot.CaptureRect(b)
		if err != nil {
			// Skip individual display failures — better to ship a
			// partial composite than fail the whole frame.
			continue
		}
		if firstSub == nil {
			firstSub = sub
		}
		offset := b.Min.Sub(union.Min)
		target := image.Rect(offset.X, offset.Y, offset.X+b.Dx(), offset.Y+b.Dy())
		draw.Draw(canvas, target, sub, sub.Bounds().Min, draw.Src)
	}

	if !c.firstFrameChecked {
		c.firstFrameChecked = true
		if firstSub == nil || isAllBlack(firstSub) {
			c.permissionDenied = true
			return nil, ErrPermissionDenied
		}
	}
	return canvas, nil
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
