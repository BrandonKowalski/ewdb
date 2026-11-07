package services

import (
	"os"

	"ewdb/internal/config"
	"ewdb/internal/session"
)

type Deps struct {
	Config      *config.Config
	LauncherDir string
	SystemPath  string
}

func Register(r *session.Registry, d *Deps) {
	r.Handle("shell", shellHandler(d))
	r.Handle("exec:", execHandler(d))
	r.Handle("sync:", syncHandler())
}

func Env(d *Deps, term string) []string {
	home := "/"
	if st, err := os.Stat("/root"); err == nil && st.IsDir() {
		home = "/root"
	}
	if term == "" {
		term = "dumb"
	}
	path := d.SystemPath
	if d.LauncherDir != "" {
		path = d.LauncherDir + ":" + path
	}
	return []string{
		"PATH=" + path,
		"HOME=" + home,
		"TERM=" + term,
		"EWDB_CONFIG=" + d.Config.Path,
	}
}
