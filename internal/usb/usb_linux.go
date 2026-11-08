package usb

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"

	"ewdb/internal/wire"
)

const (
	headerLen    = 24
	maxZeroReads = 64
	reapInterval = 20 * time.Millisecond
)

var (
	errClosed      = errors.New("usb: transport closed")
	errHostStalled = errors.New("usb: host stopped reading")
	errNoHello     = errors.New("usb: host did not connect")
)

func openTarget(t target) (Endpoint, error) {
	switch t.kind {
	case "ffs":
		return openFFS(t.path)
	case "android":
		return openAndroid(t.path)
	}
	return nil, fmt.Errorf("%w %q", ErrBadSpec, t.kind)
}

func openDevNull() (int, error) {
	fd, err := openFile("/dev/null", unix.O_RDWR|unix.O_CLOEXEC)
	if err != nil {
		return -1, fmt.Errorf("usb: open /dev/null: %w", err)
	}
	return fd, nil
}

func openFile(path string, flags int) (int, error) {
	for {
		fd, err := unix.Open(path, flags, 0)
		if err == unix.EINTR {
			continue
		}
		return fd, err
	}
}

func writeRetry(fd int, p []byte) (int, error) {
	for {
		n, err := unix.Write(fd, p)
		if err == unix.EINTR {
			continue
		}
		return n, err
	}
}

func ioSigmask() unix.Sigset_t {
	var s unix.Sigset_t
	for i := range s.Val {
		s.Val[i]--
	}
	bits := uint(unsafe.Sizeof(s.Val[0]) * 8)
	for _, sig := range []unix.Signal{unix.SIGURG, unix.SIGSEGV, unix.SIGBUS, unix.SIGFPE, unix.SIGILL, unix.SIGTRAP} {
		n := uint(sig) - 1
		s.Val[n/bits] &^= 1 << (n % bits)
	}
	return s
}

var blockedSignals = ioSigmask()

type transport struct {
	rfd, wfd   int
	devnull    int
	writeChunk int
	readChunk  int
	gen        uint64
	zeros      int

	wmu      sync.Mutex
	checksum bool

	writeSince atomic.Int64
	spoke      atomic.Bool

	mu     sync.Mutex
	closed bool
	cause  error
	tids   map[int]struct{}
	once   sync.Once
	done   chan struct{}
}

func newTransport(rfd, wfd, devnull, readChunk, writeChunk int) *transport {
	return &transport{
		rfd: rfd, wfd: wfd, devnull: devnull,
		readChunk: readChunk, writeChunk: writeChunk,
		checksum: true,
		tids:     map[int]struct{}{},
		done:     make(chan struct{}),
	}
}

func (t *transport) io(fd int, p []byte, write bool) (int, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	var old unix.Sigset_t
	if err := unix.PthreadSigmask(unix.SIG_BLOCK, &blockedSignals, &old); err == nil {
		defer unix.PthreadSigmask(unix.SIG_SETMASK, &old, nil)
	}
	tid := unix.Gettid()
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return 0, t.closedErr()
	}
	t.tids[tid] = struct{}{}
	t.mu.Unlock()
	defer func() {
		t.mu.Lock()
		delete(t.tids, tid)
		t.mu.Unlock()
	}()
	for {
		var n int
		var err error
		if write {
			n, err = unix.Write(fd, p)
		} else {
			n, err = unix.Read(fd, p)
		}
		if t.isClosed() {
			return 0, t.closedErr()
		}
		if err == unix.EINTR {
			continue
		}
		return n, err
	}
}

func (t *transport) closedErr() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.cause != nil {
		return t.cause
	}
	return errClosed
}

func (t *transport) fail(err error) {
	t.mu.Lock()
	if !t.closed && t.cause == nil {
		t.cause = err
	}
	t.mu.Unlock()
	t.Close()
}

func (t *transport) isClosed() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.closed
}

type fdReader struct{ t *transport }

func (r fdReader) Read(p []byte) (int, error) {
	t := r.t
	if len(p) > t.readChunk {
		p = p[:t.readChunk]
	}
	n, err := t.io(t.rfd, p, false)
	if err != nil {
		return 0, err
	}
	if n == 0 {
		if t.zeros++; t.zeros > maxZeroReads {
			return 0, io.ErrNoProgress
		}
		return 0, nil
	}
	t.zeros = 0
	t.spoke.Store(true)
	return n, nil
}

func (t *transport) Read() (wire.Message, error) {
	return wire.Read(fdReader{t}, wire.MaxPayload)
}

func (t *transport) Write(m wire.Message) error {
	t.wmu.Lock()
	defer t.wmu.Unlock()
	var b bytes.Buffer
	if err := wire.Write(&b, m, t.checksum); err != nil {
		return err
	}
	raw := b.Bytes()
	if err := t.writeAll(raw[:headerLen]); err != nil {
		return err
	}
	for p := raw[headerLen:]; len(p) > 0; {
		n := min(len(p), t.writeChunk)
		if err := t.writeAll(p[:n]); err != nil {
			return err
		}
		p = p[n:]
	}
	return nil
}

func (t *transport) writeAll(p []byte) error {
	t.writeSince.Store(time.Now().UnixNano())
	defer t.writeSince.Store(0)
	for len(p) > 0 {
		n, err := t.io(t.wfd, p, true)
		if err != nil {
			return err
		}
		if n <= 0 {
			return io.ErrShortWrite
		}
		p = p[n:]
	}
	return nil
}

func (t *transport) watch(stall time.Duration, paused func() bool) {
	go func() {
		tick := time.NewTicker(stall / 4)
		defer tick.Stop()
		for {
			select {
			case <-t.done:
				return
			case <-tick.C:
			}
			since := t.writeSince.Load()
			if since == 0 {
				continue
			}
			now := time.Now()
			if paused() {
				t.writeSince.CompareAndSwap(since, now.UnixNano())
				continue
			}
			if now.Sub(time.Unix(0, since)) >= stall {
				t.fail(errHostStalled)
				return
			}
		}
	}()
}

func (t *transport) expectHello(wait time.Duration) {
	time.AfterFunc(wait, func() {
		if !t.spoke.Load() {
			t.fail(errNoHello)
		}
	})
}

func (t *transport) SetChecksum(on bool) {
	t.wmu.Lock()
	t.checksum = on
	t.wmu.Unlock()
}

func (t *transport) Close() error {
	t.once.Do(func() {
		t.mu.Lock()
		t.closed = true
		t.mu.Unlock()
		unix.Dup3(t.devnull, t.rfd, unix.O_CLOEXEC)
		if t.wfd != t.rfd {
			unix.Dup3(t.devnull, t.wfd, unix.O_CLOEXEC)
		}
		go t.reap()
	})
	return nil
}

func (t *transport) reap() {
	pid := unix.Getpid()
	for {
		t.mu.Lock()
		tids := make([]int, 0, len(t.tids))
		for tid := range t.tids {
			tids = append(tids, tid)
		}
		t.mu.Unlock()
		if len(tids) == 0 {
			break
		}
		for _, tid := range tids {
			unix.Tgkill(pid, tid, unix.SIGURG)
		}
		time.Sleep(reapInterval)
	}
	unix.Close(t.rfd)
	if t.wfd != t.rfd {
		unix.Close(t.wfd)
	}
	close(t.done)
}
