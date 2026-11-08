//go:build !testlistener

package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"time"

	"ewdb/internal/config"
	"ewdb/internal/session"
	"ewdb/internal/usb"
)

const (
	openRetry = time.Second
	autoSpec  = "auto"
)

func registerFlags(*flag.FlagSet) {}

func checkFlags() error { return nil }

func openUSB(ctx context.Context, spec string, open func(string) (usb.Endpoint, error)) (usb.Endpoint, string, error) {
	var lastSpec, lastAuto string
	for {
		ep, err := open(spec)
		if err == nil {
			return ep, spec, nil
		}
		if spec != autoSpec {
			if errors.Is(err, usb.ErrBadSpec) {
				log.Printf("%v; using %s", err, autoSpec)
				spec = autoSpec
				continue
			}
			if msg := err.Error(); msg != lastSpec {
				log.Printf("%v; trying %s", err, autoSpec)
				lastSpec = msg
			}
			if ep, err = open(autoSpec); err == nil {
				return ep, autoSpec, nil
			}
		}
		if msg := err.Error(); msg != lastAuto {
			log.Printf("%v; retrying every %v", err, openRetry)
			lastAuto = msg
		}
		select {
		case <-time.After(openRetry):
		case <-ctx.Done():
			return nil, "", ctx.Err()
		}
	}
}

func run(ctx context.Context, cfg *config.Config, reg *session.Registry) int {
	ep, spec, err := openUSB(ctx, cfg.USB, usb.Open)
	if err != nil {
		if ctx.Err() != nil {
			return 0
		}
		log.Print(err)
		return 1
	}
	log.Printf("serving adb over usb (%s)", spec)
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
