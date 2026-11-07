//go:build e2e

package e2e

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var (
	ewdbdBin   string
	adbPort    string
	adbPath    string
	adbMissing bool
)

func freePort() (string, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	defer l.Close()
	_, p, err := net.SplitHostPort(l.Addr().String())
	return p, err
}

func TestMain(m *testing.M) {
	os.Exit(runMain(m))
}

func runMain(m *testing.M) int {
	dir, err := os.MkdirTemp("", "ewdb-e2e")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer os.RemoveAll(dir)

	adbPath, err = exec.LookPath("adb")
	if err != nil {
		adbMissing = true
	}

	ewdbdBin = filepath.Join(dir, "ewdbd")
	build := exec.Command("go", "build", "-tags", "testlistener", "-o", ewdbdBin, "ewdb/cmd/ewdbd")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "build ewdbd: %v\n%s", err, out)
		return 1
	}

	if !adbMissing {
		adbPort, err = freePort()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		defer func() {
			cmd := exec.Command(adbPath, "kill-server")
			cmd.Env = adbEnv()
			cmd.Run()
		}()
	}
	return m.Run()
}

func adbEnv() []string {
	return append(os.Environ(), "ANDROID_ADB_SERVER_PORT="+adbPort)
}

func requireAdb(t *testing.T) {
	t.Helper()
	if adbMissing {
		t.Skip("adb not on PATH")
	}
}

func adb(t *testing.T, serial string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	requireAdb(t)
	if serial != "" {
		args = append([]string{"-s", serial}, args...)
	}
	cmd := exec.Command(adbPath, args...)
	cmd.Env = adbEnv()
	var so, se bytes.Buffer
	cmd.Stdout, cmd.Stderr = &so, &se
	err := cmd.Run()
	code = 0
	if err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("adb %v: %v", args, err)
		}
		code = ee.ExitCode()
	}
	return so.String(), se.String(), code
}

func startDaemon(t *testing.T, conf string) (serial string) {
	t.Helper()
	requireAdb(t)
	dir := t.TempDir()
	confPath := filepath.Join(dir, "ewdbd.conf")
	if err := os.WriteFile(confPath, []byte(conf), 0o644); err != nil {
		t.Fatal(err)
	}
	port, err := freePort()
	if err != nil {
		t.Fatal(err)
	}
	addr := "127.0.0.1:" + port
	var logs bytes.Buffer
	d := exec.Command(ewdbdBin, "-c", confPath, "-listen", addr)
	d.Stderr = &logs
	if err := d.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan struct{})
	go func() {
		d.Wait()
		close(exited)
	}()
	t.Cleanup(func() {
		adb(t, "", "disconnect", addr)
		d.Process.Kill()
		<-exited
	})

	deadline := time.Now().Add(10 * time.Second)
	for {
		select {
		case <-exited:
			t.Fatalf("ewdbd exited early: %s", logs.String())
		default:
		}
		c, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			c.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("ewdbd did not listen: %s", logs.String())
		}
		time.Sleep(10 * time.Millisecond)
	}

	for {
		adb(t, "", "connect", addr)
		out, _, code := adb(t, addr, "get-state")
		if code == 0 && strings.TrimSpace(out) == "device" {
			return addr
		}
		if time.Now().After(deadline) {
			t.Fatalf("device %s never reached state device (last %q); logs: %s", addr, out, logs.String())
		}
		time.Sleep(50 * time.Millisecond)
	}
}
