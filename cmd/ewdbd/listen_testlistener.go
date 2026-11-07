//go:build testlistener

package main

import (
	"context"
	"flag"
	"log"

	"ewdb/internal/config"
	"ewdb/internal/session"
	"ewdb/internal/tcplisten"
)

var listenAddr *string

func registerFlags() {
	listenAddr = flag.String("listen", "127.0.0.1:5555", "loopback address to listen on")
}

func checkFlags() error {
	return tcplisten.Loopback(*listenAddr)
}

func run(ctx context.Context, cfg *config.Config, reg *session.Registry) int {
	l, err := tcplisten.Listen(ctx, *listenAddr)
	if err != nil {
		log.Print(err)
		return 1
	}
	log.Printf("listening on %s", l.Addr())
	err = tcplisten.Accept(ctx, l, func(t *tcplisten.Transport) {
		defer t.Close()
		if err := session.Serve(ctx, t, cfg, reg); err != nil {
			log.Printf("session ended: %v", err)
		}
	})
	if err != nil {
		log.Print(err)
		return 1
	}
	return 0
}
