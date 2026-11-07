//go:build !testlistener

package main

import (
	"context"
	"log"

	"ewdb/internal/config"
	"ewdb/internal/session"
)

func registerFlags() {}

func checkFlags() error { return nil }

func run(ctx context.Context, cfg *config.Config, reg *session.Registry) int {
	log.Print("no transport available")
	return 1
}
