package services

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"

	"ewdb/internal/session"
)

type listener struct {
	local string
	to    string
	l     net.Listener
}

type reverseTable struct {
	mu        sync.Mutex
	listeners map[string]*listener
	closed    bool
}

var (
	tablesMu sync.Mutex
	tables   = map[*session.Session]*reverseTable{}
)

func tableFor(sess *session.Session) *reverseTable {
	tablesMu.Lock()
	defer tablesMu.Unlock()
	if t, ok := tables[sess]; ok {
		return t
	}
	t := &reverseTable{listeners: map[string]*listener{}}
	tables[sess] = t
	context.AfterFunc(sess.Context(), func() {
		t.closeAll()
		tablesMu.Lock()
		delete(tables, sess)
		tablesMu.Unlock()
	})
	return t
}

func (t *reverseTable) closeAll() {
	t.mu.Lock()
	t.closed = true
	ls := t.listeners
	t.listeners = map[string]*listener{}
	t.mu.Unlock()
	for _, l := range ls {
		l.l.Close()
	}
}

func reverseHandler(ctx context.Context, s *session.Stream, arg string) {
	sess := s.Session()
	t := tableFor(sess)
	switch {
	case arg == "list-forward":
		writeProto(s, t.format())
	case arg == "killforward-all":
		t.removeAll()
		s.Write([]byte("OKAY"))
	case strings.HasPrefix(arg, "killforward:"):
		spec := arg[len("killforward:"):]
		if spec == "" || strings.Contains(spec, ";") {
			writeFail(s, "bad killforward: "+spec)
			return
		}
		if !t.remove(spec) {
			writeFail(s, fmt.Sprintf("listener '%s' not found", spec))
			return
		}
		s.Write([]byte("OKAY"))
	case strings.HasPrefix(arg, "forward:"):
		rest := arg[len("forward:"):]
		noRebind := false
		if strings.HasPrefix(rest, "norebind:") {
			noRebind = true
			rest = rest[len("norebind:"):]
		}
		parts := strings.Split(rest, ";")
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" || parts[1][0] == '*' {
			writeFail(s, "bad forward: "+rest)
			return
		}
		port, err := t.install(sess, parts[0], parts[1], noRebind)
		if err != nil {
			writeFail(s, err.Error())
			return
		}
		s.Write([]byte("OKAY"))
		if port != 0 {
			writeProto(s, strconv.Itoa(port))
		}
	default:
		writeFail(s, "not a reverse forwarding command")
	}
}

func writeProto(s *session.Stream, msg string) {
	s.Write([]byte(fmt.Sprintf("%04x%s", len(msg), msg)))
}

func writeFail(s *session.Stream, msg string) {
	s.Write([]byte(fmt.Sprintf("FAIL%04x%s", len(msg), msg)))
}

func (t *reverseTable) format() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	keys := make([]string, 0, len(t.listeners))
	for k := range t.listeners {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		l := t.listeners[k]
		b.WriteString("(reverse) " + l.local + " " + l.to + "\n")
	}
	return b.String()
}

func (t *reverseTable) removeAll() {
	t.mu.Lock()
	ls := t.listeners
	t.listeners = map[string]*listener{}
	t.mu.Unlock()
	for _, l := range ls {
		l.l.Close()
	}
}

func (t *reverseTable) remove(spec string) bool {
	t.mu.Lock()
	l, ok := t.listeners[spec]
	delete(t.listeners, spec)
	t.mu.Unlock()
	if ok {
		l.l.Close()
	}
	return ok
}

func (t *reverseTable) install(sess *session.Session, local, to string, noRebind bool) (int, error) {
	t.mu.Lock()
	old := t.listeners[local]
	if old != nil {
		if noRebind {
			t.mu.Unlock()
			return 0, errors.New("cannot rebind existing socket")
		}
		delete(t.listeners, local)
	}
	t.mu.Unlock()
	if old != nil {
		old.l.Close()
	}
	l, resolved, err := listenSpec(local)
	if err != nil {
		return 0, errors.New("cannot bind listener: " + err.Error())
	}
	entry := &listener{local: resolved, to: to, l: l}
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		l.Close()
		return 0, errors.New("session closed")
	}
	t.listeners[resolved] = entry
	t.mu.Unlock()
	go acceptLoop(sess, entry)
	port := 0
	if ta, ok := l.Addr().(*net.TCPAddr); ok && local == "tcp:0" {
		port = ta.Port
	}
	return port, nil
}

func listenSpec(spec string) (net.Listener, string, error) {
	switch {
	case strings.HasPrefix(spec, "tcp:"):
		port := spec[len("tcp:"):]
		if _, err := strconv.ParseUint(port, 10, 16); err != nil {
			return nil, "", errors.New("bad tcp port: " + port)
		}
		l, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", port))
		if err != nil {
			return nil, "", err
		}
		return l, "tcp:" + strconv.Itoa(l.Addr().(*net.TCPAddr).Port), nil
	case strings.HasPrefix(spec, "localabstract:"):
		addr, err := abstractAddr(spec[len("localabstract:"):])
		if err != nil {
			return nil, "", err
		}
		l, err := net.Listen("unix", addr)
		return l, spec, err
	case strings.HasPrefix(spec, "localfilesystem:"):
		path := spec[len("localfilesystem:"):]
		if st, err := os.Lstat(path); err == nil && st.Mode()&os.ModeSocket != 0 {
			os.Remove(path)
		}
		l, err := net.Listen("unix", path)
		return l, spec, err
	}
	return nil, "", errors.New("unsupported listener: " + spec)
}

func acceptLoop(sess *session.Session, entry *listener) {
	ctx := sess.Context()
	var wg sync.WaitGroup
	defer wg.Wait()
	for {
		c, err := entry.l.Accept()
		if err != nil {
			return
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			st, err := sess.Dial(ctx, entry.to)
			if err != nil {
				c.Close()
				return
			}
			defer st.Close()
			pipe(ctx, st, c)
		}()
	}
}
