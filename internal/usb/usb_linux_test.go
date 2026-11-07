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

func TestTransportReadsHeaderThenPayloadAndSkipsZeroLengthPackets(t *testing.T) {
	dev, host := seqpacket(t)
	defer unix.Close(host)
	tr := newTransport(dev, dev, devNull(t), ffsChunk, ffsChunk, true)
	defer tr.Close()
	payload := bytes.Repeat([]byte{0xab}, 2*ffsChunk)
	raw := encode(t, wire.Message{Command: wire.WRTE, Arg0: 1, Arg1: 2, Data: payload})
	sendRecord(t, host, raw[:headerLen])
	sendRecord(t, host, raw[headerLen:headerLen+ffsChunk])
	sendRecord(t, host, raw[headerLen+ffsChunk:])
	sendRecord(t, host, nil)
	sendRecord(t, host, encode(t, wire.Message{Command: wire.OKAY, Arg0: 3, Arg1: 4}))
	m, err := tr.Read()
	if err != nil {
		t.Fatal(err)
	}
	if m.Command != wire.WRTE || !bytes.Equal(m.Data, payload) {
		t.Fatalf("first message %x len %d", m.Command, len(m.Data))
	}
	m, err = tr.Read()
	if err != nil {
		t.Fatal(err)
	}
	if m.Command != wire.OKAY || m.Arg0 != 3 || m.Arg1 != 4 {
		t.Fatalf("second message %+v", m)
	}
}

func TestTransportWritesHeaderAndPayloadAsSeparateTransfers(t *testing.T) {
	dev, host := seqpacket(t)
	defer unix.Close(host)
	tr := newTransport(dev, dev, devNull(t), ffsChunk, ffsChunk, true)
	defer tr.Close()
	payload := bytes.Repeat([]byte{7}, ffsChunk+100)
	go tr.Write(wire.Message{Command: wire.WRTE, Arg0: 1, Arg1: 2, Data: payload})
	buf := make([]byte, 4*ffsChunk)
	var sizes []int
	for _, want := range []int{headerLen, ffsChunk, 100} {
		n, err := unix.Read(host, buf)
		if err != nil {
			t.Fatal(err)
		}
		sizes = append(sizes, n)
		if n != want {
			t.Fatalf("record sizes %v, want 24, %d, 100", sizes, ffsChunk)
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
			tr := newTransport(rfd, wfd, devNull(t), androidChunk, androidChunk, kind == "pipe")
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
