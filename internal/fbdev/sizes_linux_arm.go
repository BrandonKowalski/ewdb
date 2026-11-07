package fbdev

import "unsafe"

const (
	_ = uint(unsafe.Sizeof(fixScreenInfo{}) - 68)
	_ = uint(68 - unsafe.Sizeof(fixScreenInfo{}))
)
