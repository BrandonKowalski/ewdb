package main

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func sockDir(t *testing.T) string {
	d, err := os.MkdirTemp("", "ewi")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(d) })
	return d
}

func start(t *testing.T, path string, run runner) context.CancelFunc {
	ln, err := listen(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		serveLoop(ctx, ln, run)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	return cancel
}

func TestProtocol(t *testing.T) {
	path := filepath.Join(sockDir(t), "sub", "in.sock")
	var got [][]string
	start(t, path, func(args []string) error {
		got = append(got, args)
		if args[0] == "bad" {
			return errors.New("it broke")
		}
		return nil
	})
	fi, err := os.Stat(path)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("%v %v", fi, err)
	}
	var stderr bytes.Buffer
	if rc := client(path, []string{"keyevent", "A", "B C"}, &stderr); rc != 0 {
		t.Fatalf("rc %d %s", rc, stderr.String())
	}
	if len(got) != 1 || strings.Join(got[0], "|") != "keyevent|A|B C" {
		t.Fatalf("%q", got)
	}
	stderr.Reset()
	if rc := client(path, []string{"bad"}, &stderr); rc != 1 || stderr.String() != "ewdb-input: it broke\n" {
		t.Fatalf("rc %d %q", rc, stderr.String())
	}
}

func TestClientNoServer(t *testing.T) {
	var stderr bytes.Buffer
	rc := client(filepath.Join(sockDir(t), "none.sock"), []string{"tap", "1", "2"}, &stderr)
	if rc != 1 || stderr.String() != "ewdb-input: ewdb-input --serve is not running\n" {
		t.Fatalf("rc %d %q", rc, stderr.String())
	}
}

func TestSerialConnections(t *testing.T) {
	path := filepath.Join(sockDir(t), "in.sock")
	active, overlap := 0, false
	start(t, path, func(args []string) error {
		active++
		if active > 1 {
			overlap = true
		}
		time.Sleep(50 * time.Millisecond)
		active--
		return nil
	})
	done := make(chan int, 3)
	for i := 0; i < 3; i++ {
		go func() {
			var b bytes.Buffer
			done <- client(path, []string{"keyevent", "A"}, &b)
		}()
	}
	for i := 0; i < 3; i++ {
		if rc := <-done; rc != 0 {
			t.Fatal(rc)
		}
	}
	if overlap {
		t.Fatal("requests overlapped")
	}
}

func TestStaleSocket(t *testing.T) {
	path := filepath.Join(sockDir(t), "in.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	ln.(*net.UnixListener).SetUnlinkOnClose(false)
	ln.Close()
	if _, err := os.Stat(path); err != nil {
		t.Fatal("stale socket file missing")
	}
	cancel := start(t, path, func([]string) error { return nil })
	var b bytes.Buffer
	if rc := client(path, []string{"keyevent", "A"}, &b); rc != 0 {
		t.Fatal(b.String())
	}
	if _, err := listen(path); err == nil {
		t.Fatal("second listener on a live socket")
	}
	cancel()
}

func TestRefusesNonSocket(t *testing.T) {
	path := filepath.Join(sockDir(t), "file")
	os.WriteFile(path, []byte("x"), 0o644)
	if _, err := listen(path); err == nil {
		t.Fatal("removed a regular file")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}
