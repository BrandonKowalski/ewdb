package uinput

import (
	"encoding/binary"
	"strconv"
)

const (
	TimevalSize  = 2 * (strconv.IntSize / 8)
	KeyboardName = "ewdb keyboard"

	userDevSize = 1116
	setupSize   = 92
	maxNameSize = 80
	absCount    = 64
)

type ID struct {
	Bus, Vendor, Product, Version uint16
}

type AbsInfo struct {
	Value, Min, Max, Fuzz, Flat, Resolution int32
}

func EncodeEvent(timevalSize int, typ, code uint16, value int32) []byte {
	b := make([]byte, timevalSize+8)
	le := binary.LittleEndian
	le.PutUint16(b[timevalSize:], typ)
	le.PutUint16(b[timevalSize+2:], code)
	le.PutUint32(b[timevalSize+4:], uint32(value))
	return b
}

func putName(b []byte, name string) {
	if len(name) > maxNameSize-1 {
		name = name[:maxNameSize-1]
	}
	copy(b, name)
}

func putID(b []byte, id ID) {
	le := binary.LittleEndian
	le.PutUint16(b[0:], id.Bus)
	le.PutUint16(b[2:], id.Vendor)
	le.PutUint16(b[4:], id.Product)
	le.PutUint16(b[6:], id.Version)
}

func EncodeUserDev(name string, id ID, abs map[uint16]AbsInfo) []byte {
	b := make([]byte, userDevSize)
	putName(b, name)
	putID(b[maxNameSize:], id)
	le := binary.LittleEndian
	base := maxNameSize + 8 + 4
	for code, a := range abs {
		if code >= absCount {
			continue
		}
		i := 4 * int(code)
		le.PutUint32(b[base+i:], uint32(a.Max))
		le.PutUint32(b[base+absCount*4+i:], uint32(a.Min))
		le.PutUint32(b[base+2*absCount*4+i:], uint32(a.Fuzz))
		le.PutUint32(b[base+3*absCount*4+i:], uint32(a.Flat))
	}
	return b
}

func EncodeSetup(name string, id ID) []byte {
	b := make([]byte, setupSize)
	putID(b, id)
	putName(b[8:88], name)
	return b
}
