//go:build e2e

package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestAdbBugreport(t *testing.T) {
	requireAdb(t)
	bin := filepath.Join(t.TempDir(), "ewdb-bugreportz")
	build := exec.Command("go", "build", "-o", bin, "ewdb/cmd/ewdb-bugreportz")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	remote := t.TempDir()
	local := t.TempDir()
	serial := startDaemon(t, "[device]\ndevice = rgds\n[commands]\nbugreportz = "+bin+"\n[bugreport]\ndir = "+remote+"\nuname = echo hello\n")
	out, se, code := adb(t, serial, "bugreport", local)
	if code != 0 {
		t.Fatalf("adb bugreport: %d stdout %q stderr %q", code, out, se)
	}
	zips, _ := filepath.Glob(filepath.Join(local, "*.zip"))
	if len(zips) != 1 || !strings.Contains(filepath.Base(zips[0]), "bugreport-rgds-") {
		t.Fatalf("local zips %v (stdout %q stderr %q)", zips, out, se)
	}
}
