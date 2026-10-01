package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
)

// Icons are drawn in code rather than shipped as PNGs.
//
// A menu bar icon is 18 points tall, so only a very simple shape reads at all,
// and there are two states to show. Drawing them here keeps binary assets out
// of the repository and makes the "no phone" state impossible to forget.
//
// The same rasteriser produces the application icon for the Dock and for the
// Windows .exe, so a release has no artwork to keep in sync.
const (
	iconUnits = 36 // logical units for the tray glyph
	appUnits  = 64 // logical units for the square application icon
	ssScale   = 4  // draw big, average down: cheap anti-aliasing
)

type point struct{ x, y float64 }

// trayIcon returns a PNG for the current connection state.
//
// macOS template icons use only the alpha channel — the system tints the shape
// for light and dark menu bars — so the mask is drawn in alpha, RGB left at
// zero. Windows and Linux tint these too, or render them as-is against their
// own tray background.
func trayIcon(connected bool) []byte {
	m := newMask(iconUnits)

	// Two arrows pointing opposite ways: the plain "transfer" shape.
	m.fillRect(7, 10, 21, 13, true)
	m.fillTriangle(point{21, 7}, point{21, 16}, point{30, 11.5}, true)

	m.fillRect(15, 23, 29, 26, true)
	m.fillTriangle(point{15, 20}, point{15, 29}, point{6, 24.5}, true)

	if !connected {
		// A slash, knocked out of the arrows rather than laid over them, so
		// the gap reads as a break instead of two crossing strokes.
		m.stroke(5, 31, 31, 5, 3.0, false)
		m.stroke(5, 31, 31, 5, 1.4, true)
	}

	return m.png(36)
}

// appIconPNG is the coloured application icon at the given pixel size.
func appIconPNG(pixels int) []byte {
	const (
		bgR, bgG, bgB = 0x0a, 0x84, 0xff // the accent blue used throughout the UI
	)

	// Rounded square background.
	bg := newMask(appUnits)
	bg.fillPolyRounded(3, 3, appUnits-3, appUnits-3, 14, true)

	// Arrows, scaled to sit inside it.
	fg := newMask(appUnits)
	fg.fillRect(14, 22, 40, 27, true)
	fg.fillTriangle(point{40, 17}, point{40, 32}, point{53, 24.5}, true)

	fg.fillRect(24, 39, 50, 44, true)
	fg.fillTriangle(point{24, 34}, point{24, 49}, point{11, 41.5}, true)

	img := image.NewRGBA(image.Rect(0, 0, pixels, pixels))
	for y := 0; y < pixels; y++ {
		for x := 0; x < pixels; x++ {
			a := bg.coverage(x, y, pixels)
			f := fg.coverage(x, y, pixels)
			alpha := math.Max(a, f)
			if alpha <= 0.002 {
				continue
			}
			// White arrows over the blue field.
			r := bgR + (255-bgR)*f
			g := bgG + (255-bgG)*f
			b := bgB + (255-bgB)*f
			img.SetRGBA(x, y, color.RGBA{
				R: uint8(math.Round(r)),
				G: uint8(math.Round(g)),
				B: uint8(math.Round(b)),
				A: uint8(math.Round(math.Min(1, alpha) * 255)),
			})
		}
	}
	return encodePNG(img)
}

// --- rasteriser ----------------------------------------------------------

// mask is an alpha coverage buffer in canvas space.
type mask struct {
	units int // logical size the drawing coordinates refer to
	n     int // buffer side, units*ssScale
	buf   []float64
}

func newMask(units int) *mask {
	n := units * ssScale
	return &mask{units: units, n: n, buf: make([]float64, n*n)}
}

// s maps a drawing coordinate into canvas pixels.
func (m *mask) s(v float64) float64 { return v / float64(m.units) * float64(m.n) }

func (m *mask) ink(x, y int, a float64) {
	if x < 0 || y < 0 || x >= m.n || y >= m.n || a <= 0 {
		return
	}
	if a > 1 {
		a = 1
	}
	i := y*m.n + x
	m.buf[i] = m.buf[i] + a - m.buf[i]*a // source-over
}

func (m *mask) fillRect(x0, y0, x1, y1 float64, ink bool) {
	m.fillPoly([]point{{x0, y0}, {x1, y0}, {x1, y1}, {x0, y1}}, ink)
}

func (m *mask) fillTriangle(a, b, c point, ink bool) {
	m.fillPoly([]point{a, b, c}, ink)
}

// fillPolyRounded fills a rectangle with rounded corners. The corners are
// approximated with a handful of segments, which is plenty at icon scale.
func (m *mask) fillPolyRounded(x0, y0, x1, y1, r float64, ink bool) {
	const steps = 8
	var pts []point
	corners := []struct{ cx, cy, start float64 }{
		{x1 - r, y0 + r, -90}, // top right
		{x1 - r, y1 - r, 0},   // bottom right
		{x0 + r, y1 - r, 90},  // bottom left
		{x0 + r, y0 + r, 180}, // top left
	}
	for _, c := range corners {
		for i := 0; i <= steps; i++ {
			a := (c.start + 90*float64(i)/steps) * math.Pi / 180
			pts = append(pts, point{c.cx + r*math.Cos(a), c.cy + r*math.Sin(a)})
		}
	}
	m.fillPoly(pts, ink)
}

// fillPoly scanline-fills a polygon.
//
// Vertical coverage is 4× oversampled and horizontal coverage computed
// exactly, which is enough to keep these blocky shapes clean without pulling
// in a general rasteriser.
func (m *mask) fillPoly(pts []point, ink bool) {
	scaled := make([]point, len(pts))
	for i, p := range pts {
		scaled[i] = point{m.s(p.x), m.s(p.y)}
	}
	m.scanFill(scaled, ink)
}

func (m *mask) scanFill(scaled []point, ink bool) {
	minY, maxY := scaled[0].y, scaled[0].y
	for _, p := range scaled {
		minY = math.Min(minY, p.y)
		maxY = math.Max(maxY, p.y)
	}

	const sub = 4
	for y := int(math.Floor(minY)); y <= int(math.Ceil(maxY)); y++ {
		for s := 0; s < sub; s++ {
			xs := crossings(scaled, float64(y)+(float64(s)+0.5)/sub)
			for i := 0; i+1 < len(xs); i += 2 {
				x0, x1 := xs[i], xs[i+1]
				for x := int(math.Floor(x0)); x < int(math.Ceil(x1)); x++ {
					if !ink {
						// Erasing clears the whole pixel any sub-span touches:
						// that is what a knock-out needs.
						if x >= 0 && y >= 0 && x < m.n && y < m.n {
							m.buf[y*m.n+x] = 0
						}
						continue
					}
					lo := math.Max(x0, float64(x))
					hi := math.Min(x1, float64(x+1))
					if hi > lo {
						m.ink(x, y, (hi-lo)/sub)
					}
				}
			}
		}
	}
}

// stroke paints (or clears) a band of the given half-width along a segment.
func (m *mask) stroke(x0, y0, x1, y1, halfWidth float64, ink bool) {
	ax, ay := m.s(x0), m.s(y0)
	bx, by := m.s(x1), m.s(y1)
	hw := halfWidth / float64(m.units) * float64(m.n)

	dx, dy := bx-ax, by-ay
	length := math.Hypot(dx, dy)
	if length == 0 {
		return
	}
	// Unit normal, so the segment becomes a rectangle.
	nx, ny := -dy/length*hw, dx/length*hw

	m.scanFill([]point{
		{ax + nx, ay + ny},
		{bx + nx, by + ny},
		{bx - nx, by - ny},
		{ax - nx, ay - ny},
	}, ink)
}

// crossings returns the x where a horizontal line meets the polygon edges,
// sorted, so spans can be taken pairwise.
func crossings(pts []point, y float64) []float64 {
	var xs []float64
	n := len(pts)
	for i := 0; i < n; i++ {
		a, b := pts[i], pts[(i+1)%n]
		if (a.y <= y && b.y > y) || (b.y <= y && a.y > y) {
			t := (y - a.y) / (b.y - a.y)
			xs = append(xs, a.x+t*(b.x-a.x))
		}
	}
	// Insertion sort: there are only ever a couple of crossings.
	for i := 1; i < len(xs); i++ {
		for j := i; j > 0 && xs[j] < xs[j-1]; j-- {
			xs[j], xs[j-1] = xs[j-1], xs[j]
		}
	}
	return xs
}

// coverage averages the supersampled buffer down to one output pixel.
func (m *mask) coverage(x, y, outSize int) float64 {
	f := m.n / outSize
	area := float64(f * f)
	var sum float64
	for dy := 0; dy < f; dy++ {
		for dx := 0; dx < f; dx++ {
			sum += m.buf[(y*f+dy)*m.n+(x*f+dx)]
		}
	}
	return sum / area
}

// png renders the mask as a black icon with the mask in its alpha channel.
func (m *mask) png(size int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			a := m.coverage(x, y, size)
			if a <= 0.002 {
				continue
			}
			img.SetRGBA(x, y, color.RGBA{A: uint8(math.Round(math.Min(1, a) * 255))})
		}
	}
	return encodePNG(img)
}

func encodePNG(img image.Image) []byte {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil
	}
	return buf.Bytes()
}
