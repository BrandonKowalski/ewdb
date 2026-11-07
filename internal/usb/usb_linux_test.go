package usb

import (
	"bytes"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"ewdb/internal/wire"
)

func seqpacket(t *testing.T) (int, int) {
	t.Helper()
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	return fds[0], fds[1]
}

func devNull(t *testing.T) int {
	t.Helper()
	fd, err := openDevNull()
	if err != nil {
		t.Fatal(err)
	}
	return fd
}

func encode(t *testing.T, m wire.Message) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := wire.Write(&b, m, false); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func sendRecord(t *testing.T, fd int, p []byte) {
	t.Helper()
	if _, err := unix.Write(fd, p); err != nil {
		t.Fatal(err)
	}
}

const chunk = 16384

func readWithin(t *testing.T, tr *transport) wire.Message {
	t.Helper()
	type result struct {
		m   wire.Message
		err error
	}
	c := make(chan result, 1)
	go func() {
		m, err := tr.Read()
		c <- result{m, err}
	}()
	select {
	case r := <-c:
		if r.err != nil {
			t.Fatal(r.err)
		}
		return r.m
	case <-time.After(2 * time.Second):
		t.Fatal("Read blocked: part of a host transfer was lost")
	}
	return wire.Message{}
}

func TestFFSTransportReadsEachHostTransferWithOneRead(t *testing.T) {
	dev, host := seqpacket(t)
	defer unix.Close(host)
	tr := newFFSTransport(dev, dev, devNull(t))
	defer tr.Close()
	payload := make([]byte, 3*chunk+100)
	for i := range payload {
		payload[i] = byte(i * 7)
	}
	raw := encode(t, wire.Message{Command: wire.WRTE, Arg0: 1, Arg1: 2, Data: payload})
	sendRecord(t, host, raw[:headerLen])
	sendRecord(t, host, raw[headerLen:])
	sendRecord(t, host, nil)
	sendRecord(t, host, encode(t, wire.Message{Command: wire.OKAY, Arg0: 3, Arg1: 4}))
	m := readWithin(t, tr)
	if m.Command != wire.WRTE || !bytes.Equal(m.Data, payload) {
		t.Fatalf("first message %x len %d", m.Command, len(m.Data))
	}
	m = readWithin(t, tr)
	if m.Command != wire.OKAY || m.Arg0 != 3 || m.Arg1 != 4 {
		t.Fatalf("second message %+v", m)
	}
}

func TestFFSTransportReadsPayloadSplitAcrossTransfers(t *testing.T) {
	dev, host := seqpacket(t)
	defer unix.Close(host)
	tr := newFFSTransport(dev, dev, devNull(t))
	defer tr.Close()
	payload := bytes.Repeat([]byte{0xab}, 2*chunk)
	raw := encode(t, wire.Message{Command: wire.WRTE, Arg0: 1, Arg1: 2, Data: payload})
	sendRecord(t, host, raw[:headerLen])
	sendRecord(t, host, raw[headerLen:headerLen+chunk])
	sendRecord(t, host, raw[headerLen+chunk:])
	sendRecord(t, host, nil)
	sendRecord(t, host, encode(t, wire.Message{Command: wire.OKAY, Arg0: 3, Arg1: 4}))
	m := readWithin(t, tr)
	if m.Command != wire.WRTE || !bytes.Equal(m.Data, payload) {
		t.Fatalf("first message %x len %d", m.Command, len(m.Data))
	}
	m = readWithin(t, tr)
	if m.Command != wire.OKAY || m.Arg0 != 3 || m.Arg1 != 4 {
		t.Fatalf("second message %+v", m)
	}
}

func TestFFSTransportWritesHeaderAndPayloadAsOneTransferEach(t *testing.T) {
	dev, host := seqpacket(t)
	defer unix.Close(host)
	tr := newFFSTransport(dev, dev, devNull(t))
	defer tr.Close()
	payload := bytes.Repeat([]byte{7}, 3*chunk+100)
	go tr.Write(wire.Message{Command: wire.WRTE, Arg0: 1, Arg1: 2, Data: payload})
	buf := make([]byte, 8*chunk)
	var sizes []int
	for _, want := range []int{headerLen, len(payload)} {
		n, err := unix.Read(host, buf)
		if err != nil {
			t.Fatal(err)
		}
		sizes = append(sizes, n)
		if n != want {
			t.Fatalf("record sizes %v, want %d, %d", sizes, headerLen, len(payload))
		}
	}
}

func TestCloseUnblocksPendingReadAndFinishes(t *testing.T) {
	for _, kind := range []string{"seqpacket", "pipe"} {
		t.Run(kind, func(t *testing.T) {
			var rfd, wfd, peer int
			if kind == "pipe" {
				var p [2]int
				if err := unix.Pipe2(p[:], unix.O_CLOEXEC); err != nil {
					t.Fatal(err)
				}
				rfd, peer = p[0], p[1]
				wfd = rfd
			} else {
				rfd, peer = seqpacket(t)
				wfd = rfd
			}
			defer unix.Close(peer)
			tr := newTransport(rfd, wfd, devNull(t), androidChunk, androidChunk)
			errc := make(chan error, 1)
			go func() {
				_, err := tr.Read()
				errc <- err
			}()
			time.Sleep(100 * time.Millisecond)
			tr.Close()
			tr.Close()
			select {
			case err := <-errc:
				if err != errClosed {
					t.Fatalf("Read after Close = %v", err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("Read still blocked after Close")
			}
			select {
			case <-tr.done:
			case <-time.After(2 * time.Second):
				t.Fatal("transport never finished")
			}
			if _, err := tr.Read(); err != errClosed {
				t.Fatalf("Read on closed transport = %v", err)
			}
			if err := tr.Write(wire.Message{Command: wire.OKAY}); err != errClosed {
				t.Fatalf("Write on closed transport = %v", err)
			}
		})
	}
}
