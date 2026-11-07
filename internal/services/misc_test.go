package services

import (
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"

	"ewdb/internal/config"
)

func screencapDeps(t *testing.T, script string) *Deps {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "screencap"), []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return &Deps{Config: &config.Config{Shell: "/bin/sh", Commands: map[string]string{}}, SystemPath: dir}
}

func TestFramebufferHeader(t *testing.T) {
	d := screencapDeps(t, `printf '\002\000\000\000\001\000\000\000\001\000\000\000'; printf 'ABCDEFGHextra'`)
	var out bytes.Buffer
	framebuffer(context.Background(), &out, d)
	b := out.Bytes()
	if len(b) != 52+8 {
		t.Fatalf("got %d bytes", len(b))
	}
	want := []uint32{1, 32, 8, 2, 1, 0, 8, 16, 8, 8, 8, 24, 8}
	for i, w := range want {
		if g := binary.LittleEndian.Uint32(b[4*i:]); g != w {
			t.Errorf("field %d: got %d want %d", i, g, w)
		}
	}
	if string(b[52:]) != "ABCDEFGH" {
		t.Errorf("pixels %q", b[52:])
	}
}

func TestFramebufferRejectsOtherFormats(t *testing.T) {
	d := screencapDeps(t, `printf '\002\000\000\000\001\000\000\000\004\000\000\000'; printf 'ABCDEFGH'`)
	var out bytes.Buffer
	framebuffer(context.Background(), &out, d)
	if out.Len() != 0 {
		t.Fatalf("got %q", out.Bytes())
	}
}

func TestFramebufferFailureSendsNothing(t *testing.T) {
	d := screencapDeps(t, `exit 1`)
	var out bytes.Buffer
	framebuffer(context.Background(), &out, d)
	if out.Len() != 0 {
		t.Fatalf("got %q", out.Bytes())
	}
}

func TestFramebufferKilledOnContext(t *testing.T) {
	d := screencapDeps(t, `exec sleep 30`)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	done := make(chan struct{})
	go func() {
		framebuffer(ctx, &bytes.Buffer{}, d)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("framebuffer did not stop")
	}
}

type closeRecorder struct {
	bytes.Buffer
	closed bool
}

func (c *closeRecorder) Close() error {
	c.closed = true
	return nil
}

func TestRebootRunsMappingAfterClosing(t *testing.T) {
	mark := filepath.Join(t.TempDir(), "mark")
	d := &Deps{
		Config:     &config.Config{Shell: "/bin/sh", Commands: map[string]string{"reboot.bootloader": "touch " + mark}},
		SystemPath: "/usr/bin:/bin",
	}
	var s closeRecorder
	reboot(&s, d, "bootloader")
	if !s.closed {
		t.Error("stream not closed")
	}
	if _, err := os.Stat(mark); err != nil {
		t.Error(err)
	}
}
