// Command mkgif assembles an animated GIF from PNG frames listed in a manifest.
//
//	mkgif -manifest frames/manifest.json -out docs/screenshots/demo.gif
//
// The manifest is a JSON array of {"file": "000.png", "delayMs": 400}; files are
// resolved relative to the manifest. It uses only the standard library: one
// global palette (median cut), no dithering (the frames are flat UI colours) and
// per-frame cropping to the region that changed, which keeps the file small.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"os"
	"path/filepath"
	"sort"
)

var speed = 1.0

type frame struct {
	File    string `json:"file"`
	DelayMs int    `json:"delayMs"`
}

func main() {
	manifest := flag.String("manifest", "", "manifest.json written by the demo spec")
	out := flag.String("out", "demo.gif", "output GIF")
	spd := flag.Float64("speed", 1, "playback speed factor (2 = twice as fast)")
	flag.Parse()
	if *manifest == "" || *spd <= 0 {
		fmt.Fprintln(os.Stderr, "usage: mkgif -manifest frames/manifest.json -out demo.gif [-speed 1.3]")
		os.Exit(2)
	}
	speed = *spd
	if err := run(*manifest, *out); err != nil {
		fmt.Fprintln(os.Stderr, "mkgif:", err)
		os.Exit(1)
	}
}

func run(manifestPath, out string) error {
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		return err
	}
	var frames []frame
	if err := json.Unmarshal(raw, &frames); err != nil {
		return err
	}
	if len(frames) == 0 {
		return fmt.Errorf("manifest has no frames")
	}
	dir := filepath.Dir(manifestPath)
	imgs := make([]image.Image, len(frames))
	hist := map[uint16]int{}
	for i, f := range frames {
		fh, err := os.Open(filepath.Join(dir, f.File))
		if err != nil {
			return err
		}
		img, err := png.Decode(fh)
		_ = fh.Close()
		if err != nil {
			return fmt.Errorf("%s: %w", f.File, err)
		}
		imgs[i] = img
		b := img.Bounds()
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				hist[key(img.At(x, y))]++
			}
		}
	}
	pal := medianCut(hist, 255)
	cache := map[uint16]uint8{}
	index := func(c color.Color) uint8 {
		k := key(c)
		if v, ok := cache[k]; ok {
			return v
		}
		r, g, b := expand(k)
		best, bestD := 0, int(^uint(0)>>1)
		for i, p := range pal {
			pr, pg, pb, _ := p.RGBA()
			dr, dg, db := int(r)-int(pr>>8), int(g)-int(pg>>8), int(b)-int(pb>>8)
			if d := dr*dr + dg*dg + db*db; d < bestD {
				best, bestD = i, d
			}
		}
		cache[k] = uint8(best)
		return uint8(best)
	}

	full := image.Rect(0, 0, imgs[0].Bounds().Dx(), imgs[0].Bounds().Dy())
	canvas := image.NewPaletted(full, append(color.Palette{}, pal...))
	for y := 0; y < full.Dy(); y++ {
		for x := 0; x < full.Dx(); x++ {
			canvas.SetColorIndex(x, y, index(imgs[0].At(x, y)))
		}
	}
	g := &gif.GIF{LoopCount: 0}
	for i, img := range imgs {
		b := img.Bounds()
		if b.Dx() != full.Dx() || b.Dy() != full.Dy() {
			return fmt.Errorf("frame %s has a different size", frames[i].File)
		}
		next := image.NewPaletted(full, canvas.Palette)
		for y := 0; y < full.Dy(); y++ {
			for x := 0; x < full.Dx(); x++ {
				next.SetColorIndex(x, y, index(img.At(x, y)))
			}
		}
		box := full
		if i > 0 {
			box = changed(canvas, next)
		}
		delay := int(float64(frames[i].DelayMs)/speed+5) / 10
		if delay < 2 {
			delay = 2
		}
		if i > 0 && box.Empty() { // identical frame: only extend the previous one
			g.Delay[len(g.Delay)-1] += delay
			continue
		}
		sub := image.NewPaletted(box, canvas.Palette)
		for y := box.Min.Y; y < box.Max.Y; y++ {
			for x := box.Min.X; x < box.Max.X; x++ {
				sub.SetColorIndex(x, y, next.ColorIndexAt(x, y))
			}
		}
		g.Image = append(g.Image, sub)
		g.Delay = append(g.Delay, delay)
		g.Disposal = append(g.Disposal, gif.DisposalNone)
		canvas = next
	}
	g.Config = image.Config{ColorModel: canvas.Palette, Width: full.Dx(), Height: full.Dy()}
	fh, err := os.Create(out)
	if err != nil {
		return err
	}
	if err := gif.EncodeAll(fh, g); err != nil {
		_ = fh.Close()
		return err
	}
	if err := fh.Close(); err != nil {
		return err
	}
	st, _ := os.Stat(out)
	total := 0
	for _, d := range g.Delay {
		total += d
	}
	fmt.Printf("wrote %s: %d frames, %.1fs, %.2f MB\n", out, len(g.Image), float64(total)/100, float64(st.Size())/1e6)
	return nil
}

// changed returns the bounding box of the pixels that differ between a and b.
func changed(a, b *image.Paletted) image.Rectangle {
	r := image.Rectangle{Min: image.Pt(1<<30, 1<<30), Max: image.Pt(-1, -1)}
	bounds := a.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if a.ColorIndexAt(x, y) != b.ColorIndexAt(x, y) {
				if x < r.Min.X {
					r.Min.X = x
				}
				if y < r.Min.Y {
					r.Min.Y = y
				}
				if x+1 > r.Max.X {
					r.Max.X = x + 1
				}
				if y+1 > r.Max.Y {
					r.Max.Y = y + 1
				}
			}
		}
	}
	if r.Max.X < 0 {
		return image.Rectangle{}
	}
	return r
}

func key(c color.Color) uint16 {
	r, g, b, _ := c.RGBA()
	return uint16(r>>11)<<10 | uint16(g>>11)<<5 | uint16(b>>11)
}

func expand(k uint16) (r, g, b uint8) {
	r5, g5, b5 := uint8(k>>10&31), uint8(k>>5&31), uint8(k&31)
	return r5<<3 | r5>>2, g5<<3 | g5>>2, b5<<3 | b5>>2
}

type bucket struct {
	keys  []uint16
	count int
}

// medianCut reduces the weighted histogram to at most n palette entries.
func medianCut(hist map[uint16]int, n int) color.Palette {
	all := make([]uint16, 0, len(hist))
	total := 0
	for k, c := range hist {
		all = append(all, k)
		total += c
	}
	sort.Slice(all, func(i, j int) bool { return all[i] < all[j] })
	buckets := []bucket{{keys: all, count: total}}
	for len(buckets) < n {
		bi, best := -1, 0
		for i, b := range buckets {
			if len(b.keys) > 1 && b.count > best {
				bi, best = i, b.count
			}
		}
		if bi < 0 {
			break
		}
		b := buckets[bi]
		var lo, hi [3]uint8
		for c := 0; c < 3; c++ {
			lo[c], hi[c] = 255, 0
		}
		for _, k := range b.keys {
			r, g, bl := expand(k)
			for c, v := range [3]uint8{r, g, bl} {
				if v < lo[c] {
					lo[c] = v
				}
				if v > hi[c] {
					hi[c] = v
				}
			}
		}
		axis := 0
		for c := 1; c < 3; c++ {
			if hi[c]-lo[c] > hi[axis]-lo[axis] {
				axis = c
			}
		}
		comp := func(k uint16) uint8 {
			r, g, bl := expand(k)
			return [3]uint8{r, g, bl}[axis]
		}
		keys := append([]uint16(nil), b.keys...)
		sort.Slice(keys, func(i, j int) bool { return comp(keys[i]) < comp(keys[j]) })
		half, acc := b.count/2, 0
		cut := 1
		for i, k := range keys {
			acc += hist[k]
			if acc >= half {
				cut = i + 1
				break
			}
		}
		if cut >= len(keys) {
			cut = len(keys) - 1
		}
		left, right := keys[:cut], keys[cut:]
		lc := 0
		for _, k := range left {
			lc += hist[k]
		}
		buckets[bi] = bucket{keys: left, count: lc}
		buckets = append(buckets, bucket{keys: right, count: b.count - lc})
	}
	pal := make(color.Palette, 0, len(buckets)+1)
	for _, b := range buckets {
		var sr, sg, sb, w int
		for _, k := range b.keys {
			r, g, bl := expand(k)
			c := hist[k]
			sr, sg, sb, w = sr+int(r)*c, sg+int(g)*c, sb+int(bl)*c, w+c
		}
		if w == 0 {
			continue
		}
		pal = append(pal, color.RGBA{uint8(sr / w), uint8(sg / w), uint8(sb / w), 255})
	}
	return pal
}
