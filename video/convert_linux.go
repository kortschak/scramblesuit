//go:build linux

package video

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
)

// EncodeFrame renders an image into raw bytes for the given pixel
// format, suitable for writing to a loopback device with WriteFrame.
// If the pf is PixelFormatMJPEG and qual is non-zero, qual specifies
// the JPEG quality, see [jpeg.Options].
func EncodeFrame(img image.Image, pf PixelFormat, qual int) ([]byte, error) {
	switch pf {
	case PixelFormatMJPEG:
		var opt *jpeg.Options
		if qual != 0 {
			opt = &jpeg.Options{Quality: qual}
		}
		var buf bytes.Buffer
		err := jpeg.Encode(&buf, img, opt)
		if err != nil {
			return nil, err
		}
		return buf.Bytes(), nil
	case PixelFormatYUYV:
		return imageToYUYV(img), nil
	default:
		return nil, fmt.Errorf("unsupported pixel format %s", pf)
	}
}

// imageToYUYV converts an image to YUYV 4:2:2 packed bytes.
//
// Each pair of horizontal pixels shares chroma (Cb/Cr) samples,
// averaged from both pixels:
//
//	[Y0 U Y1 V]
func imageToYUYV(img image.Image) []byte {
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	data := make([]byte, width*height*2)
	for row := 0; row < height; row++ {
		off := row * width * 2
		for col := 0; col < width; col += 2 {
			r0, g0, b0, _ := img.At(bounds.Min.X+col, bounds.Min.Y+row).RGBA()
			y0, cb0, cr0 := color.RGBToYCbCr(uint8(r0>>8), uint8(g0>>8), uint8(b0>>8))

			col1 := col + 1
			if col1 >= width {
				col1 = col
			}
			r1, g1, b1, _ := img.At(bounds.Min.X+col1, bounds.Min.Y+row).RGBA()
			y1, cb1, cr1 := color.RGBToYCbCr(uint8(r1>>8), uint8(g1>>8), uint8(b1>>8))

			i := off + col*2
			data[i] = y0
			data[i+1] = uint8((uint16(cb0) + uint16(cb1)) / 2)
			data[i+2] = y1
			data[i+3] = uint8((uint16(cr0) + uint16(cr1)) / 2)
		}
	}
	return data
}

// yuyvToImage converts a YUYV 4:2:2 frame to an NRGBA image.
//
// YUYV packs two pixels into every four bytes:
//
//	[Y0 U Y1 V]
//
// Each pixel pair shares U (Cb) and V (Cr) chroma samples.
// We use color.YCbCrToRGB from stdlib for the conversion.
func yuyvToImage(data []byte, width, height int) image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		off := y * width * 2
		for x := 0; x < width; x += 2 {
			i := off + x*2
			if i+3 >= len(data) {
				return img
			}
			y0, u, y1, v := data[i], data[i+1], data[i+2], data[i+3]

			r0, g0, b0 := color.YCbCrToRGB(y0, u, v)
			r1, g1, b1 := color.YCbCrToRGB(y1, u, v)

			img.SetNRGBA(x, y, color.NRGBA{R: r0, G: g0, B: b0, A: 255})
			img.SetNRGBA(x+1, y, color.NRGBA{R: r1, G: g1, B: b1, A: 255})
		}
	}
	return img
}
