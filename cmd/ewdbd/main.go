package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"ewdb/internal/config"
	"ewdb/internal/launchers"
	"ewdb/internal/services"
	"ewdb/internal/session"
)

const defaultPath = "/usr/sbin:/usr/bin:/sbin:/bin"

func main() {
	code, opts := dispatch(os.Args, os.Stdout, os.Stderr)
	if opts == nil {
		os.Exit(code)
	}
	os.Exit(daemon(opts))
}

func daemon(opts *daemonOpts) int {
	log.SetFlags(log.LstdFlags)
	log.SetPrefix("ewdbd: ")

	cfg := config.Load(opts.conf)
	for _, w := range cfg.Warnings {
		log.Printf("config: %s", w)
	}
	systemPath := os.Getenv("PATH")
	if systemPath == "" {
		systemPath = defaultPath
	}
	launcherDir := opts.launchers
	if err := launchers.Build(launcherDir, cfg.Commands, systemPath); err != nil {
		log.Printf("launchers: %v; running without launchers", err)
		launcherDir = ""
	}
	reg := &session.Registry{}
	services.Register(reg, &services.Deps{Config: cfg, LauncherDir: launcherDir, SystemPath: systemPath})

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	return run(ctx, cfg, reg)
}
