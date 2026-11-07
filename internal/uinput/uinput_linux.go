package uinput

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

const (
	iocWrite = 1
	iocRead  = 2
)

func ioc(dir, typ, nr, size uintptr) uintptr {
	return dir<<30 | size<<16 | typ<<8 | nr
}

var (
	uiDevCreate  = ioc(0, 'U', 1, 0)
	uiDevDestroy = ioc(0, 'U', 2, 0)
	uiDevSetup   = ioc(iocWrite, 'U', 3, setupSize)
	uiSetEvbit   = ioc(iocWrite, 'U', 100, 4)
	uiSetKeybit  = ioc(iocWrite, 'U', 101, 4)
	uiSetRelbit  = ioc(iocWrite, 'U', 102, 4)
	uiSetAbsbit  = ioc(iocWrite, 'U', 103, 4)
	uiSetMscbit  = ioc(iocWrite, 'U', 104, 4)
	uiSetLedbit  = ioc(iocWrite, 'U', 105, 4)
	uiSetSndbit  = ioc(iocWrite, 'U', 106, 4)
	uiSetSwbit   = ioc(iocWrite, 'U', 109, 4)
)

func eviocgname(n int) uintptr { return ioc(iocRead, 'E', 0x06, uintptr(n)) }

func eviocgprop(n int) uintptr { return ioc(iocRead, 'E', 0x09, uintptr(n)) }

func eviocgbit(ev uint16, n int) uintptr { return ioc(iocRead, 'E', 0x20+uintptr(ev), uintptr(n)) }

func eviocgabs(abs uint16) uintptr { return ioc(iocRead, 'E', 0x40+uintptr(abs), 24) }

func ioctlPtr(fd int, req uintptr, p unsafe.Pointer) error {
	if _, _, e := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), req, uintptr(p)); e != 0 {
		return e
	}
	return nil
}

func ioctlInt(fd int, req uintptr, v int) error {
	if _, _, e := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), req, uintptr(v)); e != 0 {
		return e
	}
	return nil
}

func ioctlBuf(fd int, req uintptr, b []byte) error {
	return ioctlPtr(fd, req, unsafe.Pointer(&b[0]))
}

func testBit(b []byte, n int) bool {
	return n/8 < len(b) && b[n/8]>>(uint(n)%8)&1 == 1
}

type spec struct {
	name  string
	id    ID
	types []uint16
	codes map[uint16][]uint16
}

type Device struct {
	fd      int
	name    string
	owned   bool
	support map[uint32]bool
	abs     map[uint16]AbsInfo
}

func (d *Device) Name() string { return d.name }

func (d *Device) Supports(typ, code uint16) bool {
	if typ == EV_SYN {
		return true
	}
	return d.support[uint32(typ)<<16|uint32(code)]
}

func (d *Device) Abs(code uint16) (AbsInfo, bool) {
	a, ok := d.abs[code]
	return a, ok
}

func (d *Device) Write(typ, code uint16, value int32) error {
	if !d.Supports(typ, code) {
		return nil
	}
	b := EncodeEvent(TimevalSize, typ, code, value)
	n, err := unix.Write(d.fd, b)
	if err != nil {
		return err
	}
	if n != len(b) {
		return fmt.Errorf("short write to %s", d.name)
	}
	return nil
}

func (d *Device) Lock(timeout time.Duration) (func(), error) {
	deadline := time.Now().Add(timeout)
	for {
		err := unix.Flock(d.fd, unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			return func() { unix.Flock(d.fd, unix.LOCK_UN) }, nil
		}
		if err != unix.EWOULDBLOCK && err != unix.EINTR {
			return nil, err
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("timed out waiting for %s", d.name)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (d *Device) Close() error {
	if d.owned {
		ioctlInt(d.fd, uiDevDestroy, 0)
	}
	return unix.Close(d.fd)
}

func openUinput() (int, error) {
	var last error
	for _, p := range []string{"/dev/uinput", "/dev/input/uinput", "/dev/misc/uinput"} {
		fd, err := unix.Open(p, unix.O_WRONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
		if err == nil {
			return fd, nil
		}
		last = err
	}
	return -1, fmt.Errorf("open uinput: %w", last)
}

func codeRequest(typ uint16) (uintptr, bool) {
	switch typ {
	case EV_KEY:
		return uiSetKeybit, true
	case EV_REL:
		return uiSetRelbit, true
	case EV_ABS:
		return uiSetAbsbit, true
	case EV_MSC:
		return uiSetMscbit, true
	case EV_SW:
		return uiSetSwbit, true
	case EV_LED:
		return uiSetLedbit, true
	case EV_SND:
		return uiSetSndbit, true
	}
	return 0, false
}

func isUnsupported(err error) bool {
	return errors.Is(err, unix.ENOTTY) || errors.Is(err, unix.EINVAL)
}

func create(s spec) (*Device, error) {
	fd, err := openUinput()
	if err != nil {
		return nil, err
	}
	d := &Device{fd: fd, name: s.name, owned: true, support: map[uint32]bool{}}
	fail := func(err error) (*Device, error) {
		unix.Close(fd)
		return nil, fmt.Errorf("create %q: %w", s.name, err)
	}
	for _, typ := range s.types {
		if err := ioctlInt(fd, uiSetEvbit, int(typ)); err != nil {
			return fail(err)
		}
		req, ok := codeRequest(typ)
		if !ok {
			continue
		}
		for _, c := range s.codes[typ] {
			if err := ioctlInt(fd, req, int(c)); err != nil {
				return fail(err)
			}
			d.support[uint32(typ)<<16|uint32(c)] = true
		}
	}
	if err := ioctlBuf(fd, uiDevSetup, EncodeSetup(s.name, s.id)); err != nil {
		if !isUnsupported(err) {
			return fail(err)
		}
		legacy := EncodeUserDev(s.name, s.id, nil)
		if n, werr := unix.Write(fd, legacy); werr != nil || n != len(legacy) {
			if werr == nil {
				werr = errors.New("short write")
			}
			return fail(fmt.Errorf("uinput_user_dev: %w", werr))
		}
	}
	if err := ioctlInt(fd, uiDevCreate, 0); err != nil {
		return fail(err)
	}
	return d, nil
}

func Keyboard(name string, codes []uint16) (*Device, error) {
	return create(spec{
		name:  name,
		id:    ID{Bus: BUS_VIRTUAL, Vendor: 1, Product: 1, Version: 1},
		types: []uint16{EV_KEY},
		codes: map[uint16][]uint16{EV_KEY: codes},
	})
}

func readName(fd int) (string, error) {
	buf := make([]byte, 256)
	if err := ioctlBuf(fd, eviocgname(len(buf)), buf); err != nil {
		return "", err
	}
	n := 0
	for n < len(buf) && buf[n] != 0 {
		n++
	}
	return string(buf[:n]), nil
}

var codeMax = map[uint16]int{
	EV_KEY: KEY_MAX, EV_REL: REL_MAX, EV_ABS: ABS_MAX, EV_MSC: MSC_MAX,
	EV_SW: SW_MAX, EV_LED: LED_MAX, EV_SND: SND_MAX,
}

func readCaps(fd int, d *Device) error {
	evBits := make([]byte, EV_MAX/8+1)
	if err := ioctlBuf(fd, eviocgbit(0, len(evBits)), evBits); err != nil {
		return fmt.Errorf("event bits: %w", err)
	}
	for t := 0; t <= EV_MAX; t++ {
		typ := uint16(t)
		max, ok := codeMax[typ]
		if !testBit(evBits, t) || !ok {
			continue
		}
		bits := make([]byte, max/8+1)
		if err := ioctlBuf(fd, eviocgbit(typ, len(bits)), bits); err != nil {
			continue
		}
		for c := 0; c <= max; c++ {
			if testBit(bits, c) {
				d.support[uint32(typ)<<16|uint32(c)] = true
			}
		}
	}
	for c := uint16(0); c <= ABS_MAX; c++ {
		if !d.support[uint32(EV_ABS)<<16|uint32(c)] {
			continue
		}
		var a [24]byte
		if err := ioctlBuf(fd, eviocgabs(c), a[:]); err != nil {
			return fmt.Errorf("abs %#x: %w", c, err)
		}
		le := func(i int) int32 {
			return int32(uint32(a[i]) | uint32(a[i+1])<<8 | uint32(a[i+2])<<16 | uint32(a[i+3])<<24)
		}
		d.abs[c] = AbsInfo{Value: le(0), Min: le(4), Max: le(8), Fuzz: le(12), Flat: le(16), Resolution: le(20)}
	}
	return nil
}

func OpenEvent(path string) (*Device, error) {
	fd, err := unix.Open(path, unix.O_WRONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	name, err := readName(fd)
	if err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("%s: name: %w", path, err)
	}
	d := &Device{fd: fd, name: name, support: map[uint32]bool{}, abs: map[uint16]AbsInfo{}}
	if err := readCaps(fd, d); err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return d, nil
}

func FindEvent(name string) (*Device, error) {
	paths, _ := filepath.Glob("/dev/input/event*")
	sort.Strings(paths)
	for _, p := range paths {
		d, err := OpenEvent(p)
		if err != nil {
			continue
		}
		if d.name == name {
			return d, nil
		}
		d.Close()
	}
	return nil, fmt.Errorf("input device %q not found", name)
}
