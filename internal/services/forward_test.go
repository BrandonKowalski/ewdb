package services

import (
	"context"
	"io"
	"net"
	"testing"
	"time"
)

func TestTCPTarget(t *testing.T) {
	cases := map[string]string{
		"5555":           "127.0.0.1:5555",
		"example.com:80": "example.com:80",
		":80":            "127.0.0.1:80",
		"[::1]:80":       "[::1]:80",
	}
	for in, want := range cases {
		got, err := tcpTarget(in)
		if err != nil || got != want {
			t.Errorf("%q: got %q %v want %q", in, got, err, want)
		}
	}
	if _, err := tcpTarget(""); err == nil {
		t.Error("empty target accepted")
	}
}

func TestPipeReturnsOnContextAndClosesBoth(t *testing.T) {
	a1, a2 := net.Pipe()
	b1, b2 := net.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		pipe(ctx, a2, b2)
		close(done)
	}()
	go a1.Write([]byte("x"))
	buf := make([]byte, 1)
	if _, err := io.ReadFull(b1, buf); err != nil || buf[0] != 'x' {
		t.Fatalf("copy: %v %q", err, buf)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("pipe did not return")
	}
	if _, err := a1.Read(buf); err == nil {
		t.Fatal("a side still open")
	}
	if _, err := b1.Read(buf); err == nil {
		t.Fatal("b side still open")
	}
}

func TestPipeEndsWhenOneSideCloses(t *testing.T) {
	a1, a2 := net.Pipe()
	b1, b2 := net.Pipe()
	done := make(chan struct{})
	go func() {
		pipe(context.Background(), a2, b2)
		close(done)
	}()
	a1.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("pipe did not return")
	}
	if _, err := b1.Read(make([]byte, 1)); err == nil {
		t.Fatal("b side still open")
	}
}
