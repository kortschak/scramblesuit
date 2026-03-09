package fracture

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"io"
	"math"
	"runtime"
	"slices"
	"sync"

	"github.com/kortschak/scramblesuit/density"
)

// VectorImage holds the result of a vector fracture. Layers contains
// one cell slice for mono, three (R, G, B) for colour.
type VectorImage struct {
	rect   image.Rectangle
	mono   bool
	layers [][]vectorCell
}

// Vector performs gen generations of vector dipole subdivision
// on img and returns a vectorImage of convex polygon cells.
func Vector(img image.Image, gen int, mono, weighted bool) *VectorImage {
	vi := &VectorImage{rect: img.Bounds(), mono: mono}

	if mono {
		dm := newVectorDipoleMap(img, density.AvgDensity, density.NegAvgDensity, uint(1<<gen))
		for range gen {
			dm.fracture(weighted)
		}
		vi.layers = [][]vectorCell{dm.renderVector()}
	} else {
		cdm := newVectorColorDipoleMap(img, uint(1<<gen))
		for range gen {
			cdm.fracture(weighted)
		}
		var rCells, gCells, bCells []vectorCell
		var wg sync.WaitGroup
		wg.Add(3)
		go func() { rCells = cdm.R.renderVector(); wg.Done() }()
		go func() { gCells = cdm.G.renderVector(); wg.Done() }()
		go func() { bCells = cdm.B.renderVector(); wg.Done() }()
		wg.Wait()
		vi.layers = [][]vectorCell{rCells, gCells, bCells}
	}
	return vi
}

// WriteSVG writes the vector image as an SVG document.
func (vi *VectorImage) WriteSVG(w io.Writer) error {
	r := vi.rect
	if _, err := fmt.Fprintf(w, "<svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"%d %d %d %d\">\n",
		r.Min.X, r.Min.Y, r.Dx(), r.Dy()); err != nil {
		return err
	}

	if vi.mono {
		for _, cell := range vi.layers[0] {
			v := cell.Intensity >> 8
			if err := writeSVGPolygon(w, cell.Vertices, fmt.Sprintf("rgb(%d,%d,%d)", v, v, v)); err != nil {
				return err
			}
		}
	} else {
		// Black background ensures screen blending composes the
		// channels additively.
		fmt.Fprintf(w, "<rect x=\"%d\" y=\"%d\" width=\"%d\" height=\"%d\" fill=\"black\"/>\n",
			r.Min.X, r.Min.Y, r.Dx(), r.Dy())

		type chanSpec struct {
			cells []vectorCell
			color func(uint16) string
		}
		channels := []chanSpec{
			{vi.layers[0], func(v uint16) string { return fmt.Sprintf("rgb(%d,0,0)", v>>8) }},
			{vi.layers[1], func(v uint16) string { return fmt.Sprintf("rgb(0,%d,0)", v>>8) }},
			{vi.layers[2], func(v uint16) string { return fmt.Sprintf("rgb(0,0,%d)", v>>8) }},
		}
		for _, ch := range channels {
			fmt.Fprint(w, "<g style=\"mix-blend-mode:screen\">\n")
			for _, cell := range ch.cells {
				if err := writeSVGPolygon(w, cell.Vertices, ch.color(cell.Intensity)); err != nil {
					return err
				}
			}
			fmt.Fprint(w, "</g>\n")
		}
	}

	_, err := fmt.Fprint(w, "</svg>\n")
	return err
}

// writeSVGPolygon writes a single SVG polygon element with the given
// fill colour.
func writeSVGPolygon(w io.Writer, verts []point2D, fill string) error {
	if _, err := fmt.Fprintf(w, "<polygon fill=\"%s\" points=\"", fill); err != nil {
		return err
	}
	for i, v := range verts {
		if i > 0 {
			fmt.Fprint(w, " ")
		}
		fmt.Fprintf(w, "%.4f,%.4f", v.X, v.Y)
	}
	_, err := fmt.Fprint(w, "\"/>\n")
	return err
}

// RenderTo rasterises the vector polygons into dst, which may have
// different dimensions and aspect ratio from the source image.
func (vi *VectorImage) RenderTo(dst draw.Image) {
	dstB := dst.Bounds()
	srcB := vi.rect
	dstW := dstB.Dx()
	dstH := dstB.Dy()

	// Affine transform: dst_coord = src_coord*scale + offset.
	// The offset accounts for both destination origin and scaled
	// source origin so that srcB maps exactly onto dstB.
	sx := float64(dstW) / float64(srcB.Dx())
	sy := float64(dstH) / float64(srcB.Dy())
	ox := float64(dstB.Min.X) - float64(srcB.Min.X)*sx
	oy := float64(dstB.Min.Y) - float64(srcB.Min.Y)*sy

	nCh := len(vi.layers)
	buf := make([]uint16, nCh*dstW*dstH)

	for ch, layer := range vi.layers {
		chBuf := buf[ch*dstW*dstH : (ch+1)*dstW*dstH]
		for _, cell := range layer {
			scaled := scaleVertices(cell.Vertices, sx, sy, ox, oy)
			scanlineFill(chBuf, dstW, scaled, cell.Intensity, dstB)
		}
	}

	if vi.mono {
		for y := dstB.Min.Y; y < dstB.Max.Y; y++ {
			for x := dstB.Min.X; x < dstB.Max.X; x++ {
				i := (x - dstB.Min.X) + (y-dstB.Min.Y)*dstW
				v := uint8(buf[i] >> 8)
				dst.Set(x, y, color.NRGBA{R: v, G: v, B: v, A: 0xff})
			}
		}
	} else {
		rBuf := buf[:dstW*dstH]
		gBuf := buf[dstW*dstH : 2*dstW*dstH]
		bBuf := buf[2*dstW*dstH:]
		for y := dstB.Min.Y; y < dstB.Max.Y; y++ {
			for x := dstB.Min.X; x < dstB.Max.X; x++ {
				i := (x - dstB.Min.X) + (y-dstB.Min.Y)*dstW
				dst.Set(x, y, color.NRGBA{
					R: uint8(rBuf[i] >> 8),
					G: uint8(gBuf[i] >> 8),
					B: uint8(bBuf[i] >> 8),
					A: 0xff,
				})
			}
		}
	}
}

// vectorDipole extends dipole with accumulated half-plane constraints
// that define its spatial region geometrically.
type vectorDipole struct {
	dipole
	planes []constraint
}

// vectorDipoleMap holds vector dipoles over a single density channel.
type vectorDipoleMap struct {
	dipoles []vectorDipole
	work    []vectorDipole
	rect    image.Rectangle
}

// newVectorDipoleMap creates a vectorDipoleMap from img using the given
// north and south density models, pre-allocated for n dipoles.
func newVectorDipoleMap(img image.Image, north, south density.Model, n uint) *vectorDipoleMap {
	if n == 0 {
		n = 1
	}
	r := img.Bounds()
	dm := &vectorDipoleMap{
		dipoles: make([]vectorDipole, 1, n),
		work:    make([]vectorDipole, n),
		rect:    r,
	}

	mask := density.NewFullMap(r)
	var northSrc, southSrc *density.Map
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { northSrc = density.MapFrom(img, north); wg.Done() }()
	go func() { southSrc = density.MapFrom(img, south); wg.Done() }()
	wg.Wait()
	dm.dipoles[0] = vectorDipole{
		dipole: dipole{
			northSrc:   northSrc,
			southSrc:   southSrc,
			northStats: northSrc.Stats(),
			southStats: southSrc.Stats(),
			mask:       mask,
		},
	}
	return dm
}

// fracture splits each existing dipole in parallel, bounded by NumCPU,
// and appends the new halves to the dipole list.
func (dm *vectorDipoleMap) fracture(weighted bool) {
	l := len(dm.dipoles)
	b := make([]*vectorDipole, l)

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
	b = slices.DeleteFunc(b, func(d *vectorDipole) bool {
		return d == nil
	})
	for _, d := range b {
		dm.dipoles = append(dm.dipoles, *d)
	}
}

// split divides dipole i and records the half-plane constraint on both
// halves.
func (dm *vectorDipoleMap) split(i int, weighted bool) *vectorDipole {
	dp := &dm.dipoles[i]
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

	// Build the child's plane list before modifying the parent.
	// ThresholdSplit returns nm1 as the "below" side (positive signed
	// distance → below: true for the parent) and nm2 as the "above"
	// side (below: false for the child).
	childPlanes := make([]constraint, len(dp.planes)+1)
	copy(childPlanes, dp.planes)
	childPlanes[len(dp.planes)] = constraint{line: line, below: false}

	dp.planes = append(dp.planes, constraint{line: line, below: true})
	dp.dipole.setMask(nm1)

	dm.work[i] = vectorDipole{
		dipole: newDipole(dp.northSrc, dp.southSrc, nm2),
		planes: childPlanes,
	}
	return &dm.work[i]
}

// renderVector computes polygon vertices and fill intensities for all
// dipoles.
func (dm *vectorDipoleMap) renderVector() []vectorCell {
	rect := []point2D{
		{X: float64(dm.rect.Min.X), Y: float64(dm.rect.Min.Y)},
		{X: float64(dm.rect.Max.X), Y: float64(dm.rect.Min.Y)},
		{X: float64(dm.rect.Max.X), Y: float64(dm.rect.Max.Y)},
		{X: float64(dm.rect.Min.X), Y: float64(dm.rect.Max.Y)},
	}

	cells := make([]vectorCell, 0, len(dm.dipoles))
	for i := range dm.dipoles {
		d := &dm.dipoles[i]
		poly := make([]point2D, len(rect))
		copy(poly, rect)
		for _, c := range d.planes {
			poly = clipPolygon(poly, c)
			if len(poly) == 0 {
				break
			}
		}
		if len(poly) == 0 {
			continue
		}

		// Mass ratio scaled to uint16 range; equivalent to the raster
		// path's density.Map.Intersect which computes the same quotient
		// per pixel.
		fill := uint16(d.northStats.Mass * 0xffff / d.mask.Mass())
		cells = append(cells, vectorCell{Vertices: poly, Intensity: fill})
	}
	return cells
}

// vectorCell is a convex polygon with a flat fill intensity.
type vectorCell struct {
	Vertices  []point2D
	Intensity uint16
}

// clipPolygon clips a convex polygon against a half-plane using
// Sutherland-Hodgman.
func clipPolygon(poly []point2D, c constraint) []point2D {
	if len(poly) == 0 {
		return nil
	}
	var out []point2D
	prev := poly[len(poly)-1]
	prevIn := c.inside(prev)
	for _, curr := range poly {
		currIn := c.inside(curr)
		if currIn {
			if !prevIn {
				out = append(out, c.intersectEdge(prev, curr))
			}
			out = append(out, curr)
		} else if prevIn {
			out = append(out, c.intersectEdge(prev, curr))
		}
		prev = curr
		prevIn = currIn
	}
	return out
}

// constraint represents one side of a split line.
type constraint struct {
	line  splitLine
	below bool // true: below/left side (side A in ThresholdSplit)
}

// signedDist returns the signed distance of p from the boundary line.
// Positive means above/right; negative means below/left.
func (c constraint) signedDist(p point2D) float64 {
	if c.line.horiz {
		return p.Y - c.line.cy - c.line.slope*(p.X-c.line.cx)
	}
	return p.X - c.line.cx - c.line.slope*(p.Y-c.line.cy)
}

// inside reports whether p is on the inside of the half-plane.
func (c constraint) inside(p point2D) bool {
	d := c.signedDist(p)
	if c.below {
		return d < 0
	}
	return d >= 0
}

// intersectEdge returns the point where edge p1→p2 crosses the
// constraint's boundary line.
func (c constraint) intersectEdge(p1, p2 point2D) point2D {
	d1 := c.signedDist(p1)
	d2 := c.signedDist(p2)
	t := d1 / (d1 - d2)
	return point2D{
		X: p1.X + t*(p2.X-p1.X),
		Y: p1.Y + t*(p2.Y-p1.Y),
	}
}

// splitLine describes the perpendicular bisector of a dipole split.
// When horiz is true the line is y = cy + slope*(x-cx) and the
// threshold tests y; when false the line is x = cx + slope*(y-cy)
// and the threshold tests x.
type splitLine struct {
	cx, cy float64
	slope  float64
	horiz  bool
}

// computeSplitLine determines the perpendicular bisector between the
// north and south centres of mass. The bisector is parameterised along
// whichever image axis keeps the slope shallowest: when horiz is true
// it runs as y(x), otherwise as x(y). The equal-displacement case
// falls back to a straight horizontal or vertical cut along the
// shorter bounds dimension.
func computeSplitLine(northStats, southStats density.Stats, bounds image.Rectangle, weighted bool) splitLine {
	x0, y0 := northStats.Center()
	x1, y1 := southStats.Center()

	// When weighted, the midpoint is pushed toward the heavier pole
	// by weighting each centre with the opposite pole's mass. This
	// gives the lighter pole more area to compensate for its lower
	// density.
	var cx, cy float64
	if weighted {
		w0 := float64(northStats.Mass)
		w1 := float64(southStats.Mass)
		cx = (x0*w1 + x1*w0) / (w0 + w1)
		cy = (y0*w1 + y1*w0) / (w0 + w1)
	} else {
		cx = (x0 + x1) / 2
		cy = (y0 + y1) / 2
	}

	dx := x1 - x0
	dy := y1 - y0
	var h bool
	var slope float64
	if math.Abs(dx) < math.Abs(dy) {
		h = true
		slope = -dx / dy
	} else if math.Abs(dy) < math.Abs(dx) {
		slope = -dy / dx
	} else {
		h = bounds.Dx() < bounds.Dy()
	}

	return splitLine{cx: cx, cy: cy, slope: slope, horiz: h}
}

// vectorColorDipoleMap applies vector dipole subdivision independently
// to R, G, B channels.
type vectorColorDipoleMap struct {
	wg      sync.WaitGroup
	R, G, B *vectorDipoleMap
}

// newVectorColorDipoleMap initialises three vector dipole maps, one
// per RGB channel, concurrently.
func newVectorColorDipoleMap(img image.Image, n uint) *vectorColorDipoleMap {
	c := &vectorColorDipoleMap{}
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		c.R = newVectorDipoleMap(img, density.RedDensity, density.NegRedDensity, n)
		wg.Done()
	}()
	go func() {
		c.G = newVectorDipoleMap(img, density.GreenDensity, density.NegGreenDensity, n)
		wg.Done()
	}()
	go func() {
		c.B = newVectorDipoleMap(img, density.BlueDensity, density.NegBlueDensity, n)
		wg.Done()
	}()
	wg.Wait()
	return c
}

// fracture performs one generation of subdivision on all three channels
// concurrently.
func (c *vectorColorDipoleMap) fracture(weighted bool) {
	c.wg.Add(3)
	go func() { c.R.fracture(weighted); c.wg.Done() }()
	go func() { c.G.fracture(weighted); c.wg.Done() }()
	go func() { c.B.fracture(weighted); c.wg.Done() }()
	c.wg.Wait()
}

// scaleVertices applies an affine transform to map source-image
// coordinates into destination-image coordinates.
func scaleVertices(verts []point2D, sx, sy, ox, oy float64) []point2D {
	scaled := make([]point2D, len(verts))
	for i, v := range verts {
		scaled[i] = point2D{X: v.X*sx + ox, Y: v.Y*sy + oy}
	}
	return scaled
}

// scanlineFill rasterises a convex polygon into buf using pixel-center
// sampling. Each filled pixel is set to val.
func scanlineFill(buf []uint16, stride int, poly []point2D, val uint16, bounds image.Rectangle) {
	if len(poly) < 3 {
		return
	}

	yMin, yMax := poly[0].Y, poly[0].Y
	for _, v := range poly[1:] {
		if v.Y < yMin {
			yMin = v.Y
		}
		if v.Y > yMax {
			yMax = v.Y
		}
	}

	// Map continuous Y range to discrete rows using pixel-center
	// sampling: a pixel at row y covers [y, y+1) and its centre is
	// y+0.5. Subtracting 0.5 converts from continuous to the
	// integer row whose centre first/last falls inside the polygon.
	yStart := int(math.Ceil(yMin - 0.5))
	yEnd := int(math.Floor(yMax - 0.5))
	if yStart < bounds.Min.Y {
		yStart = bounds.Min.Y
	}
	if yEnd >= bounds.Max.Y {
		yEnd = bounds.Max.Y - 1
	}

	n := len(poly)
	for y := yStart; y <= yEnd; y++ {
		fy := float64(y) + 0.5
		xL := math.Inf(1)
		xR := math.Inf(-1)
		for i := range n {
			p1 := poly[i]
			p2 := poly[(i+1)%n]
			if (p1.Y <= fy && p2.Y > fy) || (p2.Y <= fy && p1.Y > fy) {
				t := (fy - p1.Y) / (p2.Y - p1.Y)
				x := p1.X + t*(p2.X-p1.X)
				if x < xL {
					xL = x
				}
				if x > xR {
					xR = x
				}
			}
		}
		if math.IsInf(xL, 1) {
			continue
		}
		xStart := int(math.Ceil(xL - 0.5))
		xEnd := int(math.Floor(xR - 0.5))
		if xStart < bounds.Min.X {
			xStart = bounds.Min.X
		}
		if xEnd >= bounds.Max.X {
			xEnd = bounds.Max.X - 1
		}
		row := (y - bounds.Min.Y) * stride
		for x := xStart; x <= xEnd; x++ {
			buf[row+x-bounds.Min.X] = val
		}
	}
}

// point2D is a 2D point with floating-point coordinates.
type point2D struct {
	X, Y float64
}
