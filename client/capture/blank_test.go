package capture

import (
	"image"
	"image/color"
	"testing"
)

func newImage(t *testing.T, w, h int, fill color.Color) image.Image {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, fill)
		}
	}
	return img
}

func TestIsAllBlack(t *testing.T) {
	tests := []struct {
		name string
		img  image.Image
		want bool
	}{
		{"nil image", nil, false},
		{"zero size", image.NewRGBA(image.Rect(0, 0, 0, 0)), false},
		{"all black opaque", newImage(t, 100, 100, color.RGBA{0, 0, 0, 255}), true},
		{"all white", newImage(t, 100, 100, color.RGBA{255, 255, 255, 255}), false},
		{"all red", newImage(t, 100, 100, color.RGBA{255, 0, 0, 255}), false},
		{"all transparent black (alpha 0 still counts as black RGB)", newImage(t, 100, 100, color.RGBA{0, 0, 0, 0}), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isAllBlack(tt.img); got != tt.want {
				t.Errorf("isAllBlack() = %v; want %v", got, tt.want)
			}
		})
	}
}

func TestIsAllBlack_SingleNonBlackPixelAtSamplePointFlipsResult(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 100, 100))
	// Fill all black first.
	for y := 0; y < 100; y++ {
		for x := 0; x < 100; x++ {
			img.Set(x, y, color.RGBA{0, 0, 0, 255})
		}
	}
	if !isAllBlack(img) {
		t.Fatal("baseline: pre-fill should be all black")
	}
	// Center of the first sample quadrant — known to be sampled.
	img.Set(12, 12, color.RGBA{1, 0, 0, 255})
	if isAllBlack(img) {
		t.Error("expected isAllBlack to be false after a non-black sample pixel")
	}
}
