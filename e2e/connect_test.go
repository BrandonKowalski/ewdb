//go:build e2e

package e2e

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func deviceLine(t *testing.T, serial string) string {
	t.Helper()
	out, _, code := adb(t, "", "devices", "-l")
	if code != 0 {
		t.Fatalf("adb devices -l exit %d", code)
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, serial) {
			return line
		}
	}
	t.Fatalf("no line for %s in %q", serial, out)
	return ""
}

func TestDevicesShowsModel(t *testing.T) {
	serial := startDaemon(t, "[device]\nname = dsos\nmodel = RG DS Plus\ndevice = rgds-plus\n")
	line := deviceLine(t, serial)
	if !strings.Contains(line, "product:dsos model:RG_DS_Plus device:rgds-plus") {
		t.Fatalf("unexpected line %q", line)
	}
}

func TestGetState(t *testing.T) {
	serial := startDaemon(t, "")
	out, _, code := adb(t, serial, "get-state")
	if code != 0 || strings.TrimSpace(out) != "device" {
		t.Fatalf("get-state = %q code %d", out, code)
	}
}

func TestListenRefusesNonLoopback(t *testing.T) {
	requireAdb(t)
	cmd := exec.Command(ewdbdBin, "-c", "/nonexistent", "-listen", "0.0.0.0:5555")
	var se bytes.Buffer
	cmd.Stderr = &se
	done := make(chan error, 1)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		ee, ok := err.(*exec.ExitError)
		if !ok || ee.ExitCode() != 2 {
			t.Fatalf("exit = %v", err)
		}
	case <-time.After(5 * time.Second):
		cmd.Process.Kill()
		t.Fatal("ewdbd did not exit")
	}
	if !strings.Contains(se.String(), "ewdbd: -listen only accepts loopback addresses") {
		t.Fatalf("stderr = %q", se.String())
	}
}

func TestBadConfigStillServes(t *testing.T) {
	serial := startDaemon(t, "garbage")
	line := deviceLine(t, serial)
	if !strings.Contains(line, "model:Linux_handheld") {
		t.Fatalf("unexpected line %q", line)
	}
}
