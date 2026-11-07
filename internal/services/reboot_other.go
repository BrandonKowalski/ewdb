//go:build !linux || testlistener

package services

import "io"

func restart2(s io.Closer, arg string) string {
	return rebootUnavailable(arg)
}
