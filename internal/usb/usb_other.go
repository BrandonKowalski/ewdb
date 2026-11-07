//go:build !linux

package usb

import "errors"

func openTarget(t target) (Endpoint, error) {
	return nil, errors.New("usb: " + t.kind + " transport needs Linux")
}
