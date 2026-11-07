package usb

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

func findUDC(mountsPath, udcClassDir, ffsDir string) string {
	ffsDir = filepath.Clean(ffsDir)
	var instance string
	var configfs []string
	if f, err := os.Open(mountsPath); err == nil {
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			fields := strings.Fields(sc.Text())
			if len(fields) < 3 {
				continue
			}
			switch fields[2] {
			case "functionfs":
				if filepath.Clean(fields[1]) == ffsDir {
					instance = fields[0]
				}
			case "configfs":
				configfs = append(configfs, fields[1])
			}
		}
		f.Close()
	}
	if instance != "" {
		for _, root := range configfs {
			gadgets, _ := filepath.Glob(filepath.Join(root, "usb_gadget", "*"))
			for _, g := range gadgets {
				if _, err := os.Stat(filepath.Join(g, "functions", "ffs."+instance)); err != nil {
					continue
				}
				b, err := os.ReadFile(filepath.Join(g, "UDC"))
				if err != nil {
					return ""
				}
				return strings.TrimSpace(string(b))
			}
		}
	}
	entries, err := os.ReadDir(udcClassDir)
	if err != nil || len(entries) != 1 {
		return ""
	}
	return entries[0].Name()
}
