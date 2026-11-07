package wire

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"

	"ewdb/internal/config"
)

const (
	CNXN = 0x4e584e43
	OPEN = 0x4e45504f
	OKAY = 0x59414b4f
	CLSE = 0x45534c43
	WRTE = 0x45545257
	AUTH = 0x48545541
	STLS = 0x534c5453
	SYNC = 0x434e5953

	Version    = 0x01000001
	VersionMin = 0x01000000
	MaxPayload = 1048576

	headerSize = 24
)

var Features = []string{"shell_v2", "stat_v2", "ls_v2", "fixed_push_mkdir"}

type Message struct {
	Command, Arg0, Arg1 uint32
	Data                []byte
}

func Read(r io.Reader, max uint32) (Message, error) {
	var h [headerSize]byte
	if _, err := io.ReadFull(r, h[:]); err != nil {
		return Message{}, err
	}
	m := Message{
		Command: binary.LittleEndian.Uint32(h[0:]),
		Arg0:    binary.LittleEndian.Uint32(h[4:]),
		Arg1:    binary.LittleEndian.Uint32(h[8:]),
	}
	length := binary.LittleEndian.Uint32(h[12:])
	magic := binary.LittleEndian.Uint32(h[20:])
	if magic != m.Command^0xffffffff {
		return Message{}, errors.New("wire: bad magic")
	}
	if length > max {
		return Message{}, fmt.Errorf("wire: payload %d exceeds %d", length, max)
	}
	if length > 0 {
		m.Data = make([]byte, length)
		if _, err := io.ReadFull(r, m.Data); err != nil {
			return Message{}, err
		}
	}
	return m, nil
}

func Write(w io.Writer, m Message, checksum bool) error {
	buf := make([]byte, headerSize+len(m.Data))
	binary.LittleEndian.PutUint32(buf[0:], m.Command)
	binary.LittleEndian.PutUint32(buf[4:], m.Arg0)
	binary.LittleEndian.PutUint32(buf[8:], m.Arg1)
	binary.LittleEndian.PutUint32(buf[12:], uint32(len(m.Data)))
	if checksum {
		var sum uint32
		for _, b := range m.Data {
			sum += uint32(b)
		}
		binary.LittleEndian.PutUint32(buf[16:], sum)
	}
	binary.LittleEndian.PutUint32(buf[20:], m.Command^0xffffffff)
	copy(buf[headerSize:], m.Data)
	_, err := w.Write(buf)
	return err
}

func Banner(c *config.Config, features []string) []byte {
	return []byte("device::ro.product.name=" + c.Name +
		";ro.product.model=" + c.Model +
		";ro.product.device=" + c.Device +
		";features=" + strings.Join(features, ","))
}
