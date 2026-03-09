//go:build linux

package video

import (
	"unsafe"

	"golang.org/x/sys/unix"
)

// #include <linux/videodev2.h>
import "C"

// v4l2ioctl issues a V4L2 ioctl, retrying on EINTR.
func v4l2ioctl(fd int, req uintptr, arg unsafe.Pointer) error {
	for {
		_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), req, uintptr(arg))
		if errno == 0 {
			return nil
		}
		if errno == unix.EINTR {
			continue
		}
		return errno
	}
}

// PixelFormat is a V4L2 pixel-format code (a FourCC packed into a uint32).
type PixelFormat uint32

// String returns the four-character code representation.
func (pf PixelFormat) String() string {
	return string([]byte{byte(pf), byte(pf >> 8), byte(pf >> 16), byte(pf >> 24)})
}

// Pixel format codes for the two formats we can decode.
// See include/uapi/linux/videodev2.h.
const (
	PixelFormatYUYV  = PixelFormat(C.V4L2_PIX_FMT_YUYV)  // uncompressed 4:2:2
	PixelFormatMJPEG = PixelFormat(C.V4L2_PIX_FMT_MJPEG) // motion-JPEG
)

// Buffer types for single-planar capture and output.
// See enum v4l2_buf_type in include/uapi/linux/videodev2.h.
const (
	bufTypeVideoCapture = uint32(C.V4L2_BUF_TYPE_VIDEO_CAPTURE)
	bufTypeVideoOutput  = uint32(C.V4L2_BUF_TYPE_VIDEO_OUTPUT)
)

// Memory model: kernel-allocated buffers mapped into userspace.
// See enum v4l2_memory in include/uapi/linux/videodev2.h.
const memoryMMAP = uint32(C.V4L2_MEMORY_MMAP)

// Capability flags from include/uapi/linux/videodev2.h.
const (
	capVideoCapture = uint32(C.V4L2_CAP_VIDEO_CAPTURE)
	capStreaming    = uint32(C.V4L2_CAP_STREAMING)
)

// Field ordering: progressive (no interlace).
// See enum v4l2_field in include/uapi/linux/videodev2.h.
const fieldNone = uint32(C.V4L2_FIELD_NONE)

// V4L2 struct definitions (64-bit linux ABI)
//
// These must match the kernel's struct layout exactly, because the
// ioctl number encodes sizeof(struct). A mismatch causes ENOTTY.
//
// The layouts are identical on amd64 and arm64: both have 8-byte
// pointers and 64-bit time_t, which determine the sizes of the union
// and struct timeval fields in v4l2_buffer. On 32-bit architectures
// the layouts would differ; the init() below panic-checks every size
// at startup to catch this.
//
// All structs are from include/uapi/linux/videodev2.h.

// v4l2Capability matches struct v4l2_capability (104 bytes).
type v4l2Capability struct {
	Driver       [16]byte
	Card         [32]byte
	BusInfo      [32]byte
	Version      uint32
	Capabilities uint32
	DeviceCaps   uint32
	Reserved     [3]uint32
}

func _() {
	var (
		goType v4l2Capability
		cType  C.struct_v4l2_capability
	)
	var x [1]struct{}
	_ = x[unsafe.Sizeof(goType)-unsafe.Sizeof(cType)]
	_ = x[unsafe.Offsetof(goType.Driver)-unsafe.Offsetof(cType.driver)]
	_ = x[unsafe.Offsetof(goType.Card)-unsafe.Offsetof(cType.card)]
	_ = x[unsafe.Offsetof(goType.BusInfo)-unsafe.Offsetof(cType.bus_info)]
	_ = x[unsafe.Offsetof(goType.Version)-unsafe.Offsetof(cType.version)]
	_ = x[unsafe.Offsetof(goType.Capabilities)-unsafe.Offsetof(cType.capabilities)]
	_ = x[unsafe.Offsetof(goType.DeviceCaps)-unsafe.Offsetof(cType.device_caps)]
	_ = x[unsafe.Offsetof(goType.Reserved)-unsafe.Offsetof(cType.reserved)]
}

// v4l2PixFormat matches struct v4l2_pix_format (48 bytes), the
// single-planar pixel format description inside v4l2Format.
type v4l2PixFormat struct {
	Width        uint32
	Height       uint32
	PixelFormat  PixelFormat
	Field        uint32
	BytesPerLine uint32
	SizeImage    uint32
	Colorspace   uint32
	Priv         uint32
	Flags        uint32
	YCbCrEnc     uint32
	Quantization uint32
	XferFunc     uint32
}

func _() {
	var (
		goType v4l2PixFormat
		cType  C.struct_v4l2_pix_format
	)
	var x [1]struct{}
	_ = x[unsafe.Sizeof(goType)-unsafe.Sizeof(cType)]
	_ = x[unsafe.Offsetof(goType.Width)-unsafe.Offsetof(cType.width)]
	_ = x[unsafe.Offsetof(goType.Height)-unsafe.Offsetof(cType.height)]
	_ = x[unsafe.Offsetof(goType.PixelFormat)-unsafe.Offsetof(cType.pixelformat)]
	_ = x[unsafe.Offsetof(goType.Field)-unsafe.Offsetof(cType.field)]
	_ = x[unsafe.Offsetof(goType.BytesPerLine)-unsafe.Offsetof(cType.bytesperline)]
	_ = x[unsafe.Offsetof(goType.SizeImage)-unsafe.Offsetof(cType.sizeimage)]
	_ = x[unsafe.Offsetof(goType.Colorspace)-unsafe.Offsetof(cType.colorspace)]
	_ = x[unsafe.Offsetof(goType.Priv)-unsafe.Offsetof(cType.priv)]
	_ = x[unsafe.Offsetof(goType.Flags)-unsafe.Offsetof(cType.flags)]
	_ = x[unsafe.Offsetof(goType.YCbCrEnc)-unsafe.Offsetof(cType.anon0)]
	_ = x[unsafe.Offsetof(goType.Quantization)-unsafe.Offsetof(cType.quantization)]
	_ = x[unsafe.Offsetof(goType.XferFunc)-unsafe.Offsetof(cType.xfer_func)]
}

// v4l2Format wraps struct v4l2_format. The fmt union is represented as
// a raw [200]byte; callers overlay v4l2PixFormat on it via pixFormat().
// On amd64 the union is 8-byte aligned (v4l2_window contains
// pointers), so 4 bytes of padding sit between Type and Fmt.
type v4l2Format struct {
	Type uint32
	_    [4]byte
	Fmt  [200]byte
}

func _() {
	var (
		goType v4l2Format
		cType  C.struct_v4l2_format
	)
	var x [1]struct{}
	_ = x[unsafe.Sizeof(goType)-unsafe.Sizeof(cType)]
	_ = x[unsafe.Offsetof(goType.Type)-unsafe.Offsetof(cType._type)]
	_ = x[unsafe.Offsetof(goType.Fmt)-unsafe.Offsetof(cType.fmt)]
}

func (f *v4l2Format) pixFormat() *v4l2PixFormat {
	return (*v4l2PixFormat)(unsafe.Pointer(&f.Fmt[0]))
}

// v4l2RequestBuffers matches struct v4l2_requestbuffers (20 bytes).
type v4l2RequestBuffers struct {
	Count        uint32
	Type         uint32
	Memory       uint32
	Capabilities uint32
	Flags        uint8
	Reserved     [3]uint8
}

func _() {
	var (
		goType v4l2RequestBuffers
		cType  C.struct_v4l2_requestbuffers
	)
	var x [1]struct{}
	_ = x[unsafe.Sizeof(goType)-unsafe.Sizeof(cType)]
	_ = x[unsafe.Offsetof(goType.Count)-unsafe.Offsetof(cType.count)]
	_ = x[unsafe.Offsetof(goType.Type)-unsafe.Offsetof(cType._type)]
	_ = x[unsafe.Offsetof(goType.Memory)-unsafe.Offsetof(cType.memory)]
	_ = x[unsafe.Offsetof(goType.Capabilities)-(unsafe.Offsetof(cType.memory)+unsafe.Sizeof(cType.memory))]
}

// v4l2Timecode matches struct v4l2_timecode (16 bytes).
type v4l2Timecode struct {
	Type     uint32
	Flags    uint32
	Frames   uint8
	Seconds  uint8
	Minutes  uint8
	Hours    uint8
	Userbits [4]uint8
}

func _() {
	var (
		goType v4l2Timecode
		cType  C.struct_v4l2_timecode
	)
	var x [1]struct{}
	_ = x[unsafe.Sizeof(goType)-unsafe.Sizeof(cType)]
	_ = x[unsafe.Offsetof(goType.Type)-unsafe.Offsetof(cType._type)]
	_ = x[unsafe.Offsetof(goType.Flags)-unsafe.Offsetof(cType.flags)]
	_ = x[unsafe.Offsetof(goType.Frames)-unsafe.Offsetof(cType.frames)]
	_ = x[unsafe.Offsetof(goType.Seconds)-unsafe.Offsetof(cType.seconds)]
	_ = x[unsafe.Offsetof(goType.Minutes)-unsafe.Offsetof(cType.minutes)]
	_ = x[unsafe.Offsetof(goType.Userbits)-unsafe.Offsetof(cType.userbits)]
}

// v4l2Buffer matches struct v4l2_buffer on amd64 (88 bytes).
//
// The union m is represented as Offset (m.offset for MMAP mode)
// followed by 4 padding bytes to fill the 8-byte union slot.
type v4l2Buffer struct {
	Index     uint32
	Type      uint32
	BytesUsed uint32
	Flags     uint32
	Field     uint32
	_         uint32       // pad to align Timestamp
	Timestamp [2]int64     // struct timeval
	Timecode  v4l2Timecode // 16 bytes
	Sequence  uint32
	Memory    uint32
	Offset    uint32 // union m. Only m.offset used (MMAP).
	_         uint32 // rest of 8-byte union
	Length    uint32
	Reserved2 uint32
	RequestFD int32
	_         uint32 // trailing pad (struct alignment 8)
}

func _() {
	var (
		goType v4l2Buffer
		cType  C.struct_v4l2_buffer
	)
	var x [1]struct{}
	_ = x[unsafe.Sizeof(goType)-unsafe.Sizeof(cType)]
	_ = x[unsafe.Offsetof(goType.Index)-unsafe.Offsetof(cType.index)]
	_ = x[unsafe.Offsetof(goType.Type)-unsafe.Offsetof(cType._type)]
	_ = x[unsafe.Offsetof(goType.BytesUsed)-unsafe.Offsetof(cType.bytesused)]
	_ = x[unsafe.Offsetof(goType.Flags)-unsafe.Offsetof(cType.flags)]
	_ = x[unsafe.Offsetof(goType.Field)-unsafe.Offsetof(cType.field)]
	_ = x[unsafe.Offsetof(goType.Timestamp)-unsafe.Offsetof(cType.timestamp)]
	_ = x[unsafe.Offsetof(goType.Timecode)-unsafe.Offsetof(cType.timecode)]
	_ = x[unsafe.Offsetof(goType.Sequence)-unsafe.Offsetof(cType.sequence)]
	_ = x[unsafe.Offsetof(goType.Memory)-unsafe.Offsetof(cType.memory)]
	_ = x[unsafe.Offsetof(goType.Offset)-unsafe.Offsetof(cType.m)]
	_ = x[unsafe.Offsetof(goType.Length)-unsafe.Offsetof(cType.length)]
	_ = x[unsafe.Offsetof(goType.Reserved2)-unsafe.Offsetof(cType.reserved2)]
	_ = x[unsafe.Offsetof(goType.RequestFD)-(unsafe.Offsetof(cType.reserved2)+unsafe.Sizeof(cType.reserved2))]
}

// v4l2FmtDesc matches struct v4l2_fmtdesc (64 bytes), returned by
// VIDIOC_ENUM_FMT.
type v4l2FmtDesc struct {
	Index       uint32
	Type        uint32
	Flags       uint32
	Description [32]byte
	PixelFormat PixelFormat
	MbusCode    uint32
	Reserved    [3]uint32
}

func _() {
	var (
		goType v4l2FmtDesc
		cType  C.struct_v4l2_fmtdesc
	)
	var x [1]struct{}
	_ = x[unsafe.Sizeof(goType)-unsafe.Sizeof(cType)]
	_ = x[unsafe.Offsetof(goType.Index)-unsafe.Offsetof(cType.index)]
	_ = x[unsafe.Offsetof(goType.Type)-unsafe.Offsetof(cType._type)]
	_ = x[unsafe.Offsetof(goType.Flags)-unsafe.Offsetof(cType.flags)]
	_ = x[unsafe.Offsetof(goType.Description)-unsafe.Offsetof(cType.description)]
	_ = x[unsafe.Offsetof(goType.PixelFormat)-unsafe.Offsetof(cType.pixelformat)]
	_ = x[unsafe.Offsetof(goType.MbusCode)-(unsafe.Offsetof(cType.pixelformat)+unsafe.Sizeof(cType.pixelformat))]
}

// v4l2Control matches struct v4l2_control (8 bytes), used by
// VIDIOC_G_CTRL and VIDIOC_S_CTRL.
type v4l2Control struct {
	ID    uint32
	Value int32
}

func _() {
	var (
		goType v4l2Control
		cType  C.struct_v4l2_control
	)
	var x [1]struct{}
	_ = x[unsafe.Sizeof(goType)-unsafe.Sizeof(cType)]
	_ = x[unsafe.Offsetof(goType.ID)-unsafe.Offsetof(cType.id)]
	_ = x[unsafe.Offsetof(goType.Value)-unsafe.Offsetof(cType.value)]
}

// v4l2Queryctrl matches struct v4l2_queryctrl (68 bytes), returned by
// VIDIOC_QUERYCTRL.
type v4l2Queryctrl struct {
	ID           uint32
	Type         uint32
	Name         [32]byte
	Minimum      int32
	Maximum      int32
	Step         int32
	DefaultValue int32
	Flags        uint32
	Reserved     [2]uint32
}

func _() {
	var (
		goType v4l2Queryctrl
		cType  C.struct_v4l2_queryctrl
	)
	var x [1]struct{}
	_ = x[unsafe.Sizeof(goType)-unsafe.Sizeof(cType)]
	_ = x[unsafe.Offsetof(goType.ID)-unsafe.Offsetof(cType.id)]
	_ = x[unsafe.Offsetof(goType.Type)-unsafe.Offsetof(cType._type)]
	_ = x[unsafe.Offsetof(goType.Name)-unsafe.Offsetof(cType.name)]
	_ = x[unsafe.Offsetof(goType.Minimum)-unsafe.Offsetof(cType.minimum)]
	_ = x[unsafe.Offsetof(goType.Maximum)-unsafe.Offsetof(cType.maximum)]
	_ = x[unsafe.Offsetof(goType.Step)-unsafe.Offsetof(cType.step)]
	_ = x[unsafe.Offsetof(goType.DefaultValue)-unsafe.Offsetof(cType.default_value)]
	_ = x[unsafe.Offsetof(goType.Flags)-unsafe.Offsetof(cType.flags)]
	_ = x[unsafe.Offsetof(goType.Reserved)-unsafe.Offsetof(cType.reserved)]
}

// V4L2 control types.
// See enum v4l2_ctrl_type in include/uapi/linux/videodev2.h.
const (
	ctrlTypeInteger   = uint32(C.V4L2_CTRL_TYPE_INTEGER)
	ctrlTypeBoolean   = uint32(C.V4L2_CTRL_TYPE_BOOLEAN)
	ctrlTypeMenu      = uint32(C.V4L2_CTRL_TYPE_MENU)
	ctrlTypeButton    = uint32(C.V4L2_CTRL_TYPE_BUTTON)
	ctrlTypeInteger64 = uint32(C.V4L2_CTRL_TYPE_INTEGER64)
	ctrlTypeCtrlClass = uint32(C.V4L2_CTRL_TYPE_CTRL_CLASS)
	ctrlTypeString    = uint32(C.V4L2_CTRL_TYPE_STRING)
	ctrlTypeBitmask   = uint32(C.V4L2_CTRL_TYPE_BITMASK)
)

// V4L2 control flags from include/uapi/linux/videodev2.h.
const (
	ctrlFlagNextCtrl = uint32(C.V4L2_CTRL_FLAG_NEXT_CTRL)
	ctrlFlagDisabled = uint32(C.V4L2_CTRL_FLAG_DISABLED)
)

// Well-known V4L2 control IDs from include/uapi/linux/v4l2-controls.h.
const (
	CIDBrightness       = uint32(C.V4L2_CID_BRIGHTNESS)
	CIDContrast         = uint32(C.V4L2_CID_CONTRAST)
	CIDSaturation       = uint32(C.V4L2_CID_SATURATION)
	CIDHue              = uint32(C.V4L2_CID_HUE)
	CIDAutoWhiteBalance = uint32(C.V4L2_CID_AUTO_WHITE_BALANCE)
	CIDGamma            = uint32(C.V4L2_CID_GAMMA)
	CIDGain             = uint32(C.V4L2_CID_GAIN)
	CIDWhiteBalanceTemp = uint32(C.V4L2_CID_WHITE_BALANCE_TEMPERATURE)
	CIDSharpness        = uint32(C.V4L2_CID_SHARPNESS)
	CIDBacklightComp    = uint32(C.V4L2_CID_BACKLIGHT_COMPENSATION)

	CIDExposureAuto     = uint32(C.V4L2_CID_EXPOSURE_AUTO)
	CIDExposureAbsolute = uint32(C.V4L2_CID_EXPOSURE_ABSOLUTE)
	CIDExposureAutoPri  = uint32(C.V4L2_CID_EXPOSURE_AUTO_PRIORITY)
	CIDFocusAbsolute    = uint32(C.V4L2_CID_FOCUS_ABSOLUTE)
	CIDFocusAuto        = uint32(C.V4L2_CID_FOCUS_AUTO)
)

// ioctl request codes. See include/uapi/linux/videodev2.h and
// include/uapi/asm-generic/ioctl.h.
const (
	vidiocQuerycap  = C.VIDIOC_QUERYCAP
	vidiocEnumFmt   = C.VIDIOC_ENUM_FMT
	vidiocSFmt      = C.VIDIOC_S_FMT
	vidiocReqbufs   = C.VIDIOC_REQBUFS
	vidiocQuerybuf  = C.VIDIOC_QUERYBUF
	vidiocQbuf      = C.VIDIOC_QBUF
	vidiocDqbuf     = C.VIDIOC_DQBUF
	vidiocStreamon  = C.VIDIOC_STREAMON
	vidiocStreamoff = C.VIDIOC_STREAMOFF
	vidiocGCtrl     = C.VIDIOC_G_CTRL
	vidiocSCtrl     = C.VIDIOC_S_CTRL
	vidiocQueryctrl = C.VIDIOC_QUERYCTRL
)
