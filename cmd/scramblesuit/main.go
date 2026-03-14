// The scramblesuit program reads images or image streams from
// a video device and either write the image to a file or the
// video stream to a v4l2loopback device, optionally obfuscating
// the images by dipole Voronoi subdivision.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"log"
	"os"
	"os/signal"
	"strings"

	"golang.org/x/image/draw"

	"github.com/kortschak/scramblesuit/fracture"
	"github.com/kortschak/scramblesuit/video"
)

func main() {
	dev := flag.String("dev", "/dev/video0", "V4L2 device path")
	out := flag.String("out", "frame.png", "output PNG path")
	warmup := flag.Int("warmup", 20, "frames to discard for auto-exposure settling")
	list := flag.Bool("list", false, "list supported formats and controls, then exit")
	loopback := flag.String("loopback", "", "v4l2loopback device to mirror frames to: processed[,pass-through] (e.g. /dev/video2 or /dev/video2,/dev/video3)")

	scramble := flag.Bool("scramble", true, "scramble output")
	qual := flag.Int("qual", 75, "jpeg quality")
	gen := flag.Int("g", 8, "generations")
	format := flag.String("format", "png", "single shot image format (png or jpeg)")
	down := flag.Float64("downsample", 1, "down sample scaling factor")

	flag.Parse()

	if *list {
		d, err := video.ProbeDevice(*dev)
		if err != nil {
			log.Fatal(err)
		}
		defer d.Close()
		printDeviceInfo(d)
		return
	}

	if *loopback != "" {
		ok, err := isLive("v4l2loopback")
		if err != nil {
			log.Fatal(err)
		}
		if !ok {
			n, ok := strings.CutPrefix(*loopback, "/dev/video")
			if !ok {
				log.Fatal("no live v4l2loopback module: run modprobe v4l2loopback ...")
			}
			log.Fatalf("no live v4l2loopback module: run modprobe v4l2loopback exclusive_caps=1 video_nr=%s card_label=...", n)
		}

		runLoopback(*dev, *loopback, *scramble, *gen, *down, *qual)
		return
	}

	d, err := video.OpenDevice(*dev)
	if err != nil {
		log.Fatal(err)
	}
	defer d.Close()

	fmt.Printf("capturing %dx%d (format %s) from %s\n", d.Width, d.Height, d.PixFmt.String(), *dev)

	img, err := d.CaptureFrame(*warmup)
	if err != nil {
		log.Fatal(err)
	}

	if *scramble {
		bounds := img.Bounds()
		dst := image.NewNRGBA(bounds)
		if *down != 1 {
			tmp := image.NewNRGBA(image.Rectangle{
				Max: image.Point{
					X: int(float64(bounds.Dx()) * *down),
					Y: int(float64(bounds.Dy()) * *down),
				},
			})
			draw.BiLinear.Scale(tmp, tmp.Bounds(), img, img.Bounds(), draw.Src, nil)
			img = tmp
		}
		vi := fracture.Vector(img, *gen, false, false)
		vi.RenderTo(dst)
		writeRaster(dst, *out, *format, *qual)
		return
	}

	f, err := os.Create(*out)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	err = png.Encode(f, img)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("wrote %s\n", *out)
}

func isLive(mod string) (bool, error) {
	modules, err := os.Open("/proc/modules")
	if err != nil {
		log.Fatal(err)
	}
	const ( // See https://github.com/torvalds/linux/blob/651690480a965ca196ce42d4562543f3e61cb226/kernel/module/procfs.c#L74-L104.
		name = iota
		size
		usage
		deps
		state
		offset
	)
	sc := bufio.NewScanner(modules)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if fields[name] != mod {
			continue
		}
		if fields[state] == "Live" {
			return true, nil
		}
	}
	return false, sc.Err()
}

// runLoopback captures frames from devPath and writes them to the
// v4l2loopback device at loopPath until interrupted.
func runLoopback(devPath, loopPath string, scramble bool, gen int, down float64, qual int) {
	d, err := video.OpenDevice(devPath)
	if err != nil {
		log.Fatal(err)
	}
	defer d.Close()

	loopPath, passThrough, ok := strings.Cut(loopPath, ",")
	lb, err := video.OpenLoopback(loopPath, d)
	if err != nil {
		log.Fatal(err)
	}
	defer lb.Close()
	var pt *video.Loopback
	if ok && scramble {
		pt, err = video.OpenLoopback(passThrough, d)
		if err != nil {
			log.Fatal(err)
		}
		defer pt.Close()
	}

	err = d.Start()
	if err != nil {
		log.Fatal(err)
	}
	defer d.Stop()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	var process = "mirroring"
	if scramble {
		process = "scrambling"
	}
	fmt.Printf("%s %dx%d (%s) %s → %s",
		process, d.Width, d.Height, d.PixFmt, devPath, loopPath)
	if pt != nil {
		fmt.Printf(" (pass-through %s → %s)", devPath, passThrough)
	}
	fmt.Println("\n(ctrl-c to stop)")

	var frames uint64
	token := make(chan struct{}, 1)
	for ctx.Err() == nil {
		if !scramble {
			raw, err := d.ReadRawFrame()
			if err != nil {
				if ctx.Err() != nil {
					break
				}
				log.Fatal(err)
			}
			err = lb.WriteFrame(raw)
			if err != nil {
				log.Fatal(err)
			}
			frames++

			continue
		}

		if pt != nil {
			// If we are running pass-through, we don't want to hold
			// up the unscrambled frames, so handle them first.
			raw, err := d.ReadRawFrame()
			if err != nil {
				if ctx.Err() != nil {
					break
				}
				log.Fatal(err)
			}
			err = pt.WriteFrame(raw)
			if err != nil {
				log.Fatal(err)
			}

			// We also don't want to block pass-through on scrambling,
			// so drop scramble frames if we are still working on one.
			go func() {
				select {
				case token <- struct{}{}:
					// We have an opportunity to run
					// a scramble frame, take it and
					// block any other frame scrambling
					// until we are done.
					defer func() {
						<-token
					}()
				default:
					// Drop frame.
					return
				}
				img, err := d.Decode(raw)
				if err != nil {
					log.Fatal(err)
				}
				err = scrambleTo(lb, img, down, gen, d.PixFmt, qual)
				if err != nil {
					log.Fatal(err)
				}
			}()
		} else {
			img, err := d.ReadFrame()
			if err != nil {
				if ctx.Err() != nil {
					break
				}
				log.Fatal(err)
			}

			// If we're not running pass-through, always scramble. If
			// we lose frames, it's better here to lose them at the
			// V4L2 frame capture rather than collecting that and then
			// dropping it with the logic above.
			err = scrambleTo(lb, img, down, gen, d.PixFmt, qual)
			if err != nil {
				log.Fatal(err)
			}
		}

		frames++
	}
	fmt.Printf("\n%d frames mirrored\n", frames)
}

func scrambleTo(dev *video.Loopback, img image.Image, down float64, gen int, pixfmt video.PixelFormat, qual int) error {
	bounds := img.Bounds()
	dst := image.NewNRGBA(bounds)
	if down != 1 {
		tmp := image.NewNRGBA(image.Rectangle{
			Max: image.Point{
				X: int(float64(bounds.Dx()) * down),
				Y: int(float64(bounds.Dy()) * down),
			},
		})
		draw.BiLinear.Scale(tmp, tmp.Bounds(), img, img.Bounds(), draw.Src, nil)
		img = tmp
	}
	vi := fracture.Vector(img, gen, false, false)
	vi.RenderTo(dst)

	raw, err := video.EncodeFrame(dst, pixfmt, qual)
	if err != nil {
		return err
	}
	return dev.WriteFrame(raw)
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

// printDeviceInfo lists a device's supported formats and controls to stdout.
func printDeviceInfo(d *video.Device) {
	formats := d.ListFormats()
	fmt.Printf("%s:\n  Formats (%d):\n", d.Path(), len(formats))
	for _, f := range formats {
		fmt.Printf("    %-6s %s\n", f.PixelFormat, f.Description)
	}

	controls := d.ListControls()
	fmt.Printf("  Controls (%d):\n", len(controls))
	for _, c := range controls {
		cur := "?"
		val, err := d.GetControl(c.ID)
		if err == nil {
			cur = fmt.Sprintf("%d", val)
		}
		fmt.Printf("    %-32s %-7s min=%-6d max=%-6d step=%-3d default=%-6d current=%s\n",
			c.Name, c.TypeName(), c.Minimum, c.Maximum, c.Step, c.DefaultValue, cur)
	}
}
