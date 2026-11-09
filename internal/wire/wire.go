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

	HeaderSize = 24

	pooledMin = 4096
)

var payloads = make(chan []byte, 2)

func payload() []byte {
	select {
	case b := <-payloads:
		return b
	default:
		return make([]byte, MaxPayload)
	}
}

func Release(b []byte) {
	if cap(b) != MaxPayload {
		return
	}
	select {
	case payloads <- b[:MaxPayload]:
	default:
	}
}

var Features = []string{"shell_v2", "stat_v2", "ls_v2", "fixed_push_mkdir"}

type Message struct {
	Command, Arg0, Arg1 uint32
	Data                []byte
}

func Read(r io.Reader, max uint32) (Message, error) {
	var h [HeaderSize]byte
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
	if length >= pooledMin && length <= MaxPayload {
		m.Data = payload()[:length]
	} else if length > 0 {
		m.Data = make([]byte, length)
	}
	if length > 0 {
		if _, err := io.ReadFull(r, m.Data); err != nil {
			Release(m.Data)
			return Message{}, err
		}
	}
	return m, nil
}

func Header(m Message, checksum bool) [HeaderSize]byte {
	var h [HeaderSize]byte
	binary.LittleEndian.PutUint32(h[0:], m.Command)
	binary.LittleEndian.PutUint32(h[4:], m.Arg0)
	binary.LittleEndian.PutUint32(h[8:], m.Arg1)
	binary.LittleEndian.PutUint32(h[12:], uint32(len(m.Data)))
	if checksum {
		var sum uint32
		for _, b := range m.Data {
			sum += uint32(b)
		}
		binary.LittleEndian.PutUint32(h[16:], sum)
	}
	binary.LittleEndian.PutUint32(h[20:], m.Command^0xffffffff)
	return h
}

func Write(w io.Writer, m Message, checksum bool) error {
	h := Header(m, checksum)
	buf := make([]byte, HeaderSize+len(m.Data))
	copy(buf, h[:])
	copy(buf[HeaderSize:], m.Data)
	_, err := w.Write(buf)
	return err
}

func Banner(c *config.Config, features []string) []byte {
	return []byte("device::ro.product.name=" + c.Name +
		";ro.product.model=" + c.Model +
		";ro.product.device=" + c.Device +
		";features=" + strings.Join(features, ","))
}
