//go:build e2e

package e2e

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func sha(t *testing.T, p string) [32]byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return sha256.Sum256(b)
}

func TestPushPullRoundTrip(t *testing.T) {
	serial := startDaemon(t, "")
	dir := t.TempDir()
	src := filepath.Join(dir, "src.bin")
	data := make([]byte, 5<<20)
	rand.Read(data)
	if err := os.WriteFile(src, data, 0o644); err != nil {
		t.Fatal(err)
	}
	remote := filepath.Join(dir, "device", "deep", "blob.bin")
	if _, se, code := adb(t, serial, "push", src, remote); code != 0 {
		t.Fatalf("push: %d %s", code, se)
	}
	if sha(t, remote) != sha(t, src) {
		t.Fatal("device copy differs")
	}
	back := filepath.Join(dir, "back.bin")
	if _, se, code := adb(t, serial, "pull", remote, back); code != 0 {
		t.Fatalf("pull: %d %s", code, se)
	}
	if sha(t, back) != sha(t, src) {
		t.Fatal("pulled copy differs")
	}
}

func TestPushPullOddNames(t *testing.T) {
	serial := startDaemon(t, "")
	dir := t.TempDir()
	name := "a b ü.txt"
	src := filepath.Join(dir, name)
	os.WriteFile(src, []byte("odd"), 0o644)
	remote := filepath.Join(dir, "device", name)
	if _, se, code := adb(t, serial, "push", src, remote); code != 0 {
		t.Fatalf("push: %d %s", code, se)
	}
	back := filepath.Join(dir, "back")
	if _, se, code := adb(t, serial, "pull", remote, back); code != 0 {
		t.Fatalf("pull: %d %s", code, se)
	}
	if b, _ := os.ReadFile(back); string(b) != "odd" {
		t.Fatalf("got %q", b)
	}
}

func TestPushDirectory(t *testing.T) {
	serial := startDaemon(t, "")
	dir := t.TempDir()
	tree := filepath.Join(dir, "tree")
	os.MkdirAll(filepath.Join(tree, "x", "y"), 0o755)
	os.WriteFile(filepath.Join(tree, "top.txt"), []byte("1"), 0o644)
	os.WriteFile(filepath.Join(tree, "x", "mid.txt"), []byte("2"), 0o644)
	os.WriteFile(filepath.Join(tree, "x", "y", "leaf.txt"), []byte("3"), 0o644)
	remote := filepath.Join(dir, "device")
	os.MkdirAll(remote, 0o755)
	if _, se, code := adb(t, serial, "push", tree, remote); code != 0 {
		t.Fatalf("push: %d %s", code, se)
	}
	out, _, code := adb(t, serial, "shell", "find", filepath.Join(remote, "tree"), "-type", "f")
	if code != 0 {
		t.Fatalf("find: %d", code)
	}
	var got []string
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		got = append(got, strings.TrimPrefix(l, remote+"/tree/"))
	}
	sort.Strings(got)
	want := []string{"top.txt", "x/mid.txt", "x/y/leaf.txt"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestPushSyncSkips(t *testing.T) {
	serial := startDaemon(t, "")
	dir := t.TempDir()
	src := filepath.Join(dir, "f.txt")
	os.WriteFile(src, []byte("same"), 0o644)
	remote := filepath.Join(dir, "device", "f.txt")
	if _, se, code := adb(t, serial, "push", "--sync", src, remote); code != 0 {
		t.Fatalf("first push: %d %s", code, se)
	}
	out, se, code := adb(t, serial, "push", "--sync", src, remote)
	if code != 0 || !strings.Contains(out+se, "0 files pushed, 1 skipped") {
		t.Fatalf("second push: %d %q %q", code, out, se)
	}
}

func TestPullPreservesMode(t *testing.T) {
	serial := startDaemon(t, "")
	dir := t.TempDir()
	remote := filepath.Join(dir, "tool.sh")
	os.WriteFile(remote, []byte("#!/bin/sh\n"), 0o755)
	os.Chmod(remote, 0o755)
	back := filepath.Join(dir, "back.sh")
	if _, se, code := adb(t, serial, "pull", "-a", remote, back); code != 0 {
		t.Fatalf("pull: %d %s", code, se)
	}
	fi, err := os.Stat(back)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o755 {
		t.Fatalf("mode %v", fi.Mode().Perm())
	}
}

func TestPullMissing(t *testing.T) {
	serial := startDaemon(t, "")
	dst := filepath.Join(t.TempDir(), "x")
	_, se, code := adb(t, serial, "pull", "/nonexistent", dst)
	if code != 1 || !strings.Contains(se, "No such file") {
		t.Fatalf("code %d stderr %q", code, se)
	}
}

func TestLsV2(t *testing.T) {
	serial := startDaemon(t, "")
	if out, _, code := adb(t, serial, "shell", "echo", "ok"); code != 0 || !bytes.Equal([]byte(out), []byte("ok\n")) {
		t.Fatalf("shell: %d %q", code, out)
	}
	out, se, code := adb(t, serial, "ls", "/")
	if code != 0 {
		t.Fatalf("ls: %d %s", code, se)
	}
	found := false
	for _, l := range strings.Split(out, "\n") {
		f := strings.Fields(l)
		if len(f) > 0 && f[len(f)-1] == "etc" {
			found = true
		}
	}
	if !found {
		t.Fatalf("etc not listed: %q", out)
	}
}
