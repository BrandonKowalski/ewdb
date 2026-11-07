package usb

import (
	"context"
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
		tr := newTransport(dev, dev, f.e.devnull, ffsChunk, ffsChunk, true)
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
	buf := make([]byte, ffsChunk)
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
