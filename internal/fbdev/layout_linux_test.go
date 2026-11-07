package fbdev

import (
	"testing"
	"unsafe"
)

func TestLayouts(t *testing.T) {
	wantFix := uintptr(68)
	if unsafe.Sizeof(uintptr(0)) == 8 {
		wantFix = 80
	}
	if got := unsafe.Sizeof(fixScreenInfo{}); got != wantFix {
		t.Errorf("fix_screeninfo size %d want %d", got, wantFix)
	}
	if got := unsafe.Sizeof(varScreenInfo{}); got != 160 {
		t.Errorf("var_screeninfo size %d want 160", got)
	}
}
