//go:build linux && !testlistener

package services

import (
	"io"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

func restart2(s io.Closer, arg string) string {
	p, err := syscall.BytePtrFromString(arg)
	if err != nil {
		return rebootUnavailable(arg)
	}
	s.Close()
	syscall.Syscall6(syscall.SYS_REBOOT, unix.LINUX_REBOOT_MAGIC1, unix.LINUX_REBOOT_MAGIC2, unix.LINUX_REBOOT_CMD_RESTART2, uintptr(unsafe.Pointer(p)), 0, 0)
	return ""
}
