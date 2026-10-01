package icon

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
)

// Icons are drawn in code rather than shipped as PNGs.
//
// The artwork — a cable bent into a U with an arrow entering one end and
// leaving the other — is defined here as path data, the same way it would be
// in a vector editor, and rasterised by the small rasteriser below. Drawing
// them at runtime keeps binary assets out of the repository, means the tray
// states can never drift from the app icon, and lets small sizes simplify
// their own detail instead of needing a hand-tuned variant per size.
//
// The artwork: one shape, "data goes in here, comes out there".
const (
	iconUnits = 36 // logical units for the tray glyph
	appUnits  = 64 // logical units for the square application icon
	ssScale   = 4  // draw big, average down: cheap anti-aliasing
)

// --- path data ------------------------------------------------------------

type point struct{ x, y float64 }

// Path is a set of subpaths in logical drawing units, flattened into
// polylines as it is built. Everything the icons need — lines, rounded
// corners, curves — is expressed through it.
type Path struct {
	subs [][]point
	cur  point
	open bool
}

// Move starts a new subpath at (x, y).
func (p *Path) Move(x, y float64) {
	p.subs = append(p.subs, []point{{x, y}})
	p.cur = point{x, y}
	p.open = true
}

// Line appends a straight segment to (x, y).
func (p *Path) Line(x, y float64) {
	if !p.open {
		p.Move(x, y)
		return
	}
	p.cur = point{x, y}
	p.append(p.cur)
}

// Cubic appends a cubic bézier from the current point through the two control
// points to (x, y), flattened finely enough that no chord is visible at icon
// scale (4× supersampling of a 64-unit canvas puts a segment well under a
// quarter output pixel).
func (p *Path) Cubic(c1x, c1y, c2x, c2y, x, y float64) {
	if !p.open {
		p.Move(c1x, c1y)
	}
	p0 := p.cur
	const steps = 24
	for i := 1; i <= steps; i++ {
		t := float64(i) / steps
		mt := 1 - t
		px := mt*mt*mt*p0.x + 3*mt*mt*t*c1x + 3*mt*t*t*c2x + t*t*t*x
		py := mt*mt*mt*p0.y + 3*mt*mt*t*c1y + 3*mt*t*t*c2y + t*t*t*y
		p.cur = point{px, py}
		p.append(p.cur)
	}
}

func (p *Path) append(pt point) {
	p.subs[len(p.subs)-1] = append(p.subs[len(p.subs)-1], pt)
}

// roundedRectPath is a rectangle with rounded corners, built from cubics —
// the standard kappa-approximated arc.
func roundedRectPath(x0, y0, x1, y1, r float64) *Path {
	const k = 0.5523 // circle constant: a kappa-length control handle
	p := &Path{}
	p.Move(x0+r, y0)
	p.Line(x1-r, y0)
	p.Cubic(x1-r+k*r, y0, x1, y0+r-k*r, x1, y0+r)
	p.Line(x1, y1-r)
	p.Cubic(x1, y1-r+k*r, x1-r+k*r, y1, x1-r, y1)
	p.Line(x0+r, y1)
	p.Cubic(x0+r-k*r, y1, x0, y1-r+k*r, x0, y1-r)
	p.Line(x0, y0+r)
	p.Cubic(x0, y0+r-k*r, x0+r-k*r, y0, x0+r, y0)
	return p
}

func circlePath(cx, cy, r float64) *Path {
	const steps = 28
	p := &Path{}
	for i := 0; i <= steps; i++ {
		a := 2 * math.Pi * float64(i) / steps
		x, y := cx+r*math.Cos(a), cy+r*math.Sin(a)
		if i == 0 {
			p.Move(x, y)
		} else {
			p.Line(x, y)
		}
	}
	return p
}

// --- the CableDrop artwork ------------------------------------------------

// appCablePath returns the cable as a stroked centerline, plus the two arrow
// heads as filled polygons, in the 64-unit app canvas.
//
// The centerline is a U: down the left arm, around the bottom, up the right
// arm. The left arrow points down into the cable, the right one points up out
// of it — one in, one out.
func appCablePath(stroke float64) (center *Path, arrows *Path) {
	const (
		leftX  = 22.0
		rightX = 42.0
		botY   = 47.0 // bottom of the U, centre of the turn
		armTop = 28.0 // where the arms stop to leave room for the arrows
	)

	center = &Path{}
	center.Move(leftX, armTop)
	center.Line(leftX, botY-9)
	// Bottom turn: arms run vertically into the corners, a single cubic
	// carries the U across.
	center.Cubic(leftX, botY, rightX, botY, rightX, botY-9)
	center.Line(rightX, armTop)

	// Arrow heads. Width ~2.6× the stroke; tips reach just over the arm ends
	// so head and cable touch without growing together.
	arrows = &Path{}
	headW, headD := stroke*2.6, stroke*1.9
	// Left: pointing down, into the cable. Tip at the bottom of the head.
	tipY := armTop + 3
	baseY := tipY - headD
	arrows.Move(leftX-headW/2, baseY)
	arrows.Line(leftX+headW/2, baseY)
	arrows.Line(leftX, tipY)
	// Right: pointing up, out of the cable. Same shape, flipped.
	baseY = armTop - 3
	tipY = baseY - headD
	arrows.Move(rightX-headW/2, baseY)
	arrows.Line(rightX+headW/2, baseY)
	arrows.Line(rightX, tipY)
	return center, arrows
}

// trayCablePath is the same artwork on the 36-unit tray canvas, minus detail
// that would not survive 9 px: the heads sit directly on the arms.
//
// broken splits the bottom of the U in the middle — the cable unplugged — and
// nudges the right half outward so the gap reads as a pull, not a crop.
func trayCablePath(stroke float64, broken bool) (center *Path, arrows *Path) {
	const (
		leftX  = 12.0
		rightX = 24.0
		botY   = 26.0
		armTop = 13.0
	)
	midX := (leftX + rightX) / 2

	center = &Path{}
	center.Move(leftX, armTop)
	center.Line(leftX, botY-4.5)
	center.Cubic(leftX, botY, midX, botY+2.5, midX, botY+2.5)
	if broken {
		// Left half stops at the middle with a round cap. The right half is
		// drawn as its own path, dropped and pushed out a touch — pulled
		// apart rather than clipped.
	} else {
		center.Cubic(midX, botY+2.5, rightX, botY, rightX, botY-4.5)
		center.Line(rightX, armTop)
	}

	if broken {
		offX, offY := 1.8, 2.4
		center.Move(rightX+offX, armTop+offY)
		center.Line(rightX+offX, botY-4.5+offY)
		center.Cubic(rightX+offX, botY+offY, midX+offX*1.6, botY+3.6+offY, midX+1.1, botY+3.2+offY)
	}

	headW, headD := stroke*2.6, stroke*1.8
	arrows = &Path{}
	tipY := armTop + 2.6
	baseY := tipY - headD
	arrows.Move(leftX-headW/2, baseY)
	arrows.Line(leftX+headW/2, baseY)
	arrows.Line(leftX, tipY)
	tipY = armTop + offRight(broken) - 2.6 - headD
	baseY = armTop + offRight(broken) - 2.6
	arrows.Move(rightX+offRight(broken)-headW/2, baseY)
	arrows.Line(rightX+offRight(broken)+headW/2, baseY)
	arrows.Line(rightX+offRight(broken), tipY)
	return center, arrows
}

func offRight(broken bool) float64 {
	if broken {
		return 1.6
	}
	return 0
}

// TrayIcon returns a PNG for the current connection state.
//
// macOS template icons use only the alpha channel — the system tints the shape
// for light and dark menu bars — so the mask is drawn in alpha, RGB left at
// zero.
func TrayIcon(connected bool) []byte {
	m := newMask(iconUnits)
	const stroke = 3.4
	center, arrows := trayCablePath(stroke, !connected)
	m.strokePath(center, stroke, true)
	m.fillPath(arrows, true)
	return m.png(36)
}

// AppIconPNG is the coloured application icon at the given pixel size.
//
// A vertical gradient field (the brand blue, lit from above) with the cable
// knocked out in white. Small sizes thicken the stroke a little so the cable
// does not dissolve into the anti-aliasing.
func AppIconPNG(pixels int) []byte {
	stroke := 6.0
	if pixels < 48 {
		stroke = 7.0
	}

	bg := newMask(appUnits)
	bg.fillPath(roundedRectPath(3, 3, appUnits-3, appUnits-3, 14.5), true)

	fg := newMask(appUnits)
	center, arrows := appCablePath(stroke)
	fg.strokePath(center, stroke, true)
	fg.fillPath(arrows, true)

	// The gradient runs the full height of the canvas.
	top := [3]float64{0x3c, 0x9e, 0xff}
	bottom := [3]float64{0x06, 0x5f, 0xd6}

	img := image.NewNRGBA(image.Rect(0, 0, pixels, pixels))
	for y := 0; y < pixels; y++ {
		t := (float64(y) + 0.5) / float64(pixels)
		br := lerp(top[0], bottom[0], t)
		bgc := lerp(top[1], bottom[1], t)
		bb := lerp(top[2], bottom[2], t)
		for x := 0; x < pixels; x++ {
			b := bg.coverage(x, y, pixels)
			f := fg.coverage(x, y, pixels)
			alpha := math.Max(b, f)
			if alpha <= 0.002 {
				continue
			}
			// White cable over the gradient field.
			r := br + (255-br)*f
			g := bgc + (255-bgc)*f
			bl := bb + (255-bb)*f
			img.SetNRGBA(x, y, color.NRGBA{
				R: uint8(math.Round(r)),
				G: uint8(math.Round(g)),
				B: uint8(math.Round(bl)),
				A: uint8(math.Round(math.Min(1, alpha) * 255)),
			})
		}
	}
	return encodePNG(img)
}

func lerp(a, b, t float64) float64 { return a + (b-a)*t }

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

// fillPath fills every subpath of p.
func (m *mask) fillPath(p *Path, ink bool) {
	for _, sub := range p.subs {
		if len(sub) < 3 {
			continue
		}
		scaled := make([]point, len(sub))
		for i, pt := range sub {
			scaled[i] = point{m.s(pt.x), m.s(pt.y)}
		}
		m.scanFill(scaled, ink)
	}
}

// strokePath paints a round-capped, round-joined stroke of the given width
// along p. Flattened segments become quads, and a disc at every vertex covers
// the joins and caps — on a curve, disc-overlap is what makes consecutive
// segments read as one continuous stroke.
func (m *mask) strokePath(p *Path, width float64, ink bool) {
	hw := m.s(width / 2)
	for _, sub := range p.subs {
		if len(sub) < 2 {
			continue
		}
		prev := point{m.s(sub[0].x), m.s(sub[0].y)}
		for i := 1; i < len(sub); i++ {
			cur := point{m.s(sub[i].x), m.s(sub[i].y)}
			m.strokeSeg(prev, cur, hw, ink)
			prev = cur
		}
		for _, pt := range sub {
			m.fillPath(circlePath(pt.x, pt.y, width/2), ink)
		}
	}
}

func (m *mask) strokeSeg(a, b point, hw float64, ink bool) {
	dx, dy := b.x-a.x, b.y-a.y
	length := math.Hypot(dx, dy)
	if length == 0 {
		return
	}
	nx, ny := -dy/length*hw, dx/length*hw
	m.scanFill([]point{
		{a.x + nx, a.y + ny},
		{b.x + nx, b.y + ny},
		{b.x - nx, b.y - ny},
		{a.x - nx, a.y - ny},
	}, ink)
}

// scanFill scanline-fills a polygon.
//
// Vertical coverage is 4× oversampled and horizontal coverage computed
// exactly. The sub-row contributions of one polygon are unioned additively
// into the row before being committed: compositing them one by one would cap
// any solid interior at 1-(1-¼)⁴ ≈ 68% ink.
func (m *mask) scanFill(scaled []point, ink bool) {
	minY, maxY := scaled[0].y, scaled[0].y
	for _, p := range scaled {
		minY = math.Min(minY, p.y)
		maxY = math.Max(maxY, p.y)
	}
	if minY >= float64(m.n) || maxY < 0 {
		return
	}

	const sub = 4
	row := make([]float64, m.n)
	for y := int(math.Floor(minY)); y <= int(math.Ceil(maxY)); y++ {
		if y < 0 || y >= m.n {
			continue
		}
		clear(row)
		for s := 0; s < sub; s++ {
			xs := crossings(scaled, float64(y)+(float64(s)+0.5)/sub)
			for i := 0; i+1 < len(xs); i += 2 {
				x0, x1 := xs[i], xs[i+1]
				for x := int(math.Floor(x0)); x < int(math.Ceil(x1)); x++ {
					if !ink {
						// Erasing clears the whole pixel any sub-span touches:
						// that is what a knock-out needs.
						if x >= 0 && x < m.n {
							m.buf[y*m.n+x] = 0
						}
						continue
					}
					lo := math.Max(x0, float64(x))
					hi := math.Min(x1, float64(x+1))
					if hi > lo {
						row[x] += (hi - lo) / sub
					}
				}
			}
		}
		for x, a := range row {
			if a > 0 {
				m.ink(x, y, a)
			}
		}
	}
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
//
// Output sizes larger than the buffer (the 512 and 1024 artwork come from the
// same 256-sample canvas as everything else) bilinearly upsample the
// anti-aliased coverage instead, which stays smooth because the buffer already
// is.
func (m *mask) coverage(x, y, outSize int) float64 {
	if outSize > m.n {
		fx := (float64(x)+0.5)*float64(m.n)/float64(outSize) - 0.5
		fy := (float64(y)+0.5)*float64(m.n)/float64(outSize) - 0.5
		return m.sample(fx, fy)
	}
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

// sample reads the buffer at a fractional position, bilinearly.
func (m *mask) sample(x, y float64) float64 {
	x0, y0 := math.Floor(x), math.Floor(y)
	tx, ty := x-x0, y-y0
	at := func(cx, cy int) float64 {
		if cx < 0 {
			cx = 0
		}
		if cy < 0 {
			cy = 0
		}
		if cx >= m.n {
			cx = m.n - 1
		}
		if cy >= m.n {
			cy = m.n - 1
		}
		return m.buf[cy*m.n+cx]
	}
	top := at(int(x0), int(y0))*(1-tx) + at(int(x0)+1, int(y0))*tx
	bot := at(int(x0), int(y0)+1)*(1-tx) + at(int(x0)+1, int(y0)+1)*tx
	return top*(1-ty) + bot*ty
}

// png renders the mask as a black icon with the mask in its alpha channel.
func (m *mask) png(size int) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			a := m.coverage(x, y, size)
			if a <= 0.002 {
				continue
			}
			img.SetNRGBA(x, y, color.NRGBA{A: uint8(math.Round(math.Min(1, a) * 255))})
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
