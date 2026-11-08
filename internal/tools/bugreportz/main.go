package bugreportz

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"ewdb/internal/config"
)

const version = "1.2"

func itemTimeout() time.Duration {
	if v := os.Getenv("EWDB_BUGREPORT_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	return 30 * time.Second
}

func runItem(command string, timeout time.Duration) []byte {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = 200 * time.Millisecond
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	if ctx.Err() != nil {
		buf.WriteString("timed out\n")
	} else if err != nil && buf.Len() == 0 {
		fmt.Fprintf(&buf, "%v\n", err)
	}
	return buf.Bytes()
}

func entryName(name string) string {
	return strings.NewReplacer("/", "_", "\\", "_").Replace(name) + ".txt"
}

func fail(stdout io.Writer, format string, a ...any) int {
	fmt.Fprintf(stdout, "FAIL:%s\n", fmt.Sprintf(format, a...))
	return 1
}

func execute(args []string, stdout, stderr io.Writer) int {
	progress := false
	for _, a := range args {
		switch a {
		case "-v":
			fmt.Fprintln(stderr, version)
			return 0
		case "-p":
			progress = true
		}
	}
	path := os.Getenv("EWDB_CONFIG")
	if path == "" {
		path = "/etc/ewdbd.conf"
	}
	c := config.Load(path)
	dir := "/tmp"
	var items []config.KV
	for _, kv := range c.Section("bugreport") {
		if kv.Key == "dir" {
			if kv.Value != "" {
				dir = kv.Value
			}
			continue
		}
		items = append(items, kv)
	}
	zipPath := filepath.Join(dir, fmt.Sprintf("bugreport-%s-%s.zip", c.Device, time.Now().Format("2006-01-02-15-04-05")))
	f, err := os.Create(zipPath)
	if err != nil {
		return fail(stdout, "%v", err)
	}
	ok := false
	defer func() {
		f.Close()
		if !ok {
			os.Remove(zipPath)
		}
	}()
	if progress {
		fmt.Fprintf(stdout, "BEGIN:%s\n", zipPath)
	}
	timeout := itemTimeout()
	zw := zip.NewWriter(f)
	for i, it := range items {
		w, err := zw.Create(entryName(it.Key))
		if err != nil {
			return fail(stdout, "%v", err)
		}
		if _, err := w.Write(runItem(it.Value, timeout)); err != nil {
			return fail(stdout, "%v", err)
		}
		if progress {
			fmt.Fprintf(stdout, "PROGRESS:%d/%d\n", i+1, len(items))
		}
	}
	if err := zw.Close(); err != nil {
		return fail(stdout, "%v", err)
	}
	if err := f.Close(); err != nil {
		return fail(stdout, "%v", err)
	}
	ok = true
	fmt.Fprintf(stdout, "OK:%s\n", zipPath)
	return 0
}

func Main(args []string, stdout, stderr io.Writer) int {
	return execute(args, stdout, stderr)
}
