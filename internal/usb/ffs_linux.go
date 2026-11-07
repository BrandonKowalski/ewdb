package usb

import (
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"

	"ewdb/internal/session"
	"ewdb/internal/wire"
)

const (
	evBind = iota
	evUnbind
	evEnable
	evDisable
	evSetup
	evSuspend
	evResume

	eventSize = 12
	usbDirIn  = 0x80

	settleDelay     = 250 * time.Millisecond
	disconnectHold  = 300 * time.Millisecond
	minSettle       = 50 * time.Millisecond
	reconnectWait   = 5 * time.Second
	connectAttempts = 10
	connectRetry    = 500 * time.Millisecond
	minBackoff      = 100 * time.Millisecond
	maxBackoff      = 5 * time.Second

	procMounts = "/proc/mounts"
	udcClass   = "/sys/class/udc"
)

var eventNames = map[byte]string{
	evBind: "BIND", evUnbind: "UNBIND", evEnable: "ENABLE", evDisable: "DISABLE",
	evSetup: "SETUP", evSuspend: "SUSPEND", evResume: "RESUME",
}

type ffsEndpoint struct {
	dir     string
	ep0     int
	devnull int

	open      func(gen uint64) (*transport, error)
	reconnect func(ctx context.Context) bool

	mu      sync.Mutex
	enabled bool
	gen     uint64
	changed chan struct{}
	cur     *transport

	last *transport
}

func openFFS(dir string) (*ffsEndpoint, error) {
	devnull, err := openDevNull()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "ep0")
	ep0, err := openFile(path, unix.O_RDWR|unix.O_CLOEXEC)
	if err != nil {
		unix.Close(devnull)
		return nil, fmt.Errorf("usb: open %s: %w", path, err)
	}
	if err := writeDescriptors(ep0); err != nil {
		unix.Close(ep0)
		unix.Close(devnull)
		return nil, fmt.Errorf("usb: %s: %w", path, err)
	}
	e := newFFSEndpoint(dir, ep0, devnull)
	go e.monitor()
	return e, nil
}

func newFFSEndpoint(dir string, ep0, devnull int) *ffsEndpoint {
	e := &ffsEndpoint{dir: dir, ep0: ep0, devnull: devnull, changed: make(chan struct{})}
	e.open = e.openTransport
	e.reconnect = e.softReconnect
	return e
}

func writeDescriptors(ep0 int) error {
	if _, err := writeRetry(ep0, DescriptorsV2()); err != nil {
		if err != unix.EINVAL {
			return fmt.Errorf("write v2 descriptors: %w", err)
		}
		if _, err := writeRetry(ep0, DescriptorsV1()); err != nil {
			return fmt.Errorf("write v1 descriptors: %w", err)
		}
		log.Print("usb: kernel refused v2 descriptors, using v1")
	}
	if _, err := writeRetry(ep0, Strings()); err != nil {
		return fmt.Errorf("write strings: %w", err)
	}
	return nil
}

func nextBackoff(d time.Duration) time.Duration {
	if d < minBackoff {
		return minBackoff
	}
	return min(2*d, maxBackoff)
}

func (e *ffsEndpoint) monitor() {
	buf := make([]byte, 4*eventSize)
	var backoff time.Duration
	for {
		n, err := unix.Read(e.ep0, buf)
		if err == unix.EINTR {
			continue
		}
		if err != nil || n < eventSize {
			backoff = nextBackoff(backoff)
			if err == nil {
				err = fmt.Errorf("short read of %d bytes", n)
			}
			log.Printf("usb: ep0 events: %v; retrying in %v", err, backoff)
			time.Sleep(backoff)
			continue
		}
		backoff = 0
		for off := 0; off+eventSize <= n; off += eventSize {
			e.event(buf[off : off+eventSize])
		}
	}
}

func (e *ffsEndpoint) event(ev []byte) {
	typ := ev[8]
	if name, ok := eventNames[typ]; !ok {
		log.Printf("usb: unknown event %d", typ)
	} else if typ != evSetup {
		log.Printf("usb: %s", name)
	}
	switch typ {
	case evEnable:
		e.update(true, func() {
			e.enabled = true
			e.gen++
		})
	case evDisable, evUnbind:
		e.update(true, func() { e.enabled = false })
	case evSetup:
		e.setup(ev)
	}
}

func (e *ffsEndpoint) update(fail bool, f func()) {
	e.mu.Lock()
	f()
	close(e.changed)
	e.changed = make(chan struct{})
	cur := e.cur
	if fail {
		e.cur = nil
	}
	e.mu.Unlock()
	if fail && cur != nil {
		cur.Close()
	}
}

func (e *ffsEndpoint) setup(ev []byte) {
	length := binary.LittleEndian.Uint16(ev[6:8])
	var err error
	if ev[0]&usbDirIn != 0 {
		_, err = writeRetry(e.ep0, nil)
	} else {
		buf := make([]byte, int(length)+1)
		for {
			_, err = unix.Read(e.ep0, buf)
			if err != unix.EINTR {
				break
			}
		}
	}
	if err != nil {
		log.Printf("usb: control request 0x%02x/0x%02x: %v", ev[0], ev[1], err)
	}
}

type ffsState struct {
	enabled bool
	gen     uint64
	changed chan struct{}
}

func (e *ffsEndpoint) state() ffsState {
	e.mu.Lock()
	defer e.mu.Unlock()
	return ffsState{e.enabled, e.gen, e.changed}
}

func (e *ffsEndpoint) Next(ctx context.Context) (session.Transport, error) {
	var prevGen uint64
	hadPrev := false
	if p := e.last; p != nil {
		p.Close()
		if err := waitDone(ctx, p.done); err != nil {
			return nil, err
		}
		prevGen, hadPrev = p.gen, true
	}
	settle := time.Now().Add(settleDelay)
	reconnected := false
	var fallback time.Time
	var backoff time.Duration
	for {
		s := e.state()
		wait := time.Duration(-1)
		if s.enabled {
			now := time.Now()
			fresh := !hadPrev || s.gen != prevGen
			switch {
			case fresh || (reconnected && !now.Before(fallback)):
				t, err := e.open(s.gen)
				if err == nil {
					if e.register(t) {
						return t, nil
					}
					t.Close()
					continue
				}
				backoff = nextBackoff(backoff)
				log.Printf("usb: %v; retrying in %v", err, backoff)
				wait = backoff
			case !reconnected && now.Before(settle):
				wait = settle.Sub(now)
			case !reconnected:
				reconnected = true
				fallback = time.Now()
				if e.reconnect(ctx) {
					fallback = time.Now().Add(reconnectWait)
				}
				continue
			default:
				wait = fallback.Sub(now)
			}
		}
		if err := waitChange(ctx, s.changed, wait); err != nil {
			return nil, err
		}
	}
}

func waitChange(ctx context.Context, changed <-chan struct{}, wait time.Duration) error {
	var timeout <-chan time.Time
	if wait >= 0 {
		tm := time.NewTimer(wait)
		defer tm.Stop()
		timeout = tm.C
	}
	select {
	case <-changed:
	case <-timeout:
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}

func (e *ffsEndpoint) register(t *transport) bool {
	e.mu.Lock()
	ok := e.enabled && e.gen == t.gen
	if ok {
		e.cur = t
	}
	e.mu.Unlock()
	if ok {
		e.last = t
	}
	return ok
}

func (e *ffsEndpoint) openTransport(gen uint64) (*transport, error) {
	out, err := openFile(filepath.Join(e.dir, "ep1"), unix.O_RDONLY|unix.O_CLOEXEC)
	if err != nil {
		return nil, fmt.Errorf("open ep1: %w", err)
	}
	in, err := openFile(filepath.Join(e.dir, "ep2"), unix.O_WRONLY|unix.O_CLOEXEC)
	if err != nil {
		unix.Close(out)
		return nil, fmt.Errorf("open ep2: %w", err)
	}
	t := newFFSTransport(out, in, e.devnull)
	t.gen = gen
	return t, nil
}

func newFFSTransport(out, in, devnull int) *transport {
	return newTransport(out, in, devnull, wire.MaxPayload, wire.MaxPayload)
}

func (e *ffsEndpoint) softReconnect(ctx context.Context) bool {
	udc, udcFile := findUDC(procMounts, udcClass, e.dir)
	if udc == "" {
		log.Print("usb: no UDC found to reconnect; waiting for the host")
		return false
	}
	path := filepath.Join(udcClass, udc, "soft_connect")
	log.Printf("usb: reconnecting %s so the host starts a new connection", udc)
	return cycleSoftConnect(ctx, udcFile, func(v string) error { return writeSysfs(path, v) })
}

func cycleSoftConnect(ctx context.Context, udcFile string, write func(string) error) bool {
	if err := write("disconnect"); err != nil {
		log.Printf("usb: soft_connect disconnect: %v; waiting for the host", err)
		return false
	}
	if sleepCtx(ctx, disconnectHold) != nil {
		time.Sleep(minSettle)
	}
	var err error
	for i := 1; i <= connectAttempts; i++ {
		if i > 1 {
			time.Sleep(connectRetry)
		}
		if err = write("connect"); err == nil {
			return true
		}
		if udcFile != "" {
			if b, rerr := os.ReadFile(udcFile); rerr == nil && strings.TrimSpace(string(b)) == "" {
				log.Print("usb: UDC was unbound; it connects again when rebound")
				return false
			}
		}
	}
	log.Printf("usb: soft_connect connect: %v", err)
	return false
}

func writeSysfs(path, value string) error {
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	_, err = f.WriteString(value)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}
