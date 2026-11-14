package config

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type KV struct {
	Key   string
	Value string
}

type Config struct {
	Name, Model, Device string
	State, USB          string
	SerialFile, Shell   string
	MaxData             uint32
	Props               []KV
	Commands            map[string]string
	Sections            map[string][]KV
	Path                string
	Warnings            []string
}

const (
	commandTimeout = 2 * time.Second
	minMaxData     = 4096
	maxMaxData     = 1048576
	defaultMaxData = 32768
)

func defaults() *Config {
	return &Config{
		Name:     "linux",
		Model:    "Linux handheld",
		Device:   "handheld",
		State:    "/var/lib/ewdb",
		USB:      "auto",
		Shell:    "/bin/sh",
		MaxData:  defaultMaxData,
		Commands: map[string]string{},
		Sections: map[string][]KV{},
	}
}

func Parse(r io.Reader) (*Config, error) {
	return parse(r, true)
}

func parse(r io.Reader, run bool) (*Config, error) {
	c := defaults()
	device := map[string]string{}
	section := ""
	sc := bufio.NewScanner(r)
	n := 0
	for sc.Scan() {
		n++
		line := strings.TrimSpace(sc.Text())
		if line == "" || line[0] == '#' || line[0] == ';' {
			continue
		}
		if line[0] == '[' && line[len(line)-1] == ']' {
			section = strings.TrimSpace(line[1 : len(line)-1])
			continue
		}
		i := strings.IndexByte(line, '=')
		if i < 0 {
			return nil, fmt.Errorf("line %d: expected key = value or [section]", n)
		}
		kv := KV{strings.TrimSpace(line[:i]), strings.TrimSpace(line[i+1:])}
		if kv.Key == "" {
			return nil, fmt.Errorf("line %d: empty key", n)
		}
		switch section {
		case "device":
			device[kv.Key] = kv.Value
		case "props":
			c.Props = append(c.Props, kv)
		case "commands":
			c.Commands[kv.Key] = kv.Value
		default:
			c.Sections[section] = append(c.Sections[section], kv)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	for _, f := range []struct {
		key string
		dst *string
	}{
		{"name", &c.Name},
		{"model", &c.Model},
		{"device", &c.Device},
	} {
		if v := device[f.key]; v != "" {
			*f.dst = v
		}
		if cmd := device[f.key+"_command"]; cmd != "" && run {
			out, err := runCommand(cmd)
			if err != nil {
				c.Warnings = append(c.Warnings, fmt.Sprintf("%s_command: %v", f.key, err))
			} else {
				*f.dst = out
			}
		}
	}
	if v := device["state"]; v != "" {
		c.State = v
	}
	if v := device["usb"]; v != "" {
		c.USB = v
	}
	if v := device["serial_file"]; v != "" {
		c.SerialFile = v
	}
	if v := device["shell"]; v != "" {
		c.Shell = v
	}
	if v := device["maxdata"]; v != "" {
		n, err := strconv.ParseUint(v, 10, 32)
		if err != nil || n < minMaxData || n > maxMaxData {
			c.Warnings = append(c.Warnings, fmt.Sprintf("maxdata %q is not between %d and %d; using %d", v, minMaxData, maxMaxData, c.MaxData))
		} else {
			c.MaxData = uint32(n)
		}
	}
	return c, nil
}

func runCommand(command string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = 100 * time.Millisecond
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("timed out after %v", commandTimeout)
		}
		return "", err
	}
	first, _, _ := strings.Cut(out.String(), "\n")
	first = strings.TrimSpace(first)
	if first == "" {
		return "", fmt.Errorf("empty output")
	}
	return first, nil
}

func parseFile(path string, run bool) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	c, err := parse(f, run)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	c.Path = path
	return c, nil
}

func Load(path string) *Config {
	var warnings []string
	base, err := parseFile(path, false)
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("config: %v; using defaults", err))
		c := defaults()
		c.Warnings = warnings
		return c
	}
	chosen := path
	override := filepath.Join(base.State, "ewdbd.conf")
	if override != path {
		if _, err := os.Stat(override); err == nil {
			if _, err := parseFile(override, false); err != nil {
				warnings = append(warnings, fmt.Sprintf("config: %v; using %s", err, path))
			} else {
				chosen = override
			}
		}
	}
	c, err := parseFile(chosen, true)
	if err != nil {
		c = defaults()
		warnings = append(warnings, fmt.Sprintf("config: %v; using defaults", err))
	}
	c.Warnings = append(warnings, c.Warnings...)
	return c
}

func (c *Config) Command(name string) (string, bool) {
	v, ok := c.Commands[name]
	return v, ok
}

func (c *Config) Section(name string) []KV {
	return c.Sections[name]
}
