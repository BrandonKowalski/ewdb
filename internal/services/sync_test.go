package services

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

type memStream struct {
	r  io.Reader
	mu sync.Mutex
	b  bytes.Buffer
}

func (m *memStream) Read(p []byte) (int, error) { return m.r.Read(p) }
func (m *memStream) Write(p []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.b.Write(p)
}
func (m *memStream) Close() error {
	if c, ok := m.r.(io.Closer); ok {
		c.Close()
	}
	return nil
}
func (m *memStream) out() []byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]byte(nil), m.b.Bytes()...)
}

func req(id, path string) []byte {
	b := []byte(id)
	b = binary.LittleEndian.AppendUint32(b, uint32(len(path)))
	return append(b, path...)
}

func chunk(id string, data []byte) []byte {
	b := []byte(id)
	b = binary.LittleEndian.AppendUint32(b, uint32(len(data)))
	return append(b, data...)
}

func done(mtime uint32) []byte {
	b := []byte("DONE")
	return binary.LittleEndian.AppendUint32(b, mtime)
}

func runSync(t *testing.T, in ...[]byte) []byte {
	t.Helper()
	m := &memStream{r: bytes.NewReader(bytes.Join(in, nil))}
	serveSync(context.Background(), m)
	return m.out()
}

func TestStatMissingGivesZeros(t *testing.T) {
	out := runSync(t, req("STAT", filepath.Join(t.TempDir(), "nope")))
	want := append([]byte("STAT"), make([]byte, 12)...)
	if !bytes.Equal(out, want) {
		t.Fatalf("got %x want %x", out, want)
	}
}

func TestStat2MissingGivesErrno(t *testing.T) {
	out := runSync(t, req("STA2", filepath.Join(t.TempDir(), "nope")))
	if len(out) != 72 || string(out[:4]) != "STA2" {
		t.Fatalf("got %x", out)
	}
	if e := binary.LittleEndian.Uint32(out[4:8]); e != uint32(syscall.ENOENT) {
		t.Fatalf("errno %d", e)
	}
}

func TestStat2Regular(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f")
	os.WriteFile(p, []byte("hello"), 0o640)
	out := runSync(t, req("STA2", p))
	if len(out) != 72 {
		t.Fatalf("len %d", len(out))
	}
	if binary.LittleEndian.Uint32(out[4:8]) != 0 {
		t.Fatal("error set")
	}
	if mode := binary.LittleEndian.Uint32(out[24:28]); mode != syscall.S_IFREG|0o640 {
		t.Fatalf("mode %o", mode)
	}
	if size := binary.LittleEndian.Uint64(out[40:48]); size != 5 {
		t.Fatalf("size %d", size)
	}
}

func TestList2Terminator(t *testing.T) {
	d := t.TempDir()
	os.WriteFile(filepath.Join(d, "x"), nil, 0o644)
	out := runSync(t, req("LIS2", d))
	if len(out) < 76 {
		t.Fatalf("short %d", len(out))
	}
	tail := out[len(out)-76:]
	if string(tail[:4]) != "DONE" || !bytes.Equal(tail[4:], make([]byte, 72)) {
		t.Fatalf("terminator %x", tail)
	}
	if !bytes.Contains(out, append([]byte("DNT2"), 0, 0, 0, 0)) {
		t.Fatal("no DNT2 entry")
	}
	if !bytes.Contains(out, []byte{1, 0, 0, 0, 'x'}) {
		t.Fatal("no entry x")
	}
}

func TestListV1(t *testing.T) {
	d := t.TempDir()
	os.WriteFile(filepath.Join(d, "abc"), []byte("zz"), 0o644)
	out := runSync(t, req("LIST", d))
	if !bytes.HasSuffix(out, append([]byte("DONE"), make([]byte, 16)...)) {
		t.Fatalf("terminator %x", out)
	}
	if !bytes.Contains(out, []byte{3, 0, 0, 0, 'a', 'b', 'c'}) {
		t.Fatal("no abc")
	}
}

func TestSendCreatesParents(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a", "b", "c.txt")
	out := runSync(t,
		req("SEND", p+","+strconv.Itoa(0o100644)),
		chunk("DATA", []byte("hello ")),
		chunk("DATA", []byte("world")),
		done(1700000000),
	)
	if !bytes.Equal(out, append([]byte("OKAY"), 0, 0, 0, 0)) {
		t.Fatalf("reply %q", out)
	}
	got, err := os.ReadFile(p)
	if err != nil || string(got) != "hello world" {
		t.Fatalf("content %q err %v", got, err)
	}
	fi, _ := os.Stat(p)
	if fi.ModTime().Unix() != 1700000000 {
		t.Fatalf("mtime %v", fi.ModTime())
	}
	if fi.Mode().Perm() != 0o644 {
		t.Fatalf("mode %v", fi.Mode())
	}
}

func TestSendSymlink(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "sub", "link")
	out := runSync(t,
		req("SEND", p+","+strconv.Itoa(0o120777)),
		chunk("DATA", []byte("/some/target")),
		done(1700000000),
	)
	if !bytes.Equal(out, append([]byte("OKAY"), 0, 0, 0, 0)) {
		t.Fatalf("reply %q", out)
	}
	tgt, err := os.Readlink(p)
	if err != nil || tgt != "/some/target" {
		t.Fatalf("link %q err %v", tgt, err)
	}
}

func TestSendThenRecv(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f")
	payload := bytes.Repeat([]byte("0123456789"), 20000)
	runSync(t,
		req("SEND", p+",33188"),
		chunk("DATA", payload[:65536]),
		chunk("DATA", payload[65536:131072]),
		chunk("DATA", payload[131072:]),
		done(1),
	)
	out := runSync(t, req("RECV", p))
	var got []byte
	for len(out) >= 8 {
		id := string(out[:4])
		n := int(binary.LittleEndian.Uint32(out[4:8]))
		out = out[8:]
		if id == "DONE" {
			break
		}
		if id != "DATA" || n > 65536 {
			t.Fatalf("bad chunk %s %d", id, n)
		}
		got = append(got, out[:n]...)
		out = out[n:]
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("payload mismatch %d vs %d", len(got), len(payload))
	}
}

func TestRecvMissingFails(t *testing.T) {
	out := runSync(t, req("RECV", filepath.Join(t.TempDir(), "nope")))
	if string(out[:4]) != "FAIL" {
		t.Fatalf("got %q", out)
	}
	n := binary.LittleEndian.Uint32(out[4:8])
	msg := string(out[8 : 8+n])
	if !strings.Contains(msg, "No such file") {
		t.Fatalf("msg %q", msg)
	}
}

func TestUnknownCommandFails(t *testing.T) {
	out := runSync(t, req("ZZZZ", "x"))
	if string(out[:4]) != "FAIL" || !strings.Contains(string(out[8:]), "unknown command") {
		t.Fatalf("got %q", out)
	}
}

func TestSendCancelMidTransfer(t *testing.T) {
	p := filepath.Join(t.TempDir(), "partial")
	pr, pw := io.Pipe()
	m := &memStream{r: pr}
	ctx, cancel := context.WithCancel(context.Background())
	returned := make(chan struct{})
	go func() {
		defer close(returned)
		serveSync(ctx, m)
	}()
	pw.Write(req("SEND", p+",33188"))
	pw.Write(chunk("DATA", []byte("first half")))
	deadline := time.Now().Add(2 * time.Second)
	for {
		if b, err := os.ReadFile(p); err == nil && string(b) == "first half" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("partial data never written")
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	select {
	case <-returned:
	case <-time.After(time.Second):
		t.Fatal("handler did not return within 1s of cancel")
	}
	if _, err := os.Lstat(p); !os.IsNotExist(err) {
		t.Fatalf("partial file remains: %v", err)
	}
}

func TestSendOversizeChunkFails(t *testing.T) {
	p := filepath.Join(t.TempDir(), "big")
	out := runSync(t,
		req("SEND", p+",33188"),
		chunk("DATA", []byte("ok")),
		chunk("DATA", make([]byte, 65537)),
	)
	if string(out[:4]) != "FAIL" || !strings.Contains(string(out[8:]), "oversize data message") {
		t.Fatalf("got %q", out)
	}
	if _, err := os.Lstat(p); !os.IsNotExist(err) {
		t.Fatalf("partial file remains: %v", err)
	}
}
