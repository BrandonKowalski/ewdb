package services

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

var diskFilesystems = map[string]bool{"vfat": true, "exfat": true, "ext2": true, "ext3": true, "ext4": true}

func writableMounts(mounts string) []string {
	var out []string
	for _, line := range strings.Split(mounts, "\n") {
		f := strings.Fields(line)
		if len(f) < 4 || !diskFilesystems[f[2]] || !slices.Contains(strings.Split(f[3], ","), "rw") {
			continue
		}
		out = append(out, unescapeMount(f[1]))
	}
	slices.Reverse(out)
	return out
}

func unescapeMount(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			if v, err := strconv.ParseUint(s[i+1:i+4], 8, 8); err == nil {
				b.WriteByte(byte(v))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func userProcesses(proc string, self int) []int {
	entries, err := os.ReadDir(proc)
	if err != nil {
		return nil
	}
	var pids []int
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid <= 1 || pid == self {
			continue
		}
		cmdline, err := os.ReadFile(filepath.Join(proc, e.Name(), "cmdline"))
		if err != nil || len(cmdline) == 0 {
			continue
		}
		pids = append(pids, pid)
	}
	slices.Sort(pids)
	return pids
}
