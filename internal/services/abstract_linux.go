package services

import "errors"

func abstractAddr(name string) (string, error) {
	if name == "" {
		return "", errors.New("empty abstract socket name")
	}
	return "@" + name, nil
}
