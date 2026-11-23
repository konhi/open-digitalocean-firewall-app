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

// shieldIcon renders a 44x44 shield as PNG (or ICO on Windows); filled indicates active access.
func shieldIcon(filled bool, col color.NRGBA) []byte {
	const size = 44
	img := image.NewNRGBA(image.Rect(0, 0, size, size))

	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			// Normalize coordinates to [-1, 1]
			nx := (float64(x) + 0.5 - size/2.0) / (size / 2.0)
			ny := (float64(y) + 0.5 - size/2.0) / (size / 2.0)

			d := shieldSDF(nx, ny)
			if filled {
				if d <= 0 {
					alpha := clamp01(-d * 18.0)
					// Carve a crisp checkmark inside the filled shield.
					ck := checkSDF(nx, ny)
					if ck < 0.08 {
						alpha *= clamp01((ck - 0.04) * 24.0)
					}
					if alpha > 0 {
						c := col
						c.A = uint8(float64(col.A) * alpha)
						img.SetNRGBA(x, y, c)
					}
				}
			} else {
				// Outline ring of thickness ~0.13 in normalized space.
				dist := math.Abs(d + 0.06)
				if dist < 0.095 {
					alpha := clamp01((0.095 - dist) * 20.0)
					if alpha > 0 {
						c := col
						c.A = uint8(float64(col.A) * alpha)
						img.SetNRGBA(x, y, c)
					}
				}
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
