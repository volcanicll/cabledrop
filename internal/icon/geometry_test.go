package icon

import (
	"bytes"
	"image"
	"image/png"
	"testing"
)

// TestAppIconGeometry checks the generated artwork is centred and fully inside
// its canvas at every size the project ships.
//
// The plate is drawn in app units on a fixed-size mask, so any given output
// size exercises a different resampling path. A size that clips the rounded
// corners, or shifts the art off-centre, is a real defect on a launcher.
func TestAppIconGeometry(t *testing.T) {
	for _, size := range []int{16, 32, 48, 64, 72, 96, 128, 144, 192, 256, 512, 1024} {
		img := decode(t, AppIconPNG(size))
		left := marginX(img, false)
		right := marginX(img, true)
		top := marginY(img, false)
		bottom := marginY(img, true)

		if size >= 32 && (left == 0 || right == 0 || top == 0 || bottom == 0) {
			t.Errorf("size %d: artwork touches the canvas edge (L%d R%d T%d B%d)",
				size, left, right, top, bottom)
		}
		if d := abs(left - right); d > 1 {
			t.Errorf("size %d: not horizontally centred (L%d R%d)", size, left, right)
		}
		if d := abs(top - bottom); d > 1 {
			t.Errorf("size %d: not vertically centred (T%d B%d)", size, top, bottom)
		}
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func decode(t *testing.T, data []byte) image.Image {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return img
}

// opaque reports whether the pixel has any coverage at all.
func opaque(img image.Image, x, y int) bool {
	_, _, _, a := img.At(x, y).RGBA()
	return a > 0x2000 // ~3%
}

func marginX(img image.Image, fromRight bool) int {
	b := img.Bounds()
	for step := 0; step < b.Dx(); step++ {
		x := step
		if fromRight {
			x = b.Dx() - 1 - step
		}
		for y := 0; y < b.Dy(); y++ {
			if opaque(img, x, y) {
				return step
			}
		}
	}
	return b.Dx()
}

func marginY(img image.Image, fromBottom bool) int {
	b := img.Bounds()
	for step := 0; step < b.Dy(); step++ {
		y := step
		if fromBottom {
			y = b.Dy() - 1 - step
		}
		for x := 0; x < b.Dx(); x++ {
			if opaque(img, x, y) {
				return step
			}
		}
	}
	return b.Dy()
}
