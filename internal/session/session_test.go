package session

import (
	"bytes"
	"context"
	"errors"
	"io"
	"runtime"
	"sync"
	"testing"
	"time"

	"ewdb/internal/config"
	"ewdb/internal/wire"
)

type pipe struct {
	in       chan wire.Message
	out      chan wire.Message
	done     chan struct{}
	once     sync.Once
	mu       sync.Mutex
	checksum []bool
}

func newPipe() *pipe {
	return &pipe{in: make(chan wire.Message), out: make(chan wire.Message), done: make(chan struct{})}
}

func (p *pipe) Read() (wire.Message, error) {
	select {
	case m := <-p.in:
		return m, nil
	case <-p.done:
		return wire.Message{}, io.EOF
	}
}

func (p *pipe) Write(m wire.Message) error {
	select {
	case p.out <- m:
		return nil
	case <-p.done:
		return io.ErrClosedPipe
	}
}

func (p *pipe) Close() error {
	p.once.Do(func() { close(p.done) })
	return nil
}

func (p *pipe) SetChecksum(on bool) {
	p.mu.Lock()
	p.checksum = append(p.checksum, on)
	p.mu.Unlock()
}

type harness struct {
	t    *testing.T
	p    *pipe
	reg  *Registry
	errc chan error
}

func testConfig() *config.Config {
	return &config.Config{Name: "dsos", Model: "RG DS Plus", Device: "rgds-plus"}
}

func start(t *testing.T, reg *Registry) *harness {
	t.Helper()
	before := runtime.NumGoroutine()
	h := &harness{t: t, p: newPipe(), reg: reg, errc: make(chan error, 1)}
	go func() { h.errc <- Serve(context.Background(), h.p, testConfig(), reg) }()
	t.Cleanup(func() {
		h.p.Close()
		select {
		case <-h.errc:
		case <-time.After(2 * time.Second):
			t.Error("Serve did not return")
		}
		deadline := time.Now().Add(2 * time.Second)
		for runtime.NumGoroutine() > before && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if n := runtime.NumGoroutine(); n > before {
			buf := make([]byte, 1<<16)
			t.Errorf("goroutines: %d before, %d after\n%s", before, n, buf[:runtime.Stack(buf, true)])
		}
	})
	return h
}

func (h *harness) send(m wire.Message) {
	h.t.Helper()
	select {
	case h.p.in <- m:
	case <-time.After(2 * time.Second):
		h.t.Fatalf("session did not accept %08x", m.Command)
	}
}

func (h *harness) expect() wire.Message {
	h.t.Helper()
	select {
	case m := <-h.p.out:
		return m
	case <-time.After(2 * time.Second):
		h.t.Fatal("no message from session")
	}
	return wire.Message{}
}

func (h *harness) expectMsg(cmd, arg0, arg1 uint32, data string) wire.Message {
	h.t.Helper()
	m := h.expect()
	if m.Command != cmd || m.Arg0 != arg0 || m.Arg1 != arg1 || string(m.Data) != data {
		h.t.Fatalf("got %08x(%d,%d,%q) want %08x(%d,%d,%q)", m.Command, m.Arg0, m.Arg1, m.Data, cmd, arg0, arg1, data)
	}
	return m
}

func (h *harness) expectNone() {
	h.t.Helper()
	select {
	case m := <-h.p.out:
		h.t.Fatalf("unexpected %08x(%d,%d,%q)", m.Command, m.Arg0, m.Arg1, m.Data)
	case <-time.After(150 * time.Millisecond):
	}
}

func (h *harness) connect(maxdata uint32) {
	h.t.Helper()
	h.send(wire.Message{Command: wire.CNXN, Arg0: wire.Version, Arg1: maxdata, Data: []byte("host::\x00")})
	m := h.expect()
	if m.Command != wire.CNXN {
		h.t.Fatalf("got %08x want CNXN", m.Command)
	}
}

func (h *harness) open(local uint32, name string) uint32 {
	h.t.Helper()
	h.send(wire.Message{Command: wire.OPEN, Arg0: local, Data: []byte(name + "\x00")})
	m := h.expect()
	if m.Command != wire.OKAY || m.Arg1 != local || m.Arg0 == 0 {
		h.t.Fatalf("got %08x(%d,%d) want OKAY(x,%d)", m.Command, m.Arg0, m.Arg1, local)
	}
	return m.Arg0
}

func echo(ctx context.Context, s *Stream, arg string) {
	buf := make([]byte, 64)
	for {
		n, err := s.Read(buf)
		if n > 0 {
			if _, werr := s.Write(buf[:n]); werr != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

func TestHandshake(t *testing.T) {
	h := start(t, &Registry{})
	h.send(wire.Message{Command: wire.CNXN, Arg0: wire.Version, Arg1: 4096, Data: []byte("host::\x00")})
	m := h.expect()
	want := wire.Banner(testConfig(), wire.Features)
	if m.Command != wire.CNXN || m.Arg0 != 0x01000001 || m.Arg1 != 4096 || !bytes.Equal(m.Data, want) {
		t.Fatalf("got %08x(%x,%d,%q)", m.Command, m.Arg0, m.Arg1, m.Data)
	}
	h.p.mu.Lock()
	defer h.p.mu.Unlock()
	if len(h.p.checksum) != 1 || h.p.checksum[0] {
		t.Fatalf("checksum calls %v", h.p.checksum)
	}
}

func TestHandshakeCapsMaxdataAndChecksumsOldHosts(t *testing.T) {
	h := start(t, &Registry{})
	h.send(wire.Message{Command: wire.CNXN, Arg0: wire.VersionMin, Arg1: 4 * wire.MaxPayload})
	h.expectMsg(wire.CNXN, wire.Version, wire.MaxPayload, string(wire.Banner(testConfig(), wire.Features)))
	h.p.mu.Lock()
	defer h.p.mu.Unlock()
	if len(h.p.checksum) != 1 || !h.p.checksum[0] {
		t.Fatalf("checksum calls %v", h.p.checksum)
	}
}

func TestIgnoresEverythingBeforeConnect(t *testing.T) {
	reg := &Registry{}
	reg.Handle("echo:", echo)
	h := start(t, reg)
	h.send(wire.Message{Command: wire.OPEN, Arg0: 1, Data: []byte("echo:\x00")})
	h.send(wire.Message{Command: wire.AUTH, Arg0: 1})
	h.expectNone()
	h.connect(4096)
	h.send(wire.Message{Command: wire.STLS, Arg0: 1})
	h.send(wire.Message{Command: wire.AUTH, Arg0: 1})
	h.expectNone()
}

func TestUnknownServiceGetsClose(t *testing.T) {
	h := start(t, &Registry{})
	h.connect(4096)
	h.send(wire.Message{Command: wire.OPEN, Arg0: 7, Data: []byte("nope:\x00")})
	h.expectMsg(wire.CLSE, 0, 7, "")
}

func TestLongestPrefixWins(t *testing.T) {
	got := make(chan string, 1)
	reg := &Registry{}
	reg.Handle("shell", func(ctx context.Context, s *Stream, arg string) { got <- "shell|" + arg })
	reg.Handle("shell,v2,", func(ctx context.Context, s *Stream, arg string) { got <- "v2|" + arg })
	h := start(t, reg)
	h.connect(4096)
	ours := h.open(3, "shell,v2,raw:ls")
	h.expectMsg(wire.CLSE, ours, 3, "")
	if g := <-got; g != "v2|raw:ls" {
		t.Fatalf("got %q", g)
	}
	ours = h.open(4, "shell:ls")
	h.expectMsg(wire.CLSE, ours, 4, "")
	if g := <-got; g != "shell|:ls" {
		t.Fatalf("got %q", g)
	}
}

func TestEchoServiceFlowControl(t *testing.T) {
	reg := &Registry{}
	reg.Handle("echo:", echo)
	h := start(t, reg)
	h.connect(4096)
	ours := h.open(5, "echo:")
	h.send(wire.Message{Command: wire.WRTE, Arg0: 5, Arg1: ours, Data: []byte("hi")})
	h.expectMsg(wire.OKAY, ours, 5, "")
	h.expectMsg(wire.WRTE, ours, 5, "hi")
	h.send(wire.Message{Command: wire.WRTE, Arg0: 5, Arg1: ours, Data: []byte("yo")})
	h.expectNone()
	h.send(wire.Message{Command: wire.OKAY, Arg0: 5, Arg1: ours})
	h.expectMsg(wire.OKAY, ours, 5, "")
	h.expectMsg(wire.WRTE, ours, 5, "yo")
	h.send(wire.Message{Command: wire.OKAY, Arg0: 5, Arg1: ours})
	h.send(wire.Message{Command: wire.CLSE, Arg0: 5, Arg1: ours})
	h.expectNone()
}

func TestHostWriteWaitsForRead(t *testing.T) {
	release := make(chan struct{})
	got := make(chan string, 1)
	reg := &Registry{}
	reg.Handle("slow:", func(ctx context.Context, s *Stream, arg string) {
		<-release
		buf := make([]byte, 2)
		n, _ := io.ReadFull(s, buf)
		got <- string(buf[:n])
		<-ctx.Done()
	})
	reg.Handle("echo:", echo)
	h := start(t, reg)
	h.connect(4096)
	slow := h.open(1, "slow:")
	h.send(wire.Message{Command: wire.WRTE, Arg0: 1, Arg1: slow, Data: []byte("ab")})
	h.expectNone()
	fast := h.open(2, "echo:")
	h.send(wire.Message{Command: wire.WRTE, Arg0: 2, Arg1: fast, Data: []byte("x")})
	h.expectMsg(wire.OKAY, fast, 2, "")
	h.expectMsg(wire.WRTE, fast, 2, "x")
	close(release)
	h.expectMsg(wire.OKAY, slow, 1, "")
	if g := <-got; g != "ab" {
		t.Fatalf("got %q", g)
	}
	h.send(wire.Message{Command: wire.CLSE, Arg0: 1, Arg1: slow})
	h.send(wire.Message{Command: wire.CLSE, Arg0: 2, Arg1: fast})
}

func TestWriteSplitsAtMaxdata(t *testing.T) {
	payload := bytes.Repeat([]byte("0123456789"), 1000)
	reg := &Registry{}
	reg.Handle("big:", func(ctx context.Context, s *Stream, arg string) {
		s.Write(payload)
	})
	h := start(t, reg)
	h.connect(4096)
	ours := h.open(9, "big:")
	var all []byte
	for _, size := range []int{4096, 4096, 1808} {
		m := h.expect()
		if m.Command != wire.WRTE || m.Arg0 != ours || m.Arg1 != 9 || len(m.Data) != size {
			t.Fatalf("got %08x(%d,%d) len %d want WRTE len %d", m.Command, m.Arg0, m.Arg1, len(m.Data), size)
		}
		all = append(all, m.Data...)
		h.send(wire.Message{Command: wire.OKAY, Arg0: 9, Arg1: ours})
	}
	if !bytes.Equal(all, payload) {
		t.Fatal("payload mismatch")
	}
	h.expectMsg(wire.CLSE, ours, 9, "")
}

func TestHostCloseCancelsHandler(t *testing.T) {
	returned := make(chan struct{})
	reg := &Registry{}
	reg.Handle("wait:", func(ctx context.Context, s *Stream, arg string) {
		<-ctx.Done()
		close(returned)
	})
	h := start(t, reg)
	h.connect(4096)
	ours := h.open(2, "wait:")
	h.send(wire.Message{Command: wire.CLSE, Arg0: 2, Arg1: ours})
	select {
	case <-returned:
	case <-time.After(time.Second):
		t.Fatal("handler did not return")
	}
	h.expectNone()
}

func TestHostCloseUnblocksWriter(t *testing.T) {
	result := make(chan error, 1)
	reg := &Registry{}
	reg.Handle("w:", func(ctx context.Context, s *Stream, arg string) {
		_, err := s.Write(make([]byte, 8192))
		result <- err
	})
	h := start(t, reg)
	h.connect(4096)
	ours := h.open(2, "w:")
	h.expectMsg(wire.WRTE, ours, 2, string(make([]byte, 4096)))
	h.send(wire.Message{Command: wire.CLSE, Arg0: 2, Arg1: ours})
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("write succeeded after close")
		}
	case <-time.After(time.Second):
		t.Fatal("writer still blocked")
	}
	h.expectNone()
}

func TestSessionResetClosesStreams(t *testing.T) {
	var wg sync.WaitGroup
	reg := &Registry{}
	reg.Handle("wait:", func(ctx context.Context, s *Stream, arg string) {
		defer wg.Done()
		<-ctx.Done()
	})
	reg.Handle("read:", func(ctx context.Context, s *Stream, arg string) {
		defer wg.Done()
		io.ReadAll(s)
	})
	p := newPipe()
	before := runtime.NumGoroutine()
	errc := make(chan error, 1)
	go func() { errc <- Serve(context.Background(), p, testConfig(), reg) }()
	h := &harness{t: t, p: p, reg: reg, errc: errc}
	h.connect(4096)
	wg.Add(3)
	h.open(1, "wait:")
	h.open(2, "wait:")
	h.open(3, "read:")
	p.Close()
	select {
	case err := <-errc:
		if err == nil {
			t.Fatal("Serve returned nil")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not return")
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("handlers still running")
	}
	deadline := time.Now().Add(2 * time.Second)
	for runtime.NumGoroutine() > before && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := runtime.NumGoroutine(); n > before {
		t.Fatalf("goroutines: %d before, %d after", before, n)
	}
}

func TestServeReturnsOnContextCancel(t *testing.T) {
	reg := &Registry{}
	reg.Handle("wait:", func(ctx context.Context, s *Stream, arg string) { <-ctx.Done() })
	p := newPipe()
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { errc <- Serve(ctx, p, testConfig(), reg) }()
	h := &harness{t: t, p: p, reg: reg, errc: errc}
	h.connect(4096)
	h.open(1, "wait:")
	cancel()
	select {
	case err := <-errc:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not return")
	}
}

func TestNewConnectResetsSession(t *testing.T) {
	cancelled := make(chan struct{})
	reg := &Registry{}
	reg.Handle("wait:", func(ctx context.Context, s *Stream, arg string) {
		<-ctx.Done()
		s.Write([]byte("late"))
		close(cancelled)
	})
	reg.Handle("echo:", echo)
	h := start(t, reg)
	h.connect(4096)
	h.open(1, "wait:")
	h.connect(4096)
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("old handler not cancelled")
	}
	ours := h.open(1, "echo:")
	h.send(wire.Message{Command: wire.WRTE, Arg0: 1, Arg1: ours, Data: []byte("z")})
	h.expectMsg(wire.OKAY, ours, 1, "")
	h.expectMsg(wire.WRTE, ours, 1, "z")
	h.send(wire.Message{Command: wire.CLSE, Arg0: 1, Arg1: ours})
	h.expectNone()
}

func TestDialOpensToHost(t *testing.T) {
	result := make(chan string, 1)
	reg := &Registry{}
	reg.Handle("rev:", func(ctx context.Context, s *Stream, arg string) {
		st, err := s.Session().Dial(ctx, "tcp:9000")
		if err != nil {
			result <- err.Error()
			return
		}
		st.Write([]byte("ping"))
		buf := make([]byte, 4)
		n, _ := io.ReadFull(st, buf)
		st.Close()
		result <- string(buf[:n])
	})
	h := start(t, reg)
	h.connect(4096)
	h.open(1, "rev:")
	m := h.expect()
	if m.Command != wire.OPEN || m.Arg1 != 0 || m.Arg0 == 0 || string(m.Data) != "tcp:9000\x00" {
		t.Fatalf("got %08x(%d,%d,%q)", m.Command, m.Arg0, m.Arg1, m.Data)
	}
	ours := m.Arg0
	h.send(wire.Message{Command: wire.OKAY, Arg0: 40, Arg1: ours})
	h.expectMsg(wire.WRTE, ours, 40, "ping")
	h.send(wire.Message{Command: wire.OKAY, Arg0: 40, Arg1: ours})
	h.send(wire.Message{Command: wire.WRTE, Arg0: 40, Arg1: ours, Data: []byte("pong")})
	h.expectMsg(wire.OKAY, ours, 40, "")
	h.expectMsg(wire.CLSE, ours, 40, "")
	if r := <-result; r != "pong" {
		t.Fatalf("got %q", r)
	}
	m = h.expect()
	if m.Command != wire.CLSE || m.Arg1 != 1 {
		t.Fatalf("got %08x(%d,%d)", m.Command, m.Arg0, m.Arg1)
	}
}

func TestDialRefused(t *testing.T) {
	result := make(chan error, 1)
	reg := &Registry{}
	reg.Handle("rev:", func(ctx context.Context, s *Stream, arg string) {
		_, err := s.Session().Dial(ctx, "tcp:9001")
		result <- err
	})
	h := start(t, reg)
	h.connect(4096)
	h.open(1, "rev:")
	m := h.expect()
	if m.Command != wire.OPEN {
		t.Fatalf("got %08x", m.Command)
	}
	h.send(wire.Message{Command: wire.CLSE, Arg0: 0, Arg1: m.Arg0})
	if err := <-result; err == nil {
		t.Fatal("dial succeeded")
	}
	m = h.expect()
	if m.Command != wire.CLSE || m.Arg1 != 1 {
		t.Fatalf("got %08x(%d,%d)", m.Command, m.Arg0, m.Arg1)
	}
}

func TestCloseWriteStopsWrites(t *testing.T) {
	result := make(chan error, 1)
	reg := &Registry{}
	reg.Handle("cw:", func(ctx context.Context, s *Stream, arg string) {
		s.CloseWrite()
		_, err := s.Write([]byte("x"))
		result <- err
		<-ctx.Done()
	})
	h := start(t, reg)
	h.connect(4096)
	ours := h.open(1, "cw:")
	if err := <-result; err == nil {
		t.Fatal("write after CloseWrite succeeded")
	}
	h.expectNone()
	h.send(wire.Message{Command: wire.CLSE, Arg0: 1, Arg1: ours})
}

func TestSecondWriteWaitsForOkay(t *testing.T) {
	reg := &Registry{}
	reg.Handle("two:", func(ctx context.Context, s *Stream, arg string) {
		s.Write([]byte("a"))
		s.Write([]byte("b"))
	})
	h := start(t, reg)
	h.connect(4096)
	ours := h.open(6, "two:")
	h.expectMsg(wire.WRTE, ours, 6, "a")
	h.expectNone()
	h.send(wire.Message{Command: wire.OKAY, Arg0: 6, Arg1: ours})
	h.expectMsg(wire.WRTE, ours, 6, "b")
	h.send(wire.Message{Command: wire.OKAY, Arg0: 6, Arg1: ours})
	h.expectMsg(wire.CLSE, ours, 6, "")
}
