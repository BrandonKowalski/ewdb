package services

import (
	"context"

	"ewdb/internal/session"
)

func execHandler(d *Deps) session.Handler {
	return func(ctx context.Context, s *session.Stream, arg string) {
		run(ctx, s, d, shellArgs{command: arg, session: s.Session().Context()})
	}
}
