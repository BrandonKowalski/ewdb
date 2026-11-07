package fbdev

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/unix"

	"ewdb/internal/capture"
)

const (
	getVScreenInfo = 0x4600
	getFScreenInfo = 0x4602
)

type bitfield struct {
	offset, length, msbRight uint32
}

type varScreenInfo struct {
	xres, yres, xresVirtual, yresVirtual, xoffset, yoffset uint32
	bitsPerPixel, grayscale                                uint32
	red, green, blue, transp                               bitfield
	nonstd, activate, height, width, accelFlags            uint32
	pixclock, leftMargin, rightMargin                      uint32
	upperMargin, lowerMargin, hsyncLen, vsyncLen           uint32
	sync, vmode, rotate, colorspace                        uint32
	reserved                                               [4]uint32
}

type fixScreenInfo struct {
	id           [16]byte
	smemStart    uintptr
	smemLen      uint32
	typ          uint32
	typeAux      uint32
	visual       uint32
	xpanstep     uint16
	ypanstep     uint16
	ywrapstep    uint16
	lineLength   uint32
	mmioStart    uintptr
	mmioLen      uint32
	accel        uint32
	capabilities uint16
	reserved     [2]uint16
}

func ioctl(fd int, req uintptr, arg unsafe.Pointer) error {
	for {
		_, _, e := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), req, uintptr(arg))
		if e == unix.EINTR || e == unix.EAGAIN {
			continue
		}
		if e != 0 {
			return e
		}
		return nil
	}
}

func Grab(path string) (capture.Image, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return capture.Image{}, fmt.Errorf("%s: %v", path, err)
	}
	defer unix.Close(fd)
	var v varScreenInfo
	var f fixScreenInfo
	if err := ioctl(fd, getVScreenInfo, unsafe.Pointer(&v)); err != nil {
		return capture.Image{}, fmt.Errorf("%s: %v", path, err)
	}
	if err := ioctl(fd, getFScreenInfo, unsafe.Pointer(&f)); err != nil {
		return capture.Image{}, fmt.Errorf("%s: %v", path, err)
	}
	format := Format(v.bitsPerPixel,
		Field{v.red.offset, v.red.length}, Field{v.green.offset, v.green.length},
		Field{v.blue.offset, v.blue.length}, Field{v.transp.offset, v.transp.length})
	if format == 0 {
		return capture.Image{}, fmt.Errorf("%s: unsupported pixel layout of %d bits per pixel", path, v.bitsPerPixel)
	}
	if f.lineLength == 0 {
		f.lineLength = v.xresVirtual * v.bitsPerPixel / 8
	}
	size := int(f.smemLen)
	if size == 0 {
		return capture.Image{}, fmt.Errorf("%s: empty framebuffer", path)
	}
	data, err := unix.Mmap(fd, 0, size, unix.PROT_READ, unix.MAP_SHARED)
	if err != nil {
		return capture.Image{}, fmt.Errorf("%s: cannot map: %v", path, err)
	}
	defer unix.Munmap(data)
	start := int(v.yoffset)*int(f.lineLength) + int(v.xoffset)*int(v.bitsPerPixel/8)
	if start > len(data) {
		return capture.Image{}, fmt.Errorf("%s: visible area is outside the framebuffer", path)
	}
	pix, err := capture.ToRGBA(format, data[start:], int(f.lineLength), int(v.xres), int(v.yres))
	if err != nil {
		return capture.Image{}, fmt.Errorf("%s: %v", path, err)
	}
	return capture.Image{W: int(v.xres), H: int(v.yres), Pix: pix}, nil
}

const (
	_ = uint(unsafe.Sizeof(varScreenInfo{}) - 160)
	_ = uint(160 - unsafe.Sizeof(varScreenInfo{}))
)
