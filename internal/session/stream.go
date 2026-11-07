package session

import (
	"context"
	"io"
	"sync"

	"ewdb/internal/wire"
)

type Stream struct {
	sess   *Session
	local  uint32
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
	opened chan struct{}
	ready  chan struct{}

	mu          sync.Mutex
	cond        *sync.Cond
	remote      uint32
	queue       [][]byte
	eof         bool
	writeClosed bool

	writeMu sync.Mutex

	sendMu     sync.Mutex
	sendClosed bool
}

func newStream(s *Session, local, remote uint32) *Stream {
	ctx, cancel := context.WithCancel(s.ctx)
	st := &Stream{
		sess:   s,
		local:  local,
		ctx:    ctx,
		cancel: cancel,
		done:   make(chan struct{}),
		opened: make(chan struct{}),
		ready:  make(chan struct{}, 1),
		remote: remote,
	}
	st.cond = sync.NewCond(&st.mu)
	if remote != 0 {
		close(st.opened)
	}
	return st
}

func (st *Stream) Session() *Session {
	return st.sess
}

func (st *Stream) peer() uint32 {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.remote
}

func (st *Stream) send(m wire.Message) error {
	st.sendMu.Lock()
	defer st.sendMu.Unlock()
	if st.sendClosed {
		return io.ErrClosedPipe
	}
	return st.sess.c.write(m)
}

func (st *Stream) Read(p []byte) (int, error) {
	st.mu.Lock()
	for len(st.queue) == 0 && !st.eof {
		st.cond.Wait()
	}
	if len(st.queue) == 0 {
		st.mu.Unlock()
		return 0, io.EOF
	}
	if len(p) == 0 {
		st.mu.Unlock()
		return 0, nil
	}
	n := copy(p, st.queue[0])
	ack := n == len(st.queue[0])
	if ack {
		st.queue[0] = nil
		st.queue = st.queue[1:]
	} else {
		st.queue[0] = st.queue[0][n:]
	}
	remote := st.remote
	st.mu.Unlock()
	if ack {
		st.send(wire.Message{Command: wire.OKAY, Arg0: st.local, Arg1: remote})
	}
	return n, nil
}

func (st *Stream) Write(p []byte) (int, error) {
	st.writeMu.Lock()
	defer st.writeMu.Unlock()
	max := int(st.sess.maxdata)
	n := 0
	for len(p) > 0 {
		st.mu.Lock()
		closed := st.writeClosed
		remote := st.remote
		st.mu.Unlock()
		if closed {
			return n, io.ErrClosedPipe
		}
		chunk := p
		if len(chunk) > max {
			chunk = chunk[:max]
		}
		select {
		case <-st.ready:
		default:
		}
		if err := st.send(wire.Message{Command: wire.WRTE, Arg0: st.local, Arg1: remote, Data: chunk}); err != nil {
			return n, err
		}
		select {
		case <-st.ready:
		case <-st.done:
			return n, io.ErrClosedPipe
		}
		n += len(chunk)
		p = p[len(chunk):]
	}
	return n, nil
}

func (st *Stream) CloseWrite() error {
	st.mu.Lock()
	st.writeClosed = true
	st.mu.Unlock()
	return nil
}

func (st *Stream) Close() error {
	st.sendMu.Lock()
	var err error
	if !st.sendClosed {
		st.sendClosed = true
		err = st.sess.c.write(wire.Message{Command: wire.CLSE, Arg0: st.local, Arg1: st.peer()})
	}
	st.sendMu.Unlock()
	st.finish()
	return err
}

func (st *Stream) finish() {
	st.mu.Lock()
	if !st.eof {
		st.eof = true
		st.writeClosed = true
		close(st.done)
		st.cond.Broadcast()
	}
	st.mu.Unlock()
	st.cancel()
	st.sess.remove(st.local)
}

func (st *Stream) onOkay(remote uint32) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.eof {
		return
	}
	if st.remote == 0 {
		st.remote = remote
		close(st.opened)
		return
	}
	if remote != st.remote {
		return
	}
	select {
	case st.ready <- struct{}{}:
	default:
	}
}

func (st *Stream) onData(data []byte) {
	if len(data) == 0 {
		st.send(wire.Message{Command: wire.OKAY, Arg0: st.local, Arg1: st.peer()})
		return
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.eof {
		return
	}
	st.queue = append(st.queue, data)
	st.cond.Broadcast()
}

func (st *Stream) onRemoteClose() {
	st.sendMu.Lock()
	st.sendClosed = true
	st.sendMu.Unlock()
	st.finish()
}
