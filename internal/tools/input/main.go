package input

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"ewdb/internal/config"
)

func loadConfig() *config.Config {
	path := os.Getenv("EWDB_CONFIG")
	if path == "" {
		path = "/etc/ewdbd.conf"
	}
	return config.Load(path)
}

func setting(c *config.Config, key string) string {
	v := ""
	for _, e := range c.Section("input") {
		if e.Key == key {
			v = e.Value
		}
	}
	return v
}

func socketPath(c *config.Config) string {
	if s := setting(c, "socket"); s != "" {
		return s
	}
	return defaultSock
}

func serve(c *config.Config) int {
	for _, w := range c.Warnings {
		log.Print(w)
	}
	kb, closeKb, err := openKeyboard()
	if err != nil {
		log.Print(err)
		return 1
	}
	defer closeKb()
	ln, err := listen(socketPath(c))
	if err != nil {
		log.Print(err)
		return 1
	}
	defer os.Remove(socketPath(c))
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	serveLoop(ctx, ln, textRunner(kb, time.Sleep))
	return 0
}

func local(c *config.Config, devs Devices, args []string, stderr io.Writer) int {
	keys, warnings := parseKeys(c.Section("keys"))
	for _, w := range warnings {
		fmt.Fprintf(stderr, "ewdb-input: warning: %s\n", w)
	}
	in := &injector{devs: devs, keys: keys, touch: setting(c, "touch"), sleep: time.Sleep}
	if err := in.run(args); err != nil {
		fmt.Fprintf(stderr, "ewdb-input: %v\n", err)
		return 1
	}
	return 0
}

func execute(args []string, stderr io.Writer) int {
	c := loadConfig()
	if len(args) == 1 && args[0] == "--serve" {
		return serve(c)
	}
	if len(args) > 0 && strings.HasPrefix(args[0], "--") {
		fmt.Fprintf(stderr, "ewdb-input: unknown option %s\n", args[0])
		return 1
	}
	if cmd, _ := command(args); cmd == "text" {
		return client(socketPath(c), args, stderr)
	}
	if len(args) == 0 {
		return client(socketPath(c), args, stderr)
	}
	return local(c, systemDevices{}, args, stderr)
}

func Main(args []string, stdout, stderr io.Writer) int {
	log.SetOutput(stderr)
	log.SetFlags(0)
	log.SetPrefix("ewdb-input: ")
	return execute(args, stderr)
}
