package drm

import (
	"fmt"
	"unsafe"

	"ewdb/internal/capture"
)

const (
	iocWrite = 1
	iocRead  = 2

	drmBase = 'd'

	clientCapUniversalPlanes = 2
	connectorConnected       = 1
	objectPlane              = 0xeeeeeeee
	planeTypePrimary         = 1
	fbModifiers              = 1 << 1
	modifierLinear           = 0
	dmaBufSyncRead           = 1 << 0
	dmaBufSyncEnd            = 1 << 2
	propNameLen              = 32
)

func ioc(dir, typ, nr, size uintptr) uint32 {
	return uint32(dir<<30 | size<<16 | typ<<8 | nr)
}

type modeCardRes struct {
	fbIDPtr        uint64
	crtcIDPtr      uint64
	connectorIDPtr uint64
	encoderIDPtr   uint64
	countFBs       uint32
	countCRTCs     uint32
	countConns     uint32
	countEncoders  uint32
	minWidth       uint32
	maxWidth       uint32
	minHeight      uint32
	maxHeight      uint32
}

type modeGetConnector struct {
	encodersPtr     uint64
	modesPtr        uint64
	propsPtr        uint64
	propValuesPtr   uint64
	countModes      uint32
	countProps      uint32
	countEncoders   uint32
	encoderID       uint32
	connectorID     uint32
	connectorType   uint32
	connectorTypeID uint32
	connection      uint32
	mmWidth         uint32
	mmHeight        uint32
	subpixel        uint32
	pad             uint32
}

type modeGetEncoder struct {
	encoderID      uint32
	encoderType    uint32
	crtcID         uint32
	possibleCRTCs  uint32
	possibleClones uint32
}

type modeGetPlaneRes struct {
	planeIDPtr  uint64
	countPlanes uint32
	pad         uint32
}

type modeGetPlane struct {
	planeID          uint32
	crtcID           uint32
	fbID             uint32
	possibleCRTCs    uint32
	gammaSize        uint32
	countFormatTypes uint32
	formatTypePtr    uint64
}

type modeFBCmd struct {
	fbID   uint32
	width  uint32
	height uint32
	pitch  uint32
	bpp    uint32
	depth  uint32
	handle uint32
}

type modeFBCmd2 struct {
	fbID        uint32
	width       uint32
	height      uint32
	pixelFormat uint32
	flags       uint32
	handles     [4]uint32
	pitches     [4]uint32
	offsets     [4]uint32
	pad         uint32
	modifier    [4]uint64
}

type primeHandle struct {
	handle uint32
	flags  uint32
	fd     int32
}

type modeMapDumb struct {
	handle uint32
	pad    uint32
	offset uint64
}

type gemClose struct {
	handle uint32
	pad    uint32
}

type setClientCap struct {
	capability uint64
	value      uint64
}

type modeObjGetProperties struct {
	propsPtr      uint64
	propValuesPtr uint64
	countProps    uint32
	objID         uint32
	objType       uint32
	pad           uint32
}

type modeGetProperty struct {
	valuesPtr    uint64
	enumBlobPtr  uint64
	propID       uint32
	flags        uint32
	name         [propNameLen]byte
	countValues  uint32
	countEnumBlb uint32
}

type dmaBufSync struct {
	flags uint64
}

var (
	ioctlGetResources      = ioc(iocRead|iocWrite, drmBase, 0xA0, unsafe.Sizeof(modeCardRes{}))
	ioctlGetEncoder        = ioc(iocRead|iocWrite, drmBase, 0xA6, unsafe.Sizeof(modeGetEncoder{}))
	ioctlGetConnector      = ioc(iocRead|iocWrite, drmBase, 0xA7, unsafe.Sizeof(modeGetConnector{}))
	ioctlGetProperty       = ioc(iocRead|iocWrite, drmBase, 0xAA, unsafe.Sizeof(modeGetProperty{}))
	ioctlGetFB             = ioc(iocRead|iocWrite, drmBase, 0xAD, unsafe.Sizeof(modeFBCmd{}))
	ioctlMapDumb           = ioc(iocRead|iocWrite, drmBase, 0xB3, unsafe.Sizeof(modeMapDumb{}))
	ioctlGetPlaneResources = ioc(iocRead|iocWrite, drmBase, 0xB5, unsafe.Sizeof(modeGetPlaneRes{}))
	ioctlGetPlane          = ioc(iocRead|iocWrite, drmBase, 0xB6, unsafe.Sizeof(modeGetPlane{}))
	ioctlObjGetProperties  = ioc(iocRead|iocWrite, drmBase, 0xB9, unsafe.Sizeof(modeObjGetProperties{}))
	ioctlGetFB2            = ioc(iocRead|iocWrite, drmBase, 0xCE, unsafe.Sizeof(modeFBCmd2{}))
	ioctlGemClose          = ioc(iocWrite, drmBase, 0x09, unsafe.Sizeof(gemClose{}))
	ioctlSetClientCap      = ioc(iocWrite, drmBase, 0x0D, unsafe.Sizeof(setClientCap{}))
	ioctlPrimeHandleToFD   = ioc(iocRead|iocWrite, drmBase, 0x2D, unsafe.Sizeof(primeHandle{}))
	ioctlDmaBufSync        = ioc(iocWrite, 'b', 0, unsafe.Sizeof(dmaBufSync{}))
)

const (
	_ = uint(unsafe.Sizeof(modeCardRes{}) - 64)
	_ = uint(64 - unsafe.Sizeof(modeCardRes{}))
	_ = uint(unsafe.Sizeof(modeGetConnector{}) - 80)
	_ = uint(80 - unsafe.Sizeof(modeGetConnector{}))
	_ = uint(unsafe.Sizeof(modeGetPlaneRes{}) - 16)
	_ = uint(16 - unsafe.Sizeof(modeGetPlaneRes{}))
	_ = uint(unsafe.Sizeof(modeGetPlane{}) - 32)
	_ = uint(32 - unsafe.Sizeof(modeGetPlane{}))
	_ = uint(unsafe.Sizeof(modeFBCmd2{}) - 104)
	_ = uint(104 - unsafe.Sizeof(modeFBCmd2{}))
	_ = uint(unsafe.Sizeof(modeObjGetProperties{}) - 32)
	_ = uint(32 - unsafe.Sizeof(modeObjGetProperties{}))
	_ = uint(unsafe.Sizeof(modeGetProperty{}) - 64)
	_ = uint(64 - unsafe.Sizeof(modeGetProperty{}))
	_ = uint(unsafe.Sizeof(modeGetEncoder{}) - 20)
	_ = uint(20 - unsafe.Sizeof(modeGetEncoder{}))
	_ = uint(unsafe.Sizeof(modeFBCmd{}) - 28)
	_ = uint(28 - unsafe.Sizeof(modeFBCmd{}))
	_ = uint(unsafe.Sizeof(primeHandle{}) - 12)
	_ = uint(12 - unsafe.Sizeof(primeHandle{}))
	_ = uint(unsafe.Sizeof(modeMapDumb{}) - 16)
	_ = uint(16 - unsafe.Sizeof(modeMapDumb{}))
	_ = uint(unsafe.Sizeof(gemClose{}) - 8)
	_ = uint(8 - unsafe.Sizeof(gemClose{}))
	_ = uint(unsafe.Sizeof(setClientCap{}) - 16)
	_ = uint(16 - unsafe.Sizeof(setClientCap{}))
	_ = uint(unsafe.Sizeof(dmaBufSync{}) - 8)
	_ = uint(8 - unsafe.Sizeof(dmaBufSync{}))
)

var connectorTypeNames = []string{
	"Unknown", "VGA", "DVI-I", "DVI-D", "DVI-A", "Composite", "SVIDEO", "LVDS",
	"Component", "DIN", "DP", "HDMI-A", "HDMI-B", "TV", "eDP", "Virtual",
	"DSI", "DPI", "Writeback", "SPI", "USB",
}

func ConnectorName(typ, id uint32) string {
	name := "Unknown"
	if int(typ) < len(connectorTypeNames) {
		name = connectorTypeNames[typ]
	}
	return fmt.Sprintf("%s-%d", name, id)
}

func legacyFormat(bpp, depth uint32) uint32 {
	switch {
	case bpp == 32 && depth == 24:
		return capture.FourCC('X', 'R', '2', '4')
	case bpp == 32 && depth == 32:
		return capture.FourCC('A', 'R', '2', '4')
	case bpp == 24 && depth == 24:
		return capture.FourCC('R', 'G', '2', '4')
	case bpp == 16 && depth == 16:
		return capture.FourCC('R', 'G', '1', '6')
	case bpp == 16 && depth == 15:
		return capture.FourCC('X', 'R', '1', '5')
	}
	return 0
}
