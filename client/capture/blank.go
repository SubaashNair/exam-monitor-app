package capture

import "image"

// isAllBlack samples a small grid of pixels and returns true if all are
// effectively black. Used to detect the macOS Screen Recording permission
// state — CoreGraphics returns a fully black image when TCC denies capture.
//
// Sampling rather than scanning every pixel keeps this O(1) regardless of
// resolution; 16 evenly-spaced points are enough to distinguish a black
// permission-denied frame from a real screen with even a single non-black
// pixel.
func isAllBlack(img image.Image) bool {
	if img == nil {
		return false
	}
	b := img.Bounds()
	if b.Dx() == 0 || b.Dy() == 0 {
		return false
	}
	for gy := 0; gy < 4; gy++ {
		for gx := 0; gx < 4; gx++ {
			x := b.Min.X + (b.Dx()*gx)/4 + b.Dx()/8
			y := b.Min.Y + (b.Dy()*gy)/4 + b.Dy()/8
			r, g, bl, _ := img.At(x, y).RGBA()
			if r > 0 || g > 0 || bl > 0 {
				return false
			}
		}
	}
	return true
}
