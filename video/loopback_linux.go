//go:build linux

// Loopback support for mirroring a capture device to a v4l2loopback
// output device.
//
// The Loopback type opens a v4l2loopback device, sets its output
// format to match the capture device, and writes frames via plain
// write() syscalls. It verifies the loopback accepted the requested
// pixel format.
//
// ReadRawFrame (in device.go) returns raw frame bytes without
// decoding, so bytes pass straight through from capture to loopback
// with no decode/re-encode overhead.
//
// Usage:
//
//	# Load the loopback module (once)
//	sudo modprobe v4l2loopback exclusive_caps=1
//
//	# Mirror /dev/video0 to the loopback device
//	./ioctl -dev /dev/video0 -loopback /dev/video2
//
// Signal handling uses signal.NotifyContext on os.Interrupt. Since
// ReadRawFrame blocks on DQBUF (typically returns within one frame
// period, ~33ms at 30fps), there is at most one frame of latency
// between ctrl-c and clean exit.

package video

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Loopback represents an open v4l2loopback device configured for
// output. Frames written with WriteFrame appear on the capture side
// for consumers (video chat apps, etc.).
type Loopback struct {
	fd int
}

// OpenLoopback opens a v4l2loopback device and sets its output format
// to match the capture device d. The loopback device must already
// exist (modprobe v4l2loopback).
func OpenLoopback(path string, d *Device) (*Loopback, error) {
	fd, err := unix.Open(path, unix.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}

	var vf v4l2Format
	vf.Type = bufTypeVideoOutput
	pix := vf.pixFormat()
	pix.Width = uint32(d.Width)
	pix.Height = uint32(d.Height)
	pix.PixelFormat = d.PixFmt
	pix.Field = fieldNone

	err = v4l2ioctl(fd, vidiocSFmt, unsafe.Pointer(&vf))
	if err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("VIDIOC_S_FMT on %s (%s): %w", path, d.PixFmt, err)
	}

	if pix.PixelFormat != d.PixFmt {
		unix.Close(fd)
		return nil, fmt.Errorf("loopback rejected format %s, negotiated %s instead", d.PixFmt, pix.PixelFormat)
	}

	return &Loopback{fd: fd}, nil
}

// WriteFrame writes one complete raw frame to the loopback device.
func (l *Loopback) WriteFrame(data []byte) error {
	for len(data) > 0 {
		n, err := unix.Write(l.fd, data)
		if err != nil {
			if err == unix.EINTR {
				continue
			}
			return fmt.Errorf("write loopback: %w", err)
		}
		data = data[n:]
	}
	return nil
}

// Close closes the loopback device file descriptor.
func (l *Loopback) Close() error {
	return unix.Close(l.fd)
}
