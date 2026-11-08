package logcat

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type safeBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *safeBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *safeBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

const sample = "Nov  7 15:00:00 dsos user.info frontend: started\n" +
	"Nov  7 15:00:01 dsos kern.info kernel: boot\n" +
	"Nov  7 15:00:02 dsos user.info frontend[12]: pid tagged\n" +
	"Nov  7 15:00:03 dsos user.info frontend2: other\n"

func setup(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "messages")
	if content != "" {
		if err := os.WriteFile(log, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	conf := filepath.Join(dir, "c.conf")
	if err := os.WriteFile(conf, []byte("[logcat]\nfile = "+log+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("EWDB_CONFIG", conf)
	return log
}

func runDump(args ...string) (string, string, int) {
	var o, e bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	code := execute(ctx, args, &o, &e)
	return o.String(), e.String(), code
}

func waitFor(t *testing.T, out *safeBuf, want string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for !strings.Contains(out.String(), want) {
		if time.Now().After(deadline) {
			t.Fatalf("never saw %q, got %q", want, out.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func startFollow(t *testing.T, args ...string) (*safeBuf, func()) {
	t.Helper()
	out := &safeBuf{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int)
	go func() { done <- execute(ctx, args, out, &bytes.Buffer{}) }()
	return out, func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("follow did not stop")
		}
	}
}

func TestDumpExits(t *testing.T) {
	setup(t, sample)
	out, _, code := runDump("-d")
	if code != 0 || out != sample {
		t.Fatalf("code %d out %q", code, out)
	}
}

func TestClearTruncates(t *testing.T) {
	log := setup(t, sample)
	if _, _, code := runDump("-c"); code != 0 {
		t.Fatalf("code %d", code)
	}
	st, err := os.Stat(log)
	if err != nil || st.Size() != 0 {
		t.Fatalf("stat %v %v", st, err)
	}
}

func TestTagFilter(t *testing.T) {
	setup(t, sample)
	for _, args := range [][]string{{"-d", "frontend"}, {"-d", "frontend:I", "*:S"}, {"-v", "brief", "-b", "main", "-s", "-d", "frontend"}} {
		out, _, _ := runDump(args...)
		if !strings.Contains(out, "frontend: started") || !strings.Contains(out, "frontend[12]: pid tagged") {
			t.Fatalf("%v: %q", args, out)
		}
		if strings.Contains(out, "kernel:") || strings.Contains(out, "frontend2:") {
			t.Fatalf("%v: %q", args, out)
		}
	}
}

func TestFollowSeesAppend(t *testing.T) {
	log := setup(t, sample)
	out, stop := startFollow(t)
	defer stop()
	waitFor(t, out, "frontend2: other")
	f, _ := os.OpenFile(log, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString("Nov  7 15:00:04 dsos user.info frontend: later\n")
	f.Close()
	waitFor(t, out, "frontend: later")
}

func TestFollowSurvivesTruncate(t *testing.T) {
	log := setup(t, sample)
	out, stop := startFollow(t)
	defer stop()
	waitFor(t, out, "frontend2: other")
	os.Truncate(log, 0)
	time.Sleep(300 * time.Millisecond)
	f, _ := os.OpenFile(log, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString("Nov  7 15:00:05 dsos user.info a: new\n")
	f.Close()
	waitFor(t, out, "a: new")
}

func TestFollowSurvivesReplace(t *testing.T) {
	log := setup(t, sample)
	out, stop := startFollow(t)
	defer stop()
	waitFor(t, out, "frontend2: other")
	os.Rename(log, log+".0")
	os.WriteFile(log, []byte(strings.Repeat("Nov  7 15:00:06 dsos user.info b: rotated\n", 5)), 0o644)
	waitFor(t, out, "b: rotated")
}

func TestMissingFile(t *testing.T) {
	log := setup(t, "")
	useKmsg(t, "")
	out, errOut, code := runDump("-d")
	if code != 1 || out != "" || errOut != "ewdb-logcat: no log at "+log+"\n" {
		t.Fatalf("code %d out %q err %q", code, out, errOut)
	}
}

func TestTailCountPrintsLastLinesAndExits(t *testing.T) {
	setup(t, sample)
	lines := strings.SplitAfter(sample, "\n")
	for _, args := range [][]string{{"-t", "2"}, {"-t2"}, {"-t", "100"}} {
		out, _, code := runDump(args...)
		want := strings.Join(lines[2:], "")
		if args[len(args)-1] == "100" {
			want = sample
		}
		if code != 0 || out != want {
			t.Fatalf("%v: code %d out %q", args, code, out)
		}
	}
}

func TestTailCountAppliesAfterTagFilter(t *testing.T) {
	setup(t, sample)
	out, _, _ := runDump("-t", "1", "frontend")
	if out != "Nov  7 15:00:02 dsos user.info frontend[12]: pid tagged\n" {
		t.Fatalf("out %q", out)
	}
}

func TestOptionValuesAreNotTags(t *testing.T) {
	setup(t, sample)
	for _, args := range [][]string{{"-d", "-n", "4"}, {"-d", "-r", "16"}, {"-d", "-f", "/tmp/x"}, {"-d", "-m", "5"}, {"-d", "-e", "boot"}} {
		out, _, _ := runDump(args...)
		if out != sample {
			t.Fatalf("%v: %q", args, out)
		}
	}
}

func TestRecentCountThenFollows(t *testing.T) {
	log := setup(t, sample)
	out, stop := startFollow(t, "-T", "1")
	defer stop()
	waitFor(t, out, "frontend2: other")
	if strings.Contains(out.String(), "kernel:") {
		t.Fatalf("printed more than the last line: %q", out.String())
	}
	f, _ := os.OpenFile(log, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString("Nov  7 15:00:04 dsos user.info frontend: later\n")
	f.Close()
	waitFor(t, out, "frontend: later")
}
