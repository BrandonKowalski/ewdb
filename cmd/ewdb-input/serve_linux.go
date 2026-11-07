//go:build linux

package main

import (
	"fmt"
	"log"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"ewdb/internal/config"
	"ewdb/internal/uinput"
)

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func eventPaths() []string {
	paths, _ := filepath.Glob("/dev/input/event*")
	sort.Slice(paths, func(i, j int) bool {
		if len(paths[i]) != len(paths[j]) {
			return len(paths[i]) < len(paths[j])
		}
		return paths[i] < paths[j]
	})
	return paths
}

func touchFor(d *uinput.Device) Touch {
	t := Touch{Device: d.Name()}
	if a, ok := d.Abs(uinput.ABS_MT_POSITION_X); ok {
		t.MaxX = a.Max
	} else if a, ok := d.Abs(uinput.ABS_X); ok {
		t.MaxX = a.Max
	}
	if a, ok := d.Abs(uinput.ABS_MT_POSITION_Y); ok {
		t.MaxY = a.Max
	} else if a, ok := d.Abs(uinput.ABS_Y); ok {
		t.MaxY = a.Max
	}
	return t
}

func openDevices(c *config.Config) (runner, func(), error) {
	devices := map[string]*uinput.Device{}
	closeAll := func() {
		for _, d := range devices {
			d.Close()
		}
	}
	wanted := splitList(setting(c, "clone"))
	paths := eventPaths()
	names := map[string]string{}
	for _, p := range paths {
		n, err := uinput.DeviceName(p)
		if err != nil {
			continue
		}
		if _, dup := names[n]; !dup {
			names[n] = p
		}
	}
	for _, w := range wanted {
		p, ok := names[w]
		if !ok {
			log.Printf("warning: input device %q not found, skipped", w)
			continue
		}
		d, err := uinput.Clone(p)
		if err != nil {
			log.Printf("warning: clone %q: %v", w, err)
			continue
		}
		devices[w] = d
	}
	kb, err := uinput.Keyboard(uinput.KeyboardName)
	if err != nil {
		closeAll()
		return nil, nil, err
	}
	devices[uinput.KeyboardName] = kb
	keys, warnings := parseKeys(c.Section("keys"))
	for _, w := range warnings {
		log.Print("warning: ", w)
	}
	var touch Touch
	if name := setting(c, "touch"); name != "" {
		if d, ok := devices[name]; ok {
			touch = touchFor(d)
		} else {
			log.Printf("warning: touch device %q was not cloned", name)
		}
	}
	run := func(args []string) error {
		events, err := Plan(args, keys, touch)
		if err != nil {
			return err
		}
		for _, e := range events {
			if devices[e.Device] == nil {
				return fmt.Errorf("device %q is not available", e.Device)
			}
		}
		for _, e := range events {
			if e.Delay > 0 {
				time.Sleep(e.Delay)
			}
			if err := devices[e.Device].Write(e.Type, e.Code, e.Value); err != nil {
				return err
			}
		}
		return nil
	}
	return run, closeAll, nil
}
