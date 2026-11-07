package launchers

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var Android = []string{"am", "pm", "monkey", "content", "ime", "uiautomator", "run-as", "cmd", "settings", "svc", "wm", "dumpsys", "setprop", "screenrecord", "logcat", "getprop", "input", "screencap", "bugreportz"}

const marker = ".ewdb-launchers"

func Build(dir string, commands map[string]string, systemPath string) error {
	dir = filepath.Clean(dir)
	exists, err := owned(dir)
	if err != nil {
		return err
	}
	parent, base := filepath.Split(dir)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(parent, "."+base+".new-")
	if err != nil {
		return err
	}
	if err := fill(tmp, commands, systemPath); err != nil {
		os.RemoveAll(tmp)
		return err
	}
	if !exists {
		if err := os.Rename(tmp, dir); err != nil {
			os.RemoveAll(tmp)
			return err
		}
		return nil
	}
	old, err := os.MkdirTemp(parent, "."+base+".old-")
	if err != nil {
		os.RemoveAll(tmp)
		return err
	}
	stale := filepath.Join(old, base)
	if err := os.Rename(dir, stale); err != nil {
		os.RemoveAll(tmp)
		os.Remove(old)
		return err
	}
	if err := os.Rename(tmp, dir); err != nil {
		os.Rename(stale, dir)
		os.RemoveAll(tmp)
		os.Remove(old)
		return err
	}
	return os.RemoveAll(old)
}

func owned(dir string) (bool, error) {
	st, err := os.Lstat(dir)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !st.IsDir() {
		return false, fmt.Errorf("%s exists and is not a launcher directory", dir)
	}
	if m, err := os.Lstat(filepath.Join(dir, marker)); err != nil || !m.Mode().IsRegular() {
		return false, fmt.Errorf("%s exists and has no %s marker; not replacing it", dir, marker)
	}
	return true, nil
}

func fill(dir string, commands map[string]string, systemPath string) error {
	if err := os.Chmod(dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, marker), nil, 0o644); err != nil {
		return err
	}
	for name, command := range commands {
		if !validName(name) {
			continue
		}
		script := "#!/bin/sh\nPATH=" + quote(systemPath) + "; export PATH; exec " + command + " \"$@\"\n"
		if err := write(dir, name, script); err != nil {
			return err
		}
	}
	for _, name := range Android {
		if _, mapped := commands[name]; mapped || onPath(name, systemPath) {
			continue
		}
		script := "#!/bin/sh\necho " + quote(name+": not available on this device") + " >&2\nexit 127\n"
		if err := write(dir, name, script); err != nil {
			return err
		}
	}
	return nil
}

func validName(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsRune(name, '/')
}

func quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func write(dir, name, script string) error {
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		return err
	}
	return os.Chmod(path, 0o755)
}

func onPath(name, systemPath string) bool {
	for _, d := range filepath.SplitList(systemPath) {
		if d == "" {
			continue
		}
		st, err := os.Stat(filepath.Join(d, name))
		if err == nil && st.Mode().IsRegular() && st.Mode().Perm()&0o111 != 0 {
			return true
		}
	}
	return false
}
