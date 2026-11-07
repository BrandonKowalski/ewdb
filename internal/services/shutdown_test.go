package services

import (
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
)

const mountsSample = `/dev/root / squashfs ro,relatime 0 0
devtmpfs /dev devtmpfs rw,relatime,size=480000k 0 0
proc /proc proc rw,relatime 0 0
tmpfs /tmp tmpfs rw,relatime 0 0
/dev/mmcblk1p1 /mnt/sdcard vfat rw,relatime,fmask=0022,dmask=0022,codepage=437,errors=remount-ro 0 0
/dev/mmcblk0p5 /userdata ext4 rw,relatime 0 0
/dev/mmcblk0p6 /userdata/games\040dir vfat rw,noatime 0 0
/dev/mmcblk0p7 /boot vfat ro,relatime 0 0
/dev/mmcblk0p8 /mnt/ext3 ext3 rw 0 0
/dev/sda1 /mnt/usb exfat rw,nosuid 0 0
/dev/mmcblk0p9 /mnt/old ext2 rw 0 0
`

func TestWritableMountsPicksRwDiskFilesystemsInnermostFirst(t *testing.T) {
	got := writableMounts(mountsSample)
	want := []string{"/mnt/old", "/mnt/usb", "/mnt/ext3", "/userdata/games dir", "/userdata", "/mnt/sdcard"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestWritableMountsIgnoresMalformedLines(t *testing.T) {
	if got := writableMounts("garbage\n\n/dev/x /a vfat\n"); len(got) != 0 {
		t.Fatalf("got %q", got)
	}
}

func TestUserProcessesSkipsSelfInitKernelThreadsAndZombies(t *testing.T) {
	dir := t.TempDir()
	add := func(pid int, cmdline string) {
		p := filepath.Join(dir, strconv.Itoa(pid))
		os.MkdirAll(p, 0o755)
		os.WriteFile(filepath.Join(p, "cmdline"), []byte(cmdline), 0o644)
	}
	add(1, "/sbin/init\x00")
	add(2, "")
	add(40, "/usr/bin/frontend\x00")
	add(41, "/usr/sbin/ewdbd\x00")
	add(42, "")
	add(77, "retroarch\x00-L\x00")
	os.MkdirAll(filepath.Join(dir, "self"), 0o755)
	os.WriteFile(filepath.Join(dir, "mounts"), nil, 0o644)
	got := userProcesses(dir, 41)
	if want := []int{40, 77}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}
