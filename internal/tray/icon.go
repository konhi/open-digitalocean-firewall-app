package tray

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"math"
	"runtime"
)

type iconPair struct {
	template []byte
	regular  []byte
}

type iconSet struct {
	trayIdle   iconPair
	trayActive iconPair

	dotActive  []byte
	dotIdle    []byte
	dotWarning []byte
	dotError   []byte

	globe    iconPair
	database iconPair
	allow    iconPair
	revoke   iconPair
	refresh  iconPair
	clock    iconPair
	gear     iconPair
	key      iconPair
	user     iconPair
	power    iconPair
}

func newIconSet() iconSet {
	black := color.NRGBA{R: 0, G: 0, B: 0, A: 255}
	doBlue := color.NRGBA{R: 0, G: 128, B: 255, A: 255}
	slate := color.NRGBA{R: 120, G: 135, B: 155, A: 255}

	green := color.NRGBA{R: 52, G: 199, B: 89, A: 255}
	amber := color.NRGBA{R: 255, G: 159, B: 10, A: 255}
	red := color.NRGBA{R: 255, G: 59, B: 48, A: 255}
	gray := color.NRGBA{R: 142, G: 142, B: 147, A: 255}

	makePair := func(render func(color.NRGBA) []byte, regCol color.NRGBA) iconPair {
		return iconPair{
			template: render(black),
			regular:  render(regCol),
		}
	}

	return iconSet{
		trayIdle:   iconPair{template: shieldIcon(false, black), regular: shieldIcon(false, slate)},
		trayActive: iconPair{template: shieldIcon(true, black), regular: shieldIcon(true, doBlue)},

		dotActive:  statusDotIcon(green),
		dotIdle:    statusDotIcon(gray),
		dotWarning: statusDotIcon(amber),
		dotError:   statusDotIcon(red),

		globe:    makePair(globeIcon, slate),
		database: makePair(databaseIcon, doBlue),
		allow:    makePair(allowIcon, green),
		revoke:   makePair(revokeIcon, red),
		refresh:  makePair(refreshIcon, slate),
		clock:    makePair(clockIcon, slate),
		gear:     makePair(gearIcon, slate),
		key:      makePair(keyIcon, slate),
		user:     makePair(userIcon, slate),
		power:    makePair(powerIcon, slate),
	}
}

func renderIcon(size int, col color.NRGBA, alphaAt func(nx, ny float64) float64) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	half := float64(size) / 2.0

	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			nx := (float64(x) + 0.5 - half) / half
			ny := (float64(y) + 0.5 - half) / half
			a := clamp01(alphaAt(nx, ny))
			if a > 0 {
				c := col
				c.A = uint8(float64(col.A) * a)
				img.SetNRGBA(x, y, c)
			}
		}
	}

	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	pngBytes := buf.Bytes()

	if runtime.GOOS == "windows" {
		return wrapPNGAsICO(pngBytes, size)
	}
	return pngBytes
}

// shieldIcon renders a 44x44 shield as PNG (or ICO on Windows); filled indicates active access.
func shieldIcon(filled bool, col color.NRGBA) []byte {
	return renderIcon(44, col, func(nx, ny float64) float64 {
		d := shieldSDF(nx, ny)
		if filled {
			if d > 0 {
				return 0
			}
			alpha := clamp01(-d * 18.0)
			ck := checkSDF(nx, ny)
			if ck < 0.08 {
				alpha *= clamp01((ck - 0.04) * 24.0)
			}
			return alpha
		}
		dist := math.Abs(d + 0.06)
		return strokeAlpha(dist, 0.095)
	})
}

func statusDotIcon(col color.NRGBA) []byte {
	return renderIcon(32, col, func(nx, ny float64) float64 {
		r := math.Hypot(nx, ny)
		return clamp01((0.46 - r) * 18.0)
	})
}

func globeIcon(col color.NRGBA) []byte {
	return renderIcon(32, col, func(nx, ny float64) float64 {
		r := math.Hypot(nx, ny)
		ring := strokeAlpha(math.Abs(r-0.72), 0.085)
		if r > 0.78 {
			return ring
		}
		eq := strokeAlpha(math.Abs(ny), 0.07)
		meridian := strokeAlpha(math.Abs(math.Hypot(nx/0.40, ny/0.72)-1.0)*0.40, 0.075)
		return maxFloat(ring, eq, meridian)
	})
}

func databaseIcon(col color.NRGBA) []byte {
	return renderIcon(32, col, func(nx, ny float64) float64 {
		const (
			rx = 0.62
			ry = 0.22
		)
		topCap := ellipseStroke(nx, ny+0.44, rx, ry, 0.08)
		midArc := 0.0
		if ny >= -0.02 {
			midArc = ellipseStroke(nx, ny-0.02, rx, ry, 0.075)
		}
		botArc := 0.0
		if ny >= 0.44 {
			botArc = ellipseStroke(nx, ny-0.44, rx, ry, 0.08)
		}
		walls := 0.0
		if ny >= -0.44 && ny <= 0.44 {
			walls = strokeAlpha(math.Abs(math.Abs(nx)-rx), 0.08)
		}
		return maxFloat(topCap, midArc, botArc, walls)
	})
}

func allowIcon(col color.NRGBA) []byte {
	return renderIcon(32, col, func(nx, ny float64) float64 {
		ring := strokeAlpha(math.Abs(math.Hypot(nx, ny)-0.72), 0.085)
		ck := strokeAlpha(distSegment(nx, ny, -0.32, 0.02, -0.08, 0.26), 0.09)
		ck2 := strokeAlpha(distSegment(nx, ny, -0.08, 0.26, 0.34, -0.22), 0.09)
		return maxFloat(ring, ck, ck2)
	})
}

func revokeIcon(col color.NRGBA) []byte {
	return renderIcon(32, col, func(nx, ny float64) float64 {
		ring := strokeAlpha(math.Abs(math.Hypot(nx, ny)-0.72), 0.085)
		s1 := strokeAlpha(distSegment(nx, ny, -0.28, -0.28, 0.28, 0.28), 0.09)
		s2 := strokeAlpha(distSegment(nx, ny, 0.28, -0.28, -0.28, 0.28), 0.09)
		return maxFloat(ring, s1, s2)
	})
}

func refreshIcon(col color.NRGBA) []byte {
	return renderIcon(32, col, func(nx, ny float64) float64 {
		r := math.Hypot(nx, ny)
		ang := math.Atan2(ny, nx) // [-pi, pi]
		arc := 0.0
		// Leave a gap in the upper-right quadrant for the arrowhead.
		if ang < -0.55 || ang > 0.15 {
			arc = strokeAlpha(math.Abs(r-0.66), 0.09)
		}
		// Arrowhead pointing clockwise at the top-right end of the arc.
		h1 := strokeAlpha(distSegment(nx, ny, 0.56, -0.35, 0.22, -0.35), 0.09)
		h2 := strokeAlpha(distSegment(nx, ny, 0.56, -0.35, 0.56, -0.69), 0.09)
		return maxFloat(arc, h1, h2)
	})
}

func clockIcon(col color.NRGBA) []byte {
	return renderIcon(32, col, func(nx, ny float64) float64 {
		ring := strokeAlpha(math.Abs(math.Hypot(nx, ny)-0.72), 0.085)
		minHand := strokeAlpha(distSegment(nx, ny, 0, 0, 0, -0.42), 0.085)
		hourHand := strokeAlpha(distSegment(nx, ny, 0, 0, 0.30, 0.18), 0.085)
		return maxFloat(ring, minHand, hourHand)
	})
}

func gearIcon(col color.NRGBA) []byte {
	return renderIcon(32, col, func(nx, ny float64) float64 {
		r := math.Hypot(nx, ny)
		ang := math.Atan2(ny, nx)
		tooth := 0.58 + 0.14*math.Max(0, math.Cos(8*ang))
		outer := clamp01((tooth - r) * 18.0)
		inner := clamp01((r - 0.28) * 18.0)
		return outer * inner
	})
}

func keyIcon(col color.NRGBA) []byte {
	return renderIcon(32, col, func(nx, ny float64) float64 {
		head := strokeAlpha(math.Abs(math.Hypot(nx+0.36, ny)-0.28), 0.09)
		shaft := strokeAlpha(distSegment(nx, ny, -0.08, 0, 0.68, 0), 0.09)
		tooth1 := strokeAlpha(distSegment(nx, ny, 0.42, 0, 0.42, 0.26), 0.09)
		tooth2 := strokeAlpha(distSegment(nx, ny, 0.64, 0, 0.64, 0.22), 0.09)
		return maxFloat(head, shaft, tooth1, tooth2)
	})
}

func userIcon(col color.NRGBA) []byte {
	return renderIcon(32, col, func(nx, ny float64) float64 {
		head := strokeAlpha(math.Abs(math.Hypot(nx, ny+0.34)-0.26), 0.09)
		shoulders := 0.0
		if ny >= 0.06 && ny <= 0.72 {
			shoulders = strokeAlpha(math.Abs(math.Hypot(nx/0.60, (ny-0.72)/0.56)-1.0)*0.56, 0.09)
		}
		return maxFloat(head, shoulders)
	})
}

func powerIcon(col color.NRGBA) []byte {
	return renderIcon(32, col, func(nx, ny float64) float64 {
		r := math.Hypot(nx, ny-0.06)
		ang := math.Atan2(ny-0.06, nx)
		arc := 0.0
		// Open gap at the top (-pi/2).
		if math.Abs(ang+math.Pi/2) > 0.55 {
			arc = strokeAlpha(math.Abs(r-0.64), 0.095)
		}
		stem := strokeAlpha(distSegment(nx, ny, 0, -0.74, 0, -0.06), 0.095)
		return maxFloat(arc, stem)
	})
}

func strokeAlpha(dist, halfWidth float64) float64 {
	return clamp01((halfWidth - dist) * 22.0)
}

func ellipseStroke(x, y, rx, ry, halfWidth float64) float64 {
	// Approximate distance to ellipse boundary.
	k := math.Hypot(x/rx, y/ry)
	if k == 0 {
		return 0
	}
	approxDist := math.Abs(k-1.0) * math.Min(rx, ry)
	return strokeAlpha(approxDist, halfWidth)
}

func maxFloat(vals ...float64) float64 {
	best := 0.0
	for _, v := range vals {
		if v > best {
			best = v
		}
	}
	return best
}

// shieldSDF returns a signed distance (<= 0 inside) for a heraldic shield.
func shieldSDF(x, y float64) float64 {
	ax := math.Abs(x)
	top := -0.74 + 0.08*ax*ax
	if y < top {
		return top - y
	}

	var halfW float64
	if y <= -0.05 {
		halfW = 0.68
	} else {
		t := (y + 0.05) / 0.87
		if t >= 1 {
			return y - 0.82
		}
		halfW = 0.68 * math.Pow(1-t*t, 0.72)
	}

	dx := ax - halfW
	dy := y - 0.82
	if dx > 0 && dy > 0 {
		return math.Hypot(dx, dy)
	}
	return math.Max(dx, dy)
}

// checkSDF returns unsigned distance to a checkmark centered inside the shield.
func checkSDF(x, y float64) float64 {
	d1 := distSegment(x, y, -0.28, 0.02, -0.06, 0.24)
	d2 := distSegment(x, y, -0.06, 0.24, 0.30, -0.20)
	return math.Min(d1, d2)
}

func distSegment(px, py, ax, ay, bx, by float64) float64 {
	vx, vy := bx-ax, by-ay
	wx, wy := px-ax, py-ay
	c1 := vx*wx + vy*wy
	if c1 <= 0 {
		return math.Hypot(px-ax, py-ay)
	}
	c2 := vx*vx + vy*vy
	if c2 <= c1 {
		return math.Hypot(px-bx, py-by)
	}
	t := c1 / c2
	projX, projY := ax+t*vx, ay+t*vy
	return math.Hypot(px-projX, py-projY)
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// wrapPNGAsICO wraps a PNG image inside a single-entry Windows ICO container.
func wrapPNGAsICO(pngData []byte, size int) []byte {
	var buf bytes.Buffer

	// ICONDIR header (6 bytes): reserved, type=ICO, count=1.
	_ = binary.Write(&buf, binary.LittleEndian, uint16(0))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(1))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(1))

	// ICONDIRENTRY (16 bytes) pointing to the embedded PNG payload at offset 22.
	dim := byte(size)
	if size >= 256 {
		dim = 0
	}
	buf.WriteByte(dim)
	buf.WriteByte(dim)
	buf.WriteByte(0)
	buf.WriteByte(0)
	_ = binary.Write(&buf, binary.LittleEndian, uint16(1))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(32))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(len(pngData)))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(6+16))

	buf.Write(pngData)
	return buf.Bytes()
}
