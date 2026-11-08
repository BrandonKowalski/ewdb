package usb

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"ewdb/internal/wire"
)

type fakeFFS struct {
	t          *testing.T
	e          *ffsEndpoint
	mu         sync.Mutex
	reconnects int
	opens      []uint64
	hosts      []int
	onReconn   func()
}

func newFakeFFS(t *testing.T) *fakeFFS {
	f := &fakeFFS{t: t}
	f.e = newFFSEndpoint("/nonexistent", -1, devNull(t))
	f.e.open = func(gen uint64) (*transport, error) {
		dev, host := seqpacket(t)
		f.mu.Lock()
		f.opens = append(f.opens, gen)
		f.hosts = append(f.hosts, host)
		f.mu.Unlock()
		tr := newFFSTransport(dev, dev, f.e.devnull)
		tr.gen = gen
		return tr, nil
	}
	f.e.reconnect = func(ctx context.Context) bool {
		f.mu.Lock()
		f.reconnects++
		hook := f.onReconn
		f.mu.Unlock()
		if hook != nil {
			hook()
		}
		return true
	}
	t.Cleanup(func() {
		if f.e.last != nil {
			f.e.last.Close()
		}
		for _, h := range f.hosts {
			unix.Close(h)
		}
	})
	return f
}

func (f *fakeFFS) send(typ byte) {
	ev := make([]byte, eventSize)
	ev[8] = typ
	f.e.event(ev)
}

func (f *fakeFFS) counts() (int, []uint64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reconnects, append([]uint64(nil), f.opens...)
}

func (f *fakeFFS) next(ctx context.Context) *transport {
	f.t.Helper()
	type result struct {
		tr  *transport
		err error
	}
	rc := make(chan result, 1)
	go func() {
		tr, err := f.e.Next(ctx)
		if err != nil {
			rc <- result{nil, err}
			return
		}
		rc <- result{tr.(*transport), nil}
	}()
	select {
	case r := <-rc:
		if r.err != nil {
			f.t.Fatalf("Next: %v", r.err)
		}
		return r.tr
	case <-time.After(3 * reconnectWait):
		f.t.Fatal("Next did not return")
	}
	return nil
}

func TestFFSDisableEnableOpensNewGenerationWithoutReconnect(t *testing.T) {
	f := newFakeFFS(t)
	f.send(evBind)
	f.send(evEnable)
	t1 := f.next(context.Background())
	if t1.gen != 1 {
		t.Fatalf("first transport gen %d", t1.gen)
	}
	f.send(evDisable)
	if !t1.isClosed() {
		t.Fatal("DISABLE did not fail the transport")
	}
	go func() {
		time.Sleep(50 * time.Millisecond)
		f.send(evEnable)
	}()
	t2 := f.next(context.Background())
	reconnects, opens := f.counts()
	if t2.gen != 2 || reconnects != 0 || len(opens) != 2 {
		t.Fatalf("gen %d, reconnects %d, opens %v", t2.gen, reconnects, opens)
	}
}

func TestFFSSameGenerationExitReconnectsOnce(t *testing.T) {
	f := newFakeFFS(t)
	f.onReconn = func() {
		f.send(evDisable)
		f.send(evEnable)
	}
	f.send(evEnable)
	t1 := f.next(context.Background())
	t1.Close()
	t2 := f.next(context.Background())
	reconnects, opens := f.counts()
	if reconnects != 1 || t2.gen != 2 || len(opens) != 2 {
		t.Fatalf("reconnects %d, gen %d, opens %v", reconnects, t2.gen, opens)
	}
}

func TestFFSSuspendResumeChangesNothing(t *testing.T) {
	f := newFakeFFS(t)
	f.send(evEnable)
	tr := f.next(context.Background())
	f.send(evSuspend)
	f.send(evResume)
	if tr.isClosed() {
		t.Fatal("SUSPEND or RESUME failed the transport")
	}
	if err := tr.Write(wire.Message{Command: wire.OKAY, Arg0: 1, Arg1: 2}); err != nil {
		t.Fatalf("Write after RESUME: %v", err)
	}
	f.mu.Lock()
	host := f.hosts[0]
	f.mu.Unlock()
	buf := make([]byte, chunk)
	if n, err := unix.Read(host, buf); err != nil || n != headerLen {
		t.Fatalf("host read %d, %v", n, err)
	}
	reconnects, opens := f.counts()
	if reconnects != 0 || len(opens) != 1 {
		t.Fatalf("reconnects %d, opens %v", reconnects, opens)
	}
}

func TestFFSNextReturnsWhenContextEnds(t *testing.T) {
	f := newFakeFFS(t)
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() {
		_, err := f.e.Next(ctx)
		errc <- err
	}()
	time.Sleep(50 * time.Millisecond)
	start := time.Now()
	cancel()
	select {
	case err := <-errc:
		if err != context.Canceled {
			t.Fatalf("Next = %v", err)
		}
		if d := time.Since(start); d > 100*time.Millisecond {
			t.Fatalf("Next took %v after cancel", d)
		}
	case <-time.After(time.Second):
		t.Fatal("Next ignored ctx")
	}
}

func TestSoftConnectRetriesConnectAfterContextEnds(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var writes []string
	connectFails := 1
	write := func(v string) error {
		writes = append(writes, v)
		if v == "disconnect" {
			cancel()
			return nil
		}
		if connectFails > 0 {
			connectFails--
			return unix.EBUSY
		}
		return nil
	}
	if !cycleSoftConnect(ctx, "", write) {
		t.Fatal("cycleSoftConnect reported no reconnect")
	}
	want := []string{"disconnect", "connect", "connect"}
	if len(writes) != len(want) || writes[0] != want[0] || writes[1] != want[1] || writes[2] != want[2] {
		t.Fatalf("writes %v, want %v", writes, want)
	}
}

func TestSoftConnectStopsWhenUDCUnboundAndReportsFailure(t *testing.T) {
	udcFile := t.TempDir() + "/UDC"
	if err := os.WriteFile(udcFile, []byte("\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var connects int
	write := func(v string) error {
		if v == "connect" {
			connects++
			return unix.ENODEV
		}
		return nil
	}
	if cycleSoftConnect(context.Background(), udcFile, write) {
		t.Fatal("cycleSoftConnect reported success without a connect")
	}
	if connects != 1 {
		t.Fatalf("connect attempts %d, want 1", connects)
	}
}

func (f *fakeFFS) host(i int) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.hosts[i]
}

func waitClosed(tr *transport, d time.Duration) bool {
	select {
	case <-tr.done:
		return true
	case <-time.After(d):
		return false
	}
}

func TestFFSSilentHostAfterResetReconnectsOnce(t *testing.T) {
	f := newFakeFFS(t)
	f.e.helloWait = 200 * time.Millisecond
	f.onReconn = func() {
		f.send(evDisable)
		f.send(evEnable)
	}
	f.send(evEnable)
	t1 := f.next(context.Background())
	sendRecord(t, f.host(0), encode(t, wire.Message{Command: wire.CNXN, Arg0: wire.Version, Arg1: wire.MaxPayload}))
	readWithin(t, t1)
	f.send(evEnable)
	t2 := f.next(context.Background())
	if reconnects, _ := f.counts(); reconnects != 0 || t2.gen != 2 {
		t.Fatalf("after reset: reconnects %d, gen %d", reconnects, t2.gen)
	}
	if !waitClosed(t2, 2*time.Second) {
		t.Fatal("transport kept waiting for a host that never spoke")
	}
	if _, err := t2.Read(); err != errNoHello {
		t.Fatalf("Read = %v, want %v", err, errNoHello)
	}
	t3 := f.next(context.Background())
	if reconnects, _ := f.counts(); reconnects != 1 || t3.gen != 3 {
		t.Fatalf("after silence: reconnects %d, gen %d", reconnects, t3.gen)
	}
	if waitClosed(t3, 4*f.e.helloWait) {
		t.Fatal("transport after a reconnect gave up on the host too")
	}
}

func TestFFSFirstTransportWaitsForHost(t *testing.T) {
	f := newFakeFFS(t)
	f.e.helloWait = 100 * time.Millisecond
	f.send(evEnable)
	t1 := f.next(context.Background())
	if waitClosed(t1, 4*f.e.helloWait) {
		t.Fatal("first transport gave up on the host")
	}
}

func TestFFSSuspendPausesStallWatchdog(t *testing.T) {
	f := newFakeFFS(t)
	f.send(evEnable)
	if f.e.paused() {
		t.Fatal("paused before SUSPEND")
	}
	f.send(evSuspend)
	if !f.e.paused() {
		t.Fatal("not paused after SUSPEND")
	}
	f.send(evResume)
	if f.e.paused() {
		t.Fatal("paused after RESUME")
	}
	f.send(evSuspend)
	f.send(evDisable)
	if f.e.paused() {
		t.Fatal("paused after DISABLE")
	}
}
