package main

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"ewdb/internal/uinput"
)

type write struct {
	typ, code uint16
	value     int32
}

type fakeDev struct {
	name    string
	support map[[2]uint16]bool
	abs     map[uint16]uinput.AbsInfo
	writes  []write
	failAt  int
	locked  *[]string
}

func newDev(name string, abs map[uint16]uinput.AbsInfo, codes ...[2]uint16) *fakeDev {
	d := &fakeDev{name: name, support: map[[2]uint16]bool{}, abs: abs}
	for _, c := range codes {
		d.support[c] = true
	}
	for c := range abs {
		d.support[[2]uint16{uinput.EV_ABS, c}] = true
	}
	return d
}

func (d *fakeDev) Supports(typ, code uint16) bool {
	return typ == uinput.EV_SYN || d.support[[2]uint16{typ, code}]
}

func (d *fakeDev) Abs(code uint16) (uinput.AbsInfo, bool) {
	a, ok := d.abs[code]
	return a, ok
}

func (d *fakeDev) Write(typ, code uint16, value int32) error {
	if d.failAt > 0 && len(d.writes)+1 == d.failAt {
		d.failAt = 0
		return errors.New("write failed")
	}
	d.writes = append(d.writes, write{typ, code, value})
	return nil
}

func (d *fakeDev) Lock(time.Duration) (func(), error) {
	if d.locked != nil {
		*d.locked = append(*d.locked, "lock "+d.name)
	}
	return func() {
		if d.locked != nil {
			*d.locked = append(*d.locked, "unlock "+d.name)
		}
	}, nil
}

type table map[string]*fakeDev

func (t table) Open(name string) (Target, error) {
	if d, ok := t[name]; ok {
		return d, nil
	}
	return nil, errors.New("input device \"" + name + "\" not found")
}

func panel(name string, withABS bool) *fakeDev {
	abs := map[uint16]uinput.AbsInfo{
		uinput.ABS_MT_SLOT:        {Max: 4},
		uinput.ABS_MT_TRACKING_ID: {Max: 65535},
		uinput.ABS_MT_POSITION_X:  {Min: 5, Max: 1023},
		uinput.ABS_MT_POSITION_Y:  {Min: 0, Max: 599},
	}
	if withABS {
		abs[uinput.ABS_X] = uinput.AbsInfo{Max: 1023}
		abs[uinput.ABS_Y] = uinput.AbsInfo{Max: 599}
	}
	return newDev(name, abs, [2]uint16{uinput.EV_KEY, uinput.BTN_TOUCH})
}

func newInjector(devs table) *injector {
	return &injector{
		devs:  devs,
		keys:  map[string]KeyTarget{"DPAD_UP": {Device: "pad", Code: uinput.BTN_DPAD_UP}, "HOME": {Device: "pad", Code: uinput.KEY_HOME}},
		touch: "ts",
		sleep: func(time.Duration) {},
	}
}

func TestInjectTapSkipsMissingAxes(t *testing.T) {
	ts := panel("ts", false)
	if err := newInjector(table{"ts": ts}).run([]string{"tap", "10", "20"}); err != nil {
		t.Fatal(err)
	}
	for _, w := range ts.writes {
		if w.typ == uinput.EV_ABS && (w.code == uinput.ABS_X || w.code == uinput.ABS_Y) {
			t.Fatalf("wrote unsupported axis: %v", w)
		}
	}
	if len(ts.writes) != 9 {
		t.Fatalf("writes %v", ts.writes)
	}
}

func TestInjectRangeFromAbsinfo(t *testing.T) {
	ts := panel("ts", true)
	err := newInjector(table{"ts": ts}).run([]string{"tap", "4", "20"})
	if err == nil || err.Error() != "x out of range 5..1023" {
		t.Fatalf("got %v", err)
	}
	if len(ts.writes) != 0 {
		t.Fatal("wrote on a rejected tap")
	}
}

func TestInjectKeyDeviceMissingCode(t *testing.T) {
	pad := newDev("pad", nil, [2]uint16{uinput.EV_KEY, uinput.BTN_DPAD_DOWN})
	err := newInjector(table{"pad": pad}).run([]string{"keyevent", "DPAD_UP"})
	if err == nil || err.Error() != "key DPAD_UP: device pad has no BTN_DPAD_UP" {
		t.Fatalf("got %v", err)
	}
	if len(pad.writes) != 0 {
		t.Fatal("wrote anyway")
	}
}

func TestInjectKeyWrites(t *testing.T) {
	pad := newDev("pad", nil, [2]uint16{uinput.EV_KEY, uinput.BTN_DPAD_UP})
	if err := newInjector(table{"pad": pad}).run([]string{"keyevent", "KEYCODE_DPAD_UP"}); err != nil {
		t.Fatal(err)
	}
	want := []write{
		{uinput.EV_KEY, uinput.BTN_DPAD_UP, 1}, {uinput.EV_SYN, 0, 0},
		{uinput.EV_KEY, uinput.BTN_DPAD_UP, 0}, {uinput.EV_SYN, 0, 0},
	}
	if !reflect.DeepEqual(pad.writes, want) {
		t.Fatalf("got %v", pad.writes)
	}
}

func hatInjector(devs table) *injector {
	in := newInjector(devs)
	in.keys["DPAD_RIGHT"] = KeyTarget{Device: "keys", Code: uinput.ABS_HAT0X, Axis: true, Value: 1}
	return in
}

func TestInjectAxisKeyWrites(t *testing.T) {
	keys := newDev("keys", map[uint16]uinput.AbsInfo{uinput.ABS_HAT0X: {Min: -1, Max: 1}})
	if err := hatInjector(table{"keys": keys}).run([]string{"keyevent", "DPAD_RIGHT"}); err != nil {
		t.Fatal(err)
	}
	want := []write{
		{uinput.EV_ABS, uinput.ABS_HAT0X, 1}, {uinput.EV_SYN, 0, 0},
		{uinput.EV_ABS, uinput.ABS_HAT0X, 0}, {uinput.EV_SYN, 0, 0},
	}
	if !reflect.DeepEqual(keys.writes, want) {
		t.Fatalf("got %v", keys.writes)
	}
}

func TestInjectAxisKeyDeviceMissingAxis(t *testing.T) {
	keys := newDev("keys", map[uint16]uinput.AbsInfo{uinput.ABS_HAT0Y: {Min: -1, Max: 1}}, [2]uint16{uinput.EV_KEY, uinput.KEY_Q})
	err := hatInjector(table{"keys": keys}).run([]string{"keyevent", "DPAD_RIGHT"})
	if err == nil || err.Error() != "key DPAD_RIGHT: device keys has no ABS_HAT0X" {
		t.Fatalf("got %v", err)
	}
	if len(keys.writes) != 0 {
		t.Fatal("wrote anyway")
	}
}

func TestInjectDeviceNotFound(t *testing.T) {
	err := newInjector(table{}).run([]string{"keyevent", "DPAD_UP"})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("got %v", err)
	}
	err = newInjector(table{}).run([]string{"tap", "1", "1"})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("got %v", err)
	}
}

func TestInjectReleasesAfterWriteFailure(t *testing.T) {
	ts := panel("ts", true)
	ts.failAt = 10
	err := newInjector(table{"ts": ts}).run([]string{"swipe", "5", "0", "100", "100", "100"})
	if err == nil {
		t.Fatal("no error")
	}
	n := len(ts.writes)
	tail := ts.writes[n-3:]
	want := []write{{uinput.EV_ABS, uinput.ABS_MT_TRACKING_ID, -1}, {uinput.EV_KEY, uinput.BTN_TOUCH, 0}, {uinput.EV_SYN, 0, 0}}
	if !reflect.DeepEqual(tail, want) {
		t.Fatalf("tail %v", tail)
	}
}

func TestInjectLocksInOrder(t *testing.T) {
	var log []string
	pad := newDev("pad", nil, [2]uint16{uinput.EV_KEY, uinput.BTN_DPAD_UP})
	adc := newDev("adc", nil, [2]uint16{uinput.EV_KEY, uinput.KEY_BACK})
	pad.locked, adc.locked = &log, &log
	in := newInjector(table{"pad": pad, "adc": adc})
	in.keys["BACK"] = KeyTarget{Device: "adc", Code: uinput.KEY_BACK}
	if err := in.run([]string{"keyevent", "DPAD_UP", "BACK"}); err != nil {
		t.Fatal(err)
	}
	want := []string{"lock adc", "lock pad", "unlock pad", "unlock adc"}
	if !reflect.DeepEqual(log, want) {
		t.Fatalf("got %v", log)
	}
}

func TestInjectNoTouchAxes(t *testing.T) {
	ts := newDev("ts", nil)
	err := newInjector(table{"ts": ts}).run([]string{"tap", "1", "1"})
	if err == nil || !strings.Contains(err.Error(), "no touch axes") {
		t.Fatalf("got %v", err)
	}
}

func TestTextRunner(t *testing.T) {
	kb := newDev(uinput.KeyboardName, nil, [2]uint16{uinput.EV_KEY, uinput.KEY_A})
	run := textRunner(kb, func(time.Duration) {})
	if err := run([]string{"text", "a"}); err != nil {
		t.Fatal(err)
	}
	if len(kb.writes) != 4 {
		t.Fatalf("writes %v", kb.writes)
	}
	if err := run([]string{"tap", "1", "1"}); err == nil {
		t.Fatal("server ran a tap")
	}
	if err := run([]string{"touchscreen", "keyevent", "HOME"}); err == nil {
		t.Fatal("server ran a keyevent")
	}
}

func TestTextCodes(t *testing.T) {
	codes := textCodes()
	has := map[uint16]bool{}
	for _, c := range codes {
		has[c] = true
	}
	if !has[uinput.KEY_LEFTSHIFT] || !has[uinput.KEY_A] || !has[uinput.KEY_ENTER] || has[uinput.KEY_HOME] || has[uinput.BTN_SOUTH] {
		t.Fatalf("%v", codes)
	}
}

func TestPlanRejectsNonFinite(t *testing.T) {
	for _, v := range []string{"NaN", "Inf", "-Inf", "+Inf", "1e999"} {
		if _, err := Plan([]string{"tap", v, "1"}, nil, touch); err == nil {
			t.Errorf("%s accepted", v)
		}
	}
}

func TestPlanMinRange(t *testing.T) {
	tc := Touch{Device: "ts", MinX: 5, MaxX: 10, MinY: 0, MaxY: 10}
	_, err := Plan([]string{"tap", "4", "1"}, nil, tc)
	if err == nil || err.Error() != "x out of range 5..10" {
		t.Fatalf("got %v", err)
	}
}
