package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const (
	connDeadline = 30 * time.Second
	maxRequest   = 1 << 20
	defaultSock  = "/run/ewdb/input.sock"
)

type runner func(args []string) error

func listen(path string) (net.Listener, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	if fi, err := os.Lstat(path); err == nil {
		if fi.Mode()&os.ModeSocket == 0 {
			return nil, fmt.Errorf("%s exists and is not a socket", path)
		}
		if c, err := net.DialTimeout("unix", path, time.Second); err == nil {
			c.Close()
			return nil, fmt.Errorf("another ewdb-input is already listening on %s", path)
		}
		if err := os.Remove(path); err != nil {
			return nil, err
		}
	}
	old := syscall.Umask(0o177)
	ln, err := net.Listen("unix", path)
	syscall.Umask(old)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		ln.Close()
		return nil, err
	}
	return ln, nil
}

func serveLoop(ctx context.Context, ln net.Listener, run runner) {
	go func() {
		<-ctx.Done()
		ln.Close()
	}()
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				continue
			}
			log.Printf("accept: %v", err)
			return
		}
		handle(conn, run)
	}
}

func handle(conn net.Conn, run runner) {
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(connDeadline))
	data, err := io.ReadAll(io.LimitReader(conn, maxRequest+1))
	reply := "ok\n"
	switch {
	case err != nil:
		reply = fmt.Sprintf("error: %v\n", err)
	case len(data) > maxRequest:
		reply = "error: request too large\n"
	default:
		args := strings.Split(string(data), "\x00")
		if len(data) == 0 {
			args = nil
		}
		if err := run(args); err != nil {
			reply = fmt.Sprintf("error: %s\n", strings.ReplaceAll(err.Error(), "\n", " "))
		}
	}
	conn.Write([]byte(reply))
}

func client(sock string, args []string, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: ewdb-input [SOURCE] tap X Y | swipe X1 Y1 X2 Y2 [MS] | keyevent KEY... | text STRING")
		return 1
	}
	conn, err := net.DialTimeout("unix", sock, 3*time.Second)
	if err != nil {
		fmt.Fprintln(stderr, "ewdb-input: ewdb-input --serve is not running")
		return 1
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(connDeadline + 5*time.Second))
	if _, err := conn.Write([]byte(strings.Join(args, "\x00"))); err != nil {
		fmt.Fprintf(stderr, "ewdb-input: %v\n", err)
		return 1
	}
	if uc, ok := conn.(*net.UnixConn); ok {
		uc.CloseWrite()
	}
	reply, err := io.ReadAll(io.LimitReader(conn, 64<<10))
	if err != nil && len(reply) == 0 {
		fmt.Fprintf(stderr, "ewdb-input: %v\n", err)
		return 1
	}
	text := strings.TrimRight(string(reply), "\n")
	if text == "ok" {
		return 0
	}
	if msg, ok := strings.CutPrefix(text, "error: "); ok {
		fmt.Fprintf(stderr, "ewdb-input: %s\n", msg)
	} else {
		fmt.Fprintf(stderr, "ewdb-input: unexpected reply %q\n", text)
	}
	return 1
}
