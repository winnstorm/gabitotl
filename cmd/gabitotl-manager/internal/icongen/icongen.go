// Package icongen draws the GabitoTL logo (docs/assets/logo.svg) with the
// standard library and packs it as a PNG-encoded Windows .ico. It exists so the
// application icon is reproducible from code: `go generate` rewrites
// appicon.png, appicon.ico and the Windows resource object of the manager.
//
//go:generate go run ./gen -out ../..
package icongen

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"math"
)

// Sizes are the icon sizes embedded in the .ico, smallest first.
var Sizes = []int{16, 24, 32, 48, 64, 128, 256}

// The logo is drawn on a 64x64 canvas; these are its palette colors.
var (
	navy  = color.NRGBA{R: 0x0f, G: 0x17, B: 0x2a, A: 0xff}
	sky   = color.NRGBA{R: 0x38, G: 0xbd, B: 0xf8, A: 0xff}
	amber = color.NRGBA{R: 0xfb, G: 0xbf, B: 0x24, A: 0xff}
)

// shape is a rounded rectangle on the 64x64 canvas; a circle is a square whose
// radius is half its side.
type shape struct {
	x, y, w, h, r float64
	c             color.NRGBA
}

func circle(cx, cy, r float64, c color.NRGBA) shape {
	return shape{cx - r, cy - r, 2 * r, 2 * r, r, c}
}

// logo lists the shapes back to front: rounded navy square, sky body, navy
// eyes, sky antenna, amber dot, navy mouth.
var logo = []shape{
	{0, 0, 64, 64, 14, navy},
	{14, 20, 36, 28, 8, sky},
	circle(25, 34, 4, navy),
	circle(39, 34, 4, navy),
	{30, 11, 4, 9, 2, sky},
	circle(32, 10, 3, amber),
	{24, 42, 16, 3, 1.5, navy},
}

func (s shape) contains(px, py float64) bool {
	cx := math.Min(math.Max(px, s.x+s.r), s.x+s.w-s.r)
	cy := math.Min(math.Max(py, s.y+s.r), s.y+s.h-s.r)
	dx, dy := px-cx, py-cy
	return dx*dx+dy*dy <= s.r*s.r
}

// ss is the supersampling grid per pixel side (ss*ss samples per pixel).
const ss = 4

// Render draws the logo at size x size pixels with anti-aliased edges and a
// transparent background outside the rounded square.
func Render(size int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	scale := 64 / float64(size)
	for py := 0; py < size; py++ {
		for px := 0; px < size; px++ {
			var r, g, b, a float64
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					x := (float64(px) + (float64(sx)+0.5)/ss) * scale
					y := (float64(py) + (float64(sy)+0.5)/ss) * scale
					for i := len(logo) - 1; i >= 0; i-- {
						if logo[i].contains(x, y) {
							c := logo[i].c
							r, g, b, a = r+float64(c.R), g+float64(c.G), b+float64(c.B), a+255
							break
						}
					}
				}
			}
			if a == 0 {
				continue
			}
			n := float64(ss * ss)
			img.SetNRGBA(px, py, color.NRGBA{
				R: uint8(math.Round(r * 255 / a)), G: uint8(math.Round(g * 255 / a)),
				B: uint8(math.Round(b * 255 / a)), A: uint8(math.Round(a / n)),
			})
		}
	}
	return img
}

// PNG encodes Render(size) as a PNG.
func PNG(size int) ([]byte, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, Render(size)); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ICO packs the logo at the given sizes (each 1..256) into a .ico whose images
// are PNG-encoded, the format Windows Vista and later read natively.
func ICO(sizes []int) ([]byte, error) {
	images := make([][]byte, len(sizes))
	for i, s := range sizes {
		b, err := PNG(s)
		if err != nil {
			return nil, err
		}
		images[i] = b
	}
	var out bytes.Buffer
	w := func(v any) { _ = binary.Write(&out, binary.LittleEndian, v) }
	w(uint16(0))          // reserved
	w(uint16(1))          // type: icon
	w(uint16(len(sizes))) // image count
	offset := 6 + 16*len(sizes)
	for i, s := range sizes {
		dim := uint8(s) // 256 wraps to 0, which is how .ico spells 256
		w(dim)
		w(dim)
		w(uint8(0))  // palette colors
		w(uint8(0))  // reserved
		w(uint16(1)) // color planes
		w(uint16(32))
		w(uint32(len(images[i])))
		w(uint32(offset))
		offset += len(images[i])
	}
	for _, b := range images {
		out.Write(b)
	}
	return out.Bytes(), nil
}
