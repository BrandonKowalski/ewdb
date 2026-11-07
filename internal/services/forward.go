package services

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"strings"
	"sync"

	"ewdb/internal/session"
)

func forwardHandlers(r *session.Registry) {
	r.Handle("tcp:", dialHandler("tcp:"))
	r.Handle("localabstract:", dialHandler("localabstract:"))
	r.Handle("localfilesystem:", dialHandler("localfilesystem:"))
	r.Handle("dev:", devHandler)
	r.Handle("localreserved:", func(ctx context.Context, s *session.Stream, arg string) {})
}

func dialHandler(prefix string) session.Handler {
	return func(ctx context.Context, s *session.Stream, arg string) {
		c, err := dialSpec(ctx, prefix+arg)
		if err != nil {
			return
		}
		pipe(ctx, s, c)
	}
}

func devHandler(ctx context.Context, s *session.Stream, arg string) {
	f, err := os.OpenFile(arg, os.O_RDWR, 0)
	if err != nil {
		return
	}
	pipe(ctx, s, f)
}

func tcpTarget(arg string) (string, error) {
	if arg == "" {
		return "", errors.New("empty tcp target")
	}
	if !strings.Contains(arg, ":") {
		return net.JoinHostPort("127.0.0.1", arg), nil
	}
	host, port, err := net.SplitHostPort(arg)
	if err != nil {
		return "", err
	}
	if host == "" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port), nil
}

func dialSpec(ctx context.Context, spec string) (net.Conn, error) {
	var d net.Dialer
	switch {
	case strings.HasPrefix(spec, "tcp:"):
		addr, err := tcpTarget(spec[len("tcp:"):])
		if err != nil {
			return nil, err
		}
		return d.DialContext(ctx, "tcp", addr)
	case strings.HasPrefix(spec, "localabstract:"):
		addr, err := abstractAddr(spec[len("localabstract:"):])
		if err != nil {
			return nil, err
		}
		return d.DialContext(ctx, "unix", addr)
	case strings.HasPrefix(spec, "localfilesystem:"):
		return d.DialContext(ctx, "unix", spec[len("localfilesystem:"):])
	}
	return nil, errors.New("unsupported socket spec: " + spec)
}

func pipe(ctx context.Context, a, b io.ReadWriteCloser) {
	var once sync.Once
	closeBoth := func() {
		once.Do(func() {
			a.Close()
			b.Close()
		})
	}
	stop := context.AfterFunc(ctx, closeBoth)
	defer stop()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		io.Copy(a, b)
		closeBoth()
	}()
	go func() {
		defer wg.Done()
		io.Copy(b, a)
		closeBoth()
	}()
	wg.Wait()
}
