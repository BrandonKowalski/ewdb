package main

import (
	"context"
	"flag"
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
	confPath := flag.String("c", "/etc/ewdbd.conf", "config path")
	launcherDir := flag.String("launchers", "/run/ewdbd/bin", "directory for command launchers")
	registerFlags()
	flag.Parse()
	log.SetFlags(log.LstdFlags)
	log.SetPrefix("ewdbd: ")
	if err := checkFlags(); err != nil {
		log.SetFlags(0)
		log.Print(err)
		os.Exit(2)
	}

	cfg := config.Load(*confPath)
	for _, w := range cfg.Warnings {
		log.Printf("config: %s", w)
	}
	systemPath := os.Getenv("PATH")
	if systemPath == "" {
		systemPath = defaultPath
	}
	if err := launchers.Build(*launcherDir, cfg.Commands, systemPath); err != nil {
		log.Printf("launchers: %v; running without launchers", err)
		*launcherDir = ""
	}
	reg := &session.Registry{}
	services.Register(reg, &services.Deps{Config: cfg, LauncherDir: *launcherDir, SystemPath: systemPath})

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	os.Exit(run(ctx, cfg, reg))
}
