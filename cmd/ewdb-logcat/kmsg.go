package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

var (
	kmsgPath = "/dev/kmsg"
	bootTime = func() time.Time {
		var ts unix.Timespec
		if err := unix.ClockGettime(unix.CLOCK_MONOTONIC, &ts); err != nil {
			return time.Now()
		}
		return time.Now().Add(-time.Duration(ts.Nano()))
	}
	hostName = func() string {
		h, _ := os.Hostname()
		return h
	}
)

var facilities = []string{
	"kern", "user", "mail", "daemon", "auth", "syslog", "lpr", "news", "uucp", "cron", "authpriv", "ftp",
	"ntp", "security", "console", "solaris-cron",
	"local0", "local1", "local2", "local3", "local4", "local5", "local6", "local7",
}

var levels = []string{"emerg", "alert", "crit", "err", "warning", "notice", "info", "debug"}

func formatKmsg(record string, boot time.Time, host string) (string, bool) {
	record = strings.TrimSuffix(record, "\n")
	if strings.HasPrefix(record, " ") {
		return "", false
	}
	prefix, msg, ok := strings.Cut(record, ";")
	if !ok {
		return "", false
	}
	fields := strings.Split(prefix, ",")
	if len(fields) < 3 {
		return "", false
	}
	pri, err := strconv.Atoi(fields[0])
	if err != nil || pri < 0 {
		return "", false
	}
	usec, err := strconv.ParseInt(fields[2], 10, 64)
	if err != nil || usec < 0 {
		return "", false
	}
	facility := "fac" + strconv.Itoa(pri>>3)
	if pri>>3 < len(facilities) {
		facility = facilities[pri>>3]
	}
	stamp := boot.Add(time.Duration(usec) * time.Microsecond).Format(time.Stamp)
	head := fmt.Sprintf("%s %s %s.%s", stamp, host, facility, levels[pri&7])
	if pri>>3 == 0 {
		return fmt.Sprintf("%s kernel: [%5d.%06d] %s\n", head, usec/1000000, usec%1000000, msg), true
	}
	return fmt.Sprintf("%s %s\n", head, msg), true
}

type kmsgReader struct {
	fd      int
	boot    time.Time
	host    string
	pending string
}

func openKmsg() (*kmsgReader, error) {
	fd, err := unix.Open(kmsgPath, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	return &kmsgReader{fd: fd, boot: bootTime(), host: hostName()}, nil
}

func (k *kmsgReader) drain(f *follower) error {
	buf := make([]byte, 16*1024)
	for {
		n, err := unix.Read(k.fd, buf)
		switch {
		case err == unix.EINTR || err == unix.EPIPE:
			continue
		case err == unix.EAGAIN:
			return nil
		case err != nil:
			return err
		case n == 0:
			return nil
		}
		k.pending += string(buf[:n])
		for {
			i := strings.IndexByte(k.pending, '\n')
			if i < 0 {
				break
			}
			line, ok := formatKmsg(k.pending[:i+1], k.boot, k.host)
			k.pending = k.pending[i+1:]
			if ok && f.opt.keep(line) {
				if err := f.emit(line); err != nil {
					return err
				}
			}
		}
	}
}

func runKmsg(ctx context.Context, k *kmsgReader, opt options, stdout, stderr io.Writer) int {
	defer unix.Close(k.fd)
	if opt.clear {
		fmt.Fprintln(stderr, "ewdb-logcat: the kernel log cannot be cleared")
		return 0
	}
	fol := &follower{opt: opt, out: stdout, hold: opt.tail > 0}
	if err := k.drain(fol); err != nil {
		return 0
	}
	if fol.release() != nil || opt.dump {
		return 0
	}
	tick := time.NewTicker(pollInterval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return 0
		case <-tick.C:
		}
		if k.drain(fol) != nil {
			return 0
		}
	}
}
