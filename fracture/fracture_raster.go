package fracture

import (
	"image"
	"image/color"
	"runtime"
	"slices"
	"sync"

	"github.com/kortschak/scramblesuit/density"
)

// Raster performs gen generations of dipole subdivision on img. When
// mono is true a single greyscale channel is used; otherwise R, G, B
// channels are processed independently.
func Raster(img image.Image, gen int, mono, weighted bool) image.Image {
	var dst splitter
	if mono {
		dst = newDipoleMap(img, density.AvgDensity, density.NegAvgDensity, uint(1<<gen))
	} else {
		dst = newColorDipoleMap(img, uint(1<<gen))
	}
	for range gen {
		dst.fracture(weighted)
	}
	dst.render()
	return dst
}

// splitter extends image.Image with the fracture/render cycle used by
// both dipoleMap and colorDipoleMap.
type splitter interface {
	image.Image
	fracture(weighted bool)
	render()
}

// colorDipoleMap applies dipole subdivision independently to R, G, B channels.
type colorDipoleMap struct {
	wg      sync.WaitGroup
	R, G, B *dipoleMap
}

// newColorDipoleMap creates a colorDipoleMap from img with independent
// R, G, B dipole maps, each pre-allocated for n dipoles. The three
// channels are constructed concurrently since they share only the
// read-only source image.
func newColorDipoleMap(img image.Image, n uint) *colorDipoleMap {
	c := &colorDipoleMap{}
	var wg sync.WaitGroup
	wg.Add(3)
	go func() { c.R = newDipoleMap(img, density.RedDensity, density.NegRedDensity, n); wg.Done() }()
	go func() { c.G = newDipoleMap(img, density.GreenDensity, density.NegGreenDensity, n); wg.Done() }()
	go func() { c.B = newDipoleMap(img, density.BlueDensity, density.NegBlueDensity, n); wg.Done() }()
	wg.Wait()
	return c
}

// At combines the three channel densities into an opaque RGBA colour.
func (c *colorDipoleMap) At(x, y int) color.Color {
	return color.RGBA{
		R: uint8(c.R.at(x, y) >> 8),
		G: uint8(c.G.at(x, y) >> 8),
		B: uint8(c.B.at(x, y) >> 8),
		A: 0xFF,
	}
}

// Bounds returns the image bounds, delegated to the red channel.
func (c *colorDipoleMap) Bounds() image.Rectangle { return c.R.Bounds() }

// ColorModel returns color.RGBAModel.
func (c *colorDipoleMap) ColorModel() color.Model { return color.RGBAModel }

// fracture splits all three channels concurrently.
func (c *colorDipoleMap) fracture(weighted bool) {
	c.wg.Add(3)
	go func() { c.R.fracture(weighted); c.wg.Done() }()
	go func() { c.G.fracture(weighted); c.wg.Done() }()
	go func() { c.B.fracture(weighted); c.wg.Done() }()
	c.wg.Wait()
}

// render composites all three channels concurrently.
func (c *colorDipoleMap) render() {
	c.wg.Add(3)
	go func() { c.R.render(); c.wg.Done() }()
	go func() { c.G.render(); c.wg.Done() }()
	go func() { c.B.render(); c.wg.Done() }()
	c.wg.Wait()
}

// dipoleMap holds a set of dipoles over a single density channel.
type dipoleMap struct {
	dipoles   []dipole
	work      []dipole // pre-allocated scratch; each goroutine in fracture writes to its own index to avoid locking
	rendered  []uint16
	renderBuf []uint64 // reusable accumulation buffer for render
	rect      image.Rectangle
}

// newDipoleMap creates a dipoleMap from img using the given north and
// south density models, pre-allocated for n dipoles.
func newDipoleMap(img image.Image, north, south density.Model, n uint) *dipoleMap {
	if n == 0 {
		n = 1
	}
	r := img.Bounds()
	dm := &dipoleMap{
		dipoles:   make([]dipole, 1, n),
		work:      make([]dipole, n),
		rendered:  make([]uint16, r.Dx()*r.Dy()),
		renderBuf: make([]uint64, r.Dx()*r.Dy()),
		rect:      r,
	}

	mask := density.NewFullMap(r)
	var northSrc, southSrc *density.Map
	var mapWg sync.WaitGroup
	mapWg.Add(2)
	go func() { northSrc = density.MapFrom(img, north); mapWg.Done() }()
	go func() { southSrc = density.MapFrom(img, south); mapWg.Done() }()
	mapWg.Wait()
	// The initial mask is all-0xffff, so the intersection of src
	// with mask is the identity: use Stats() directly instead of
	// the more expensive IntersectStats.
	dm.dipoles[0] = dipole{
		northSrc:   northSrc,
		southSrc:   southSrc,
		northStats: northSrc.Stats(),
		southStats: southSrc.Stats(),
		mask:       mask,
	}
	return dm
}

// At returns the rendered density at (x, y) as a Gray16 colour.
func (dm *dipoleMap) At(x, y int) color.Color { return color.Gray16{Y: dm.at(x, y)} }

// Bounds returns the image bounds.
func (dm *dipoleMap) Bounds() image.Rectangle { return dm.rect }

// ColorModel returns color.Gray16Model.
func (dm *dipoleMap) ColorModel() color.Model { return color.Gray16Model }

// at returns the rendered density value at (x, y) as a raw uint16.
func (dm *dipoleMap) at(x, y int) uint16 {
	if !(image.Point{x, y}).In(dm.rect) {
		return 0
	}
	return dm.rendered[x-dm.rect.Min.X+(y-dm.rect.Min.Y)*dm.rect.Dx()]
}

// render accumulates all dipole contributions into the rendered buffer.
// It uses uint64 accumulation so that overlapping dipole contributions
// cannot overflow, then truncates to uint16.
func (dm *dipoleMap) render() {
	buf := dm.renderBuf
	clear(buf)
	x0 := dm.rect.Min.X
	y0 := dm.rect.Min.Y
	stride := dm.rect.Dx()

	for i := range dm.dipoles {
		d := &dm.dipoles[i]
		b := d.northStats.Rect
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				buf[x-x0+(y-y0)*stride] += d.at(x, y)
			}
		}
	}

	for i, v := range buf {
		dm.rendered[i] = uint16(v)
	}
}

// fracture splits each existing dipole in parallel, bounded by NumCPU,
// and appends the new halves to the dipole list.
func (dm *dipoleMap) fracture(weighted bool) {
	l := len(dm.dipoles)
	b := make([]*dipole, l)

	var wg sync.WaitGroup
	wg.Add(l)
	limit := make(chan struct{}, runtime.NumCPU())
	for i := range l {
		limit <- struct{}{}
		go func() {
			b[i] = dm.split(i, weighted)
			<-limit
			wg.Done()
		}()
	}
	wg.Wait()
	b = slices.DeleteFunc(b, func(d *dipole) bool {
		return d == nil
	})
	// Copy each dipole by value so the main slice does not hold
	// pointers into the work scratch buffer.
	for _, d := range b {
		dm.dipoles = append(dm.dipoles, *d)
	}
}

// split divides dipole i along the perpendicular bisector of its
// north and south centres of mass.
func (dm *dipoleMap) split(i int, weighted bool) *dipole {
	dp := &dm.dipoles[i]
	// A mass of 0xffff or less means the dipole covers at most one
	// full pixel's worth of density; too small to subdivide further.
	if dp.northStats.Mass <= 0xffff {
		return nil
	}

	line := computeSplitLine(dp.northStats, dp.southStats, dp.northStats.Rect, weighted)

	nm1, nm2 := dp.mask.ThresholdSplit(func(x, y int) uint16 {
		if line.horiz {
			return threshold(y, line.cy+line.slope*(float64(x)-line.cx))
		}
		return threshold(x, line.cx+line.slope*(float64(y)-line.cy))
	})
	if nm1 == nil {
		return nil
	}

	// Shrink the original dipole to the first half of the split
	// and place a new dipole covering the second half into
	// work[i]. The caller appends the new dipole to the main list.
	dp.setMask(nm1)
	dm.work[i] = newDipole(dp.northSrc, dp.southSrc, nm2)
	return &dm.work[i]
}

// threshold returns an anti-aliased split value for integer
// coordinate d against fractional boundary t. Pixels fully below the
// boundary get 0xffff; pixels fully above get 0. The pixel that
// straddles the boundary gets a proportional fraction, giving a
// one-pixel-wide smooth transition instead of a hard staircase edge.
func threshold(d int, t float64) uint16 {
	if d < int(t) {
		return 0xffff
	}
	if d == int(t) {
		return uint16((t - float64(d)) * 0xffff)
	}
	return 0
}

// dipole pairs north and south density maps with a spatial mask. The
// src maps hold the original full-image densities; the stats hold the
// aggregate mass, weighted coordinates, and bounds of the src/mask
// intersection without materialising a per-pixel buffer.
type dipole struct {
	northSrc, southSrc     *density.Map
	northStats, southStats density.Stats
	mask                   *density.Map
}

// newDipole creates a dipole by computing intersection statistics of
// the source maps with mask.
func newDipole(northSrc, southSrc, mask *density.Map) dipole {
	return dipole{
		northSrc:   northSrc,
		southSrc:   southSrc,
		northStats: northSrc.IntersectStats(mask),
		southStats: southSrc.IntersectStats(mask),
		mask:       mask,
	}
}

// setMask replaces the dipole's mask and recomputes the intersection
// statistics.
func (d *dipole) setMask(mask *density.Map) {
	d.mask = mask
	d.northStats = d.northSrc.IntersectStats(mask)
	d.southStats = d.southSrc.IntersectStats(mask)
}

// at returns this dipole's density contribution at (x, y). The result
// is the dipole's total north mass distributed uniformly across all
// mask-weighted pixels. Original per-pixel intensities are discarded,
// producing a flat tile whose brightness equals the cell's mean density.
func (d *dipole) at(x, y int) uint64 {
	return (d.northStats.Mass * d.mask.ValueAt(x, y)) / d.mask.Mass()
}
