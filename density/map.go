// Package density implements functions to map pixels in an image
// to associated density values, and store them in a Map (not to
// be confused with the built-in type). It is mostly an adaptation
// of the code found in the standard library's image package, and
// still uses Point and Rect from that package.
//
// Although written with the intent of calculating weighted Voronoi
// maps based on images, it can be used more widely than that.
//
// Density values of a pixel as defined by the density model are
// stored as uint16 values. Maps implement the Image interface, as
// Gray16 images. Note that it's trivial to make a struct that wraps
// density maps as different colour channels (see the examples).
//
// Aggregate statistics (total mass, weighted x and y) are kept
// as uint64 to avoid overflow when summing many uint16 values.
// Per-pixel values remain uint16, and maps produce Gray16 image
// output directly.
package density

import (
	"image"
	"image/color"
)

// Map is a finite rectangular grid of density values, usually
// converted from the colors of an image.
type Map struct {
	// Values holds the map's density values. The value at (x, y)
	// starts at Values[(y-Rect.Min.Y)*Stride + (x-Rect.Min.X)*1].
	Values []uint16
	// Stride is the Values' stride between
	// vertically adjacent pixels.
	Stride int
	// Rect is the Map's bounds.
	Rect image.Rectangle
	// Total mass, weighed x and weighed y. Essentially a cache to
	// speed up a number of calculations.
	mass, wx, wy uint64
}

// NewMap returns an empty map of the given dimensions.
func NewMap(bounds image.Rectangle) *Map {
	w, h := bounds.Dx(), bounds.Dy()
	var d *Map
	if w > 0 && h > 0 {
		dv := make([]uint16, w*h)
		d = &Map{Values: dv, Stride: w, Rect: bounds}
	}
	return d
}

// NewFullMap returns a map of the given dimensions with every pixel set
// to 0xffff. The cached mass and weighted coordinates are computed
// analytically rather than per-pixel.
func NewFullMap(bounds image.Rectangle) *Map {
	w, h := bounds.Dx(), bounds.Dy()
	if w <= 0 || h <= 0 {
		return nil
	}
	const v = 0xffff
	dv := make([]uint16, w*h)
	for i := range dv {
		dv[i] = v
	}
	mass := v * uint64(w) * uint64(h)
	// wx and wy are closed-form sums of the arithmetic series
	// 0+1+...+(w-1) and 0+1+...+(h-1), each weighted by v and
	// multiplied out across every row or column respectively.
	return &Map{
		Values: dv,
		Stride: w,
		Rect:   bounds,
		mass:   mass,
		wx:     mass * (uint64(w) - 1) / 2,
		wy:     mass * (uint64(h) - 1) / 2,
	}
}

// MapFrom converts each pixel of img through density model d and returns
// the results as a new Map. For *image.RGBA, *image.NRGBA and *image.Gray
// images it avoids the per-pixel heap allocation caused by the color.Color
// interface return of img.At.
func MapFrom(img image.Image, d Model) *Map {
	r := img.Bounds()
	w, h := r.Dx(), r.Dy()
	dv := make([]uint16, w*h)
	dm := Map{Values: dv, Stride: w, Rect: r}
	switch img := img.(type) {
	case *image.RGBA:
		for y := r.Min.Y; y < r.Max.Y; y++ {
			for x := r.Min.X; x < r.Max.X; x++ {
				dm.Set(x, y, d.Convert(img.RGBAAt(x, y)))
			}
		}
	case *image.NRGBA:
		for y := r.Min.Y; y < r.Max.Y; y++ {
			for x := r.Min.X; x < r.Max.X; x++ {
				dm.Set(x, y, d.Convert(img.NRGBAAt(x, y)))
			}
		}
	case *image.Gray:
		for y := r.Min.Y; y < r.Max.Y; y++ {
			for x := r.Min.X; x < r.Max.X; x++ {
				dm.Set(x, y, d.Convert(img.GrayAt(x, y)))
			}
		}
	default:
		for y := r.Min.Y; y < r.Max.Y; y++ {
			for x := r.Min.X; x < r.Max.X; x++ {
				dm.Set(x, y, d.Convert(img.At(x, y)))
			}
		}
	}
	return &dm
}

// At(x, y) returns the density value at point (x,y).
// If (x,y) is out of bounds, it returns a density of zero.
func (d *Map) At(x, y int) color.Color {
	if (image.Point{x, y}.In(d.Rect)) {
		return color.Gray16{d.Values[d.offSet(x, y)]}
	}
	return nil
}

// Bounds returns the Map's bounding rectangle.
func (d *Map) Bounds() image.Rectangle { return d.Rect }

// ColorModel returns color.Gray16Model.
func (d *Map) ColorModel() color.Model {
	return color.Gray16Model
}

// Set updates the density value at (x, y) and adjusts the cached mass
// and weighted coordinates accordingly.
func (d *Map) Set(x, y int, v uint16) {
	if !(image.Point{x, y}.In(d.Rect)) {
		return
	}
	i := d.offSet(x, y)

	// We update mass, wx and wy by removing the old value
	// first if necessary, then adding the new value.
	dv := uint64(d.Values[i])
	if dv != 0 {
		d.mass -= dv
		d.wx -= dv * uint64(x-d.Rect.Min.X)
		d.wy -= dv * uint64(y-d.Rect.Min.Y)
	}

	d.Values[i] = v

	dv = uint64(v)
	d.mass += dv
	d.wx += dv * uint64(x-d.Rect.Min.X)
	d.wy += dv * uint64(y-d.Rect.Min.Y)
}

// ValueAt returns the density value at (x, y) as a uint64 rather than
// a color.Color. If (x, y) is out of bounds, it returns zero.
func (d *Map) ValueAt(x, y int) uint64 {
	if (image.Point{x, y}.In(d.Rect)) {
		return uint64(d.Values[d.offSet(x, y)])
	}
	return 0
}

// Center returns the centre of mass of the Map.
func (d *Map) Center() (x, y float64) {
	x = float64(d.Rect.Min.X) + (float64(d.wx) / float64(d.mass))
	y = float64(d.Rect.Min.Y) + (float64(d.wy) / float64(d.mass))
	return x, y
}

// Mass returns the total mass of the density map, its density
// summed over its entire surface.
func (d *Map) Mass() uint64 {
	return d.mass
}

// Stats returns the Map's aggregate statistics as a Stats value.
func (d *Map) Stats() Stats {
	return Stats{Rect: d.Rect, Mass: d.mass, WX: d.wx, WY: d.wy}
}

// SubMap returns a Map representing the portion of the Map d visible
// through r. The returned map shares values with the original map.
func (d *Map) SubMap(r image.Rectangle) *Map {
	r = r.Intersect(d.Rect)
	// If r1 and r2 are Rectangles, r1.Intersect(r2) is not guaranteed to be inside
	// either r1 or r2 if the intersection is empty. Without explicitly checking for
	// this, the Values[i:] expression below can panic.
	if r.Empty() {
		return nil
	}

	i := d.offSet(r.Min.X, r.Min.Y)

	sv := d.Values[i:]
	// Recalculate the mass, weighed x and weighed y
	var sm, swx, swy uint64
	ym := uint64(r.Dy())
	xm := uint64(r.Dx())
	for y := uint64(0); y < ym; y++ {
		for x := uint64(0); x < xm; x++ {
			m := uint64(sv[y*uint64(d.Stride)+x])
			sm += m
			swx += m * x
			swy += m * y
		}
	}

	return &Map{
		Values: sv,
		Stride: d.Stride,
		Rect:   r,
		mass:   sm,
		wx:     swx,
		wy:     swy,
	}
}

// Intersect returns a new Map representing the portion of the Map d visible
// as alpha-masked by density map m. Returns nil if intersection is empty.
func (d *Map) Intersect(m *Map) *Map {
	r := m.Rect.Intersect(d.Rect)
	// If r1 and r2 are Rectangles, r1.Intersect(r2) is not guaranteed to be inside
	// either r1 or r2 if the intersection is empty. Without explicitly checking for
	// this, the Values[i:] expression below can panic.
	if r.Empty() {
		return nil
	}

	// Recalculate the mass, weighed x and weighed y
	var nm, nwx, nwy uint64
	stride := r.Dx()

	dv := d.Values[d.offSet(r.Min.X, r.Min.Y):]
	mv := m.Values[m.offSet(r.Min.X, r.Min.Y):]
	nv := make([]uint16, stride*r.Dy())

	for y := 0; y < r.Dy(); y++ {
		for x := 0; x < stride; x++ {
			// Fixed-point multiply: treat both uint16 values as fractions
			// of 0xffff and round to nearest rather than truncating.
			nv[x+y*stride] = uint16((uint32(dv[x+y*d.Stride])*uint32(mv[x+y*m.Stride]) + 0x7fff) / 0xffff)

			if m := int(nv[x+y*stride]); m != 0 {
				nm += uint64(m)
				nwx += uint64(m * x)
				nwy += uint64(m * y)
			}
		}
	}

	return &Map{
		Values: nv,
		Stride: stride,
		Rect:   r,
		mass:   nm,
		wx:     nwx,
		wy:     nwy,
	}
}

// CompactIntersect returns a new Map representing the portion of the Map
// d visible as alpha-masked by density map m. Compacts to non-zero values.
// Returns nil if intersection is empty.
func (d *Map) CompactIntersect(m *Map) *Map {
	r := m.Rect.Intersect(d.Rect)
	// If r1 and r2 are Rectangles, r1.Intersect(r2) is not guaranteed to be inside
	// either r1 or r2 if the intersection is empty. Without explicitly checking for
	// this, the Values[i:] expression below can panic.
	if r.Empty() {
		return nil
	}

	// Recalculate the mass, weighed x and weighed y

	var nm, nwx, nwy uint64
	stride := r.Dx()

	dv := d.Values[d.offSet(r.Min.X, r.Min.Y):]
	mv := m.Values[m.offSet(r.Min.X, r.Min.Y):]
	nv := make([]uint16, stride*r.Dy())

	// In order to find the lowest and highest X and Y with non-zero
	// values, we initialise both at the opposite end.
	minX := stride
	minY := r.Dy()
	maxX := 0
	maxY := 0

	for y := 0; y < r.Dy(); y++ {
		for x := 0; x < stride; x++ {
			// Fixed-point multiply with round-to-nearest (see Intersect).
			nv[x+y*stride] = uint16((uint32(dv[x+y*d.Stride])*uint32(mv[x+y*m.Stride]) + 0x7fff) / 0xffff)
			m := int(nv[x+y*stride])
			if m != 0 {
				if x < minX {
					minX = x
				}
				if x >= maxX {
					maxX = x + 1
				}
				if y < minY {
					minY = y
				}
				if y >= maxY {
					maxY = y + 1
				}
				nm += uint64(m)
				nwx += uint64(m * x)
				nwy += uint64(m * y)
			}
		}
	}

	if nm == 0 { // Return an empty map
		return nil
	}

	// Tighten bounds to the non-zero bounding box. Stride is kept
	// at the full intersection width so rows remain correctly
	// spaced in the underlying buffer. Because wx and wy are
	// relative to the pre-compacted origin, Center() would give
	// wrong results on the returned map; only ValueAt and Mass
	// should be used.
	r.Max.X = r.Min.X + maxX
	r.Max.Y = r.Min.Y + maxY
	r.Min.X += minX
	r.Min.Y += minY
	return &Map{
		Values: nv[(minY*stride + minX):],
		Stride: stride,
		Rect:   r,
		mass:   nm,
		wx:     nwx,
		wy:     nwy,
	}
}

// ThresholdSplit divides m into two compact maps in a single pass. For
// each pixel, f(x, y) returns a threshold value t. Side a receives
// m[x,y]*t/0xffff and side b receives the complement
// m[x,y]*(0xffff-t)/0xffff. Both outputs are compacted to their
// non-zero bounding boxes. Returns nil, nil when either side has zero
// mass.
func (m *Map) ThresholdSplit(f func(x, y int) uint16) (a, b *Map) {
	r := m.Rect
	w := r.Dx()
	h := r.Dy()
	n := w * h

	// Single allocation split in two to halve malloc and GC overhead.
	buf := make([]uint16, 2*n)
	aN := buf[:n]
	bN := buf[n:]

	var aMass, aWX, aWY uint64
	var bMass, bWX, bWY uint64
	aMinX, aMinY, aMaxX, aMaxY := w, h, 0, 0
	bMinX, bMinY, bMaxX, bMaxY := w, h, 0, 0

	// mOff walks the source at m.Stride (may differ from w for
	// compacted maps); nOff walks the output at w (always dense).
	// Row-level slicing lets the compiler eliminate per-pixel
	// bounds checks.
	mOff := 0
	nOff := 0
	for y := 0; y < h; y++ {
		mRow := m.Values[mOff : mOff+w]
		aRow := aN[nOff : nOff+w]
		bRow := bN[nOff : nOff+w]
		uy := uint64(y)
		for x, mv := range mRow {
			if mv == 0 {
				continue
			}

			t := f(x+r.Min.X, y+r.Min.Y)

			var aVal, bVal uint16
			switch t {
			case 0xffff:
				aVal = mv
			case 0:
				bVal = mv
			default:
				mVal := uint32(mv)
				t32 := uint32(t)
				// Fixed-point multiply with round-to-nearest.
				aVal = uint16((mVal*t32 + 0x7fff) / 0xffff)
				bVal = uint16((mVal*(0xffff-t32) + 0x7fff) / 0xffff)
			}

			aRow[x] = aVal
			bRow[x] = bVal

			if aV := uint64(aVal); aV != 0 {
				if x < aMinX {
					aMinX = x
				}
				if x >= aMaxX {
					aMaxX = x + 1
				}
				if y < aMinY {
					aMinY = y
				}
				if y >= aMaxY {
					aMaxY = y + 1
				}
				aMass += aV
				aWX += aV * uint64(x)
				aWY += aV * uy
			}
			if bV := uint64(bVal); bV != 0 {
				if x < bMinX {
					bMinX = x
				}
				if x >= bMaxX {
					bMaxX = x + 1
				}
				if y < bMinY {
					bMinY = y
				}
				if y >= bMaxY {
					bMaxY = y + 1
				}
				bMass += bV
				bWX += bV * uint64(x)
				bWY += bV * uy
			}
		}
		mOff += m.Stride
		nOff += w
	}

	if aMass == 0 || bMass == 0 {
		return nil, nil
	}

	aR := r
	aR.Max.X = r.Min.X + aMaxX
	aR.Max.Y = r.Min.Y + aMaxY
	aR.Min.X += aMinX
	aR.Min.Y += aMinY

	bR := r
	bR.Max.X = r.Min.X + bMaxX
	bR.Max.Y = r.Min.Y + bMaxY
	bR.Min.X += bMinX
	bR.Min.Y += bMinY

	// Stride is kept at w (the full parent width) so rows
	// remain correctly spaced in the underlying buffer, matching
	// CompactIntersect's layout. As with CompactIntersect, wx and
	// wy are relative to the pre-compacted origin so Center()
	// should not be called on the returned maps.
	a = &Map{
		Values: aN[(aMinY*w + aMinX):],
		Stride: w,
		Rect:   aR,
		mass:   aMass,
		wx:     aWX,
		wy:     aWY,
	}
	b = &Map{
		Values: bN[(bMinY*w + bMinX):],
		Stride: w,
		Rect:   bR,
		mass:   bMass,
		wx:     bWX,
		wy:     bWY,
	}
	return a, b
}

// Stats holds aggregate statistics for a density intersection without
// materialising a per-pixel buffer. It stores the same mass and
// weighted-coordinate data that Map caches internally.
type Stats struct {
	Rect   image.Rectangle
	Mass   uint64
	WX, WY uint64
}

// Center returns the centre of mass, matching Map.Center semantics.
func (s Stats) Center() (x, y float64) {
	x = float64(s.Rect.Min.X) + float64(s.WX)/float64(s.Mass)
	y = float64(s.Rect.Min.Y) + float64(s.WY)/float64(s.Mass)
	return x, y
}

// IntersectStats computes the intersection statistics of d masked by m
// without allocating a pixel buffer. It returns the same mass, weighted
// coordinates, and bounding rect that Intersect would, but avoids the
// per-pixel buffer allocation.
func (d *Map) IntersectStats(m *Map) Stats {
	r := m.Rect.Intersect(d.Rect)
	if r.Empty() {
		return Stats{}
	}

	var nm, nwx, nwy uint64

	dOff := d.offSet(r.Min.X, r.Min.Y)
	mOff := m.offSet(r.Min.X, r.Min.Y)
	stride := r.Dx()
	h := r.Dy()

	// Row-level slicing lets the compiler eliminate per-pixel
	// bounds checks in the inner loop.
	for y := 0; y < h; y++ {
		dRow := d.Values[dOff : dOff+stride]
		mRow := m.Values[mOff : mOff+stride]
		uy := uint64(y)
		for x, dv := range dRow {
			// Fixed-point multiply with round-to-nearest (see Intersect).
			v := uint64((uint32(dv)*uint32(mRow[x]) + 0x7fff) / 0xffff)
			if v != 0 {
				nm += v
				nwx += v * uint64(x)
				nwy += v * uy
			}
		}
		dOff += d.Stride
		mOff += m.Stride
	}

	return Stats{Rect: r, Mass: nm, WX: nwx, WY: nwy}
}

// offSet returns the index into Values for the pixel at (x, y).
func (d *Map) offSet(x, y int) int {
	return (y-d.Rect.Min.Y)*d.Stride + (x - d.Rect.Min.X)
}
