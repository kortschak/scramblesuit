// The fracture program applies a dipole Voronoi subdivision filter to an image.
package main

import (
	"flag"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/kortschak/scramblesuit/fracture"
)

func main() {
	qual := flag.Int("qual", 90, "jpeg quality")
	gen := flag.Int("g", 7, "generations")
	mono := flag.Bool("mono", false, "monochrome")
	weighted := flag.Bool("weighted", false, "weighted splits")
	vector := flag.Bool("vector", false, "vector output (SVG by default, raster with -size)")
	size := flag.String("size", "", "output size WxH, implies raster output (requires -vector)")
	out := flag.String("o", "", "output path")
	flag.Parse()

	path := flag.Arg(0)
	f, err := os.Open(path)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	img, format, err := image.Decode(f)
	if err != nil {
		log.Fatalln(err, "Could not decode image:", path)
	}

	ext := filepath.Ext(path)

	if !*vector {
		img = fracture.Raster(img, *gen, *mono, *weighted)

		if *out != "" {
			path = *out
		} else {
			path = filepath.Base(strings.TrimSuffix(path, ext) + "_dipole" + ext)
		}
		writeRaster(img, path, format, *qual)
		return
	}

	vi := fracture.Vector(img, *gen, *mono, *weighted)
	if *size != "" {
		var w, h int
		_, err := fmt.Sscanf(*size, "%dx%d", &w, &h)
		if err != nil {
			log.Fatalf("invalid size %q: %v", *size, err)
		}
		dst := image.NewNRGBA(image.Rect(0, 0, w, h))
		vi.RenderTo(dst)

		if *out != "" {
			path = *out
		} else {
			path = filepath.Base(strings.TrimSuffix(path, ext) + "_dipole" + ext)
		}
		writeRaster(dst, path, format, *qual)
		return
	}

	if *out != "" {
		path = *out
	} else {
		path = filepath.Base(strings.TrimSuffix(path, ext) + "_dipole.svg")
	}
	output, err := os.Create(path)
	if err != nil {
		log.Fatal(err)
	}
	defer output.Close()
	if err := vi.WriteSVG(output); err != nil {
		log.Fatal(err)
	}
}

func writeRaster(img image.Image, path, format string, qual int) {
	f, err := os.Create(path)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	switch format {
	case "png":
		png.Encode(f, img)
	case "jpeg":
		jpeg.Encode(f, img, &jpeg.Options{Quality: qual})
	default:
		panic(format)
	}
}
