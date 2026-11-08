//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRebootRunsMapping(t *testing.T) {
	mark := filepath.Join(t.TempDir(), "mark")
	serial := startDaemon(t, "[commands]\nreboot.bootloader = touch "+mark+"\n")
	adb(t, serial, "reboot", "bootloader")
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(mark); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("mapping did not run")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestTcpipKeepsSession(t *testing.T) {
	serial := startDaemon(t, "")
	out, se, _ := adb(t, serial, "tcpip", "5555")
	if !strings.Contains(out+se, "Wi-Fi adb is not available on this device") {
		t.Fatalf("stdout %q stderr %q", out, se)
	}
	if _, _, code := adb(t, serial, "shell", "true"); code != 0 {
		t.Fatalf("shell true: %d", code)
	}
}

func TestUsbMessage(t *testing.T) {
	serial := startDaemon(t, "")
	out, se, _ := adb(t, serial, "usb")
	if !strings.Contains(out+se, "already running on USB") {
		t.Fatalf("stdout %q stderr %q", out, se)
	}
}

func TestRemountMessage(t *testing.T) {
	serial := startDaemon(t, "")
	out, se, _ := adb(t, serial, "remount")
	if !strings.Contains(out+se, "not available") {
		t.Fatalf("stdout %q stderr %q", out, se)
	}
}

func TestVerityMessage(t *testing.T) {
	serial := startDaemon(t, "")
	out, se, _ := adb(t, serial, "disable-verity")
	if !strings.Contains(out+se, "verity is not available on this device") {
		t.Fatalf("stdout %q stderr %q", out, se)
	}
}

func TestJdwpReturnsQuickly(t *testing.T) {
	serial := startDaemon(t, "")
	start := time.Now()
	adb(t, serial, "jdwp")
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("jdwp took %v", d)
	}
}

func TestReconnectRecovers(t *testing.T) {
	serial := startDaemon(t, "")
	start := time.Now()
	adb(t, serial, "reconnect")
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("reconnect took %v", d)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		adb(t, "", "connect", serial)
		if _, _, code := adb(t, serial, "shell", "true"); code == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("shell did not work after reconnect")
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func TestSubcommandMapping(t *testing.T) {
	serial := startDaemon(t, "[device]\nmodel = Multi Model\n[commands]\ngetprop = "+ewdbdBin+" getprop\n")
	out, se, code := adb(t, serial, "shell", "getprop", "ro.product.model")
	if code != 0 || out != "Multi Model\n" {
		t.Fatalf("getprop: %d stdout %q stderr %q", code, out, se)
	}
}
