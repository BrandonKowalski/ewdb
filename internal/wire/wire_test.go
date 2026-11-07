package wire

import (
	"bytes"
	"encoding/binary"
	"reflect"
	"testing"

	"ewdb/internal/config"
)

func TestRoundTrip(t *testing.T) {
	in := Message{Command: WRTE, Arg0: 7, Arg1: 9, Data: []byte("hello")}
	var buf bytes.Buffer
	if err := Write(&buf, in, true); err != nil {
		t.Fatal(err)
	}
	out, err := Read(&buf, MaxPayload)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Fatalf("got %+v want %+v", out, in)
	}
}

func TestRoundTripEmpty(t *testing.T) {
	in := Message{Command: OKAY, Arg0: 1, Arg1: 2}
	var buf bytes.Buffer
	if err := Write(&buf, in, false); err != nil {
		t.Fatal(err)
	}
	out, err := Read(&buf, MaxPayload)
	if err != nil {
		t.Fatal(err)
	}
	if out.Command != OKAY || out.Arg0 != 1 || out.Arg1 != 2 || len(out.Data) != 0 {
		t.Fatalf("got %+v", out)
	}
}

func header(cmd, length, magic uint32) []byte {
	h := make([]byte, 24)
	binary.LittleEndian.PutUint32(h[0:], cmd)
	binary.LittleEndian.PutUint32(h[12:], length)
	binary.LittleEndian.PutUint32(h[20:], magic)
	return h
}

func TestRejectsBadMagic(t *testing.T) {
	h := header(CNXN, 0, CNXN)
	if _, err := Read(bytes.NewReader(h), MaxPayload); err == nil {
		t.Fatal("expected error")
	}
}

func TestRejectsOversize(t *testing.T) {
	h := header(WRTE, 1048577, WRTE^0xffffffff)
	if _, err := Read(bytes.NewReader(h), 1048576); err == nil {
		t.Fatal("expected error")
	}
}

func TestChecksumIsByteSum(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, Message{Command: WRTE, Data: []byte{1, 2, 250}}, true); err != nil {
		t.Fatal(err)
	}
	if got := binary.LittleEndian.Uint32(buf.Bytes()[16:20]); got != 253 {
		t.Fatalf("checksum %d", got)
	}
	buf.Reset()
	if err := Write(&buf, Message{Command: WRTE, Data: []byte{1, 2, 250}}, false); err != nil {
		t.Fatal(err)
	}
	if got := binary.LittleEndian.Uint32(buf.Bytes()[16:20]); got != 0 {
		t.Fatalf("checksum %d", got)
	}
}

type countWriter struct{ calls int }

func (c *countWriter) Write(p []byte) (int, error) { c.calls++; return len(p), nil }

func TestWriteIsOneCall(t *testing.T) {
	var cw countWriter
	if err := Write(&cw, Message{Command: WRTE, Data: []byte("abc")}, true); err != nil {
		t.Fatal(err)
	}
	if cw.calls != 1 {
		t.Fatalf("calls %d", cw.calls)
	}
}

func TestBanner(t *testing.T) {
	c := &config.Config{Name: "dsos", Model: "RG DS Plus", Device: "rgds-plus"}
	want := "device::ro.product.name=dsos;ro.product.model=RG DS Plus;ro.product.device=rgds-plus;features=shell_v2,stat_v2,ls_v2,fixed_push_mkdir"
	if got := string(Banner(c, Features)); got != want {
		t.Fatalf("got %q", got)
	}
}
