//go:build linux && !testlistener

package services

import (
	"os"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

const (
	termGrace = 2 * time.Second
	killGrace = time.Second
	termPoll  = 50 * time.Millisecond
)

func restart2(arg string) string {
	p, err := syscall.BytePtrFromString(arg)
	if err != nil {
		return rebootUnavailable(arg)
	}
	quiesce()
	_, _, errno := syscall.Syscall6(syscall.SYS_REBOOT, unix.LINUX_REBOOT_MAGIC1, unix.LINUX_REBOOT_MAGIC2, unix.LINUX_REBOOT_CMD_RESTART2, uintptr(unsafe.Pointer(p)), 0, 0)
	if errno != 0 {
		return "reboot " + arg + " failed: " + errno.Error() + "\n"
	}
	return ""
}

func quiesce() {
	self := os.Getpid()
	unix.Kill(-1, unix.SIGTERM)
	waitForExit(self, termGrace)
	unix.Kill(-1, unix.SIGKILL)
	waitForExit(self, killGrace)
	unix.Sync()
	if b, err := os.ReadFile("/proc/mounts"); err == nil {
		for _, m := range writableMounts(string(b)) {
			unix.Mount("", m, "", unix.MS_REMOUNT|unix.MS_RDONLY, "")
		}
	}
	unix.Sync()
}

func waitForExit(self int, grace time.Duration) {
	for deadline := time.Now().Add(grace); time.Now().Before(deadline); time.Sleep(termPoll) {
		if len(userProcesses("/proc", self)) == 0 {
			return
		}
	}
}
