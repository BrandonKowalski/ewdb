package services

import (
	"context"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"ewdb/internal/session"
)

const actionTimeout = 30 * time.Second

var staticReplies = []struct {
	prefix string
	reply  string
}{
	{"unroot:", "unroot is not available on this device\n"},
	{"tcpip:", "Wi-Fi adb is not available on this device\n"},
	{"usb:", "already running on USB\n"},
	{"disable-verity:", "verity is not available on this device\n"},
	{"enable-verity:", "verity is not available on this device\n"},
	{"jdwp", ""},
	{"track-jdwp", ""},
	{"track-app", ""},
	{"sideload:", "sideload is not available on this device\n"},
	{"sideload-host:", "sideload-host is not available on this device\n"},
	{"abb:", "abb is not available on this device\n"},
	{"abb_exec:", "abb_exec is not available on this device\n"},
	{"backup:", "backup is not available on this device\n"},
	{"restore:", "restore is not available on this device\n"},
}

func miscHandlers(r *session.Registry, d *Deps) {
	for _, sr := range staticReplies {
		reply := sr.reply
		r.Handle(sr.prefix, func(ctx context.Context, s *session.Stream, arg string) {
			io.WriteString(s, reply)
		})
	}
	r.Handle("reboot:", func(ctx context.Context, s *session.Stream, arg string) {
		reboot(s, d, arg)
	})
	r.Handle("remount:", func(ctx context.Context, s *session.Stream, arg string) {
		mapped(ctx, s, d, "remount", "remount is not available on this device\n")
	})
	r.Handle("root:", func(ctx context.Context, s *session.Stream, arg string) {
		fallback := "root is not available on this device\n"
		if os.Geteuid() == 0 {
			fallback = "adbd is already running as root\n"
		}
		mapped(ctx, s, d, "root", fallback)
	})
	r.Handle("reconnect", func(ctx context.Context, s *session.Stream, arg string) {
		io.WriteString(s, "done")
		s.Session().Kick()
	})
	r.Handle("framebuffer:", func(ctx context.Context, s *session.Stream, arg string) {
		framebuffer(ctx, s, d)
	})
}

func runAction(ctx context.Context, d *Deps, command string, out io.Writer) {
	ctx, cancel := context.WithTimeout(ctx, actionTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, d.Config.Shell, "-c", command)
	cmd.Env = Env(d, "")
	cmd.Dir = "/"
	cmd.Stdout, cmd.Stderr = out, out
	cmd.WaitDelay = time.Second
	cmd.Run()
}

func mapped(ctx context.Context, w io.Writer, d *Deps, name, fallback string) {
	if command, ok := d.Config.Command(name); ok {
		runAction(ctx, d, command, w)
		return
	}
	io.WriteString(w, fallback)
}

func reboot(s io.WriteCloser, d *Deps, arg string) {
	syscall.Sync()
	key := "reboot"
	if arg != "" {
		key += "." + arg
	}
	command, ok := d.Config.Command(key)
	if !ok && arg == "" {
		command, ok = "reboot", true
	}
	if ok {
		s.Close()
		runAction(context.Background(), d, command, io.Discard)
		return
	}
	io.WriteString(s, restart2(arg))
}

func rebootUnavailable(arg string) string {
	return "reboot " + strings.TrimSpace(arg) + " is not available on this device\n"
}
