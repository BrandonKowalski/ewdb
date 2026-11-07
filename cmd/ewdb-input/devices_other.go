//go:build !linux

package main

import "errors"

type systemDevices struct{}

func (systemDevices) Open(name string) (Target, error) {
	return nil, errors.New("not supported on this OS")
}

func openKeyboard() (Target, func(), error) {
	return nil, nil, errors.New("not supported on this OS")
}
