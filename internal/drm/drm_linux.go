package drm

import (
	"bytes"
	"errors"
	"fmt"
	"runtime"
	"unsafe"

	"golang.org/x/sys/unix"
)

type Device struct {
	fd int
}

type Connector struct {
	ID   uint32
	Name string
	FB   uint32
}

type Frame struct {
	Width  int
	Height int
	Format uint32
	Pitch  int
	Pix    []byte
}

func ioctl(fd int, req uint32, arg unsafe.Pointer) error {
	for {
		_, _, e := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(req), uintptr(arg))
		if e == unix.EINTR || e == unix.EAGAIN {
			continue
		}
		if e != 0 {
			return e
		}
		return nil
	}
}

var heapSink any

func alloc[T any](n int) []T {
	s := make([]T, n)
	heapSink = s
	return s
}

func ptr64[T any](s []T) uint64 {
	if len(s) == 0 {
		return 0
	}
	return uint64(uintptr(unsafe.Pointer(&s[0])))
}

func Open(path string) (*Device, error) {
	fd, err := unix.Open(path, unix.O_RDWR|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, &pathError{path, err}
	}
	d := &Device{fd: fd}
	c := setClientCap{capability: clientCapUniversalPlanes, value: 1}
	ioctl(fd, ioctlSetClientCap, unsafe.Pointer(&c))
	var res modeCardRes
	if err := ioctl(fd, ioctlGetResources, unsafe.Pointer(&res)); err != nil {
		unix.Close(fd)
		return nil, &pathError{path, err}
	}
	return d, nil
}

type pathError struct {
	path string
	err  error
}

func (e *pathError) Error() string { return e.path + ": " + e.err.Error() }
func (e *pathError) Unwrap() error { return e.err }

func (d *Device) Close() error {
	return unix.Close(d.fd)
}

func (d *Device) connectorIDs() ([]uint32, error) {
	for try := 0; try < 8; try++ {
		var res modeCardRes
		if err := ioctl(d.fd, ioctlGetResources, unsafe.Pointer(&res)); err != nil {
			return nil, err
		}
		ids := alloc[uint32](int(res.countConns) + 1)
		res = modeCardRes{countConns: uint32(len(ids)), connectorIDPtr: ptr64(ids)}
		err := ioctl(d.fd, ioctlGetResources, unsafe.Pointer(&res))
		runtime.KeepAlive(ids)
		if err != nil {
			return nil, err
		}
		if int(res.countConns) <= len(ids) {
			return ids[:res.countConns], nil
		}
	}
	return nil, errors.New("connector list keeps changing")
}

func (d *Device) getConnector(id uint32) (modeGetConnector, error) {
	c := modeGetConnector{connectorID: id}
	err := ioctl(d.fd, ioctlGetConnector, unsafe.Pointer(&c))
	return c, err
}

func (d *Device) crtcOf(encoder uint32) uint32 {
	if encoder == 0 {
		return 0
	}
	e := modeGetEncoder{encoderID: encoder}
	if err := ioctl(d.fd, ioctlGetEncoder, unsafe.Pointer(&e)); err != nil {
		return 0
	}
	return e.crtcID
}

func (d *Device) isPrimary(plane uint32) bool {
	for try := 0; try < 8; try++ {
		o := modeObjGetProperties{objID: plane, objType: objectPlane}
		if err := ioctl(d.fd, ioctlObjGetProperties, unsafe.Pointer(&o)); err != nil {
			return false
		}
		ids := alloc[uint32](int(o.countProps) + 1)
		vals := alloc[uint64](len(ids))
		o = modeObjGetProperties{objID: plane, objType: objectPlane, countProps: uint32(len(ids)), propsPtr: ptr64(ids), propValuesPtr: ptr64(vals)}
		err := ioctl(d.fd, ioctlObjGetProperties, unsafe.Pointer(&o))
		runtime.KeepAlive(ids)
		runtime.KeepAlive(vals)
		if err != nil {
			return false
		}
		if int(o.countProps) > len(ids) {
			continue
		}
		for i := 0; i < int(o.countProps); i++ {
			p := modeGetProperty{propID: ids[i]}
			if err := ioctl(d.fd, ioctlGetProperty, unsafe.Pointer(&p)); err != nil {
				continue
			}
			name := p.name[:]
			if i := bytes.IndexByte(name, 0); i >= 0 {
				name = name[:i]
			}
			if string(name) == "type" {
				return vals[i] == planeTypePrimary
			}
		}
		return false
	}
	return false
}

type primaryPlane struct {
	crtc uint32
	fb   uint32
}

func (d *Device) primaryPlanes() ([]primaryPlane, error) {
	var ids []uint32
	for try := 0; ; try++ {
		if try == 8 {
			return nil, errors.New("plane list keeps changing")
		}
		var r modeGetPlaneRes
		if err := ioctl(d.fd, ioctlGetPlaneResources, unsafe.Pointer(&r)); err != nil {
			return nil, err
		}
		ids = alloc[uint32](int(r.countPlanes) + 1)
		r = modeGetPlaneRes{planeIDPtr: ptr64(ids), countPlanes: uint32(len(ids))}
		err := ioctl(d.fd, ioctlGetPlaneResources, unsafe.Pointer(&r))
		runtime.KeepAlive(ids)
		if err != nil {
			return nil, err
		}
		if int(r.countPlanes) <= len(ids) {
			ids = ids[:r.countPlanes]
			break
		}
	}
	var out []primaryPlane
	for _, id := range ids {
		p := modeGetPlane{planeID: id}
		if err := ioctl(d.fd, ioctlGetPlane, unsafe.Pointer(&p)); err != nil {
			continue
		}
		if p.crtcID != 0 && p.fbID != 0 && d.isPrimary(id) {
			out = append(out, primaryPlane{p.crtcID, p.fbID})
		}
	}
	return out, nil
}

func (d *Device) Connectors() ([]Connector, error) {
	ids, err := d.connectorIDs()
	if err != nil {
		return nil, err
	}
	planes, err := d.primaryPlanes()
	if err != nil {
		return nil, err
	}
	var out []Connector
	for _, id := range ids {
		c, err := d.getConnector(id)
		if err != nil || c.connection != connectorConnected {
			continue
		}
		conn := Connector{ID: id, Name: ConnectorName(c.connectorType, c.connectorTypeID)}
		if crtc := d.crtcOf(c.encoderID); crtc != 0 {
			for _, p := range planes {
				if p.crtc == crtc {
					conn.FB = p.fb
					break
				}
			}
		}
		out = append(out, conn)
	}
	return out, nil
}

type fbInfo struct {
	width, height, format, flags uint32
	handles, pitches, offsets    [4]uint32
	modifier                     uint64
}

func (d *Device) getFB(id uint32) (fbInfo, error) {
	c := modeFBCmd2{fbID: id}
	err := ioctl(d.fd, ioctlGetFB2, unsafe.Pointer(&c))
	if err == nil {
		return fbInfo{c.width, c.height, c.pixelFormat, c.flags, c.handles, c.pitches, c.offsets, c.modifier[0]}, nil
	}
	if err != unix.EINVAL && err != unix.ENOTTY {
		return fbInfo{}, err
	}
	l := modeFBCmd{fbID: id}
	if err := ioctl(d.fd, ioctlGetFB, unsafe.Pointer(&l)); err != nil {
		return fbInfo{}, err
	}
	return fbInfo{
		width: l.width, height: l.height, format: legacyFormat(l.bpp, l.depth),
		handles: [4]uint32{l.handle}, pitches: [4]uint32{l.pitch},
	}, nil
}

func (d *Device) closeHandles(h [4]uint32) {
	for i := 0; i < 4; i++ {
		seen := false
		for j := 0; j < i; j++ {
			seen = seen || h[j] == h[i]
		}
		if h[i] != 0 && !seen {
			c := gemClose{handle: h[i]}
			ioctl(d.fd, ioctlGemClose, unsafe.Pointer(&c))
		}
	}
}

func (d *Device) mapDmaBuf(handle uint32, need int) ([]byte, int, error) {
	p := primeHandle{handle: handle, flags: unix.O_CLOEXEC}
	if err := ioctl(d.fd, ioctlPrimeHandleToFD, unsafe.Pointer(&p)); err != nil {
		return nil, -1, err
	}
	dfd := int(p.fd)
	size, err := unix.Seek(dfd, 0, 2)
	if err == nil && size < int64(need) {
		err = errors.New("buffer is smaller than its size")
	}
	if err != nil {
		unix.Close(dfd)
		return nil, -1, err
	}
	m, err := unix.Mmap(dfd, 0, int(size), unix.PROT_READ, unix.MAP_SHARED)
	if err != nil {
		unix.Close(dfd)
		return nil, -1, err
	}
	return m, dfd, nil
}

func (d *Device) mapDumb(handle uint32, need int) ([]byte, error) {
	m := modeMapDumb{handle: handle}
	if err := ioctl(d.fd, ioctlMapDumb, unsafe.Pointer(&m)); err != nil {
		return nil, err
	}
	return unix.Mmap(d.fd, int64(m.offset), need, unix.PROT_READ, unix.MAP_SHARED)
}

func (d *Device) Read(fb uint32, name string, fn func(Frame) error) error {
	info, err := d.getFB(fb)
	if err != nil {
		return fmt.Errorf("cannot read the %s framebuffer, run as root", name)
	}
	defer d.closeHandles(info.handles)
	if info.handles[0] == 0 {
		return fmt.Errorf("cannot read the %s framebuffer, run as root", name)
	}
	if info.flags&fbModifiers != 0 && info.modifier != modifierLinear {
		return fmt.Errorf("the %s framebuffer is not linear (modifier 0x%x)", name, info.modifier)
	}
	need := int(info.offsets[0]) + int(info.pitches[0])*int(info.height)
	data, dfd, derr := d.mapDmaBuf(info.handles[0], need)
	if derr != nil {
		var merr error
		data, merr = d.mapDumb(info.handles[0], need)
		if merr != nil {
			return fmt.Errorf("cannot map the %s framebuffer: %v", name, derr)
		}
		dfd = -1
	}
	defer unix.Munmap(data)
	if dfd >= 0 {
		defer unix.Close(dfd)
		s := dmaBufSync{flags: dmaBufSyncRead}
		ioctl(dfd, ioctlDmaBufSync, unsafe.Pointer(&s))
		defer func() {
			e := dmaBufSync{flags: dmaBufSyncEnd | dmaBufSyncRead}
			ioctl(dfd, ioctlDmaBufSync, unsafe.Pointer(&e))
		}()
	}
	return fn(Frame{
		Width: int(info.width), Height: int(info.height), Format: info.format,
		Pitch: int(info.pitches[0]), Pix: data[info.offsets[0]:],
	})
}
