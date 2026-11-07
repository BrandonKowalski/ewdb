package uinput

import (
	"bytes"
	"encoding/binary"
	"strconv"
	"testing"
)

func TestEncodeEvent8(t *testing.T) {
	b := EncodeEvent(8, EV_ABS, ABS_MT_POSITION_X, -2)
	want := []byte{0, 0, 0, 0, 0, 0, 0, 0, 3, 0, 0x35, 0, 0xfe, 0xff, 0xff, 0xff}
	if !bytes.Equal(b, want) {
		t.Fatalf("got %v", b)
	}
}

func TestEncodeEvent16(t *testing.T) {
	b := EncodeEvent(16, EV_KEY, BTN_TOUCH, 1)
	if len(b) != 24 {
		t.Fatalf("len %d", len(b))
	}
	if !bytes.Equal(b[:16], make([]byte, 16)) {
		t.Fatal("time not zero")
	}
	if binary.LittleEndian.Uint16(b[16:]) != EV_KEY || binary.LittleEndian.Uint16(b[18:]) != BTN_TOUCH || binary.LittleEndian.Uint32(b[20:]) != 1 {
		t.Fatalf("got %v", b)
	}
}

func TestTimevalSize(t *testing.T) {
	if TimevalSize != 2*strconv.IntSize/8 {
		t.Fatalf("%d", TimevalSize)
	}
	if len(EncodeEvent(TimevalSize, 0, 0, 0)) != TimevalSize+8 {
		t.Fatal("size")
	}
}

func TestUserDevLayout(t *testing.T) {
	abs := map[uint16]AbsInfo{
		ABS_X:             {Min: -5, Max: 1023, Fuzz: 2, Flat: 3},
		ABS_MT_POSITION_X: {Min: 0, Max: 1023},
		0x80:              {Max: 1},
	}
	b := EncodeUserDev("Goodix", ID{Bus: 0x18, Vendor: 0x1, Product: 0x2, Version: 0x3}, abs)
	if len(b) != 1116 {
		t.Fatalf("size %d", len(b))
	}
	if string(b[:6]) != "Goodix" || b[6] != 0 {
		t.Fatal("name")
	}
	le := binary.LittleEndian
	if le.Uint16(b[80:]) != 0x18 || le.Uint16(b[82:]) != 1 || le.Uint16(b[84:]) != 2 || le.Uint16(b[86:]) != 3 {
		t.Fatal("id")
	}
	if le.Uint32(b[88:]) != 0 {
		t.Fatal("ff")
	}
	absmax := 92
	absmin := absmax + 64*4
	absfuzz := absmin + 64*4
	absflat := absfuzz + 64*4
	if int32(le.Uint32(b[absmax+4*ABS_X:])) != 1023 || int32(le.Uint32(b[absmin+4*ABS_X:])) != -5 {
		t.Fatal("abs x")
	}
	if le.Uint32(b[absfuzz+4*ABS_X:]) != 2 || le.Uint32(b[absflat+4*ABS_X:]) != 3 {
		t.Fatal("fuzz flat")
	}
	if le.Uint32(b[absmax+4*ABS_MT_POSITION_X:]) != 1023 {
		t.Fatal("mt x")
	}
	if absflat+64*4 != 1116 {
		t.Fatal("layout")
	}
}

func TestUserDevNameTruncated(t *testing.T) {
	long := string(bytes.Repeat([]byte("n"), 200))
	b := EncodeUserDev(long, ID{}, nil)
	if b[79] != 0 || b[78] != 'n' {
		t.Fatal("name not terminated")
	}
}

func TestSetupLayouts(t *testing.T) {
	s := EncodeSetup("pad", ID{Bus: 3, Vendor: 4, Product: 5, Version: 6})
	if len(s) != 92 || string(s[8:11]) != "pad" || binary.LittleEndian.Uint16(s[0:]) != 3 || binary.LittleEndian.Uint16(s[6:]) != 6 {
		t.Fatalf("setup %v", s)
	}
}
