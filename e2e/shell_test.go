//go:build e2e

package e2e

import (
	"bytes"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
)

func TestShellExitCode(t *testing.T) {
	serial := startDaemon(t, "")
	if _, _, code := adb(t, serial, "shell", "false"); code != 1 {
		t.Errorf("false: code %d", code)
	}
	if _, _, code := adb(t, serial, "shell", "exit", "42"); code != 42 {
		t.Errorf("exit 42: code %d", code)
	}
}

func TestShellSeparatesStderr(t *testing.T) {
	serial := startDaemon(t, "")
	out, errOut, code := adb(t, serial, "shell", "echo out; echo err >&2")
	if out != "out\n" || errOut != "err\n" || code != 0 {
		t.Fatalf("stdout %q stderr %q code %d", out, errOut, code)
	}
}

func TestShellNoCRLF(t *testing.T) {
	serial := startDaemon(t, "")
	out, _, code := adb(t, serial, "shell", "echo", "hi")
	if out != "hi\n" || code != 0 {
		t.Fatalf("stdout %q code %d", out, code)
	}
}

func TestShellT(t *testing.T) {
	serial := startDaemon(t, "")
	out, _, code := adb(t, serial, "shell", "-T", "echo", "hi")
	if out != "hi\n" || code != 0 {
		t.Fatalf("stdout %q code %d", out, code)
	}
}

func TestExecOutBinary(t *testing.T) {
	serial := startDaemon(t, "")
	var script strings.Builder
	script.WriteString("printf '")
	want := make([]byte, 256)
	for i := range want {
		want[i] = byte(i)
		fmt.Fprintf(&script, "\\%03o", i)
	}
	script.WriteString("'")
	out, errOut, code := adb(t, serial, "exec-out", script.String())
	if code != 0 || !bytes.Equal([]byte(out), want) {
		t.Fatalf("code %d stderr %q got %d bytes %q", code, errOut, len(out), out)
	}
}

func TestShellUsesLaunchers(t *testing.T) {
	serial := startDaemon(t, "[commands]\ngetprop = echo mapped\n")
	out, errOut, code := adb(t, serial, "shell", "getprop", "x")
	if out != "mapped x\n" || code != 0 {
		t.Fatalf("stdout %q stderr %q code %d", out, errOut, code)
	}
	out, errOut, code = adb(t, serial, "shell", "pm", "list", "packages")
	if out != "" || errOut != "pm: not available on this device\n" || code != 127 {
		t.Fatalf("pm: stdout %q stderr %q code %d", out, errOut, code)
	}
}

func sleepPids(t *testing.T, arg string) map[int]bool {
	t.Helper()
	out, err := exec.Command("ps", "-axo", "pid=,command=").Output()
	if err != nil {
		t.Fatal(err)
	}
	pids := map[int]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) == 3 && f[1] == "sleep" && f[2] == arg {
			pid, _ := strconv.Atoi(f[0])
			pids[pid] = true
		}
	}
	return pids
}

func TestShellHostGoneKillsChild(t *testing.T) {
	serial := startDaemon(t, "")
	before := sleepPids(t, "300")
	cmd := exec.Command(adbPath, "-s", serial, "shell", "sleep 300")
	cmd.Env = adbEnv()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()
	var fresh []int
	deadline := time.Now().Add(5 * time.Second)
	for len(fresh) == 0 {
		for pid := range sleepPids(t, "300") {
			if !before[pid] {
				fresh = append(fresh, pid)
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("sleep 300 never started")
		}
		time.Sleep(20 * time.Millisecond)
	}
	cmd.Process.Kill()
	cmd.Wait()
	time.Sleep(3 * time.Second)
	now := sleepPids(t, "300")
	for _, pid := range fresh {
		if now[pid] {
			syscall.Kill(pid, syscall.SIGKILL)
			t.Fatalf("sleep 300 (pid %d) still running 3 s after the host went away", pid)
		}
	}
}

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func TestInteractiveShellSignalsAndResize(t *testing.T) {
	serial := startDaemon(t, "")
	cmd := exec.Command(adbPath, "-s", serial, "shell", "-t", "sh", "-c", "'stty size; sleep 30'")
	cmd.Env = append(adbEnv(), "TERM=xterm")
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 40, Cols: 100})
	if err != nil {
		t.Fatal(err)
	}
	defer ptmx.Close()
	defer cmd.Process.Kill()
	var out syncBuffer
	go io.Copy(&out, ptmx)
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(out.String(), "40 100") {
		if time.Now().After(deadline) {
			t.Fatalf("no size in output %q", out.String())
		}
		select {
		case err := <-exited:
			t.Fatalf("adb exited early (%v): %q", err, out.String())
		case <-time.After(20 * time.Millisecond):
		}
	}
	if _, err := ptmx.Write([]byte{3}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-exited:
		var ee *exec.ExitError
		if err == nil {
			t.Fatalf("adb exited 0 after ^C")
		}
		if e, ok := err.(*exec.ExitError); ok {
			ee = e
		}
		if ee == nil || ee.ExitCode() != 130 {
			t.Fatalf("adb exit %v, want 130", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("adb still running 2 s after ^C: %q", out.String())
	}
}
