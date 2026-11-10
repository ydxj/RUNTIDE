// Command genicon renders the RUNTIDE application icon (a flat rounded square
// with two tide waves) at several sizes and writes PNG + ICO files.
//
//	go run ./build/tools/genicon
package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
)

var (
	bg = color.NRGBA{0x0F, 0x6C, 0xBD, 0xFF} // Fluent-style blue
	fg = color.NRGBA{0xFF, 0xFF, 0xFF, 0xFF}
)

// render draws the icon at size px using 4x supersampling.
func render(size int) *image.NRGBA {
	const ss = 4
	n := size * ss
	hi := image.NewNRGBA(image.Rect(0, 0, n, n))
	r := float64(n) * 0.22
	inRound := func(x, y float64) bool {
		fx, fy := float64(n), float64(n)
		cx := math.Min(math.Max(x, r), fx-r)
		cy := math.Min(math.Max(y, r), fy-r)
		return (x-cx)*(x-cx)+(y-cy)*(y-cy) <= r*r
	}
	wave := func(x, y, yc, amp, phase, thick float64) bool {
		t := x / float64(n)
		wy := yc + amp*math.Sin(t*2*math.Pi*1.0+phase)
		return math.Abs(y-wy) <= thick/2 && t > 0.2 && t < 0.8
	}
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			fx, fy := float64(x)+0.5, float64(y)+0.5
			if !inRound(fx, fy) {
				continue
			}
			c := bg
			N := float64(n)
			switch {
			case wave(fx, fy, N*0.42, N*0.075, 0, N*0.085):
				c = fg
			case wave(fx, fy, N*0.62, N*0.075, math.Pi*0.35, N*0.085):
				c = color.NRGBA{0xBF, 0xDD, 0xF5, 0xFF}
			}
			hi.SetNRGBA(x, y, c)
		}
	}
	out := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			var r, g, b, a, cnt int
			for dy := 0; dy < ss; dy++ {
				for dx := 0; dx < ss; dx++ {
					p := hi.NRGBAAt(x*ss+dx, y*ss+dy)
					a += int(p.A)
					r += int(p.R) * int(p.A)
					g += int(p.G) * int(p.A)
					b += int(p.B) * int(p.A)
					cnt++
				}
			}
			if a > 0 {
				out.SetNRGBA(x, y, color.NRGBA{uint8(r / a), uint8(g / a), uint8(b / a), uint8(a / cnt)})
			}
		}
	}
	return out
}

func encode(img image.Image) []byte {
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}

func main() {
	sizes := []int{16, 24, 32, 48, 64, 128, 256}
	var frames [][]byte
	for _, s := range sizes {
		frames = append(frames, encode(render(s)))
	}
	// ICO with PNG frames (supported since Windows Vista).
	var ico bytes.Buffer
	binary.Write(&ico, binary.LittleEndian, [3]uint16{0, 1, uint16(len(sizes))})
	off := 6 + 16*len(sizes)
	for i, s := range sizes {
		w := byte(s)
		if s >= 256 {
			w = 0
		}
		ico.Write([]byte{w, w, 0, 0})
		binary.Write(&ico, binary.LittleEndian, [2]uint16{1, 32})
		binary.Write(&ico, binary.LittleEndian, uint32(len(frames[i])))
		binary.Write(&ico, binary.LittleEndian, uint32(off))
		off += len(frames[i])
	}
	for _, f := range frames {
		ico.Write(f)
	}
	must(os.WriteFile("build/windows/icon.ico", ico.Bytes(), 0o644))
	must(os.WriteFile("build/appicon.png", encode(render(512)), 0o644))
	must(os.WriteFile("internal/desktop/tray.png", encode(render(32)), 0o644))
	must(os.MkdirAll("frontend/public", 0o755))
	must(os.WriteFile("frontend/public/icon.png", encode(render(128)), 0o644))
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
