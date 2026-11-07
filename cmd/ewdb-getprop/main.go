package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"runtime"
	"sort"
	"strings"

	"ewdb/internal/config"
)

func abi() string {
	switch runtime.GOARCH {
	case "arm64":
		return "arm64-v8a"
	case "arm":
		return "armeabi-v7a"
	case "amd64":
		return "x86_64"
	case "386":
		return "x86"
	}
	return runtime.GOARCH
}

func firstLine(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	if sc.Scan() {
		return strings.TrimSpace(sc.Text())
	}
	return ""
}

func properties(c *config.Config) map[string]string {
	m := map[string]string{
		"ro.product.name":    c.Name,
		"ro.product.model":   c.Model,
		"ro.product.device":  c.Device,
		"ro.product.cpu.abi": abi(),
	}
	if c.SerialFile != "" {
		if s := firstLine(c.SerialFile); s != "" {
			m["ro.serialno"] = s
		}
	}
	for _, kv := range c.Props {
		m[kv.Key] = kv.Value
	}
	return m
}

func execute(args []string, out io.Writer) {
	path := os.Getenv("EWDB_CONFIG")
	if path == "" {
		path = "/etc/ewdbd.conf"
	}
	m := properties(config.Load(path))
	switch len(args) {
	case 0:
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(out, "[%s]: [%s]\n", k, m[k])
		}
	default:
		if v, ok := m[args[0]]; ok {
			fmt.Fprintln(out, v)
		} else if len(args) > 1 {
			fmt.Fprintln(out, args[1])
		} else {
			fmt.Fprintln(out)
		}
	}
}

func main() {
	execute(os.Args[1:], os.Stdout)
}
