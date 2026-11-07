package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestParseSectionsAndDefaults(t *testing.T) {
	c, err := Parse(strings.NewReader("[device]\nmodel = RG DS Plus\n[commands]\nlogcat = logread -f\n"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Model != "RG DS Plus" || c.Name != "linux" || c.Device != "handheld" {
		t.Fatalf("got %q %q %q", c.Model, c.Name, c.Device)
	}
	if c.Commands["logcat"] != "logread -f" {
		t.Fatalf("logcat = %q", c.Commands["logcat"])
	}
	if c.Shell != "/bin/sh" || c.State != "/var/lib/ewdb" || c.USB != "auto" {
		t.Fatalf("defaults %q %q %q", c.Shell, c.State, c.USB)
	}
}

func TestParseRejectsBadLine(t *testing.T) {
	_, err := Parse(strings.NewReader("[device]\nnot a pair\n"))
	if err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("err = %v", err)
	}
}

func TestParseCommentsDuplicatesAndSections(t *testing.T) {
	in := "# c\n; d\n\n[props]\na = 1\na = 2\n[commands]\nx = one\nx = two\n[touch]\nname = ft5\n"
	c, err := Parse(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Props) != 2 || c.Props[1] != (KV{"a", "2"}) {
		t.Fatalf("props = %v", c.Props)
	}
	if v, ok := c.Command("x"); !ok || v != "two" {
		t.Fatalf("x = %q %v", v, ok)
	}
	if s := c.Section("touch"); len(s) != 1 || s[0] != (KV{"name", "ft5"}) {
		t.Fatalf("touch = %v", s)
	}
}

func TestModelCommandWins(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.conf")
	writeFile(t, p, "[device]\nmodel = Fixed\nmodel_command = echo From Command\n")
	c := Load(p)
	if c.Model != "From Command" {
		t.Fatalf("model = %q", c.Model)
	}
}

func TestModelCommandFailureFallsBack(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.conf")
	writeFile(t, p, "[device]\nmodel = Fixed\nmodel_command = exit 3\n")
	c := Load(p)
	if c.Model != "Fixed" || len(c.Warnings) != 1 || !strings.Contains(c.Warnings[0], "model_command") {
		t.Fatalf("model = %q warnings = %v", c.Model, c.Warnings)
	}
}

func TestModelCommandTimeout(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.conf")
	writeFile(t, p, "[device]\nmodel = Fixed\nmodel_command = sleep 10\n")
	start := time.Now()
	c := Load(p)
	if time.Since(start) > 3*time.Second {
		t.Fatalf("took %v", time.Since(start))
	}
	if c.Model != "Fixed" {
		t.Fatalf("model = %q", c.Model)
	}
}

func TestModelCommandGrandchildHoldingPipe(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.conf")
	writeFile(t, p, "[device]\nmodel = Fixed\nmodel_command = (sleep 10 &) ; sleep 10\n")
	start := time.Now()
	c := Load(p)
	if time.Since(start) > 3*time.Second {
		t.Fatalf("took %v", time.Since(start))
	}
	if c.Model != "Fixed" {
		t.Fatalf("model = %q", c.Model)
	}
}

func TestCardOverrideReplacesBase(t *testing.T) {
	dir := t.TempDir()
	state := filepath.Join(dir, "state")
	if err := os.Mkdir(state, 0o755); err != nil {
		t.Fatal(err)
	}
	override := filepath.Join(state, "ewdbd.conf")
	writeFile(t, override, "[device]\nmodel = Card\n")
	base := filepath.Join(dir, "base.conf")
	writeFile(t, base, "[device]\nmodel = Base\nstate = "+state+"\n")
	c := Load(base)
	if c.Model != "Card" || c.Path != override {
		t.Fatalf("model = %q path = %q", c.Model, c.Path)
	}
}

func TestMalformedOverrideFallsBackToBase(t *testing.T) {
	dir := t.TempDir()
	state := filepath.Join(dir, "state")
	if err := os.Mkdir(state, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(state, "ewdbd.conf"), "garbage")
	base := filepath.Join(dir, "base.conf")
	writeFile(t, base, "[device]\nmodel = Base\nstate = "+state+"\n")
	c := Load(base)
	if c.Model != "Base" || len(c.Warnings) != 1 || c.Path != base {
		t.Fatalf("model = %q warnings = %v path = %q", c.Model, c.Warnings, c.Path)
	}
}

func TestMalformedBaseFallsBackToDefaults(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.conf")
	writeFile(t, p, "[device]\nmodel = Base\nbroken\n")
	c := Load(p)
	if c.Model != "Linux handheld" || len(c.Warnings) != 1 {
		t.Fatalf("model = %q warnings = %v", c.Model, c.Warnings)
	}
}

func TestMissingFileGivesDefaults(t *testing.T) {
	c := Load("/nonexistent")
	if c == nil || c.Name != "linux" || len(c.Warnings) != 1 {
		t.Fatalf("c = %+v", c)
	}
}
