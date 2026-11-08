package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const kmsgSample = "6,602,602147767,-;android_work: sent uevent USB_STATE=DISCONNECTED\n" +
	" SUBSYSTEM=platform\n" +
	" DEVICE=+platform:soc\n" +
	"3,603,602646736,-;ERR: Operation not supported\n" +
	"14,604,603000000,-;frontend[12]: started\n"

var testBoot = time.Date(2026, time.November, 8, 0, 0, 0, 0, time.UTC)

func useKmsg(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "kmsg")
	if content != "" {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	oldPath, oldBoot, oldHost := kmsgPath, bootTime, hostName
	kmsgPath = path
	bootTime = func() time.Time { return testBoot }
	hostName = func() string { return "rg35xxsp" }
	t.Cleanup(func() { kmsgPath, bootTime, hostName = oldPath, oldBoot, oldHost })
	return path
}

func TestFormatKmsg(t *testing.T) {
	cases := []struct {
		in, want string
		ok       bool
	}{
		{"6,602,602147767,-;android_work: sent uevent\n", "Nov  8 00:10:02 rg35xxsp kern.info kernel: [  602.147767] android_work: sent uevent\n", true},
		{"3,603,1500,c;ERR: x\n", "Nov  8 00:00:00 rg35xxsp kern.err kernel: [    0.001500] ERR: x\n", true},
		{"14,604,3000000,-;frontend[12]: started\n", "Nov  8 00:00:03 rg35xxsp user.info frontend[12]: started\n", true},
		{"30,9,1,-;svc: up", "Nov  8 00:00:00 rg35xxsp daemon.info svc: up\n", true},
		{" SUBSYSTEM=platform\n", "", false},
		{"garbage\n", "", false},
		{"x,1,2,-;bad priority\n", "", false},
	}
	for _, c := range cases {
		got, ok := formatKmsg(c.in, testBoot, "rg35xxsp")
		if ok != c.ok || got != c.want {
			t.Errorf("%q: got %q %v, want %q %v", c.in, got, ok, c.want, c.ok)
		}
	}
}

const kmsgNote = "ewdb-logcat: no syslog at %s, showing the kernel log\n"

func TestMissingSyslogFallsBackToKernelLog(t *testing.T) {
	log := setup(t, "")
	useKmsg(t, kmsgSample)
	out, errOut, code := runDump("-d")
	want := "Nov  8 00:10:02 rg35xxsp kern.info kernel: [  602.147767] android_work: sent uevent USB_STATE=DISCONNECTED\n" +
		"Nov  8 00:10:02 rg35xxsp kern.err kernel: [  602.646736] ERR: Operation not supported\n" +
		"Nov  8 00:10:03 rg35xxsp user.info frontend[12]: started\n"
	if code != 0 || out != want {
		t.Fatalf("code %d out %q", code, out)
	}
	if errOut != strings.Replace(kmsgNote, "%s", log, 1) {
		t.Fatalf("stderr %q", errOut)
	}
}

func TestExistingSyslogIsPreferred(t *testing.T) {
	setup(t, sample)
	useKmsg(t, kmsgSample)
	out, errOut, code := runDump("-d")
	if code != 0 || out != sample || errOut != "" {
		t.Fatalf("code %d out %q err %q", code, out, errOut)
	}
}

func TestKernelLogTagFilterAndTail(t *testing.T) {
	setup(t, "")
	useKmsg(t, kmsgSample)
	out, _, _ := runDump("-d", "kernel")
	if strings.Count(out, "\n") != 2 || strings.Contains(out, "frontend") {
		t.Fatalf("kernel filter: %q", out)
	}
	out, _, _ = runDump("-t", "1")
	if out != "Nov  8 00:10:03 rg35xxsp user.info frontend[12]: started\n" {
		t.Fatalf("tail: %q", out)
	}
}

func TestKernelLogClearIsNoop(t *testing.T) {
	log := setup(t, "")
	kmsg := useKmsg(t, kmsgSample)
	_, errOut, code := runDump("-c")
	if code != 0 {
		t.Fatalf("code %d", code)
	}
	if errOut != strings.Replace(kmsgNote, "%s", log, 1)+"ewdb-logcat: the kernel log cannot be cleared\n" {
		t.Fatalf("stderr %q", errOut)
	}
	if b, _ := os.ReadFile(kmsg); string(b) != kmsgSample {
		t.Fatalf("kmsg changed: %q", b)
	}
}

func TestKernelLogFollow(t *testing.T) {
	setup(t, "")
	kmsg := useKmsg(t, kmsgSample)
	out, stop := startFollow(t)
	defer stop()
	waitFor(t, out, "frontend[12]: started")
	f, _ := os.OpenFile(kmsg, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString("6,605,604000000,-;later message\n")
	f.Close()
	waitFor(t, out, "kernel: [  604.000000] later message")
}

func TestNoSyslogAndNoKernelLog(t *testing.T) {
	log := setup(t, "")
	useKmsg(t, "")
	out, errOut, code := runDump("-d")
	if code != 1 || out != "" || errOut != "ewdb-logcat: no log at "+log+"\n" {
		t.Fatalf("code %d out %q err %q", code, out, errOut)
	}
}
