package services

import (
	"context"
	"encoding/binary"
	"io"
	"os/exec"
	"time"
)

const (
	framebufferTimeout = 10 * time.Second
	formatRGBA8888     = 1
)

func framebuffer(ctx context.Context, w io.Writer, d *Deps) {
	ctx, cancel := context.WithTimeout(ctx, framebufferTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, d.Config.Shell, "-c", "exec screencap")
	cmd.Env = Env(d, "")
	cmd.Dir = "/"
	cmd.WaitDelay = time.Second
	out, err := cmd.StdoutPipe()
	if err != nil {
		return
	}
	if err := cmd.Start(); err != nil {
		return
	}
	defer func() {
		cancel()
		cmd.Wait()
	}()
	var in [12]byte
	if _, err := io.ReadFull(out, in[:]); err != nil {
		return
	}
	width := binary.LittleEndian.Uint32(in[0:4])
	height := binary.LittleEndian.Uint32(in[4:8])
	if binary.LittleEndian.Uint32(in[8:12]) != formatRGBA8888 {
		return
	}
	size := uint64(width) * uint64(height) * 4
	if size > 1<<31 {
		return
	}
	fields := []uint32{1, 32, uint32(size), width, height, 0, 8, 16, 8, 8, 8, 24, 8}
	hdr := make([]byte, 4*len(fields))
	for i, f := range fields {
		binary.LittleEndian.PutUint32(hdr[4*i:], f)
	}
	if _, err := w.Write(hdr); err != nil {
		return
	}
	io.CopyN(w, out, int64(size))
}
