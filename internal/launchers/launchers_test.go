package launchers

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func build(t *testing.T, commands map[string]string, systemPath string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "bin")
	if err := Build(dir, commands, systemPath); err != nil {
		t.Fatal(err)
	}
	return dir
}

func runIn(t *testing.T, path string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	return runWithPath(t, "/nonexistent", path, args...)
}

func runWithPath(t *testing.T, pathEnv, path string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = []string{"PATH=" + pathEnv}
	var so, se bytes.Buffer
	cmd.Stdout, cmd.Stderr = &so, &se
	err := cmd.Run()
	if ctx.Err() != nil {
		t.Fatalf("%s did not finish within 1 s", path)
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return so.String(), se.String(), code
}

func TestLauncherRunsMappedCommand(t *testing.T) {
	dir := build(t, map[string]string{"getprop": "echo mapped"}, os.Getenv("PATH"))
	out, _, code := runIn(t, filepath.Join(dir, "getprop"), "a", "b")
	if code != 0 || out != "mapped a b\n" {
		t.Fatalf("out %q code %d", out, code)
	}
}

func TestLauncherAvoidsSelfRecursion(t *testing.T) {
	dir := build(t, map[string]string{"date": "date"}, "/usr/bin:/bin")
	_, se, code := runWithPath(t, dir+":/usr/bin:/bin", filepath.Join(dir, "date"))
	if code != 0 {
		t.Fatalf("code %d stderr %q", code, se)
	}
}

func TestLauncherQuotesSystemPath(t *testing.T) {
	sys := filepath.Join(t.TempDir(), "it's here")
	if err := os.MkdirAll(sys, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sys, "tool"), []byte("#!/bin/sh\necho tool \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	dir := build(t, map[string]string{"input": "tool x"}, sys+":/usr/bin:/bin")
	out, se, code := runIn(t, filepath.Join(dir, "input"), "y")
	if code != 0 || out != "tool x y\n" {
		t.Fatalf("out %q stderr %q code %d", out, se, code)
	}
}

func TestStubForUnmappedAndroidCommand(t *testing.T) {
	dir := build(t, nil, filepath.Join(t.TempDir(), "empty"))
	out, se, code := runIn(t, filepath.Join(dir, "pm"), "list", "packages")
	if code != 127 || out != "" || se != "pm: not available on this device\n" {
		t.Fatalf("out %q stderr %q code %d", out, se, code)
	}
	for _, name := range Android {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("no stub for %s: %v", name, err)
		}
	}
}

func TestMappedAndroidCommandIsNotStubbed(t *testing.T) {
	dir := build(t, map[string]string{"logcat": "echo log"}, "/bin")
	out, _, code := runIn(t, filepath.Join(dir, "logcat"))
	if code != 0 || out != "log\n" {
		t.Fatalf("out %q code %d", out, code)
	}
}

func TestNoStubWhenSystemHasCommand(t *testing.T) {
	sys := t.TempDir()
	if err := os.WriteFile(filepath.Join(sys, "svc"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sys, "wm"), []byte("not executable"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := build(t, nil, filepath.Join(t.TempDir(), "missing")+":"+sys)
	if _, err := os.Stat(filepath.Join(dir, "svc")); !os.IsNotExist(err) {
		t.Fatalf("svc stub exists: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "wm")); err != nil {
		t.Fatalf("wm stub missing: %v", err)
	}
}

func TestBuildRebuildsFromScratch(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "bin")
	if err := Build(dir, map[string]string{"old": "true"}, ""); err != nil {
		t.Fatal(err)
	}
	if err := Build(dir, map[string]string{"new": "true"}, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "old")); !os.IsNotExist(err) {
		t.Fatalf("old launcher survived: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "new")); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Dir(dir))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("leftovers next to %s: %v", dir, entries)
	}
}

func TestBuildSkipsNamesWithSlash(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "bin")
	if err := Build(dir, map[string]string{"../escape": "true", "..": "true"}, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "escape")); !os.IsNotExist(err) {
		t.Fatalf("launcher escaped the directory: %v", err)
	}
}

func TestBuildRefusesForeignDirectory(t *testing.T) {
	dir := t.TempDir()
	keep := filepath.Join(dir, "keep")
	if err := os.WriteFile(keep, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Build(dir, map[string]string{"getprop": "true"}, ""); err == nil {
		t.Fatal("Build accepted a directory it did not create")
	}
	if b, err := os.ReadFile(keep); err != nil || string(b) != "mine" {
		t.Fatalf("keep = %q, %v", b, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "getprop")); !os.IsNotExist(err) {
		t.Fatalf("launcher written into foreign directory: %v", err)
	}
	entries, err := os.ReadDir(filepath.Dir(dir))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("leftovers next to %s: %v", dir, entries)
	}
}

func TestBuildRefusesEmptyForeignDirectory(t *testing.T) {
	if err := Build(t.TempDir(), nil, ""); err == nil {
		t.Fatal("Build accepted an existing directory without its marker")
	}
}
