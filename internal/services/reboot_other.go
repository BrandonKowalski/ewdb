//go:build !linux || testlistener

package services

func restart2(arg string) string {
	return rebootUnavailable(arg)
}
