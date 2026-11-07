package main

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"ewdb/internal/capture"
	"ewdb/internal/drm"
	"ewdb/internal/fbdev"
)

type drmSource struct {
	dev *drm.Device
	fbs map[string]uint32
}

func newBackend() backend {
	return backend{openDRM: openDRM, grabFB: func() (capture.Image, error) { return fbdev.Grab("/dev/fb0") }}
}

func openDRM() (source, error) {
	cards, _ := filepath.Glob("/dev/dri/card*")
	sort.Slice(cards, func(i, j int) bool {
		a, aerr := strconv.Atoi(strings.TrimPrefix(filepath.Base(cards[i]), "card"))
		b, berr := strconv.Atoi(strings.TrimPrefix(filepath.Base(cards[j]), "card"))
		if aerr != nil || berr != nil {
			return cards[i] < cards[j]
		}
		return a < b
	})
	var last error = errNoDRM
	for _, c := range cards {
		dev, err := drm.Open(c)
		if err == nil {
			return &drmSource{dev: dev, fbs: map[string]uint32{}}, nil
		}
		last = err
	}
	return nil, last
}

func (s *drmSource) Connectors() ([]connector, error) {
	cs, err := s.dev.Connectors()
	if err != nil {
		return nil, err
	}
	var out []connector
	for _, c := range cs {
		s.fbs[c.Name] = c.FB
		out = append(out, connector{Name: c.Name, Lit: c.FB != 0})
	}
	return out, nil
}

func (s *drmSource) Grab(name, label string) (capture.Image, error) {
	var img capture.Image
	err := s.dev.Read(s.fbs[name], label, func(f drm.Frame) error {
		pix, err := capture.ToRGBA(f.Format, f.Pix, f.Pitch, f.Width, f.Height)
		if err != nil {
			return fmt.Errorf("the %s framebuffer has %v", label, err)
		}
		img = capture.Image{W: f.Width, H: f.Height, Pix: pix}
		return nil
	})
	return img, err
}

func (s *drmSource) Close() { s.dev.Close() }
