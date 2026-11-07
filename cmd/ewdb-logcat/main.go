package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"ewdb/internal/config"
)

const pollInterval = 200 * time.Millisecond

type options struct {
	dump  bool
	clear bool
	tail  int
	tags  []string
}

const valueOptions = "bvtTnrfme"

func parseArgs(args []string) options {
	var o options
	for i := 0; i < len(args); i++ {
		a := args[i]
		if len(a) >= 2 && a[0] == '-' && strings.IndexByte(valueOptions, a[1]) >= 0 {
			value := a[2:]
			if value == "" && i+1 < len(args) {
				i++
				value = args[i]
			}
			switch a[1] {
			case 't':
				o.dump = true
				o.tail, _ = strconv.Atoi(value)
			case 'T':
				o.tail, _ = strconv.Atoi(value)
			}
			continue
		}
		switch {
		case a == "-d":
			o.dump = true
		case a == "-c" || a == "-C":
			o.clear = true
		case strings.HasPrefix(a, "-"):
		case a == "*:S":
		default:
			tag := a
			if j := strings.LastIndexByte(a, ':'); j >= 0 {
				tag = a[:j]
			}
			if tag != "" && tag != "*" {
				o.tags = append(o.tags, tag)
			}
		}
	}
	return o
}

func (o options) keep(line string) bool {
	if len(o.tags) == 0 {
		return true
	}
	f := strings.Fields(line)
	if len(f) < 6 {
		return false
	}
	for _, t := range o.tags {
		if f[5] == t+":" || strings.HasPrefix(f[5], t+"[") {
			return true
		}
	}
	return false
}

type follower struct {
	opt     options
	out     io.Writer
	pending string
	hold    bool
	held    []string
}

func (f *follower) emit(line string) error {
	if f.hold {
		f.held = append(f.held, line)
		if len(f.held) > f.opt.tail {
			f.held = f.held[1:]
		}
		return nil
	}
	_, err := io.WriteString(f.out, line)
	return err
}

func (f *follower) release() error {
	f.hold = false
	held := f.held
	f.held = nil
	for _, line := range held {
		if _, err := io.WriteString(f.out, line); err != nil {
			return err
		}
	}
	return nil
}

func (f *follower) feed(data []byte) error {
	f.pending += string(data)
	for {
		i := strings.IndexByte(f.pending, '\n')
		if i < 0 {
			return nil
		}
		line := f.pending[:i+1]
		f.pending = f.pending[i+1:]
		if f.opt.keep(line) {
			if err := f.emit(line); err != nil {
				return err
			}
		}
	}
}

func drain(file *os.File, f *follower, pos *int64) error {
	buf := make([]byte, 64*1024)
	for {
		n, err := file.Read(buf)
		if n > 0 {
			*pos += int64(n)
			if werr := f.feed(buf[:n]); werr != nil {
				return werr
			}
		}
		if err != nil || n == 0 {
			return nil
		}
	}
}

func execute(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	opt := parseArgs(args)
	path := os.Getenv("EWDB_CONFIG")
	if path == "" {
		path = "/etc/ewdbd.conf"
	}
	logPath := "/var/log/messages"
	for _, kv := range config.Load(path).Section("logcat") {
		if kv.Key == "file" && kv.Value != "" {
			logPath = kv.Value
		}
	}
	file, err := os.Open(logPath)
	if err != nil {
		fmt.Fprintf(stderr, "ewdb-logcat: no log at %s\n", logPath)
		return 1
	}
	defer func() { file.Close() }()
	if opt.clear {
		if err := os.Truncate(logPath, 0); err != nil {
			fmt.Fprintf(stderr, "ewdb-logcat: %v\n", err)
			return 1
		}
		return 0
	}
	fol := &follower{opt: opt, out: stdout, hold: opt.tail > 0}
	var pos int64
	if drain(file, fol, &pos) != nil {
		return 0
	}
	if opt.dump {
		if fol.pending != "" && opt.keep(fol.pending) {
			fol.emit(fol.pending)
		}
		fol.release()
		return 0
	}
	if fol.release() != nil {
		return 0
	}
	opened, err := file.Stat()
	if err != nil {
		return 1
	}
	tick := time.NewTicker(pollInterval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return 0
		case <-tick.C:
		}
		if drain(file, fol, &pos) != nil {
			return 0
		}
		st, err := os.Stat(logPath)
		if err != nil {
			continue
		}
		if !os.SameFile(opened, st) {
			nf, err := os.Open(logPath)
			if err != nil {
				continue
			}
			file.Close()
			file = nf
			opened, _ = file.Stat()
			pos = 0
			fol.pending = ""
			if drain(file, fol, &pos) != nil {
				return 0
			}
			continue
		}
		if st.Size() < pos {
			if _, err := file.Seek(0, io.SeekStart); err != nil {
				continue
			}
			pos = 0
			fol.pending = ""
			if drain(file, fol, &pos) != nil {
				return 0
			}
		}
	}
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	sig := make(chan os.Signal, 4)
	signal.Notify(sig, syscall.SIGHUP, syscall.SIGPIPE, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sig
		cancel()
	}()
	code := execute(ctx, os.Args[1:], os.Stdout, os.Stderr)
	os.Exit(code)
}
