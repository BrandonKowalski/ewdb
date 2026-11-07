//go:build !linux

package services

import "errors"

func abstractAddr(name string) (string, error) {
	return "", errors.New("localabstract sockets need the Linux abstract namespace")
}
