package fbdev

import "unsafe"

const (
	_ = uint(unsafe.Sizeof(fixScreenInfo{}) - 80)
	_ = uint(80 - unsafe.Sizeof(fixScreenInfo{}))
)
