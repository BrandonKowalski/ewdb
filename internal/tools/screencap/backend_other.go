//go:build !linux

package screencap

import (
	"errors"

	"ewdb/internal/capture"
)

func newBackend() backend {
	return backend{
		openDRM: func() (source, error) { return nil, errors.New("not supported on this platform") },
		grabFB:  func() (capture.Image, error) { return capture.Image{}, errors.New("framebuffer capture needs Linux") },
	}
}
