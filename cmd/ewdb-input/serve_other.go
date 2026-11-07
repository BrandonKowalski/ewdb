//go:build !linux

package main

import (
	"errors"

	"ewdb/internal/config"
)

func openDevices(c *config.Config) (runner, func(), error) {
	return nil, nil, errors.New("not supported on this OS")
}
