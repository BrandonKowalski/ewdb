package services

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/creack/pty"

	"ewdb/internal/session"
)

const (
	idStdin      = 0
	idStdout     = 1
	idStderr     = 2
	idExit       = 3
	idCloseStdin = 4
	idWindowSize = 5
)

const (
	bufSize         = 32 * 1024
	maxPacket       = 1 << 20
	firstPacketWait = 100 * time.Millisecond
	killDelay       = time.Second
	drainWait       = 2 * time.Second
)

type shellArgs struct {
	v2      bool
	pty     bool
	term    string
	command string
}

func parseShell(name string) (shellArgs, bool) {
	rest, ok := strings.CutPrefix(name, "shell")
	if !ok {
		return shellArgs{}, false
	}
	opts, command, ok := strings.Cut(rest, ":")
	if !ok || (opts != "" && opts[0] != ',') {
		return shellArgs{}, false
	}
	a := shellArgs{command: command}
	typ := ""
	for _, o := range strings.Split(opts, ",") {
		switch {
		case o == "v2":
			a.v2 = true
		case o == "raw" || o == "pty":
			typ = o
		case strings.HasPrefix(o, "TERM="):
			a.term = o[len("TERM="):]
		}
	}
	a.pty = typ == "pty" || (typ == "" && command == "")
	return a, true
}

func shellHandler(d *Deps) session.Handler {
	return func(ctx context.Context, s *session.Stream, arg string) {
		a, ok := parseShell("shell" + arg)
		if !ok {
			return
		}
		run(ctx, s, d, a)
	}
}

type source struct {
	f  *os.File
	id byte
}

type proc struct {
	stdin  *os.File
	pty    *os.File
	outs   []source
	parent []*os.File
	child  []*os.File
}

func (p *proc) closeChild() {
	for _, f := range p.child {
		f.Close()
	}
}

func (p *proc) close() {
	p.closeChild()
	for _, f := range p.parent {
		f.Close()
	}
}

func (p *proc) closeStdin() {
	if p.pty == nil {
		p.stdin.Close()
	}
}

func openPty() (*os.File, *os.File, error) {
	m, tty, err := pty.Open()
	if err != nil {
		return nil, nil, err
	}
	fd, err := syscall.Dup(int(m.Fd()))
	m.Close()
	if err == nil {
		syscall.CloseOnExec(fd)
		err = syscall.SetNonblock(fd, true)
		if err != nil {
			syscall.Close(fd)
		}
	}
	if err != nil {
		tty.Close()
		return nil, nil, err
	}
	return os.NewFile(uintptr(fd), "ptmx"), tty, nil
}

type winsize struct {
	rows, cols, x, y uint16
}

func setWinsize(f *os.File, ws winsize) {
	rc, err := f.SyscallConn()
	if err != nil {
		return
	}
	rc.Control(func(fd uintptr) {
		syscall.Syscall(syscall.SYS_IOCTL, fd, uintptr(syscall.TIOCSWINSZ), uintptr(unsafe.Pointer(&ws)))
	})
}

func open(cmd *exec.Cmd, a shellArgs) (*proc, error) {
	if a.pty {
		m, tty, err := openPty()
		if err != nil {
			return nil, err
		}
		cmd.Stdin, cmd.Stdout, cmd.Stderr = tty, tty, tty
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
		return &proc{stdin: m, pty: m, outs: []source{{m, idStdout}}, parent: []*os.File{m}, child: []*os.File{tty}}, nil
	}
	p := &proc{}
	pipe := func() (r, w *os.File, err error) {
		r, w, err = os.Pipe()
		if err != nil {
			p.close()
		}
		return
	}
	inR, inW, err := pipe()
	if err != nil {
		return nil, err
	}
	p.parent, p.child = append(p.parent, inW), append(p.child, inR)
	outR, outW, err := pipe()
	if err != nil {
		return nil, err
	}
	p.parent, p.child = append(p.parent, outR), append(p.child, outW)
	p.outs = []source{{outR, idStdout}}
	errW := outW
	if a.v2 {
		var errR *os.File
		errR, errW, err = pipe()
		if err != nil {
			return nil, err
		}
		p.parent, p.child = append(p.parent, errR), append(p.child, errW)
		p.outs = append(p.outs, source{errR, idStderr})
	}
	p.stdin = inW
	cmd.Stdin, cmd.Stdout, cmd.Stderr = inR, outW, errW
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return p, nil
}

type output struct {
	mu sync.Mutex
	w  io.Writer
	v2 bool
}

func (o *output) write(id byte, buf []byte) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.v2 {
		if id == idExit {
			return nil
		}
		_, err := o.w.Write(buf[5:])
		return err
	}
	buf[0] = id
	binary.LittleEndian.PutUint32(buf[1:5], uint32(len(buf)-5))
	_, err := o.w.Write(buf)
	return err
}

func (o *output) packet(id byte, data []byte) error {
	buf := make([]byte, 5+len(data))
	copy(buf[5:], data)
	return o.write(id, buf)
}

func (o *output) copy(f *os.File, id byte) {
	buf := make([]byte, 5+bufSize)
	for {
		n, err := f.Read(buf[5:])
		if n > 0 {
			if o.write(id, buf[:5+n]) != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

func (o *output) fail(err error) {
	o.packet(idStderr, []byte(fmt.Sprintf("error: %v\n", err)))
	o.packet(idExit, []byte{126})
}

func pump(r io.Reader, p *proc, v2 bool, first chan<- struct{}) {
	var once sync.Once
	signal := func() { once.Do(func() { close(first) }) }
	defer signal()
	if !v2 {
		buf := make([]byte, bufSize)
		for {
			n, err := r.Read(buf)
			if n > 0 {
				p.stdin.Write(buf[:n])
			}
			signal()
			if err != nil {
				p.closeStdin()
				return
			}
		}
	}
	var hdr [5]byte
	for {
		if _, err := io.ReadFull(r, hdr[:]); err != nil {
			return
		}
		n := binary.LittleEndian.Uint32(hdr[1:])
		if n > maxPacket {
			return
		}
		data := make([]byte, n)
		if _, err := io.ReadFull(r, data); err != nil {
			return
		}
		switch hdr[0] {
		case idStdin:
			p.stdin.Write(data)
		case idCloseStdin:
			p.closeStdin()
		case idWindowSize:
			if p.pty != nil {
				var rows, cols, x, y uint16
				if c, _ := fmt.Sscanf(strings.TrimRight(string(data), "\x00"), "%dx%d,%dx%d", &rows, &cols, &x, &y); c == 4 {
					setWinsize(p.pty, winsize{rows, cols, x, y})
				}
			}
		}
		signal()
	}
}

func exitCode(ps *os.ProcessState) int {
	if ps == nil {
		return 1
	}
	if ws, ok := ps.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return 128 + int(ws.Signal())
	}
	return ps.ExitCode() & 0xff
}

func run(ctx context.Context, rw io.ReadWriter, d *Deps, a shellArgs) {
	out := &output{w: rw, v2: a.v2}
	var args []string
	if a.command != "" {
		args = []string{"-c", a.command}
	}
	cmd := exec.Command(d.Config.Shell, args...)
	cmd.Env = Env(d, a.term)
	cmd.Dir = "/"
	p, err := open(cmd, a)
	if err != nil {
		out.fail(err)
		return
	}
	defer p.close()

	first := make(chan struct{})
	go pump(rw, p, a.v2, first)
	if a.pty && a.v2 {
		select {
		case <-first:
		case <-time.After(firstPacketWait):
		case <-ctx.Done():
			return
		}
	}
	if err := cmd.Start(); err != nil {
		out.fail(err)
		return
	}
	p.closeChild()
	pid := cmd.Process.Pid

	exited := make(chan int, 1)
	go func() {
		cmd.Wait()
		exited <- exitCode(cmd.ProcessState)
	}()
	var wg sync.WaitGroup
	for _, s := range p.outs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out.copy(s.f, s.id)
		}()
	}
	copied := make(chan struct{})
	go func() {
		wg.Wait()
		close(copied)
	}()

	select {
	case <-copied:
		select {
		case code := <-exited:
			out.packet(idExit, []byte{byte(code)})
			return
		case <-ctx.Done():
		}
	case <-ctx.Done():
	}

	syscall.Kill(-pid, syscall.SIGHUP)
	time.AfterFunc(killDelay, func() { syscall.Kill(-pid, syscall.SIGKILL) })
	p.close()
	drain, cancel := context.WithTimeout(context.Background(), drainWait)
	defer cancel()
	select {
	case <-copied:
	case <-drain.Done():
	}
	select {
	case <-exited:
	case <-drain.Done():
	}
}
