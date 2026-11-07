package session

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"

	"ewdb/internal/config"
	"ewdb/internal/wire"
)

type Transport interface {
	Read() (wire.Message, error)
	Write(wire.Message) error
	Close() error
}

type ChecksumSetter interface {
	SetChecksum(bool)
}

type Handler func(ctx context.Context, s *Stream, arg string)

type Registry struct {
	mu       sync.RWMutex
	handlers map[string]Handler
}

func (r *Registry) Handle(prefix string, h Handler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.handlers == nil {
		r.handlers = map[string]Handler{}
	}
	r.handlers[prefix] = h
}

func (r *Registry) lookup(name string) (Handler, string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	best := -1
	var h Handler
	for prefix, handler := range r.handlers {
		if len(prefix) > best && strings.HasPrefix(name, prefix) {
			best, h = len(prefix), handler
		}
	}
	if best < 0 {
		return nil, "", false
	}
	return h, name[best:], true
}

var ErrSessionClosed = errors.New("session: closed")

type conn struct {
	t  Transport
	mu sync.Mutex
	wg sync.WaitGroup
}

func (c *conn) write(m wire.Message) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t.Write(m)
}

func (c *conn) writeFor(s *Session, closed *atomic.Bool, m wire.Message) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if s.dead.Load() || (closed != nil && closed.Load()) {
		return io.ErrClosedPipe
	}
	return c.t.Write(m)
}

type Session struct {
	c       *conn
	reg     *Registry
	maxdata uint32
	ctx     context.Context
	cancel  context.CancelFunc
	dead    atomic.Bool

	mu      sync.Mutex
	streams map[uint32]*Stream
	next    uint32
	closed  bool
}

func Serve(ctx context.Context, t Transport, c *config.Config, r *Registry) error {
	cn := &conn{t: t}
	stop := context.AfterFunc(ctx, func() { t.Close() })
	var s *Session
	var err error
	for {
		m, rerr := t.Read()
		if rerr != nil {
			err = rerr
			break
		}
		if m.Command == wire.CNXN {
			if s != nil {
				s.reset()
			}
			s = newSession(ctx, cn, r, m.Arg1)
			if cs, ok := t.(ChecksumSetter); ok {
				cs.SetChecksum(m.Arg0 < wire.Version)
			}
			reply := wire.Message{Command: wire.CNXN, Arg0: wire.Version, Arg1: s.maxdata, Data: wire.Banner(c, wire.Features)}
			if err = cn.write(reply); err != nil {
				break
			}
			continue
		}
		if s != nil {
			s.handle(m)
		}
	}
	stop()
	t.Close()
	if s != nil {
		s.reset()
	}
	cn.wg.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

func newSession(ctx context.Context, cn *conn, r *Registry, hostMax uint32) *Session {
	maxdata := uint32(wire.MaxPayload)
	if hostMax > 0 && hostMax < maxdata {
		maxdata = hostMax
	}
	sctx, cancel := context.WithCancel(ctx)
	return &Session{c: cn, reg: r, maxdata: maxdata, ctx: sctx, cancel: cancel, streams: map[uint32]*Stream{}}
}

func (s *Session) handle(m wire.Message) {
	switch m.Command {
	case wire.OPEN:
		s.open(m)
	case wire.OKAY:
		if st := s.stream(m.Arg1); st != nil {
			st.onOkay(m.Arg0)
		} else if m.Arg0 != 0 && m.Arg1 != 0 {
			s.c.writeFor(s, nil, wire.Message{Command: wire.CLSE, Arg0: m.Arg1, Arg1: m.Arg0})
		}
	case wire.WRTE:
		if st := s.stream(m.Arg1); st != nil && st.peer() == m.Arg0 {
			st.onData(m.Data)
		}
	case wire.CLSE:
		if st := s.stream(m.Arg1); st != nil {
			if p := st.peer(); m.Arg0 == 0 || p == 0 || p == m.Arg0 {
				st.onRemoteClose()
			}
		}
	}
}

func (s *Session) open(m wire.Message) {
	if m.Arg0 == 0 {
		return
	}
	name := strings.TrimRight(string(m.Data), "\x00")
	h, arg, ok := s.reg.lookup(name)
	if !ok {
		s.c.writeFor(s, nil, wire.Message{Command: wire.CLSE, Arg1: m.Arg0})
		return
	}
	st, err := s.newStream(m.Arg0)
	if err != nil {
		return
	}
	if err := st.send(wire.Message{Command: wire.OKAY, Arg0: st.local, Arg1: m.Arg0}); err != nil {
		st.finish()
		return
	}
	s.c.wg.Add(1)
	go func() {
		defer s.c.wg.Done()
		defer st.Close()
		h(st.ctx, st, arg)
	}()
}

func (s *Session) stream(id uint32) *Stream {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.streams[id]
}

func (s *Session) newStream(remote uint32) (*Stream, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, ErrSessionClosed
	}
	for {
		s.next++
		if s.next != 0 && s.streams[s.next] == nil {
			break
		}
	}
	st := newStream(s, s.next, remote)
	s.streams[st.local] = st
	return st, nil
}

func (s *Session) remove(id uint32) {
	s.mu.Lock()
	delete(s.streams, id)
	s.mu.Unlock()
}

func (s *Session) reset() {
	s.dead.Store(true)
	s.mu.Lock()
	s.closed = true
	streams := s.streams
	s.streams = nil
	s.mu.Unlock()
	s.cancel()
	for _, st := range streams {
		st.sendClosed.Store(true)
		st.finish()
	}
}

func (s *Session) Reset() {
	s.mu.Lock()
	streams := make([]*Stream, 0, len(s.streams))
	for _, st := range s.streams {
		streams = append(streams, st)
	}
	s.mu.Unlock()
	for _, st := range streams {
		st.Close()
	}
	s.reset()
}

func (s *Session) Dial(ctx context.Context, service string) (*Stream, error) {
	st, err := s.newStream(0)
	if err != nil {
		return nil, err
	}
	if err := st.send(wire.Message{Command: wire.OPEN, Arg0: st.local, Data: []byte(service + "\x00")}); err != nil {
		st.finish()
		return nil, err
	}
	select {
	case <-st.opened:
		return st, nil
	case <-st.done:
		select {
		case <-st.opened:
			return st, nil
		default:
		}
		return nil, errors.New("session: " + service + " refused")
	case <-ctx.Done():
		st.Close()
		return nil, ctx.Err()
	}
}

func (s *Session) Context() context.Context {
	return s.ctx
}
