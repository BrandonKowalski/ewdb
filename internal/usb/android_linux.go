package usb

import (
	"context"
	"fmt"
	"log"
	"time"

	"golang.org/x/sys/unix"

	"ewdb/internal/session"
)

const (
	androidChunk = 4096
	androidRetry = 500 * time.Millisecond
)

type androidEndpoint struct {
	path    string
	fd      int
	devnull int
	cur     *transport
}

func openAndroid(path string) (*androidEndpoint, error) {
	devnull, err := openDevNull()
	if err != nil {
		return nil, err
	}
	fd, err := openFile(path, unix.O_RDWR|unix.O_CLOEXEC)
	if err != nil {
		unix.Close(devnull)
		return nil, fmt.Errorf("usb: open %s: %w", path, err)
	}
	return &androidEndpoint{path: path, fd: fd, devnull: devnull}, nil
}

func (e *androidEndpoint) Next(ctx context.Context) (session.Transport, error) {
	if e.cur != nil {
		e.cur.Close()
		if err := waitDone(ctx, e.cur.done); err != nil {
			return nil, err
		}
		e.cur = nil
		if err := sleepCtx(ctx, androidRetry); err != nil {
			return nil, err
		}
	}
	var lastErr error
	for e.fd < 0 {
		fd, err := openFile(e.path, unix.O_RDWR|unix.O_CLOEXEC)
		if err == nil {
			e.fd = fd
			break
		}
		if lastErr == nil || err.Error() != lastErr.Error() {
			log.Printf("usb: open %s: %v", e.path, err)
		}
		lastErr = err
		if err := sleepCtx(ctx, androidRetry); err != nil {
			return nil, err
		}
	}
	t := newTransport(e.fd, e.fd, e.devnull, androidChunk, androidChunk, false)
	e.fd = -1
	e.cur = t
	return t, nil
}

func waitDone(ctx context.Context, done <-chan struct{}) error {
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	tm := time.NewTimer(d)
	defer tm.Stop()
	select {
	case <-tm.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
