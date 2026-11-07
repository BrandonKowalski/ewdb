package drm

import (
	"testing"
	"unsafe"
)

func TestStructSizes(t *testing.T) {
	cases := []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{"card_res", unsafe.Sizeof(modeCardRes{}), 64},
		{"get_connector", unsafe.Sizeof(modeGetConnector{}), 80},
		{"get_encoder", unsafe.Sizeof(modeGetEncoder{}), 20},
		{"get_plane_res", unsafe.Sizeof(modeGetPlaneRes{}), 16},
		{"get_plane", unsafe.Sizeof(modeGetPlane{}), 32},
		{"fb_cmd", unsafe.Sizeof(modeFBCmd{}), 28},
		{"fb_cmd2", unsafe.Sizeof(modeFBCmd2{}), 104},
		{"prime_handle", unsafe.Sizeof(primeHandle{}), 12},
		{"map_dumb", unsafe.Sizeof(modeMapDumb{}), 16},
		{"gem_close", unsafe.Sizeof(gemClose{}), 8},
		{"set_client_cap", unsafe.Sizeof(setClientCap{}), 16},
		{"obj_get_properties", unsafe.Sizeof(modeObjGetProperties{}), 32},
		{"get_property", unsafe.Sizeof(modeGetProperty{}), 64},
		{"dma_buf_sync", unsafe.Sizeof(dmaBufSync{}), 8},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s: size %d want %d", c.name, c.got, c.want)
		}
	}
}

func TestIoctlNumbers(t *testing.T) {
	cases := []struct {
		name string
		got  uint32
		want uint32
	}{
		{"GETRESOURCES", ioctlGetResources, 0xC04064A0},
		{"GETENCODER", ioctlGetEncoder, 0xC01464A6},
		{"GETCONNECTOR", ioctlGetConnector, 0xC05064A7},
		{"GETPROPERTY", ioctlGetProperty, 0xC04064AA},
		{"GETFB", ioctlGetFB, 0xC01C64AD},
		{"MAP_DUMB", ioctlMapDumb, 0xC01064B3},
		{"GETPLANERESOURCES", ioctlGetPlaneResources, 0xC01064B5},
		{"GETPLANE", ioctlGetPlane, 0xC02064B6},
		{"OBJ_GETPROPERTIES", ioctlObjGetProperties, 0xC02064B9},
		{"GETFB2", ioctlGetFB2, 0xC06864CE},
		{"GEM_CLOSE", ioctlGemClose, 0x40086409},
		{"SET_CLIENT_CAP", ioctlSetClientCap, 0x4010640D},
		{"PRIME_HANDLE_TO_FD", ioctlPrimeHandleToFD, 0xC00C642D},
		{"DMA_BUF_IOCTL_SYNC", ioctlDmaBufSync, 0x40086200},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s: %#x want %#x", c.name, c.got, c.want)
		}
	}
}

func TestConnectorName(t *testing.T) {
	cases := []struct {
		typ, id uint32
		want    string
	}{
		{16, 2, "DSI-2"},
		{16, 1, "DSI-1"},
		{11, 1, "HDMI-A-1"},
		{14, 1, "eDP-1"},
		{10, 3, "DP-3"},
		{2, 1, "DVI-I-1"},
		{99, 4, "Unknown-4"},
	}
	for _, c := range cases {
		if got := ConnectorName(c.typ, c.id); got != c.want {
			t.Errorf("%d-%d: got %q want %q", c.typ, c.id, got, c.want)
		}
	}
}

func TestLegacyFormat(t *testing.T) {
	cases := []struct {
		bpp, depth uint32
		want       string
	}{
		{32, 24, "XR24"},
		{32, 32, "AR24"},
		{24, 24, "RG24"},
		{16, 16, "RG16"},
		{16, 15, "XR15"},
	}
	for _, c := range cases {
		got := legacyFormat(c.bpp, c.depth)
		f := string([]byte{byte(got), byte(got >> 8), byte(got >> 16), byte(got >> 24)})
		if f != c.want {
			t.Errorf("%d/%d: got %q want %q", c.bpp, c.depth, f, c.want)
		}
	}
}
