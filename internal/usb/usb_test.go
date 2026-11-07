package usb

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func le16(v uint16) []byte { return binary.LittleEndian.AppendUint16(nil, v) }

func le32(v uint32) []byte { return binary.LittleEndian.AppendUint32(nil, v) }

func cat(parts ...[]byte) []byte { return bytes.Join(parts, nil) }

func intfBytes() []byte {
	return []byte{9, 0x04, 0, 0, 2, 0xff, 0x42, 1, 1}
}

func epBytes(addr byte, maxPacket uint16) []byte {
	return cat([]byte{7, 0x05, addr, 0x02}, le16(maxPacket), []byte{0})
}

func ssCompBytes() []byte {
	return cat([]byte{6, 0x30, 4, 0}, le16(0))
}

func speedBytes(maxPacket uint16) []byte {
	return cat(intfBytes(), epBytes(0x01, maxPacket), epBytes(0x82, maxPacket))
}

func TestDescriptorsV2Bytes(t *testing.T) {
	ss := cat(intfBytes(), epBytes(0x01, 1024), ssCompBytes(), epBytes(0x82, 1024), ssCompBytes())
	body := cat(le32(7), le32(3), le32(3), le32(5), speedBytes(64), speedBytes(512), ss)
	want := cat(le32(3), le32(uint32(8+len(body))), body)
	got := DescriptorsV2()
	if !bytes.Equal(got, want) {
		t.Fatalf("DescriptorsV2\n got %x\nwant %x", got, want)
	}
	if len(got) != 105 {
		t.Fatalf("DescriptorsV2 length %d, want 105", len(got))
	}
}

func TestDescriptorsV1Bytes(t *testing.T) {
	body := cat(le32(3), le32(3), speedBytes(64), speedBytes(512))
	want := cat(le32(1), le32(uint32(8+len(body))), body)
	got := DescriptorsV1()
	if !bytes.Equal(got, want) {
		t.Fatalf("DescriptorsV1\n got %x\nwant %x", got, want)
	}
	if len(got) != 62 {
		t.Fatalf("DescriptorsV1 length %d, want 62", len(got))
	}
}

func TestStringsBytes(t *testing.T) {
	body := cat(le32(1), le32(1), le16(0x0409), []byte("ADB Interface\x00"))
	want := cat(le32(2), le32(uint32(8+len(body))), body)
	got := Strings()
	if !bytes.Equal(got, want) {
		t.Fatalf("Strings\n got %x\nwant %x", got, want)
	}
	if len(got) != 32 {
		t.Fatalf("Strings length %d, want 32", len(got))
	}
}

func TestParseSpec(t *testing.T) {
	cases := []struct {
		spec string
		want []target
		ok   bool
	}{
		{"auto", []target{{kind: "ffs", path: "/dev/usb-ffs/adb"}, {kind: "android", path: "/dev/android_adb"}}, true},
		{"", []target{{kind: "ffs", path: "/dev/usb-ffs/adb"}, {kind: "android", path: "/dev/android_adb"}}, true},
		{"ffs:/dev/usb-ffs/other", []target{{kind: "ffs", path: "/dev/usb-ffs/other"}}, true},
		{"android:/dev/android_adb0", []target{{kind: "android", path: "/dev/android_adb0"}}, true},
		{"ffs:", nil, false},
		{"android:", nil, false},
		{"tcp:5555", nil, false},
		{"usb", nil, false},
	}
	for _, c := range cases {
		got, err := parseSpec(c.spec)
		if (err == nil) != c.ok {
			t.Errorf("parseSpec(%q) err = %v", c.spec, err)
			continue
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("parseSpec(%q) = %v, want %v", c.spec, got, c.want)
		}
	}
}

func writeFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFindUDCMatchesGadgetByFFSInstance(t *testing.T) {
	root := t.TempDir()
	cfs := filepath.Join(root, "config")
	mounts := filepath.Join(root, "mounts")
	writeFile(t, mounts, "proc /proc proc rw 0 0\n"+
		"configfs "+cfs+" configfs rw 0 0\n"+
		"adb /dev/usb-ffs/adb functionfs rw 0 0\n")
	writeFile(t, filepath.Join(cfs, "usb_gadget", "other", "UDC"), "other.udc\n")
	writeFile(t, filepath.Join(cfs, "usb_gadget", "other", "functions", "acm.gs0", "x"), "")
	writeFile(t, filepath.Join(cfs, "usb_gadget", "g1", "UDC"), "fcc00000.dwc3\n")
	writeFile(t, filepath.Join(cfs, "usb_gadget", "g1", "functions", "ffs.adb", "x"), "")
	udcDir := filepath.Join(root, "udc")
	writeFile(t, filepath.Join(udcDir, "fcc00000.dwc3", "soft_connect"), "")
	writeFile(t, filepath.Join(udcDir, "other.udc", "soft_connect"), "")
	got := findUDC(mounts, udcDir, "/dev/usb-ffs/adb/")
	if got != "fcc00000.dwc3" {
		t.Fatalf("findUDC = %q", got)
	}
}

func TestFindUDCUnboundGadget(t *testing.T) {
	root := t.TempDir()
	cfs := filepath.Join(root, "config")
	mounts := filepath.Join(root, "mounts")
	writeFile(t, mounts, "configfs "+cfs+" configfs rw 0 0\nadb /dev/usb-ffs/adb functionfs rw 0 0\n")
	writeFile(t, filepath.Join(cfs, "usb_gadget", "g1", "UDC"), "\n")
	writeFile(t, filepath.Join(cfs, "usb_gadget", "g1", "functions", "ffs.adb", "x"), "")
	udcDir := filepath.Join(root, "udc")
	writeFile(t, filepath.Join(udcDir, "fcc00000.dwc3", "soft_connect"), "")
	if got := findUDC(mounts, udcDir, "/dev/usb-ffs/adb"); got != "" {
		t.Fatalf("findUDC = %q, want none", got)
	}
}

func TestFindUDCFallsBackToOnlyUDC(t *testing.T) {
	root := t.TempDir()
	mounts := filepath.Join(root, "mounts")
	writeFile(t, mounts, "adb /dev/usb-ffs/adb functionfs rw 0 0\n")
	udcDir := filepath.Join(root, "udc")
	writeFile(t, filepath.Join(udcDir, "musb-hdrc.0", "soft_connect"), "")
	if got := findUDC(mounts, udcDir, "/dev/usb-ffs/adb"); got != "musb-hdrc.0" {
		t.Fatalf("findUDC = %q", got)
	}
	writeFile(t, filepath.Join(udcDir, "second", "soft_connect"), "")
	if got := findUDC(mounts, udcDir, "/dev/usb-ffs/adb"); got != "" {
		t.Fatalf("findUDC with two UDCs = %q, want none", got)
	}
}
