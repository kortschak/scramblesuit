package density

import (
	"image/color"
)

// A Model can convert any color to a density.
type Model interface {
	Convert(c color.Color) uint16
}

// ModelFunc returns a Model that invokes f to implement the conversion.
func ModelFunc(f func(color.Color) uint16) Model {
	return &modelFunc{f}
}

// modelFunc adapts a plain function to the Model interface.
type modelFunc struct {
	f func(color.Color) uint16
}

// Convert delegates to the wrapped function.
func (m *modelFunc) Convert(c color.Color) uint16 {
	return m.f(c)
}

// Default models for density functions. These all linearly map their
// respective channels to a density value.
var (
	AvgDensity      Model = ModelFunc(avgDensity)
	RedDensity      Model = ModelFunc(redDensity)
	GreenDensity    Model = ModelFunc(greenDensity)
	BlueDensity     Model = ModelFunc(blueDensity)
	AlphaDensity    Model = ModelFunc(alphaDensity)
	NegAvgDensity   Model = ModelFunc(negAvgDensity)
	NegRedDensity   Model = ModelFunc(negRedDensity)
	NegGreenDensity Model = ModelFunc(negGreenDensity)
	NegBlueDensity  Model = ModelFunc(negBlueDensity)
	NegAlphaDensity Model = ModelFunc(negAlphaDensity)
)

func avgDensity(c color.Color) uint16 {
	r, g, b, _ := c.RGBA()
	return uint16((r + g + b + 1) / 3)
}

func redDensity(c color.Color) uint16 {
	r, _, _, _ := c.RGBA()
	return uint16(r)
}

func greenDensity(c color.Color) uint16 {
	_, g, _, _ := c.RGBA()
	return uint16(g)
}

func blueDensity(c color.Color) uint16 {
	_, _, b, _ := c.RGBA()
	return uint16(b)
}

func alphaDensity(c color.Color) uint16 {
	_, _, _, a := c.RGBA()
	return uint16(a)
}

func negAvgDensity(c color.Color) uint16 {
	r, g, b, _ := c.RGBA()
	return uint16(0xffff - (r+g+b)/3)
}

func negRedDensity(c color.Color) uint16 {
	r, _, _, _ := c.RGBA()
	return uint16(0xffff - r)
}

func negGreenDensity(c color.Color) uint16 {
	_, g, _, _ := c.RGBA()
	return uint16(0xffff - g)
}

func negBlueDensity(c color.Color) uint16 {
	_, _, b, _ := c.RGBA()
	return uint16(0xffff - b)
}

func negAlphaDensity(c color.Color) uint16 {
	_, _, _, a := c.RGBA()
	return uint16(0xffff - a)
}
