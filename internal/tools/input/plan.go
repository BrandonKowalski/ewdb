package input

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"ewdb/internal/config"
	"ewdb/internal/uinput"
)

type kv = config.KV

type Event struct {
	Device     string
	Type, Code uint16
	Value      int32
	Delay      time.Duration
}

type KeyTarget struct {
	Device string
	Code   uint16
	Axis   bool
	Value  int32
	Ranged bool
	Min    int32
	Max    int32
}

func (k KeyTarget) rest() int32 {
	if !k.Ranged {
		return 0
	}
	return int32((int64(k.Min) + int64(k.Max)) / 2)
}

type Touch struct {
	Device     string
	MinX, MinY int32
	MaxX, MaxY int32
}

const (
	tapHold       = 50 * time.Millisecond
	keyGap        = 40 * time.Millisecond
	moveInterval  = 16 * time.Millisecond
	defaultSwipe  = 300
	textGap       = 10 * time.Millisecond
	maxPlanLength = 25 * time.Second
	trackingID    = 1
)

var sources = map[string]bool{
	"keyboard": true, "touchscreen": true, "gamepad": true, "dpad": true, "touchpad": true,
}

var androidKeys = map[int]string{
	3: "HOME", 4: "BACK", 19: "DPAD_UP", 20: "DPAD_DOWN", 21: "DPAD_LEFT", 22: "DPAD_RIGHT",
	23: "DPAD_CENTER", 24: "VOLUME_UP", 25: "VOLUME_DOWN", 26: "POWER", 66: "ENTER", 82: "MENU",
	96: "BUTTON_A", 97: "BUTTON_B", 99: "BUTTON_X", 100: "BUTTON_Y", 102: "BUTTON_L1",
	103: "BUTTON_R1", 104: "BUTTON_L2", 105: "BUTTON_R2", 106: "BUTTON_THUMBL",
	107: "BUTTON_THUMBR", 108: "BUTTON_START", 109: "BUTTON_SELECT", 110: "BUTTON_MODE",
}

var androidNames = func() map[string]bool {
	m := map[string]bool{}
	for _, n := range androidKeys {
		m[n] = true
	}
	return m
}()

type textKey struct {
	code  uint16
	shift bool
}

var textKeys = buildTextKeys()

func buildTextKeys() map[byte]textKey {
	m := map[byte]textKey{}
	letters := []uint16{
		uinput.KEY_A, uinput.KEY_B, uinput.KEY_C, uinput.KEY_D, uinput.KEY_E, uinput.KEY_F,
		uinput.KEY_G, uinput.KEY_H, uinput.KEY_I, uinput.KEY_J, uinput.KEY_K, uinput.KEY_L,
		uinput.KEY_M, uinput.KEY_N, uinput.KEY_O, uinput.KEY_P, uinput.KEY_Q, uinput.KEY_R,
		uinput.KEY_S, uinput.KEY_T, uinput.KEY_U, uinput.KEY_V, uinput.KEY_W, uinput.KEY_X,
		uinput.KEY_Y, uinput.KEY_Z,
	}
	for i, c := range letters {
		m[byte('a'+i)] = textKey{c, false}
		m[byte('A'+i)] = textKey{c, true}
	}
	digits := []uint16{
		uinput.KEY_0, uinput.KEY_1, uinput.KEY_2, uinput.KEY_3, uinput.KEY_4,
		uinput.KEY_5, uinput.KEY_6, uinput.KEY_7, uinput.KEY_8, uinput.KEY_9,
	}
	for i, c := range digits {
		m[byte('0'+i)] = textKey{c, false}
	}
	for i, ch := range ")!@#$%^&*(" {
		m[byte(ch)] = textKey{digits[i], true}
	}
	pairs := []struct {
		plain, shifted byte
		code           uint16
	}{
		{'-', '_', uinput.KEY_MINUS},
		{'=', '+', uinput.KEY_EQUAL},
		{'[', '{', uinput.KEY_LEFTBRACE},
		{']', '}', uinput.KEY_RIGHTBRACE},
		{';', ':', uinput.KEY_SEMICOLON},
		{'\'', '"', uinput.KEY_APOSTROPHE},
		{'`', '~', uinput.KEY_GRAVE},
		{'\\', '|', uinput.KEY_BACKSLASH},
		{',', '<', uinput.KEY_COMMA},
		{'.', '>', uinput.KEY_DOT},
		{'/', '?', uinput.KEY_SLASH},
	}
	for _, p := range pairs {
		m[p.plain] = textKey{p.code, false}
		m[p.shifted] = textKey{p.code, true}
	}
	m[' '] = textKey{uinput.KEY_SPACE, false}
	m['\t'] = textKey{uinput.KEY_TAB, false}
	m['\n'] = textKey{uinput.KEY_ENTER, false}
	return m
}

func canonicalKey(name string) string {
	name = strings.ToUpper(strings.TrimSpace(name))
	name = strings.TrimPrefix(name, "KEYCODE_")
	if n, err := strconv.Atoi(name); err == nil {
		if k, ok := androidKeys[n]; ok {
			return k
		}
	}
	return name
}

func parseKeys(entries []kv) (map[string]KeyTarget, []string) {
	m := map[string]KeyTarget{}
	var warnings []string
	for _, e := range entries {
		name := canonicalKey(e.Key)
		fields := strings.Fields(e.Value)
		if len(fields) < 2 {
			warnings = append(warnings, fmt.Sprintf("[keys] %s: expected <device name> <code name>", e.Key))
			continue
		}
		if strings.HasPrefix(fields[len(fields)-2], "ABS_") || strings.HasPrefix(fields[len(fields)-1], "ABS_") {
			if t, warning := parseAxis(e.Key, fields); warning != "" {
				warnings = append(warnings, warning)
			} else {
				m[name] = t
			}
			continue
		}
		code, ok := uinput.CodeNames[fields[len(fields)-1]]
		if !ok {
			warnings = append(warnings, fmt.Sprintf("[keys] %s: unknown code %s", e.Key, fields[len(fields)-1]))
			continue
		}
		m[name] = KeyTarget{Device: strings.Join(fields[:len(fields)-1], " "), Code: code}
	}
	return m, warnings
}

func parseAxis(key string, fields []string) (KeyTarget, string) {
	usage := fmt.Sprintf("[keys] %s: expected <device name> <axis> <value>", key)
	if len(fields) < 3 {
		return KeyTarget{}, usage
	}
	axis := fields[len(fields)-2]
	value, err := strconv.ParseInt(fields[len(fields)-1], 10, 32)
	if err != nil || !strings.HasPrefix(axis, "ABS_") {
		return KeyTarget{}, usage
	}
	code, ok := uinput.AbsNames[axis]
	if !ok {
		return KeyTarget{}, fmt.Sprintf("[keys] %s: unknown code %s", key, axis)
	}
	return KeyTarget{Device: strings.Join(fields[:len(fields)-2], " "), Code: code, Axis: true, Value: int32(value)}, ""
}

type builder struct {
	events []Event
	delay  time.Duration
}

func (b *builder) add(dev string, typ, code uint16, value int32, delay time.Duration) {
	b.events = append(b.events, Event{Device: dev, Type: typ, Code: code, Value: value, Delay: delay})
}

func (b *builder) syn(dev string) {
	b.add(dev, uinput.EV_SYN, uinput.SYN_REPORT, 0, 0)
}

func (b *builder) position(t Touch, x, y int32, delay time.Duration) {
	b.add(t.Device, uinput.EV_ABS, uinput.ABS_MT_POSITION_X, x, delay)
	b.add(t.Device, uinput.EV_ABS, uinput.ABS_MT_POSITION_Y, y, 0)
}

func (b *builder) absolute(t Touch, x, y int32) {
	b.add(t.Device, uinput.EV_ABS, uinput.ABS_X, x, 0)
	b.add(t.Device, uinput.EV_ABS, uinput.ABS_Y, y, 0)
}

func (b *builder) down(t Touch, x, y int32) {
	b.add(t.Device, uinput.EV_ABS, uinput.ABS_MT_SLOT, 0, 0)
	b.add(t.Device, uinput.EV_ABS, uinput.ABS_MT_TRACKING_ID, trackingID, 0)
	b.position(t, x, y, 0)
	b.add(t.Device, uinput.EV_KEY, uinput.BTN_TOUCH, 1, 0)
	b.absolute(t, x, y)
	b.syn(t.Device)
}

func (b *builder) up(t Touch, delay time.Duration) {
	b.add(t.Device, uinput.EV_ABS, uinput.ABS_MT_TRACKING_ID, -1, delay)
	b.add(t.Device, uinput.EV_KEY, uinput.BTN_TOUCH, 0, 0)
	b.syn(t.Device)
}

func parseCoord(s, axis string, min, max int32) (int32, error) {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, fmt.Errorf("invalid number %q", s)
	}
	f = math.Trunc(f)
	if f < float64(min) || f > float64(max) {
		return 0, fmt.Errorf("%s out of range %d..%d", axis, min, max)
	}
	return int32(f), nil
}

func parsePoint(x, y string, t Touch) (int32, int32, error) {
	px, err := parseCoord(x, "x", t.MinX, t.MaxX)
	if err != nil {
		return 0, 0, err
	}
	py, err := parseCoord(y, "y", t.MinY, t.MaxY)
	if err != nil {
		return 0, 0, err
	}
	return px, py, nil
}

func planTap(args []string, t Touch) ([]Event, error) {
	if len(args) != 2 {
		return nil, errors.New("usage: tap X Y")
	}
	x, y, err := parsePoint(args[0], args[1], t)
	if err != nil {
		return nil, err
	}
	var b builder
	b.down(t, x, y)
	b.up(t, tapHold)
	return b.events, nil
}

func planSwipe(args []string, t Touch) ([]Event, error) {
	if len(args) != 4 && len(args) != 5 {
		return nil, errors.New("usage: swipe X1 Y1 X2 Y2 [DURATION_MS]")
	}
	x1, y1, err := parsePoint(args[0], args[1], t)
	if err != nil {
		return nil, err
	}
	x2, y2, err := parsePoint(args[2], args[3], t)
	if err != nil {
		return nil, err
	}
	ms := int64(defaultSwipe)
	if len(args) == 5 {
		ms, err = strconv.ParseInt(args[4], 10, 64)
		if err != nil || ms < 0 {
			return nil, fmt.Errorf("invalid duration %q", args[4])
		}
	}
	total := time.Duration(ms) * time.Millisecond
	if total > maxPlanLength {
		return nil, errors.New("duration too long")
	}
	steps := int((total + moveInterval - 1) / moveInterval)
	if steps < 1 {
		steps = 1
	}
	var b builder
	b.down(t, x1, y1)
	for i := 1; i <= steps; i++ {
		delay := moveInterval
		if i == steps {
			delay = total - time.Duration(steps-1)*moveInterval
		}
		x := x1 + int32(int64(x2-x1)*int64(i)/int64(steps))
		y := y1 + int32(int64(y2-y1)*int64(i)/int64(steps))
		b.position(t, x, y, delay)
		b.absolute(t, x, y)
		b.syn(t.Device)
	}
	b.up(t, 0)
	return b.events, nil
}

func planKeyevent(args []string, keys map[string]KeyTarget) ([]Event, error) {
	if len(args) == 0 {
		return nil, errors.New("usage: keyevent KEY...")
	}
	var b builder
	for i, a := range args {
		name := canonicalKey(a)
		target, ok := keys[name]
		if !ok {
			if !androidNames[name] {
				return nil, fmt.Errorf("unknown key %s", a)
			}
			return nil, fmt.Errorf("key %s is not mapped on this device", name)
		}
		var gap time.Duration
		if i > 0 {
			gap = keyGap
		}
		typ, press, rest := uint16(uinput.EV_KEY), int32(1), int32(0)
		if target.Axis {
			if target.Ranged && (target.Value < target.Min || target.Value > target.Max) {
				return nil, fmt.Errorf("key %s: value %d out of range %d..%d on %s", name, target.Value, target.Min, target.Max, target.Device)
			}
			typ, press, rest = uinput.EV_ABS, target.Value, target.rest()
		}
		b.add(target.Device, typ, target.Code, press, gap)
		b.syn(target.Device)
		b.add(target.Device, typ, target.Code, rest, keyGap)
		b.syn(target.Device)
	}
	return b.events, nil
}

func planText(args []string) ([]Event, error) {
	if len(args) == 0 {
		return nil, errors.New("usage: text STRING")
	}
	text := strings.ReplaceAll(strings.Join(args, " "), "%s", " ")
	dev := uinput.KeyboardName
	var b builder
	for i := 0; i < len(text); i++ {
		k, ok := textKeys[text[i]]
		if !ok {
			return nil, fmt.Errorf("cannot type character at byte %d", i)
		}
		if k.shift {
			b.add(dev, uinput.EV_KEY, uinput.KEY_LEFTSHIFT, 1, 0)
			b.syn(dev)
		}
		b.add(dev, uinput.EV_KEY, k.code, 1, 0)
		b.syn(dev)
		b.add(dev, uinput.EV_KEY, k.code, 0, textGap)
		b.syn(dev)
		if k.shift {
			b.add(dev, uinput.EV_KEY, uinput.KEY_LEFTSHIFT, 0, 0)
			b.syn(dev)
		}
	}
	return b.events, nil
}

func command(args []string) (string, []string) {
	if len(args) > 0 && sources[args[0]] {
		args = args[1:]
	}
	if len(args) == 0 {
		return "", nil
	}
	return args[0], args[1:]
}

func textCodes() []uint16 {
	seen := map[uint16]bool{uinput.KEY_LEFTSHIFT: true}
	for _, k := range textKeys {
		seen[k.code] = true
	}
	codes := make([]uint16, 0, len(seen))
	for c := range seen {
		codes = append(codes, c)
	}
	sort.Slice(codes, func(i, j int) bool { return codes[i] < codes[j] })
	return codes
}

func Plan(args []string, keys map[string]KeyTarget, touch Touch) ([]Event, error) {
	cmd, rest := command(args)
	if cmd == "" {
		return nil, errors.New("usage: input [SOURCE] tap|swipe|keyevent|text ARGS")
	}
	var events []Event
	var err error
	switch cmd {
	case "tap", "swipe":
		if touch.Device == "" {
			return nil, errors.New("no touch screen is configured")
		}
		if cmd == "tap" {
			events, err = planTap(rest, touch)
		} else {
			events, err = planSwipe(rest, touch)
		}
	case "keyevent":
		events, err = planKeyevent(rest, keys)
	case "text":
		events, err = planText(rest)
	default:
		return nil, fmt.Errorf("unknown command %q", cmd)
	}
	if err != nil {
		return nil, err
	}
	var total time.Duration
	for _, e := range events {
		total += e.Delay
	}
	if total > maxPlanLength {
		return nil, errors.New("input would take too long")
	}
	return events, nil
}
