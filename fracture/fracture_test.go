package fracture

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"gonum.org/v1/plot/cmpimg"
)

func TestRaster(t *testing.T) {
	for i := range 8 {
		gen := i + 1
		t.Run(fmt.Sprintf("%d_generations", gen), func(t *testing.T) {
			dstPath := fmt.Sprintf("gopher_fractured_%d.png", gen)
			cmpimg.CheckPlot(func() {
				f, err := os.Open("testdata/gopher.png")
				if err != nil {
					t.Fatalf("failed to open image: %v", err)
				}
				img, _, err := image.Decode(f)
				if err != nil {
					t.Fatalf("failed to decode image: %v", err)
				}
				img = Raster(img, gen, false, false)
				dst, err := os.Create(filepath.Join("testdata", dstPath))
				if err != nil {
					t.Fatalf("failed to create file: %v", err)
				}
				defer dst.Close()

				err = png.Encode(dst, img)
				if err != nil {
					t.Fatalf("failed to write image: %v", err)
				}
			}, t, dstPath)
			os.Remove(filepath.Join("testdata", dstPath))
		})
	}
}

func TestVector(t *testing.T) {
	for i := range 8 {
		gen := i + 1
		t.Run(fmt.Sprintf("%d_generations", gen), func(t *testing.T) {
			dstPath := fmt.Sprintf("gopher_vector_fractured_%d.png", gen)
			cmpimg.CheckPlot(func() {
				f, err := os.Open("testdata/gopher.png")
				if err != nil {
					t.Fatalf("failed to open image: %v", err)
				}
				img, _, err := image.Decode(f)
				if err != nil {
					t.Fatalf("failed to decode image: %v", err)
				}
				vi := Vector(img, gen, false, false)

				if len(vi.layers) != 3 {
					t.Fatalf("expected 3 layers, got %d", len(vi.layers))
				}
				maxCells := 1 << gen
				for ch, layer := range vi.layers {
					if len(layer) == 0 {
						t.Errorf("channel %d: no cells", ch)
					}
					if len(layer) > maxCells {
						t.Errorf("channel %d: %d cells > max %d", ch, len(layer), maxCells)
					}
					for j, cell := range layer {
						if len(cell.Vertices) < 3 {
							t.Errorf("channel %d cell %d: degenerate polygon (%d vertices)", ch, j, len(cell.Vertices))
						}
					}
				}

				dstImg := image.NewNRGBA(img.Bounds())
				vi.RenderTo(dstImg)

				dst, err := os.Create(filepath.Join("testdata", dstPath))
				if err != nil {
					t.Fatalf("failed to create file: %v", err)
				}
				defer dst.Close()

				err = png.Encode(dst, dstImg)
				if err != nil {
					t.Fatalf("failed to write image: %v", err)
				}
			}, t, dstPath)
			os.Remove(filepath.Join("testdata", dstPath))
		})
	}
}

func TestFractureVectorSVG(t *testing.T) {
	f, err := os.Open("testdata/gopher.png")
	if err != nil {
		t.Fatalf("failed to open image: %v", err)
	}
	img, _, err := image.Decode(f)
	if err != nil {
		t.Fatalf("failed to decode image: %v", err)
	}
	f.Close()

	vi := Vector(img, 3, false, false)
	var buf bytes.Buffer
	if err := vi.WriteSVG(&buf); err != nil {
		t.Fatalf("writeSVG: %v", err)
	}
	svg := buf.String()
	if !bytes.Contains([]byte(svg), []byte("<svg")) {
		t.Error("SVG output missing <svg> element")
	}
	if !bytes.Contains([]byte(svg), []byte("<polygon")) {
		t.Error("SVG output missing <polygon> elements")
	}
	if !bytes.Contains([]byte(svg), []byte("mix-blend-mode:screen")) {
		t.Error("SVG output missing screen blend mode for RGB")
	}
}

var (
	sink       image.Image
	vectorSink *VectorImage
)

func BenchmarkFracture(b *testing.B) {
	f, err := os.Open("testdata/gopher.png")
	if err != nil {
		b.Fatalf("failed to open image: %v", err)
	}
	img, _, err := image.Decode(f)
	if err != nil {
		b.Fatalf("failed to decode image: %v", err)
	}

	b.Run("raster", func(b *testing.B) {
		for i := range 8 {
			gen := i + 1
			b.Run(fmt.Sprintf("%d_generations", gen), func(b *testing.B) {
				for range b.N {
					sink = Raster(img, gen, false, false)
				}
			})
		}
	})
	b.Run("vector", func(b *testing.B) {
		for i := range 8 {
			gen := i + 1
			b.Run(fmt.Sprintf("%d_generations", gen), func(b *testing.B) {
				for range b.N {
					vectorSink = Vector(img, gen, false, false)
				}
			})
		}
	})
}
