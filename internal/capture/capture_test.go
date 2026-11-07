package capture

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"image/png"
	"testing"
)

func TestXR24(t *testing.T) {
	src := make([]byte, 24)
	px := [][]byte{{0x30, 0x20, 0x10, 0x00}, {0x00, 0x00, 0xff, 0x00}, {0x00, 0xff, 0x00, 0x00}, {0xff, 0x00, 0x00, 0x00}}
	for i, p := range px {
		copy(src[(i/2)*12+(i%2)*4:], p)
	}
	got, err := ToRGBA(FourCC('X', 'R', '2', '4'), src, 12, 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	if h := hex.EncodeToString(got); h != "102030ffff0000ff00ff00ff0000ffff" {
		t.Fatalf("got %s", h)
	}
}

func TestRG16(t *testing.T) {
	got, err := ToRGBA(FourCC('R', 'G', '1', '6'), []byte{0x00, 0xf8, 0x1f, 0x00}, 4, 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	if h := hex.EncodeToString(got); h != "ff0000ff0000ffff" {
		t.Fatalf("got %s", h)
	}
}

func TestOtherFormats(t *testing.T) {
	cases := []struct {
		format string
		src    []byte
		want   string
	}{
		{"AR24", []byte{1, 2, 3, 4}, "03020104"},
		{"XB24", []byte{1, 2, 3, 4}, "010203ff"},
		{"AB24", []byte{1, 2, 3, 4}, "01020304"},
		{"RG24", []byte{1, 2, 3}, "030201ff"},
		{"BG24", []byte{1, 2, 3}, "010203ff"},
		{"BG16", []byte{0x00, 0xf8}, "0000ffff"},
	}
	for _, c := range cases {
		f := FourCC(c.format[0], c.format[1], c.format[2], c.format[3])
		got, err := ToRGBA(f, c.src, len(c.src), 1, 1)
		if err != nil {
			t.Fatalf("%s: %v", c.format, err)
		}
		if h := hex.EncodeToString(got); h != c.want {
			t.Fatalf("%s: got %s want %s", c.format, h, c.want)
		}
	}
}

func TestUnknownFormat(t *testing.T) {
	if _, err := ToRGBA(FourCC('Y', 'U', 'Y', 'V'), make([]byte, 8), 4, 2, 1); err == nil {
		t.Fatal("expected error")
	}
}

func TestShortSource(t *testing.T) {
	if _, err := ToRGBA(FourCC('X', 'R', '2', '4'), make([]byte, 15), 8, 2, 2); err == nil {
		t.Fatal("expected error")
	}
}

func TestFormatName(t *testing.T) {
	if got := FormatName(FourCC('Y', 'U', 'Y', 'V')); got != "YUYV" {
		t.Fatalf("got %q", got)
	}
}

func sample(t *testing.T) Image {
	t.Helper()
	src := []byte{0x30, 0x20, 0x10, 0x00, 0x00, 0x00, 0xff, 0x00, 0x00, 0xff, 0x00, 0x00, 0xff, 0x00, 0x00, 0x00}
	pix, err := ToRGBA(FourCC('X', 'R', '2', '4'), src, 8, 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	return Image{W: 2, H: 2, Pix: pix}
}

func TestPNGDecodes(t *testing.T) {
	img := sample(t)
	var b bytes.Buffer
	if err := WritePNG(&b, img); err != nil {
		t.Fatal(err)
	}
	dec, err := png.Decode(&b)
	if err != nil {
		t.Fatal(err)
	}
	if dec.Bounds().Dx() != 2 || dec.Bounds().Dy() != 2 {
		t.Fatalf("bounds %v", dec.Bounds())
	}
	for i := 0; i < 4; i++ {
		r, g, bl, a := dec.At(i%2, i/2).RGBA()
		want := img.Pix[i*4 : i*4+4]
		if byte(r>>8) != want[0] || byte(g>>8) != want[1] || byte(bl>>8) != want[2] || a != 0xffff {
			t.Fatalf("pixel %d = %x %x %x %x", i, r, g, bl, a)
		}
	}
}

func TestPNGDropsAlpha(t *testing.T) {
	img := Image{W: 1, H: 1, Pix: []byte{9, 8, 7, 0}}
	var b bytes.Buffer
	if err := WritePNG(&b, img); err != nil {
		t.Fatal(err)
	}
	dec, err := png.Decode(&b)
	if err != nil {
		t.Fatal(err)
	}
	r, g, bl, a := dec.At(0, 0).RGBA()
	if byte(r>>8) != 9 || byte(g>>8) != 8 || byte(bl>>8) != 7 || a != 0xffff {
		t.Fatalf("got %x %x %x %x", r, g, bl, a)
	}
	if img.Pix[3] != 0 {
		t.Fatal("input modified")
	}
}

func TestRawHeader(t *testing.T) {
	var b bytes.Buffer
	img := sample(t)
	if err := WriteRaw(&b, img); err != nil {
		t.Fatal(err)
	}
	out := b.Bytes()
	if len(out) != 12+16 {
		t.Fatalf("len %d", len(out))
	}
	for i, want := range []uint32{2, 2, 1} {
		if got := binary.LittleEndian.Uint32(out[i*4:]); got != want {
			t.Fatalf("header %d = %d", i, got)
		}
	}
	if !bytes.Equal(out[12:], img.Pix) {
		t.Fatal("pixels differ")
	}
}

func TestStack(t *testing.T) {
	a := Image{W: 1024, H: 768, Pix: make([]byte, 1024*768*4)}
	b := Image{W: 1024, H: 768, Pix: make([]byte, 1024*768*4)}
	for i := range a.Pix {
		a.Pix[i] = 1
		b.Pix[i] = 2
	}
	s := Stack([]Image{a, b})
	if s.W != 1024 || s.H != 1536 || len(s.Pix) != 1024*1536*4 {
		t.Fatalf("stack %dx%d", s.W, s.H)
	}
	if s.Pix[0] != 1 || s.Pix[768*1024*4] != 2 {
		t.Fatal("order")
	}
}

func TestStackPadsBlack(t *testing.T) {
	a := Image{W: 2, H: 1, Pix: []byte{1, 1, 1, 1, 1, 1, 1, 1}}
	b := Image{W: 1, H: 1, Pix: []byte{2, 2, 2, 2}}
	s := Stack([]Image{a, b})
	want := []byte{1, 1, 1, 1, 1, 1, 1, 1, 2, 2, 2, 2, 0, 0, 0, 0xff}
	if s.W != 2 || s.H != 2 {
		t.Fatalf("stack %dx%d", s.W, s.H)
	}
	if !bytes.Equal(s.Pix[:12], want[:12]) || !bytes.Equal(s.Pix[12:], []byte{0, 0, 0, 0xff}) {
		t.Fatalf("pix %x", s.Pix)
	}
}

func TestStackSingle(t *testing.T) {
	a := sample(t)
	s := Stack([]Image{a})
	if !bytes.Equal(s.Pix, a.Pix) || s.W != 2 || s.H != 2 {
		t.Fatal("single differs")
	}
}
