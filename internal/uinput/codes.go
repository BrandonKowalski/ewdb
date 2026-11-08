package uinput

const (
	EV_SYN = 0x00
	EV_KEY = 0x01
	EV_REL = 0x02
	EV_ABS = 0x03
	EV_MSC = 0x04
	EV_SW  = 0x05
	EV_LED = 0x11
	EV_SND = 0x12
	EV_REP = 0x14
	EV_FF  = 0x15
	EV_MAX = 0x1f

	SYN_REPORT = 0x00

	KEY_MAX        = 0x2ff
	REL_MAX        = 0x0f
	ABS_MAX        = 0x3f
	MSC_MAX        = 0x07
	SW_MAX         = 0x10
	LED_MAX        = 0x0f
	SND_MAX        = 0x07
	INPUT_PROP_MAX = 0x1f

	ABS_X              = 0x00
	ABS_Y              = 0x01
	ABS_Z              = 0x02
	ABS_RX             = 0x03
	ABS_RY             = 0x04
	ABS_RZ             = 0x05
	ABS_HAT0X          = 0x10
	ABS_HAT0Y          = 0x11
	ABS_HAT1X          = 0x12
	ABS_HAT1Y          = 0x13
	ABS_HAT2X          = 0x14
	ABS_HAT2Y          = 0x15
	ABS_HAT3X          = 0x16
	ABS_HAT3Y          = 0x17
	ABS_MT_SLOT        = 0x2f
	ABS_MT_POSITION_X  = 0x35
	ABS_MT_POSITION_Y  = 0x36
	ABS_MT_TRACKING_ID = 0x39

	BUS_VIRTUAL = 0x06
)

const (
	KEY_ESC        = 1
	KEY_1          = 2
	KEY_2          = 3
	KEY_3          = 4
	KEY_4          = 5
	KEY_5          = 6
	KEY_6          = 7
	KEY_7          = 8
	KEY_8          = 9
	KEY_9          = 10
	KEY_0          = 11
	KEY_MINUS      = 12
	KEY_EQUAL      = 13
	KEY_BACKSPACE  = 14
	KEY_TAB        = 15
	KEY_Q          = 16
	KEY_W          = 17
	KEY_E          = 18
	KEY_R          = 19
	KEY_T          = 20
	KEY_Y          = 21
	KEY_U          = 22
	KEY_I          = 23
	KEY_O          = 24
	KEY_P          = 25
	KEY_LEFTBRACE  = 26
	KEY_RIGHTBRACE = 27
	KEY_ENTER      = 28
	KEY_A          = 30
	KEY_S          = 31
	KEY_D          = 32
	KEY_F          = 33
	KEY_G          = 34
	KEY_H          = 35
	KEY_J          = 36
	KEY_K          = 37
	KEY_L          = 38
	KEY_SEMICOLON  = 39
	KEY_APOSTROPHE = 40
	KEY_GRAVE      = 41
	KEY_LEFTSHIFT  = 42
	KEY_BACKSLASH  = 43
	KEY_Z          = 44
	KEY_X          = 45
	KEY_C          = 46
	KEY_V          = 47
	KEY_B          = 48
	KEY_N          = 49
	KEY_M          = 50
	KEY_COMMA      = 51
	KEY_DOT        = 52
	KEY_SLASH      = 53
	KEY_SPACE      = 57
	KEY_HOME       = 102
	KEY_UP         = 103
	KEY_LEFT       = 105
	KEY_RIGHT      = 106
	KEY_DOWN       = 108
	KEY_POWER      = 116
	KEY_MENU       = 139
	KEY_BACK       = 158
	KEY_VOLUMEDOWN = 114
	KEY_VOLUMEUP   = 115
	KEY_HOMEPAGE   = 172
	KEY_OK         = 0x160
	KEY_SELECT     = 0x161

	BTN_SOUTH      = 0x130
	BTN_EAST       = 0x131
	BTN_C          = 0x132
	BTN_NORTH      = 0x133
	BTN_WEST       = 0x134
	BTN_Z          = 0x135
	BTN_TL         = 0x136
	BTN_TR         = 0x137
	BTN_TL2        = 0x138
	BTN_TR2        = 0x139
	BTN_SELECT     = 0x13a
	BTN_START      = 0x13b
	BTN_MODE       = 0x13c
	BTN_THUMBL     = 0x13d
	BTN_THUMBR     = 0x13e
	BTN_A          = BTN_SOUTH
	BTN_B          = BTN_EAST
	BTN_X          = BTN_NORTH
	BTN_Y          = BTN_WEST
	BTN_TOUCH      = 0x14a
	BTN_DPAD_UP    = 0x220
	BTN_DPAD_DOWN  = 0x221
	BTN_DPAD_LEFT  = 0x222
	BTN_DPAD_RIGHT = 0x223
)

var CodeNames = map[string]uint16{
	"KEY_ESC": KEY_ESC, "KEY_1": KEY_1, "KEY_2": KEY_2, "KEY_3": KEY_3, "KEY_4": KEY_4,
	"KEY_5": KEY_5, "KEY_6": KEY_6, "KEY_7": KEY_7, "KEY_8": KEY_8, "KEY_9": KEY_9,
	"KEY_0": KEY_0, "KEY_MINUS": KEY_MINUS, "KEY_EQUAL": KEY_EQUAL,
	"KEY_BACKSPACE": KEY_BACKSPACE, "KEY_TAB": KEY_TAB,
	"KEY_Q": KEY_Q, "KEY_W": KEY_W, "KEY_E": KEY_E, "KEY_R": KEY_R, "KEY_T": KEY_T,
	"KEY_Y": KEY_Y, "KEY_U": KEY_U, "KEY_I": KEY_I, "KEY_O": KEY_O, "KEY_P": KEY_P,
	"KEY_LEFTBRACE": KEY_LEFTBRACE, "KEY_RIGHTBRACE": KEY_RIGHTBRACE, "KEY_ENTER": KEY_ENTER,
	"KEY_A": KEY_A, "KEY_S": KEY_S, "KEY_D": KEY_D, "KEY_F": KEY_F, "KEY_G": KEY_G,
	"KEY_H": KEY_H, "KEY_J": KEY_J, "KEY_K": KEY_K, "KEY_L": KEY_L,
	"KEY_SEMICOLON": KEY_SEMICOLON, "KEY_APOSTROPHE": KEY_APOSTROPHE, "KEY_GRAVE": KEY_GRAVE,
	"KEY_LEFTSHIFT": KEY_LEFTSHIFT, "KEY_BACKSLASH": KEY_BACKSLASH,
	"KEY_Z": KEY_Z, "KEY_X": KEY_X, "KEY_C": KEY_C, "KEY_V": KEY_V, "KEY_B": KEY_B,
	"KEY_N": KEY_N, "KEY_M": KEY_M, "KEY_COMMA": KEY_COMMA, "KEY_DOT": KEY_DOT,
	"KEY_SLASH": KEY_SLASH, "KEY_SPACE": KEY_SPACE,
	"KEY_HOME": KEY_HOME, "KEY_UP": KEY_UP, "KEY_LEFT": KEY_LEFT, "KEY_RIGHT": KEY_RIGHT,
	"KEY_DOWN": KEY_DOWN, "KEY_POWER": KEY_POWER, "KEY_MENU": KEY_MENU, "KEY_BACK": KEY_BACK,
	"KEY_VOLUMEDOWN": KEY_VOLUMEDOWN, "KEY_VOLUMEUP": KEY_VOLUMEUP, "KEY_HOMEPAGE": KEY_HOMEPAGE,
	"KEY_OK": KEY_OK, "KEY_SELECT": KEY_SELECT,
	"BTN_SOUTH": BTN_SOUTH, "BTN_EAST": BTN_EAST, "BTN_C": BTN_C, "BTN_NORTH": BTN_NORTH,
	"BTN_WEST": BTN_WEST, "BTN_Z": BTN_Z, "BTN_TL": BTN_TL, "BTN_TR": BTN_TR,
	"BTN_TL2": BTN_TL2, "BTN_TR2": BTN_TR2, "BTN_SELECT": BTN_SELECT, "BTN_START": BTN_START,
	"BTN_MODE": BTN_MODE, "BTN_THUMBL": BTN_THUMBL, "BTN_THUMBR": BTN_THUMBR,
	"BTN_A": BTN_A, "BTN_B": BTN_B, "BTN_X": BTN_X, "BTN_Y": BTN_Y,
	"BTN_DPAD_UP": BTN_DPAD_UP, "BTN_DPAD_DOWN": BTN_DPAD_DOWN,
	"BTN_DPAD_LEFT": BTN_DPAD_LEFT, "BTN_DPAD_RIGHT": BTN_DPAD_RIGHT,
}

var AbsNames = map[string]uint16{
	"ABS_X": ABS_X, "ABS_Y": ABS_Y, "ABS_Z": ABS_Z, "ABS_RX": ABS_RX, "ABS_RY": ABS_RY, "ABS_RZ": ABS_RZ,
	"ABS_HAT0X": ABS_HAT0X, "ABS_HAT0Y": ABS_HAT0Y, "ABS_HAT1X": ABS_HAT1X, "ABS_HAT1Y": ABS_HAT1Y,
	"ABS_HAT2X": ABS_HAT2X, "ABS_HAT2Y": ABS_HAT2Y, "ABS_HAT3X": ABS_HAT3X, "ABS_HAT3Y": ABS_HAT3Y,
}
