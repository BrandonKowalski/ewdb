//go:build linux

package input

import (
	"ewdb/internal/uinput"
)

type systemDevices struct{}

func (systemDevices) Open(name string) (Target, error) {
	d, err := uinput.FindEvent(name)
	if err != nil {
		return nil, err
	}
	return d, nil
}

func openKeyboard() (Target, func(), error) {
	d, err := uinput.Keyboard(uinput.KeyboardName, textCodes())
	if err != nil {
		return nil, nil, err
	}
	return d, func() { d.Close() }, nil
}
