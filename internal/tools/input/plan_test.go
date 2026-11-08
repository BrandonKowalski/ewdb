package input

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"ewdb/internal/uinput"
)

var touch = Touch{Device: "ts", MaxX: 1023, MaxY: 599}

var keys = map[string]KeyTarget{
	"DPAD_UP":   {Device: "pad", Code: uinput.BTN_DPAD_UP},
	"DPAD_DOWN": {Device: "pad", Code: uinput.BTN_DPAD_DOWN},
	"BACK":      {Device: "adc", Code: uinput.KEY_BACK},
	"DPAD_LEFT": {Device: "ANBERNIC-keys", Code: uinput.ABS_HAT0X, Axis: true, Value: -1},
}

func ev(dev string, typ, code uint16, value int32, delay time.Duration) Event {
	return Event{Device: dev, Type: typ, Code: code, Value: value, Delay: delay}
}

func syn(dev string, delay time.Duration) Event {
	return ev(dev, uinput.EV_SYN, uinput.SYN_REPORT, 0, delay)
}

func TestPlanTap(t *testing.T) {
	got, err := Plan([]string{"tap", "10", "20"}, keys, touch)
	if err != nil {
		t.Fatal(err)
	}
	want := []Event{
		ev("ts", uinput.EV_ABS, uinput.ABS_MT_SLOT, 0, 0),
		ev("ts", uinput.EV_ABS, uinput.ABS_MT_TRACKING_ID, 1, 0),
		ev("ts", uinput.EV_ABS, uinput.ABS_MT_POSITION_X, 10, 0),
		ev("ts", uinput.EV_ABS, uinput.ABS_MT_POSITION_Y, 20, 0),
		ev("ts", uinput.EV_KEY, uinput.BTN_TOUCH, 1, 0),
		ev("ts", uinput.EV_ABS, uinput.ABS_X, 10, 0),
		ev("ts", uinput.EV_ABS, uinput.ABS_Y, 20, 0),
		syn("ts", 0),
		ev("ts", uinput.EV_ABS, uinput.ABS_MT_TRACKING_ID, -1, 50*time.Millisecond),
		ev("ts", uinput.EV_KEY, uinput.BTN_TOUCH, 0, 0),
		syn("ts", 0),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got\n%v\nwant\n%v", got, want)
	}
}

func TestPlanSwipeSteps(t *testing.T) {
	got, err := Plan([]string{"swipe", "0", "0", "160", "320"}, keys, touch)
	if err != nil {
		t.Fatal(err)
	}
	if n := count(got, uinput.ABS_MT_POSITION_X) - 1; n != 19 {
		t.Fatalf("moves = %d, want 19", n)
	}
	var total time.Duration
	for _, e := range got {
		total += e.Delay
	}
	if total != 300*time.Millisecond {
		t.Fatalf("duration = %v", total)
	}
	var lastX, lastY int32
	for _, e := range got {
		if e.Code == uinput.ABS_MT_POSITION_X && e.Value != 0 {
			lastX = e.Value
		}
		if e.Code == uinput.ABS_MT_POSITION_Y && e.Value != 0 {
			lastY = e.Value
		}
	}
	if lastX != 160 || lastY != 320 {
		t.Fatalf("end = %d,%d", lastX, lastY)
	}
	last := got[len(got)-1]
	if last.Code != uinput.SYN_REPORT || got[len(got)-3].Value != -1 {
		t.Fatalf("no release at the end: %v", got[len(got)-3:])
	}
}

func TestPlanSwipeDuration(t *testing.T) {
	got, err := Plan([]string{"swipe", "0", "0", "10", "10", "64"}, keys, touch)
	if err != nil {
		t.Fatal(err)
	}
	if n := count(got, uinput.ABS_MT_POSITION_X) - 1; n != 4 {
		t.Fatalf("moves = %d, want 4", n)
	}
}

func count(events []Event, code uint16) int {
	n := 0
	for _, e := range events {
		if e.Code == code {
			n++
		}
	}
	return n
}

func TestPlanKeyeventNames(t *testing.T) {
	want, err := Plan([]string{"keyevent", "DPAD_UP"}, keys, touch)
	if err != nil {
		t.Fatal(err)
	}
	exact := []Event{
		ev("pad", uinput.EV_KEY, uinput.BTN_DPAD_UP, 1, 0),
		syn("pad", 0),
		ev("pad", uinput.EV_KEY, uinput.BTN_DPAD_UP, 0, 40*time.Millisecond),
		syn("pad", 0),
	}
	if !reflect.DeepEqual(want, exact) {
		t.Fatalf("got %v", want)
	}
	for _, name := range []string{"KEYCODE_DPAD_UP", "19", "keycode_dpad_up"} {
		got, err := Plan([]string{"keyevent", name}, keys, touch)
		if err != nil {
			t.Fatal(name, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: got %v", name, got)
		}
	}
}

func TestPlanKeyeventMany(t *testing.T) {
	got, err := Plan([]string{"keyevent", "DPAD_DOWN", "BACK"}, keys, touch)
	if err != nil {
		t.Fatal(err)
	}
	want := []Event{
		ev("pad", uinput.EV_KEY, uinput.BTN_DPAD_DOWN, 1, 0),
		syn("pad", 0),
		ev("pad", uinput.EV_KEY, uinput.BTN_DPAD_DOWN, 0, 40*time.Millisecond),
		syn("pad", 0),
		ev("adc", uinput.EV_KEY, uinput.KEY_BACK, 1, 40*time.Millisecond),
		syn("adc", 0),
		ev("adc", uinput.EV_KEY, uinput.KEY_BACK, 0, 40*time.Millisecond),
		syn("adc", 0),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got\n%v\nwant\n%v", got, want)
	}
}

func TestPlanSourceIgnored(t *testing.T) {
	want, _ := Plan([]string{"tap", "10", "20"}, keys, touch)
	for _, src := range []string{"touchscreen", "keyboard", "gamepad", "dpad", "touchpad"} {
		got, err := Plan([]string{src, "tap", "10", "20"}, keys, touch)
		if err != nil {
			t.Fatal(src, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s differs", src)
		}
	}
	k, err := Plan([]string{"keyboard", "keyevent", "BACK"}, keys, touch)
	if err != nil || len(k) != 4 {
		t.Fatalf("%v %v", k, err)
	}
}

func TestPlanOutOfRange(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"tap", "1024", "5"}, "x out of range 0..1023"},
		{[]string{"tap", "-1", "5"}, "x out of range 0..1023"},
		{[]string{"tap", "5", "600"}, "y out of range 0..599"},
		{[]string{"swipe", "0", "0", "2000", "5"}, "x out of range 0..1023"},
	}
	for _, c := range cases {
		_, err := Plan(c.args, keys, touch)
		if err == nil || err.Error() != c.want {
			t.Errorf("%v: got %v, want %q", c.args, err, c.want)
		}
	}
}

func TestPlanUnmappedKey(t *testing.T) {
	_, err := Plan([]string{"keyevent", "HOME"}, keys, touch)
	if err == nil || err.Error() != "key HOME is not mapped on this device" {
		t.Fatalf("got %v", err)
	}
	_, err = Plan([]string{"keyevent", "KEYCODE_NOPE"}, keys, touch)
	if err == nil || !strings.Contains(err.Error(), "NOPE") {
		t.Fatalf("got %v", err)
	}
}

func TestPlanText(t *testing.T) {
	got, err := Plan([]string{"text", "Ab"}, keys, touch)
	if err != nil {
		t.Fatal(err)
	}
	var keysOnly []Event
	for _, e := range got {
		if e.Type == uinput.EV_KEY {
			keysOnly = append(keysOnly, e)
		}
	}
	type kv struct {
		code  uint16
		value int32
	}
	want := []kv{
		{uinput.KEY_LEFTSHIFT, 1}, {uinput.KEY_A, 1}, {uinput.KEY_A, 0}, {uinput.KEY_LEFTSHIFT, 0},
		{uinput.KEY_B, 1}, {uinput.KEY_B, 0},
	}
	if len(keysOnly) != len(want) {
		t.Fatalf("got %v", keysOnly)
	}
	for i, e := range keysOnly {
		if e.Device != uinput.KeyboardName || e.Code != want[i].code || e.Value != want[i].value {
			t.Fatalf("event %d = %v, want %v", i, e, want[i])
		}
	}
}

func TestPlanTextPunctuation(t *testing.T) {
	got, err := Plan([]string{"text", "a%sb", "!"}, keys, touch)
	if err != nil {
		t.Fatal(err)
	}
	var codes []uint16
	for _, e := range got {
		if e.Type == uinput.EV_KEY && e.Value == 1 {
			codes = append(codes, e.Code)
		}
	}
	want := []uint16{uinput.KEY_A, uinput.KEY_SPACE, uinput.KEY_B, uinput.KEY_SPACE, uinput.KEY_LEFTSHIFT, uinput.KEY_1}
	if !reflect.DeepEqual(codes, want) {
		t.Fatalf("got %v", codes)
	}
	if _, err := Plan([]string{"text", "café"}, keys, touch); err == nil {
		t.Fatal("non ASCII accepted")
	}
}

func TestPlanErrors(t *testing.T) {
	for _, args := range [][]string{
		nil,
		{"tap"},
		{"tap", "1"},
		{"tap", "a", "b"},
		{"swipe", "1", "2", "3"},
		{"swipe", "0", "0", "1", "1", "-5"},
		{"keyevent"},
		{"text"},
		{"frobnicate"},
		{"touchscreen"},
	} {
		if _, err := Plan(args, keys, touch); err == nil {
			t.Errorf("%v: no error", args)
		}
	}
	if _, err := Plan([]string{"tap", "1", "1"}, keys, Touch{}); err == nil {
		t.Error("tap without a touch device")
	}
	if _, err := Plan([]string{"swipe", "0", "0", "1", "1", "999999"}, keys, touch); err == nil {
		t.Error("overlong swipe")
	}
}

func TestParseKeys(t *testing.T) {
	m, warnings := parseKeys([]kv{
		{Key: "DPAD_UP", Value: "retrogame_joypad BTN_DPAD_UP"},
		{Key: "KEYCODE_BACK", Value: "adc-keys-back KEY_BACK"},
		{Key: "POWER", Value: "rk805 pwrkey KEY_POWER"},
		{Key: "HOME", Value: "pad NO_SUCH_CODE"},
		{Key: "MENU", Value: "KEY_MENU"},
	})
	if m["DPAD_UP"] != (KeyTarget{Device: "retrogame_joypad", Code: uinput.BTN_DPAD_UP}) {
		t.Fatalf("%v", m)
	}
	if m["BACK"] != (KeyTarget{Device: "adc-keys-back", Code: uinput.KEY_BACK}) {
		t.Fatalf("%v", m)
	}
	if m["POWER"] != (KeyTarget{Device: "rk805 pwrkey", Code: uinput.KEY_POWER}) {
		t.Fatalf("%v", m)
	}
	if _, ok := m["HOME"]; ok || len(warnings) != 2 {
		t.Fatalf("%v %v", m, warnings)
	}
}

func TestPlanKeyeventAxis(t *testing.T) {
	got, err := Plan([]string{"keyevent", "DPAD_LEFT", "BACK"}, keys, touch)
	if err != nil {
		t.Fatal(err)
	}
	want := []Event{
		ev("ANBERNIC-keys", uinput.EV_ABS, uinput.ABS_HAT0X, -1, 0),
		syn("ANBERNIC-keys", 0),
		ev("ANBERNIC-keys", uinput.EV_ABS, uinput.ABS_HAT0X, 0, 40*time.Millisecond),
		syn("ANBERNIC-keys", 0),
		ev("adc", uinput.EV_KEY, uinput.KEY_BACK, 1, 40*time.Millisecond),
		syn("adc", 0),
		ev("adc", uinput.EV_KEY, uinput.KEY_BACK, 0, 40*time.Millisecond),
		syn("adc", 0),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got\n%v\nwant\n%v", got, want)
	}
}

func TestParseKeysAxis(t *testing.T) {
	m, warnings := parseKeys([]kv{
		{Key: "DPAD_UP", Value: "ANBERNIC-keys ABS_HAT0Y -1"},
		{Key: "DPAD_RIGHT", Value: "ANBERNIC-keys ABS_HAT0X 1"},
		{Key: "DPAD_DOWN", Value: "two words ABS_HAT0Y +1"},
		{Key: "DPAD_LEFT", Value: "ANBERNIC-keys ABS_HAT0X"},
		{Key: "BUTTON_A", Value: "ANBERNIC-keys ABS_HAT0X left"},
		{Key: "BUTTON_B", Value: "ANBERNIC-keys ABS_NOPE 1"},
		{Key: "BUTTON_X", Value: "ANBERNIC-keys KEY_Q 1"},
	})
	want := map[string]KeyTarget{
		"DPAD_UP":    {Device: "ANBERNIC-keys", Code: uinput.ABS_HAT0Y, Axis: true, Value: -1},
		"DPAD_RIGHT": {Device: "ANBERNIC-keys", Code: uinput.ABS_HAT0X, Axis: true, Value: 1},
		"DPAD_DOWN":  {Device: "two words", Code: uinput.ABS_HAT0Y, Axis: true, Value: 1},
	}
	if !reflect.DeepEqual(m, want) {
		t.Fatalf("got %v", m)
	}
	if len(warnings) != 4 {
		t.Fatalf("warnings %q", warnings)
	}
	for _, w := range []string{
		"[keys] DPAD_LEFT: expected <device name> <axis> <value>",
		"[keys] BUTTON_A: expected <device name> <axis> <value>",
		"[keys] BUTTON_B: unknown code ABS_NOPE",
		"[keys] BUTTON_X: unknown code 1",
	} {
		if !strings.Contains(strings.Join(warnings, "\n"), w) {
			t.Fatalf("missing %q in %q", w, warnings)
		}
	}
}

func TestPlanAxisReleaseHatIsZero(t *testing.T) {
	k := map[string]KeyTarget{"DPAD_LEFT": {Device: "pad", Code: uinput.ABS_HAT0X, Axis: true, Value: -1, Ranged: true, Min: -1, Max: 1}}
	got, err := Plan([]string{"keyevent", "DPAD_LEFT"}, k, touch)
	if err != nil {
		t.Fatal(err)
	}
	if got[2].Value != 0 {
		t.Fatalf("%v", got)
	}
}

func TestPlanAxisReleaseStickIsCentre(t *testing.T) {
	k := map[string]KeyTarget{"DPAD_LEFT": {Device: "pad", Code: uinput.ABS_X, Axis: true, Value: 0, Ranged: true, Min: 0, Max: 255}}
	got, err := Plan([]string{"keyevent", "DPAD_LEFT"}, k, touch)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Value != 0 || got[2].Value != 127 {
		t.Fatalf("%v", got)
	}
}

func TestPlanAxisValueOutOfRangeRejected(t *testing.T) {
	k := map[string]KeyTarget{"DPAD_LEFT": {Device: "pad", Code: uinput.ABS_HAT0X, Axis: true, Value: 5, Ranged: true, Min: -1, Max: 1}}
	got, err := Plan([]string{"keyevent", "DPAD_LEFT"}, k, touch)
	if err == nil || got != nil || err.Error() != "key DPAD_LEFT: value 5 out of range -1..1 on pad" {
		t.Fatalf("%v %v", got, err)
	}
}
