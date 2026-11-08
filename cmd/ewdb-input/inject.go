package main

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"ewdb/internal/uinput"
)

const lockTimeout = 30 * time.Second

type Target interface {
	Supports(typ, code uint16) bool
	Abs(code uint16) (uinput.AbsInfo, bool)
	Write(typ, code uint16, value int32) error
	Lock(timeout time.Duration) (func(), error)
}

type Devices interface {
	Open(name string) (Target, error)
}

type injector struct {
	devs  Devices
	keys  map[string]KeyTarget
	touch string
	sleep func(time.Duration)
}

func touchFor(name string, t Target) (Touch, error) {
	out := Touch{Device: name}
	x, okx := t.Abs(uinput.ABS_MT_POSITION_X)
	y, oky := t.Abs(uinput.ABS_MT_POSITION_Y)
	if !okx || !oky {
		x, okx = t.Abs(uinput.ABS_X)
		y, oky = t.Abs(uinput.ABS_Y)
	}
	if !okx || !oky {
		return out, fmt.Errorf("device %s has no touch axes", name)
	}
	out.MinX, out.MaxX, out.MinY, out.MaxY = x.Min, x.Max, y.Min, y.Max
	return out, nil
}

func codeName(typ, code uint16) string {
	table := uinput.CodeNames
	if typ == uinput.EV_ABS {
		table = uinput.AbsNames
	}
	var names []string
	for n, c := range table {
		if c == code {
			names = append(names, n)
		}
	}
	if len(names) == 0 {
		return fmt.Sprintf("code %#x", code)
	}
	sort.Strings(names)
	return names[0]
}

func (in *injector) keyName(e Event) string {
	var names []string
	for n, k := range in.keys {
		if k.Device == e.Device && k.Code == e.Code && k.Axis == (e.Type == uinput.EV_ABS) {
			names = append(names, n)
		}
	}
	if len(names) == 0 {
		return codeName(e.Type, e.Code)
	}
	sort.Strings(names)
	return names[0]
}

func (in *injector) run(args []string) error {
	opened := map[string]Target{}
	open := func(name string) (Target, error) {
		if t, ok := opened[name]; ok {
			return t, nil
		}
		t, err := in.devs.Open(name)
		if err != nil {
			return nil, err
		}
		opened[name] = t
		return t, nil
	}
	cmd, _ := command(args)
	var touch Touch
	if (cmd == "tap" || cmd == "swipe") && in.touch != "" {
		t, err := open(in.touch)
		if err != nil {
			return err
		}
		if touch, err = touchFor(in.touch, t); err != nil {
			return err
		}
	}
	keys := in.keys
	if cmd == "keyevent" {
		var err error
		if keys, err = rangedKeys(in.keys, args[1:], open); err != nil {
			return err
		}
	}
	events, err := Plan(args, keys, touch)
	if err != nil {
		return err
	}
	return in.execute(events, touch.Device, cmd == "keyevent", open)
}

func rangedKeys(keys map[string]KeyTarget, names []string, open func(string) (Target, error)) (map[string]KeyTarget, error) {
	out := make(map[string]KeyTarget, len(keys))
	for n, k := range keys {
		out[n] = k
	}
	for _, a := range names {
		name := canonicalKey(a)
		k, ok := out[name]
		if !ok || !k.Axis || k.Ranged {
			continue
		}
		t, err := open(k.Device)
		if err != nil {
			return nil, err
		}
		if info, ok := t.Abs(k.Code); ok {
			k.Ranged, k.Min, k.Max = true, info.Min, info.Max
			out[name] = k
		}
	}
	return out, nil
}

func (in *injector) execute(events []Event, touchDev string, strict bool, open func(string) (Target, error)) error {
	targets := map[string]Target{}
	var names []string
	for _, e := range events {
		if _, ok := targets[e.Device]; ok {
			continue
		}
		t, err := open(e.Device)
		if err != nil {
			return err
		}
		targets[e.Device] = t
		names = append(names, e.Device)
	}
	for _, e := range events {
		required := strict || (e.Type == uinput.EV_KEY && e.Code != uinput.BTN_TOUCH)
		if required && !targets[e.Device].Supports(e.Type, e.Code) {
			return fmt.Errorf("key %s: device %s has no %s", in.keyName(e), e.Device, codeName(e.Type, e.Code))
		}
	}
	sort.Strings(names)
	for _, n := range names {
		unlock, err := targets[n].Lock(lockTimeout)
		if err != nil {
			return err
		}
		defer unlock()
	}
	for _, e := range events {
		if e.Delay > 0 {
			in.sleep(e.Delay)
		}
		t := targets[e.Device]
		if !t.Supports(e.Type, e.Code) {
			continue
		}
		if err := t.Write(e.Type, e.Code, e.Value); err != nil {
			if t, ok := targets[touchDev]; ok && touchDev != "" {
				release(t)
			}
			return err
		}
	}
	return nil
}

func release(t Target) {
	if t.Supports(uinput.EV_ABS, uinput.ABS_MT_TRACKING_ID) {
		t.Write(uinput.EV_ABS, uinput.ABS_MT_TRACKING_ID, -1)
	}
	if t.Supports(uinput.EV_KEY, uinput.BTN_TOUCH) {
		t.Write(uinput.EV_KEY, uinput.BTN_TOUCH, 0)
	}
	t.Write(uinput.EV_SYN, uinput.SYN_REPORT, 0)
}

type fixedDevices map[string]Target

func (f fixedDevices) Open(name string) (Target, error) {
	if t, ok := f[name]; ok {
		return t, nil
	}
	return nil, fmt.Errorf("input device %q not available", name)
}

func textRunner(kb Target, sleep func(time.Duration)) runner {
	in := &injector{devs: fixedDevices{uinput.KeyboardName: kb}, sleep: sleep}
	return func(args []string) error {
		if cmd, _ := command(args); cmd != "text" {
			return errors.New("ewdb-input --serve handles only text")
		}
		return in.run(args)
	}
}
