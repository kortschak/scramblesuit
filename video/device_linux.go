//go:build linux

package video

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"slices"
	"strings"
	"unsafe"

	"golang.org/x/sys/unix"
)

// bufCount is the number of mmap'd kernel buffers to request. The
// minimum for streaming is 2 (one being filled by hardware, one being
// read by userspace). Extra buffers absorb scheduling delays so the
// driver can keep capturing without dropping frames. 4 is a common
// V4L2 default; for one-shot capture even 2 would suffice.
const bufCount = 4

// Device represents an open V4L2 video-capture device.
type Device struct {
	fd        int
	Width     int
	Height    int
	PixFmt    PixelFormat
	buffers   [][]byte // mmap'd kernel buffers
	path      string
	streaming bool
}

// ProbeDevice opens a V4L2 device and queries its capabilities but
// does not configure a format or allocate buffers. Use this to inspect
// a device's supported formats and controls without committing to
// capture. Call Close when done.
func ProbeDevice(path string) (*Device, error) {
	fd, err := unix.Open(path, unix.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}

	d := &Device{fd: fd, path: path}
	err = d.queryCap()
	if err != nil {
		unix.Close(fd)
		return nil, err
	}
	return d, nil
}

// OpenDevice opens a V4L2 device, configures the best supported pixel
// format, and allocates mmap'd capture buffers. Ready for Start or
// CaptureFrame after this returns.
func OpenDevice(path string) (*Device, error) {
	d, err := ProbeDevice(path)
	if err != nil {
		return nil, err
	}
	err = d.setFormat()
	if err != nil {
		d.Close()
		return nil, err
	}
	err = d.initBuffers()
	if err != nil {
		d.Close()
		return nil, err
	}
	return d, nil
}

// Path returns the path to the device.
func (d *Device) Path() string { return d.path }

// Close stops streaming if active, unmaps buffers, and closes the
// underlying file descriptor.
func (d *Device) Close() error {
	if d.streaming {
		d.Stop()
	}
	for _, b := range d.buffers {
		unix.Munmap(b)
	}
	return unix.Close(d.fd)
}

// Start queues all buffers and begins streaming. Call ReadFrame to
// retrieve frames, and Stop when finished.
func (d *Device) Start() error {
	if d.streaming {
		return fmt.Errorf("already streaming")
	}
	if len(d.buffers) == 0 {
		return fmt.Errorf("no buffers allocated (use OpenDevice, not ProbeDevice)")
	}
	for i := range d.buffers {
		buf := v4l2Buffer{
			Type:   bufTypeVideoCapture,
			Memory: memoryMMAP,
			Index:  uint32(i),
		}
		err := v4l2ioctl(d.fd, vidiocQbuf, unsafe.Pointer(&buf))
		if err != nil {
			return fmt.Errorf("VIDIOC_QBUF[%d]: %w", i, err)
		}
	}
	typ := bufTypeVideoCapture
	err := v4l2ioctl(d.fd, vidiocStreamon, unsafe.Pointer(&typ))
	if err != nil {
		return fmt.Errorf("VIDIOC_STREAMON: %w", err)
	}
	d.streaming = true
	return nil
}

// ReadFrame dequeues one filled buffer, decodes it into an
// image.Image, and re-queues the buffer. The returned image owns its
// pixel data. It does not alias the mmap'd buffer.
//
// Blocks until the driver has a frame ready.
func (d *Device) ReadFrame() (image.Image, error) {
	if !d.streaming {
		return nil, fmt.Errorf("not streaming (call Start first)")
	}
	var buf v4l2Buffer
	buf.Type = bufTypeVideoCapture
	buf.Memory = memoryMMAP
	err := v4l2ioctl(d.fd, vidiocDqbuf, unsafe.Pointer(&buf))
	if err != nil {
		return nil, fmt.Errorf("VIDIOC_DQBUF: %w", err)
	}

	raw := d.buffers[buf.Index][:buf.BytesUsed]
	img, err := d.Decode(raw)

	// Re-queue the buffer regardless of decode success so the
	// driver can reuse it.
	qerr := v4l2ioctl(d.fd, vidiocQbuf, unsafe.Pointer(&buf))
	if qerr != nil && err == nil {
		return nil, fmt.Errorf("VIDIOC_QBUF: %w", qerr)
	}
	return img, err
}

// Decode converts a raw frame buffer to an image using the device's
// current pixel format.
func (d *Device) Decode(raw []byte) (image.Image, error) {
	switch d.PixFmt {
	case PixelFormatMJPEG:
		return jpeg.Decode(bytes.NewReader(raw))
	case PixelFormatYUYV:
		return yuyvToImage(raw, d.Width, d.Height), nil
	default:
		return nil, fmt.Errorf("unsupported pixel format %s", d.PixFmt)
	}
}

// ReadRawFrame dequeues one filled buffer, copies its raw bytes, and
// re-queues the buffer. Returns the frame in the device's pixel format
// without decoding. This is useful for forwarding to a loopback device.
func (d *Device) ReadRawFrame() ([]byte, error) {
	if !d.streaming {
		return nil, fmt.Errorf("not streaming (call Start first)")
	}
	var buf v4l2Buffer
	buf.Type = bufTypeVideoCapture
	buf.Memory = memoryMMAP
	err := v4l2ioctl(d.fd, vidiocDqbuf, unsafe.Pointer(&buf))
	if err != nil {
		return nil, fmt.Errorf("VIDIOC_DQBUF: %w", err)
	}

	raw := make([]byte, buf.BytesUsed)
	copy(raw, d.buffers[buf.Index][:buf.BytesUsed])

	err = v4l2ioctl(d.fd, vidiocQbuf, unsafe.Pointer(&buf))
	if err != nil {
		return nil, fmt.Errorf("VIDIOC_QBUF: %w", err)
	}
	return raw, nil
}

// Stop halts streaming. Safe to call if not currently streaming.
func (d *Device) Stop() error {
	if !d.streaming {
		return nil
	}
	typ := bufTypeVideoCapture
	err := v4l2ioctl(d.fd, vidiocStreamoff, unsafe.Pointer(&typ))
	d.streaming = false
	return err
}

// queryCap checks that the device supports video capture and streaming.
func (d *Device) queryCap() error {
	var cap v4l2Capability
	err := v4l2ioctl(d.fd, vidiocQuerycap, unsafe.Pointer(&cap))
	if err != nil {
		return fmt.Errorf("VIDIOC_QUERYCAP: %w", err)
	}

	caps := cap.Capabilities
	if caps&capVideoCapture == 0 {
		return fmt.Errorf("device does not support video capture")
	}
	if caps&capStreaming == 0 {
		return fmt.Errorf("device does not support streaming I/O")
	}
	return nil
}

// setFormat enumerates the device's supported formats, picks the best
// one we can decode in the order provided by the camera, and configures
// it.
func (d *Device) setFormat() error {
	formats := d.ListFormats()

	supported := []PixelFormat{
		PixelFormatYUYV,
		PixelFormatMJPEG,
	}
	var chosen PixelFormat
	for _, f := range formats {
		if slices.Contains(supported, f.PixelFormat) {
			chosen = f.PixelFormat
			break
		}
	}

	if chosen == 0 {
		if len(formats) == 0 {
			return fmt.Errorf("device reports no supported formats")
		}
		var supported []string
		for _, f := range formats {
			supported = append(supported, f.PixelFormat.String()+" ("+f.Description+")")
		}
		return fmt.Errorf("no decodable format: device supports: %s", strings.Join(supported, ", "))
	}

	var vf v4l2Format
	vf.Type = bufTypeVideoCapture
	pix := vf.pixFormat()
	pix.Width = 640
	pix.Height = 480
	pix.PixelFormat = chosen
	pix.Field = fieldNone

	err := v4l2ioctl(d.fd, vidiocSFmt, unsafe.Pointer(&vf))
	if err != nil {
		return fmt.Errorf("VIDIOC_S_FMT (%s): %w", chosen.String(), err)
	}

	d.Width = int(pix.Width)
	d.Height = int(pix.Height)
	d.PixFmt = pix.PixelFormat
	return nil
}

// initBuffers requests kernel buffers and mmaps them into userspace.
func (d *Device) initBuffers() error {
	req := v4l2RequestBuffers{
		Count:  bufCount,
		Type:   bufTypeVideoCapture,
		Memory: memoryMMAP,
	}
	err := v4l2ioctl(d.fd, vidiocReqbufs, unsafe.Pointer(&req))
	if err != nil {
		return fmt.Errorf("VIDIOC_REQBUFS: %w", err)
	}
	if req.Count < 2 {
		return fmt.Errorf("insufficient buffer memory (got %d buffers)", req.Count)
	}

	d.buffers = make([][]byte, req.Count)
	for i := uint32(0); i < req.Count; i++ {
		buf := v4l2Buffer{
			Type:   bufTypeVideoCapture,
			Memory: memoryMMAP,
			Index:  i,
		}
		err := v4l2ioctl(d.fd, vidiocQuerybuf, unsafe.Pointer(&buf))
		if err != nil {
			return fmt.Errorf("VIDIOC_QUERYBUF[%d]: %w", i, err)
		}

		data, err := unix.Mmap(d.fd, int64(buf.Offset), int(buf.Length), unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
		if err != nil {
			return fmt.Errorf("mmap buffer %d: %w", i, err)
		}
		d.buffers[i] = data
	}
	return nil
}

// CaptureFrame is a convenience for one-shot use: starts streaming,
// discards warmup frames (letting auto-exposure settle), keeps one
// frame, and stops streaming.
func (d *Device) CaptureFrame(warmup int) (image.Image, error) {
	err := d.Start()
	if err != nil {
		return nil, err
	}
	defer d.Stop()

	for range warmup {
		_, err := d.ReadFrame()
		if err != nil {
			return nil, fmt.Errorf("warmup: %w", err)
		}
	}
	return d.ReadFrame()
}

// ---------- Format enumeration ----------

// FormatDesc describes a pixel format supported by the device.
type FormatDesc struct {
	PixelFormat PixelFormat
	Description string
	Flags       uint32
}

// ListFormats returns all pixel formats the device supports for
// video capture.
func (d *Device) ListFormats() []FormatDesc {
	var formats []FormatDesc
	for i := uint32(0); ; i++ {
		fd := v4l2FmtDesc{Index: i, Type: bufTypeVideoCapture}
		err := v4l2ioctl(d.fd, vidiocEnumFmt, unsafe.Pointer(&fd))
		if err != nil {
			break
		}
		formats = append(formats, FormatDesc{
			PixelFormat: fd.PixelFormat,
			Description: cstring(fd.Description[:]),
			Flags:       fd.Flags,
		})
	}
	return formats
}

// ---------- V4L2 controls ----------

// ControlInfo describes a V4L2 control and its value range.
type ControlInfo struct {
	ID           uint32
	Name         string
	Type         uint32
	Minimum      int32
	Maximum      int32
	Step         int32
	DefaultValue int32
	Flags        uint32
}

// TypeName returns a human-readable name for the control type.
func (c ControlInfo) TypeName() string {
	switch c.Type {
	case ctrlTypeInteger:
		return "int"
	case ctrlTypeBoolean:
		return "bool"
	case ctrlTypeMenu:
		return "menu"
	case ctrlTypeButton:
		return "button"
	case ctrlTypeInteger64:
		return "int64"
	case ctrlTypeString:
		return "string"
	case ctrlTypeBitmask:
		return "bitmask"
	default:
		return fmt.Sprintf("type(%d)", c.Type)
	}
}

// ListControls enumerates all non-disabled controls the device
// exposes, using the V4L2_CTRL_FLAG_NEXT_CTRL iteration method.
func (d *Device) ListControls() []ControlInfo {
	var controls []ControlInfo
	id := ctrlFlagNextCtrl
	for {
		qc := v4l2Queryctrl{ID: id}
		err := v4l2ioctl(d.fd, vidiocQueryctrl, unsafe.Pointer(&qc))
		if err != nil {
			break
		}
		if qc.Flags&ctrlFlagDisabled == 0 && qc.Type != ctrlTypeCtrlClass {
			controls = append(controls, ControlInfo{
				ID:           qc.ID,
				Name:         cstring(qc.Name[:]),
				Type:         qc.Type,
				Minimum:      qc.Minimum,
				Maximum:      qc.Maximum,
				Step:         qc.Step,
				DefaultValue: qc.DefaultValue,
				Flags:        qc.Flags,
			})
		}
		id = ctrlFlagNextCtrl | qc.ID
	}
	return controls
}

// cstring extracts a NUL-terminated string from a fixed-size byte array.
func cstring(b []byte) string {
	for i, c := range b {
		if c == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}

// GetControl reads the current value of a V4L2 control.
func (d *Device) GetControl(id uint32) (int32, error) {
	ctrl := v4l2Control{ID: id}
	err := v4l2ioctl(d.fd, vidiocGCtrl, unsafe.Pointer(&ctrl))
	if err != nil {
		return 0, fmt.Errorf("VIDIOC_G_CTRL(0x%x): %w", id, err)
	}
	return ctrl.Value, nil
}

// SetControl writes a value to a V4L2 control.
func (d *Device) SetControl(id uint32, value int32) error {
	ctrl := v4l2Control{ID: id, Value: value}
	err := v4l2ioctl(d.fd, vidiocSCtrl, unsafe.Pointer(&ctrl))
	if err != nil {
		return fmt.Errorf("VIDIOC_S_CTRL(0x%x, %d): %w", id, value, err)
	}
	return nil
}
