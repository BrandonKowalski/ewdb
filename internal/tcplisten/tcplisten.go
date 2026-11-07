//go:build testlistener

package tcplisten

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"sync"

	"ewdb/internal/wire"
)

type Transport struct {
	conn     net.Conn
	r        *bufio.Reader
	mu       sync.Mutex
	checksum bool
	once     sync.Once
	err      error
}

func NewTransport(c net.Conn) *Transport {
	return &Transport{conn: c, r: bufio.NewReader(c), checksum: true}
}

func (t *Transport) Read() (wire.Message, error) {
	return wire.Read(t.r, wire.MaxPayload)
}

func (t *Transport) Write(m wire.Message) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return wire.Write(t.conn, m, t.checksum)
}

func (t *Transport) SetChecksum(on bool) {
	t.mu.Lock()
	t.checksum = on
	t.mu.Unlock()
}

func (t *Transport) Close() error {
	t.once.Do(func() { t.err = t.conn.Close() })
	return t.err
}

func Loopback(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("-listen only accepts loopback addresses")
	}
	return nil
}

func Listen(ctx context.Context, addr string) (net.Listener, error) {
	if err := Loopback(addr); err != nil {
		return nil, err
	}
	var lc net.ListenConfig
	return lc.Listen(ctx, "tcp", addr)
}

func Accept(ctx context.Context, l net.Listener, handle func(*Transport)) error {
	go func() {
		<-ctx.Done()
		l.Close()
	}()
	for {
		c, err := l.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		go handle(NewTransport(c))
	}
}
