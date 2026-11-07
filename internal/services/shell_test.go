package services

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"ewdb/internal/config"
)

func TestMain(m *testing.M) {
	if os.Getenv("EWDB_HOLD") == "1" {
		syscall.Setsid()
		fmt.Println(os.Getpid())
		time.Sleep(5 * time.Minute)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

type fakeStream struct {
	r    *io.PipeReader
	w    *io.PipeWriter
	q    chan []byte
	once sync.Once
	mu   sync.Mutex
	b    bytes.Buffer
}

func newFake() *fakeStream {
	r, w := io.Pipe()
	f := &fakeStream{r: r, w: w, q: make(chan []byte, 16)}
	go func() {
		for pkt := range f.q {
			w.Write(pkt)
		}
	}()
	return f
}

func (f *fakeStream) close() {
	f.once.Do(func() {
		close(f.q)
		f.w.Close()
	})
}

func (f *fakeStream) Read(p []byte) (int, error) {
	return f.r.Read(p)
}

func (f *fakeStream) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.b.Write(p)
}

func (f *fakeStream) bytes() []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]byte(nil), f.b.Bytes()...)
}

func (f *fakeStream) send(t *testing.T, id byte, data string) {
	t.Helper()
	pkt := make([]byte, 5+len(data))
	pkt[0] = id
	binary.LittleEndian.PutUint32(pkt[1:5], uint32(len(data)))
	copy(pkt[5:], data)
	f.q <- pkt
}

type packet struct {
	id   byte
	data []byte
}

func parsePackets(t *testing.T, b []byte) []packet {
	t.Helper()
	var out []packet
	for len(b) > 0 {
		if len(b) < 5 {
			t.Fatalf("truncated header %q", b)
		}
		n := int(binary.LittleEndian.Uint32(b[1:5]))
		if len(b) < 5+n {
			t.Fatalf("truncated packet id %d len %d have %d", b[0], n, len(b)-5)
		}
		out = append(out, packet{b[0], b[5 : 5+n]})
		b = b[5+n:]
	}
	return out
}

func streams(pkts []packet) (stdout, stderr string, exit int, exits int) {
	exit = -1
	for _, p := range pkts {
		switch p.id {
		case idStdout:
			stdout += string(p.data)
		case idStderr:
			stderr += string(p.data)
		case idExit:
			exits++
			exit = int(p.data[0])
		}
	}
	return
}

func testDeps(t *testing.T) *Deps {
	return &Deps{
		Config:      &config.Config{Shell: "/bin/sh", Path: "/etc/ewdbd.conf"},
		LauncherDir: t.TempDir(),
		SystemPath:  "/usr/bin:/bin:/usr/sbin:/sbin",
	}
}

func start(t *testing.T, ctx context.Context, d *Deps, a shellArgs) (*fakeStream, chan struct{}) {
	t.Helper()
	f := newFake()
	done := make(chan struct{})
	go func() {
		defer close(done)
		run(ctx, f, d, a)
	}()
	t.Cleanup(func() {
		f.close()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("run did not return")
		}
	})
	return f, done
}

func finish(t *testing.T, done chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("run did not return")
	}
}

func TestShellArgsParse(t *testing.T) {
	cases := []struct {
		name string
		want shellArgs
	}{
		{"shell,v2,TERM=xterm,raw:ls", shellArgs{v2: true, term: "xterm", command: "ls"}},
		{"shell:", shellArgs{pty: true}},
		{"shell:ls -l", shellArgs{command: "ls -l"}},
		{"shell,v2,pty:ls", shellArgs{v2: true, pty: true, command: "ls"}},
		{"shell,raw:", shellArgs{}},
		{"shell,v2,future:echo a:b", shellArgs{v2: true, command: "echo a:b"}},
	}
	for _, c := range cases {
		got, ok := parseShell(c.name)
		if !ok || got != c.want {
			t.Errorf("parseShell(%q) = %+v, %v; want %+v", c.name, got, ok, c.want)
		}
	}
	for _, bad := range []string{"shell", "shellx:ls", "shell,v2"} {
		if _, ok := parseShell(bad); ok {
			t.Errorf("parseShell(%q) accepted", bad)
		}
	}
}

func TestEnv(t *testing.T) {
	d := testDeps(t)
	env := Env(d, "")
	home := "/"
	if st, err := os.Stat("/root"); err == nil && st.IsDir() {
		home = "/root"
	}
	want := []string{
		"PATH=" + d.LauncherDir + ":" + d.SystemPath,
		"HOME=" + home,
		"TERM=dumb",
		"EWDB_CONFIG=/etc/ewdbd.conf",
	}
	if strings.Join(env, "\n") != strings.Join(want, "\n") {
		t.Fatalf("env = %q, want %q", env, want)
	}
	if got := Env(d, "xterm"); got[2] != "TERM=xterm" {
		t.Fatalf("TERM = %q", got[2])
	}
	d.LauncherDir = ""
	if got := Env(d, ""); got[0] != "PATH="+d.SystemPath {
		t.Fatalf("PATH without launchers = %q", got[0])
	}
}

func TestRawV2SeparatesStreamsAndExitsLast(t *testing.T) {
	f, done := start(t, context.Background(), testDeps(t), shellArgs{v2: true, command: "echo out; echo err >&2; head -c 300000 /dev/zero; exit 3"})
	finish(t, done)
	pkts := parsePackets(t, f.bytes())
	stdout, stderr, exit, exits := streams(pkts)
	if stdout != "out\n"+strings.Repeat("\x00", 300000) {
		t.Errorf("stdout len %d", len(stdout))
	}
	if stderr != "err\n" {
		t.Errorf("stderr %q", stderr)
	}
	if exits != 1 || exit != 3 || pkts[len(pkts)-1].id != idExit {
		t.Errorf("exit %d exits %d last id %d", exit, exits, pkts[len(pkts)-1].id)
	}
}

func TestRawV2Stdin(t *testing.T) {
	f, done := start(t, context.Background(), testDeps(t), shellArgs{v2: true, command: "cat; echo end"})
	f.send(t, idStdin, "hello\n")
	f.send(t, idCloseStdin, "")
	finish(t, done)
	stdout, _, exit, _ := streams(parsePackets(t, f.bytes()))
	if stdout != "hello\nend\n" || exit != 0 {
		t.Fatalf("stdout %q exit %d", stdout, exit)
	}
}

func TestSignalExitCode(t *testing.T) {
	f, done := start(t, context.Background(), testDeps(t), shellArgs{v2: true, command: "kill -TERM $$"})
	finish(t, done)
	if _, _, exit, _ := streams(parsePackets(t, f.bytes())); exit != 128+int(syscall.SIGTERM) {
		t.Fatalf("exit %d", exit)
	}
}

func TestPtyV2WindowSize(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f := newFake()
	f.send(t, idWindowSize, "40x100,0x0\x00")
	done := make(chan struct{})
	go func() {
		defer close(done)
		run(ctx, f, testDeps(t), shellArgs{v2: true, pty: true, command: "stty size; exit 5"})
	}()
	defer f.close()
	finish(t, done)
	stdout, _, exit, _ := streams(parsePackets(t, f.bytes()))
	if stdout != "40 100\r\n" || exit != 5 {
		t.Fatalf("stdout %q exit %d", stdout, exit)
	}
}

func TestNonV2IsUnframedAndJoined(t *testing.T) {
	f, done := start(t, context.Background(), testDeps(t), shellArgs{command: "echo out; sleep 0.1; echo err >&2; exit 4"})
	finish(t, done)
	if got := string(f.bytes()); got != "out\nerr\n" {
		t.Fatalf("output %q", got)
	}
}

func TestStartFailureReportsError(t *testing.T) {
	d := testDeps(t)
	d.Config.Shell = "/nonexistent/sh"
	f, done := start(t, context.Background(), d, shellArgs{v2: true, command: "true"})
	finish(t, done)
	_, stderr, exit, _ := streams(parsePackets(t, f.bytes()))
	if !strings.HasPrefix(stderr, "error: ") || exit != 126 {
		t.Fatalf("stderr %q exit %d", stderr, exit)
	}
}

var pidLine = regexp.MustCompile(`(?m)^(\d+)\r?$`)

func waitPid(t *testing.T, f *fakeStream, v2 bool) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		b := f.bytes()
		if v2 {
			stdout, _, _, _ := streams(parsePackets(t, b))
			b = []byte(stdout)
		}
		if m := pidLine.FindSubmatch(b); m != nil {
			pid, _ := strconv.Atoi(string(m[1]))
			return pid
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no pid in output %q", f.bytes())
	return 0
}

func gone(pid int, within time.Duration) bool {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if syscall.Kill(pid, 0) == syscall.ESRCH {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

func goroutinesSettle(t *testing.T, base int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for runtime.NumGoroutine() > base {
		if time.Now().After(deadline) {
			buf := make([]byte, 1<<16)
			t.Fatalf("goroutines %d > %d\n%s", runtime.NumGoroutine(), base, buf[:runtime.Stack(buf, true)])
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func cancelMidCommand(t *testing.T, a shellArgs, hold string) {
	base := runtime.NumGoroutine()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f := newFake()
	done := make(chan struct{})
	go func() {
		defer close(done)
		run(ctx, f, testDeps(t), a)
	}()
	pid := waitPid(t, f, a.v2)
	if hold == "escaped" {
		t.Cleanup(func() { syscall.Kill(pid, syscall.SIGKILL) })
	}
	start := time.Now()
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("run did not return within 3 s of cancel")
	}
	t.Logf("returned %v after cancel", time.Since(start))
	f.close()
	if hold == "group" && !gone(pid, 3*time.Second) {
		t.Fatalf("grandchild %d still alive", pid)
	}
	goroutinesSettle(t, base)
}

func TestCancelWithGrandchildHoldingPipes(t *testing.T) {
	cancelMidCommand(t, shellArgs{v2: true, command: "sleep 300 & echo $!; wait"}, "group")
}

func TestCancelWithGrandchildIgnoringHangup(t *testing.T) {
	cancelMidCommand(t, shellArgs{v2: true, command: "trap '' HUP; sleep 300 & echo $!; wait"}, "group")
}

func TestCancelWithGrandchildHoldingPty(t *testing.T) {
	cancelMidCommand(t, shellArgs{v2: true, pty: true, command: "sleep 300 & echo $!; wait"}, "group")
}

func TestCancelNonV2WithGrandchild(t *testing.T) {
	cancelMidCommand(t, shellArgs{command: "sleep 300 & echo $!; wait"}, "group")
}

func holdCommand(t *testing.T) string {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return "EWDB_HOLD=1 '" + strings.ReplaceAll(filepath.Clean(exe), "'", `'\''`) + "' & wait"
}

func TestCancelWithEscapedGrandchildHoldingPipes(t *testing.T) {
	cancelMidCommand(t, shellArgs{v2: true, command: holdCommand(t)}, "escaped")
}

func TestCancelWithEscapedGrandchildHoldingPty(t *testing.T) {
	cancelMidCommand(t, shellArgs{v2: true, pty: true, command: holdCommand(t)}, "escaped")
}

func hostCloseAfter(t *testing.T, a shellArgs, data []byte) (chan struct{}, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	a.session = context.Background()
	f := newFake()
	done := make(chan struct{})
	go func() {
		defer close(done)
		run(ctx, f, testDeps(t), a)
	}()
	go func() {
		f.w.Write(data)
		f.w.Close()
	}()
	t.Cleanup(f.close)
	time.Sleep(50 * time.Millisecond)
	cancel()
	return done, cancel
}

func TestNonV2RawHostCloseDeliversAllStdin(t *testing.T) {
	out := filepath.Join(t.TempDir(), "in.bin")
	data := make([]byte, 8<<20)
	for i := range data {
		data[i] = byte(i * 31)
	}
	done, cancel := hostCloseAfter(t, shellArgs{command: "sleep 0.3; cat > '" + out + "'"}, data)
	defer cancel()
	finish(t, done)
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("child got %d bytes, want %d", len(got), len(data))
	}
}

func TestNonV2RawHostCloseStillEndsAStuckChild(t *testing.T) {
	old := stdinLinger
	stdinLinger = 300 * time.Millisecond
	defer func() { stdinLinger = old }()
	done, cancel := hostCloseAfter(t, shellArgs{command: "trap '' HUP; exec sleep 300"}, []byte("x"))
	defer cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("run did not return after the linger")
	}
}
