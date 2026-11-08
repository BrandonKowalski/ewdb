package getprop

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func setup(t *testing.T, conf string) {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "c.conf")
	conf = strings.ReplaceAll(conf, "DIR", dir)
	if err := os.WriteFile(p, []byte(conf), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("EWDB_CONFIG", p)
}

func run(args ...string) string {
	var b bytes.Buffer
	execute(args, &b)
	return b.String()
}

const base = "[device]\nname = dsos\nmodel = RG DS Plus\ndevice = rgds-plus\n[props]\nz.last = 1\na.first = 2\n"

func TestListsSorted(t *testing.T) {
	setup(t, base)
	lines := strings.Split(strings.TrimSpace(run()), "\n")
	if len(lines) < 3 || lines[0] != "[a.first]: [2]" {
		t.Fatalf("lines = %q", lines)
	}
	for i := 1; i < len(lines); i++ {
		if lines[i-1] >= lines[i] {
			t.Fatalf("not sorted: %q", lines)
		}
	}
	if !strings.Contains(strings.Join(lines, "\n"), "[ro.product.device]: [rgds-plus]") {
		t.Fatalf("lines = %q", lines)
	}
}

func TestSingleKey(t *testing.T) {
	setup(t, base)
	if got := run("ro.product.model"); got != "RG DS Plus\n" {
		t.Fatalf("got %q", got)
	}
	if got := run("nope"); got != "\n" {
		t.Fatalf("got %q", got)
	}
}

func TestDefault(t *testing.T) {
	setup(t, base)
	if got := run("missing", "x"); got != "x\n" {
		t.Fatalf("got %q", got)
	}
	if got := run("ro.product.name", "x"); got != "dsos\n" {
		t.Fatalf("got %q", got)
	}
}

func TestPropsOverrideDerived(t *testing.T) {
	setup(t, base+"ro.product.model = Custom\n")
	if got := run("ro.product.model"); got != "Custom\n" {
		t.Fatalf("got %q", got)
	}
}

func TestSerialAndAbi(t *testing.T) {
	dir := t.TempDir()
	sf := filepath.Join(dir, "serial")
	os.WriteFile(sf, []byte("ABC123\nignored\n"), 0o644)
	setup(t, "[device]\nserial_file = "+sf+"\n")
	if got := run("ro.serialno"); got != "ABC123\n" {
		t.Fatalf("got %q", got)
	}
	want := map[string]string{"arm64": "arm64-v8a", "arm": "armeabi-v7a", "amd64": "x86_64"}[runtime.GOARCH]
	if got := run("ro.product.cpu.abi"); want != "" && got != want+"\n" {
		t.Fatalf("got %q want %q", got, want)
	}
}
