package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ewdb/internal/capture"
)

type fakeSource struct {
	conns  []connector
	grabs  []string
	failOn string
	closed bool
}

func (f *fakeSource) Connectors() ([]connector, error) { return f.conns, nil }

func (f *fakeSource) Grab(name, label string) (capture.Image, error) {
	f.grabs = append(f.grabs, name+"/"+label)
	if name == f.failOn {
		return capture.Image{}, errors.New("cannot read the " + label + " framebuffer, run as root")
	}
	v := byte(len(f.grabs))
	return capture.Image{W: 2, H: 1, Pix: []byte{v, v, v, 0xff, v, v, v, 0xff}}, nil
}

func (f *fakeSource) Close() { f.closed = true }

func fakeBackend(src *fakeSource) backend {
	return backend{
		openDRM: func() (source, error) {
			if src == nil {
				return nil, errNoDRM
			}
			return src, nil
		},
		grabFB: func() (capture.Image, error) {
			return capture.Image{W: 1, H: 1, Pix: []byte{9, 9, 9, 0xff}}, nil
		},
	}
}

func exec(t *testing.T, be backend, args ...string) (int, []byte, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := run(args, &out, &errb, be)
	return code, out.Bytes(), errb.String()
}

func dims(t *testing.T, raw []byte) (uint32, uint32) {
	t.Helper()
	if len(raw) < 12 || binary.LittleEndian.Uint32(raw[8:]) != 1 {
		t.Fatalf("bad header % x", raw[:min(12, len(raw))])
	}
	return binary.LittleEndian.Uint32(raw), binary.LittleEndian.Uint32(raw[4:])
}

func TestDefaultStacksLitConnectorsInOrder(t *testing.T) {
	src := &fakeSource{conns: []connector{{"DSI-1", true}, {"HDMI-A-1", false}, {"DSI-2", true}}}
	code, out, errs := exec(t, fakeBackend(src))
	if code != 0 || errs != "" {
		t.Fatalf("code %d err %q", code, errs)
	}
	if w, h := dims(t, out); w != 2 || h != 2 {
		t.Fatalf("%dx%d", w, h)
	}
	if strings.Join(src.grabs, ",") != "DSI-1/DSI-1,DSI-2/DSI-2" || !src.closed {
		t.Fatalf("grabs %v closed %v", src.grabs, src.closed)
	}
}

func TestScreensMapNamesAndOrder(t *testing.T) {
	src := &fakeSource{conns: []connector{{"DSI-1", true}, {"DSI-2", true}}}
	code, _, _ := exec(t, fakeBackend(src), "--screens", "top=DSI-2,bottom=DSI-1")
	if code != 0 || strings.Join(src.grabs, ",") != "DSI-2/top,DSI-1/bottom" {
		t.Fatalf("code %d grabs %v", code, src.grabs)
	}
}

func TestOffScreenIsReportedAndSkipped(t *testing.T) {
	src := &fakeSource{conns: []connector{{"DSI-1", true}, {"DSI-2", false}}}
	code, out, errs := exec(t, fakeBackend(src), "--screens", "top=DSI-2,bottom=DSI-1")
	if code != 0 || errs != "screencap: the top screen is off\n" {
		t.Fatalf("code %d err %q", code, errs)
	}
	if w, h := dims(t, out); w != 2 || h != 1 {
		t.Fatalf("%dx%d", w, h)
	}
}

func TestAllOffFallsBackToFramebuffer(t *testing.T) {
	src := &fakeSource{conns: []connector{{"DSI-1", false}}}
	code, out, errs := exec(t, fakeBackend(src), "--screens", "top=DSI-1")
	if code != 0 || errs != "screencap: the top screen is off\n" {
		t.Fatalf("code %d err %q", code, errs)
	}
	if w, h := dims(t, out); w != 1 || h != 1 {
		t.Fatalf("%dx%d", w, h)
	}
}

func TestNoCardFallsBackToFramebuffer(t *testing.T) {
	code, out, _ := exec(t, fakeBackend(nil))
	if code != 0 {
		t.Fatalf("code %d", code)
	}
	if w, h := dims(t, out); w != 1 || h != 1 {
		t.Fatalf("%dx%d", w, h)
	}
}

func TestSelectByNameAndIndex(t *testing.T) {
	for _, d := range []string{"bottom", "1"} {
		src := &fakeSource{conns: []connector{{"DSI-1", true}, {"DSI-2", true}}}
		code, _, _ := exec(t, fakeBackend(src), "-d", d, "--screens", "top=DSI-2,bottom=DSI-1")
		if code != 0 || strings.Join(src.grabs, ",") != "DSI-1/bottom" {
			t.Fatalf("-d %s: code %d grabs %v", d, code, src.grabs)
		}
	}
	src := &fakeSource{conns: []connector{{"DSI-1", true}, {"HDMI-A-1", false}, {"DSI-2", true}}}
	code, _, _ := exec(t, fakeBackend(src), "-d", "1")
	if code != 0 || strings.Join(src.grabs, ",") != "DSI-2/DSI-2" {
		t.Fatalf("index into lit list: code %d grabs %v", code, src.grabs)
	}
	src = &fakeSource{conns: []connector{{"DSI-1", true}, {"DSI-2", true}}}
	code, _, _ = exec(t, fakeBackend(src), "-d", "DSI-2")
	if code != 0 || strings.Join(src.grabs, ",") != "DSI-2/DSI-2" {
		t.Fatalf("by connector name: code %d grabs %v", code, src.grabs)
	}
}

func TestUnknownScreenIsUsageError(t *testing.T) {
	src := &fakeSource{conns: []connector{{"DSI-1", true}}}
	code, _, errs := exec(t, fakeBackend(src), "-d", "7")
	if code != 2 || !strings.Contains(errs, "usage:") {
		t.Fatalf("code %d err %q", code, errs)
	}
}

func TestFailedGrabExitsOne(t *testing.T) {
	src := &fakeSource{conns: []connector{{"DSI-1", true}}, failOn: "DSI-1"}
	code, out, errs := exec(t, fakeBackend(src))
	if code != 1 || len(out) != 0 || !strings.Contains(errs, "run as root") {
		t.Fatalf("code %d out %d err %q", code, len(out), errs)
	}
}

func TestPNGFlagAndFileExtension(t *testing.T) {
	src := &fakeSource{conns: []connector{{"DSI-1", true}}}
	code, out, _ := exec(t, fakeBackend(src), "-p")
	if code != 0 {
		t.Fatalf("code %d", code)
	}
	img, err := png.Decode(bytes.NewReader(out))
	if err != nil || img.Bounds().Dx() != 2 {
		t.Fatalf("decode: %v", err)
	}
	path := filepath.Join(t.TempDir(), "shot.PNG")
	src = &fakeSource{conns: []connector{{"DSI-1", true}}}
	code, out, _ = exec(t, fakeBackend(src), path)
	if code != 0 || len(out) != 0 {
		t.Fatalf("code %d stdout %d", code, len(out))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := png.Decode(bytes.NewReader(data)); err != nil {
		t.Fatal(err)
	}
}

func TestRawFile(t *testing.T) {
	src := &fakeSource{conns: []connector{{"DSI-1", true}}}
	path := filepath.Join(t.TempDir(), "shot.raw")
	if code, _, _ := exec(t, fakeBackend(src), path); code != 0 {
		t.Fatalf("code %d", code)
	}
	data, _ := os.ReadFile(path)
	if w, h := dims(t, data); w != 2 || h != 1 || len(data) != 12+8 {
		t.Fatalf("%dx%d len %d", w, h, len(data))
	}
}

func TestBadArguments(t *testing.T) {
	be := fakeBackend(nil)
	for _, args := range [][]string{{"a", "b"}, {"-x"}, {"--screens", "nonsense"}} {
		if code, _, _ := exec(t, be, args...); code != 2 {
			t.Fatalf("%v: code %d", args, code)
		}
	}
}
