package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"ewdb/internal/config"
	"ewdb/internal/session"
)

func main() {
	confPath := flag.String("c", "/etc/ewdbd.conf", "config path")
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
	reg := &session.Registry{}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	os.Exit(run(ctx, cfg, reg))
}
