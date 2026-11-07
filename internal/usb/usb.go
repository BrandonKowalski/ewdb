package usb

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"ewdb/internal/session"
)

type Endpoint interface {
	Next(ctx context.Context) (session.Transport, error)
}

var ErrBadSpec = errors.New("usb: bad transport spec")

const (
	defaultFFSDir      = "/dev/usb-ffs/adb"
	defaultAndroidPath = "/dev/android_adb"
)

type target struct {
	kind, path string
}

func parseSpec(spec string) ([]target, error) {
	if spec == "" || spec == "auto" {
		return []target{{kind: "ffs", path: defaultFFSDir}, {kind: "android", path: defaultAndroidPath}}, nil
	}
	kind, path, ok := strings.Cut(spec, ":")
	if !ok || path == "" || (kind != "ffs" && kind != "android") {
		return nil, fmt.Errorf("%w %q: want auto, ffs:DIR or android:PATH", ErrBadSpec, spec)
	}
	return []target{{kind: kind, path: path}}, nil
}

func Open(spec string) (Endpoint, error) {
	targets, err := parseSpec(spec)
	if err != nil {
		return nil, err
	}
	var errs []error
	for _, t := range targets {
		ep, err := openTarget(t)
		if err == nil {
			return ep, nil
		}
		errs = append(errs, err)
	}
	return nil, errors.Join(errs...)
}
