package bugreportz

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func setup(t *testing.T, items string) string {
	t.Helper()
	dir := t.TempDir()
	conf := filepath.Join(dir, "c.conf")
	body := "[device]\ndevice = rgds\n[bugreport]\ndir = " + dir + "\n" + items
	if err := os.WriteFile(conf, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("EWDB_CONFIG", conf)
	return dir
}

func run(args ...string) (string, string, int) {
	var o, e bytes.Buffer
	code := execute(args, &o, &e)
	return o.String(), e.String(), code
}

func readZip(t *testing.T, path string) map[string]string {
	t.Helper()
	r, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	m := map[string]string{}
	for _, f := range r.File {
		rc, _ := f.Open()
		b, _ := io.ReadAll(rc)
		rc.Close()
		m[f.Name] = string(b)
	}
	return m
}

func TestVersion(t *testing.T) {
	setup(t, "")
	out, errOut, code := run("-v")
	if code != 0 || out != "" || errOut != "1.2\n" {
		t.Fatalf("code %d out %q err %q", code, out, errOut)
	}
}

func TestZipHasItems(t *testing.T) {
	dir := setup(t, "alpha = echo one; echo two >&2\nbeta = echo second\n")
	out, _, code := run()
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if code != 0 || len(lines) != 1 || !strings.HasPrefix(lines[0], "OK:"+dir+"/bugreport-rgds-") {
		t.Fatalf("code %d out %q", code, out)
	}
	path := strings.TrimPrefix(lines[0], "OK:")
	if !strings.HasSuffix(path, ".zip") {
		t.Fatalf("path %q", path)
	}
	m := readZip(t, path)
	if m["alpha.txt"] != "one\ntwo\n" || m["beta.txt"] != "second\n" || len(m) != 2 {
		t.Fatalf("zip = %q", m)
	}
}

func TestProgressLines(t *testing.T) {
	dir := setup(t, "a = true\nb = true\nc = true\n")
	out, _, code := run("-p")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if code != 0 || len(lines) != 5 {
		t.Fatalf("code %d lines %q", code, lines)
	}
	zipPath := strings.TrimPrefix(lines[4], "OK:")
	if lines[0] != "BEGIN:"+zipPath || lines[1] != "PROGRESS:1/3" || lines[2] != "PROGRESS:2/3" || lines[3] != "PROGRESS:3/3" || filepath.Dir(zipPath) != dir {
		t.Fatalf("lines %q", lines)
	}
}

func TestItemTimeout(t *testing.T) {
	setup(t, "slow = sleep 30\nfast = echo ok\n")
	t.Setenv("EWDB_BUGREPORT_TIMEOUT", "1s")
	start := time.Now()
	out, _, code := run()
	if time.Since(start) > 5*time.Second {
		t.Fatalf("took %v", time.Since(start))
	}
	if code != 0 {
		t.Fatalf("code %d out %q", code, out)
	}
	m := readZip(t, strings.TrimPrefix(strings.TrimSpace(out), "OK:"))
	if !strings.Contains(m["slow.txt"], "timed out") || m["fast.txt"] != "ok\n" {
		t.Fatalf("zip = %q", m)
	}
}

func TestFailLine(t *testing.T) {
	dir := t.TempDir()
	conf := filepath.Join(dir, "c.conf")
	os.WriteFile(conf, []byte("[bugreport]\ndir = "+dir+"/missing\na = true\n"), 0o644)
	t.Setenv("EWDB_CONFIG", conf)
	out, _, code := run("-p")
	if code == 0 || !strings.HasPrefix(out, "FAIL:") || strings.Contains(out, "OK:") {
		t.Fatalf("code %d out %q", code, out)
	}
}
