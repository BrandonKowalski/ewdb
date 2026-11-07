package usb

import "encoding/binary"

const (
	descriptorsMagic   = 1
	stringsMagic       = 2
	descriptorsMagicV2 = 3

	hasFSDesc = 1
	hasHSDesc = 2
	hasSSDesc = 4

	dtInterface    = 0x04
	dtEndpoint     = 0x05
	dtSSEndpointCp = 0x30

	adbClass    = 0xff
	adbSubclass = 0x42
	adbProtocol = 1

	epOut       = 0x01
	epIn        = 0x82
	xferBulk    = 0x02
	ssMaxBurst  = 4
	maxPacketFS = 64
	maxPacketHS = 512
	maxPacketSS = 1024

	langEnUS       = 0x0409
	interfaceLabel = "ADB Interface"
)

func interfaceDescriptor() []byte {
	return []byte{9, dtInterface, 0, 0, 2, adbClass, adbSubclass, adbProtocol, 1}
}

func endpointDescriptor(addr byte, maxPacket uint16) []byte {
	b := []byte{7, dtEndpoint, addr, xferBulk}
	b = binary.LittleEndian.AppendUint16(b, maxPacket)
	return append(b, 0)
}

func ssCompanionDescriptor() []byte {
	return []byte{6, dtSSEndpointCp, ssMaxBurst, 0, 0, 0}
}

func functionDescriptors(maxPacket uint16) []byte {
	b := interfaceDescriptor()
	b = append(b, endpointDescriptor(epOut, maxPacket)...)
	return append(b, endpointDescriptor(epIn, maxPacket)...)
}

func superSpeedDescriptors() []byte {
	b := interfaceDescriptor()
	b = append(b, endpointDescriptor(epOut, maxPacketSS)...)
	b = append(b, ssCompanionDescriptor()...)
	b = append(b, endpointDescriptor(epIn, maxPacketSS)...)
	return append(b, ssCompanionDescriptor()...)
}

func withHeader(magic uint32, body []byte) []byte {
	b := binary.LittleEndian.AppendUint32(nil, magic)
	b = binary.LittleEndian.AppendUint32(b, uint32(8+len(body)))
	return append(b, body...)
}

func DescriptorsV2() []byte {
	b := binary.LittleEndian.AppendUint32(nil, hasFSDesc|hasHSDesc|hasSSDesc)
	b = binary.LittleEndian.AppendUint32(b, 3)
	b = binary.LittleEndian.AppendUint32(b, 3)
	b = binary.LittleEndian.AppendUint32(b, 5)
	b = append(b, functionDescriptors(maxPacketFS)...)
	b = append(b, functionDescriptors(maxPacketHS)...)
	b = append(b, superSpeedDescriptors()...)
	return withHeader(descriptorsMagicV2, b)
}

func DescriptorsV1() []byte {
	b := binary.LittleEndian.AppendUint32(nil, 3)
	b = binary.LittleEndian.AppendUint32(b, 3)
	b = append(b, functionDescriptors(maxPacketFS)...)
	b = append(b, functionDescriptors(maxPacketHS)...)
	return withHeader(descriptorsMagic, b)
}

func Strings() []byte {
	b := binary.LittleEndian.AppendUint32(nil, 1)
	b = binary.LittleEndian.AppendUint32(b, 1)
	b = binary.LittleEndian.AppendUint16(b, langEnUS)
	b = append(b, interfaceLabel...)
	b = append(b, 0)
	return withHeader(stringsMagic, b)
}
