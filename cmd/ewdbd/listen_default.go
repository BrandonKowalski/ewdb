//go:build !testlistener

package main

import (
	"context"
	"errors"
	"log"
	"time"

	"ewdb/internal/config"
	"ewdb/internal/session"
	"ewdb/internal/usb"
)

const openRetry = time.Second

func registerFlags() {}

func checkFlags() error { return nil }

func openUSB(ctx context.Context, spec string) (usb.Endpoint, error) {
	var last string
	for {
		ep, err := usb.Open(spec)
		if err == nil {
			return ep, nil
		}
		if errors.Is(err, usb.ErrBadSpec) {
			return nil, err
		}
		if msg := err.Error(); msg != last {
			log.Printf("%v; retrying every %v", err, openRetry)
			last = msg
		}
		select {
		case <-time.After(openRetry):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

func run(ctx context.Context, cfg *config.Config, reg *session.Registry) int {
	ep, err := openUSB(ctx, cfg.USB)
	if err != nil {
		if ctx.Err() != nil {
			return 0
		}
		log.Print(err)
		return 1
	}
	log.Printf("serving adb over usb (%s)", cfg.USB)
	for {
		t, err := ep.Next(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return 0
			}
			log.Print(err)
			return 1
		}
		if err := session.Serve(ctx, t, cfg, reg); err != nil && ctx.Err() == nil {
			log.Printf("session ended: %v", err)
		}
		if ctx.Err() != nil {
			return 0
		}
	}
}
