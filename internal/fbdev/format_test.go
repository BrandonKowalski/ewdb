package fbdev

import (
	"testing"

	"ewdb/internal/capture"
)

func TestFormat(t *testing.T) {
	cases := []struct {
		name                     string
		bpp                      uint32
		red, green, blue, transp Field
		want                     string
	}{
		{"xrgb", 32, Field{16, 8}, Field{8, 8}, Field{0, 8}, Field{0, 0}, "XR24"},
		{"argb", 32, Field{16, 8}, Field{8, 8}, Field{0, 8}, Field{24, 8}, "AR24"},
		{"xbgr", 32, Field{0, 8}, Field{8, 8}, Field{16, 8}, Field{0, 0}, "XB24"},
		{"abgr", 32, Field{0, 8}, Field{8, 8}, Field{16, 8}, Field{24, 8}, "AB24"},
		{"rgb24", 24, Field{16, 8}, Field{8, 8}, Field{0, 8}, Field{}, "RG24"},
		{"bgr24", 24, Field{0, 8}, Field{8, 8}, Field{16, 8}, Field{}, "BG24"},
		{"rgb565", 16, Field{11, 5}, Field{5, 6}, Field{0, 5}, Field{}, "RG16"},
		{"bgr565", 16, Field{0, 5}, Field{5, 6}, Field{11, 5}, Field{}, "BG16"},
	}
	for _, c := range cases {
		got := Format(c.bpp, c.red, c.green, c.blue, c.transp)
		if capture.FormatName(got) != c.want {
			t.Errorf("%s: got %q want %q", c.name, capture.FormatName(got), c.want)
		}
	}
	if Format(8, Field{}, Field{}, Field{}, Field{}) != 0 {
		t.Error("8bpp should be unsupported")
	}
}
