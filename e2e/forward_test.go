//go:build e2e

package e2e

import (
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

func echoServer(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				io.Copy(c, c)
			}()
		}
	}()
	_, p, _ := net.SplitHostPort(l.Addr().String())
	return p
}

func roundTrip(t *testing.T, addr, msg string) {
	t.Helper()
	var c net.Conn
	var err error
	for i := 0; i < 50; i++ {
		c, err = net.Dial("tcp", addr)
		if err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := c.Write([]byte(msg)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, len(msg))
	if _, err := io.ReadFull(c, buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != msg {
		t.Fatalf("got %q want %q", buf, msg)
	}
}

func TestForwardTCP(t *testing.T) {
	serial := startDaemon(t, "")
	p := echoServer(t)
	l, _ := freePort()
	if _, se, code := adb(t, serial, "forward", "tcp:"+l, "tcp:"+p); code != 0 {
		t.Fatalf("forward: %d %s", code, se)
	}
	roundTrip(t, "127.0.0.1:"+l, "ping")
}

func TestForwardList(t *testing.T) {
	serial := startDaemon(t, "")
	p := echoServer(t)
	l, _ := freePort()
	if _, se, code := adb(t, serial, "forward", "tcp:"+l, "tcp:"+p); code != 0 {
		t.Fatalf("forward: %d %s", code, se)
	}
	out, _, code := adb(t, serial, "forward", "--list")
	if code != 0 || !strings.Contains(out, "tcp:"+l+" tcp:"+p) {
		t.Fatalf("list: %d %q", code, out)
	}
}

func TestReverseTCP(t *testing.T) {
	serial := startDaemon(t, "")
	h := echoServer(t)
	r, _ := freePort()
	if _, se, code := adb(t, serial, "reverse", "tcp:"+r, "tcp:"+h); code != 0 {
		t.Fatalf("reverse: %d %s", code, se)
	}
	roundTrip(t, "127.0.0.1:"+r, "pong")
}

func TestReverseListAndRemove(t *testing.T) {
	serial := startDaemon(t, "")
	h := echoServer(t)
	r, _ := freePort()
	if _, se, code := adb(t, serial, "reverse", "tcp:"+r, "tcp:"+h); code != 0 {
		t.Fatalf("reverse: %d %s", code, se)
	}
	out, _, code := adb(t, serial, "reverse", "--list")
	if code != 0 || !strings.Contains(out, "tcp:"+r+" tcp:"+h) {
		t.Fatalf("list: %d %q", code, out)
	}
	if _, se, code := adb(t, serial, "reverse", "--remove-all"); code != 0 {
		t.Fatalf("remove-all: %d %s", code, se)
	}
	out, _, code = adb(t, serial, "reverse", "--list")
	if code != 0 || strings.TrimSpace(out) != "" {
		t.Fatalf("list after remove: %d %q", code, out)
	}
	if c, err := net.DialTimeout("tcp", "127.0.0.1:"+r, time.Second); err == nil {
		c.Close()
		t.Fatal("listener still open")
	}
}

func TestReverseNoRebind(t *testing.T) {
	serial := startDaemon(t, "")
	h := echoServer(t)
	r, _ := freePort()
	if _, se, code := adb(t, serial, "reverse", "tcp:"+r, "tcp:"+h); code != 0 {
		t.Fatalf("reverse: %d %s", code, se)
	}
	_, se, code := adb(t, serial, "reverse", "--no-rebind", "tcp:"+r, "tcp:"+h)
	if code != 1 || !strings.Contains(se, "cannot rebind") {
		t.Fatalf("got %d %q", code, se)
	}
}

func TestForwardToClosedPort(t *testing.T) {
	serial := startDaemon(t, "")
	dead, _ := freePort()
	l, _ := freePort()
	if _, se, code := adb(t, serial, "forward", "tcp:"+l, "tcp:"+dead); code != 0 {
		t.Fatalf("forward: %d %s", code, se)
	}
	var c net.Conn
	var err error
	for i := 0; i < 50; i++ {
		c, err = net.Dial("tcp", "127.0.0.1:"+l)
		if err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := c.Read(make([]byte, 1)); err == nil || (err != io.EOF && !strings.Contains(err.Error(), "reset")) {
		t.Fatalf("expected close, got %v", err)
	}
	if out, se, code := adb(t, serial, "shell", "true"); code != 0 {
		t.Fatalf("shell: %d %s %s", code, out, se)
	}
}

func TestReverseListenerClosesWithSession(t *testing.T) {
	serial := startDaemon(t, "")
	h := echoServer(t)
	r, _ := freePort()
	if _, se, code := adb(t, serial, "reverse", "tcp:"+r, "tcp:"+h); code != 0 {
		t.Fatalf("reverse: %d %s", code, se)
	}
	roundTrip(t, "127.0.0.1:"+r, "up")
	adb(t, "", "disconnect", serial)
	deadline := time.Now().Add(3 * time.Second)
	for {
		c, err := net.DialTimeout("tcp", "127.0.0.1:"+r, 200*time.Millisecond)
		if err != nil {
			return
		}
		c.Close()
		if time.Now().After(deadline) {
			t.Fatal("listener outlived its session")
		}
		time.Sleep(50 * time.Millisecond)
	}
}
