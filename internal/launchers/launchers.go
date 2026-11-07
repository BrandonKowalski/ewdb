package launchers

import (
	"os"
	"path/filepath"
	"strings"
)

var Android = []string{"am", "pm", "monkey", "content", "ime", "uiautomator", "run-as", "cmd", "settings", "svc", "wm", "dumpsys", "setprop", "screenrecord", "logcat", "getprop", "input", "screencap", "bugreportz"}

func Build(dir string, commands map[string]string, systemPath string) error {
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
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
