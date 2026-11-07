//go:build !linux || testlistener

package services

import (
	"testing"

	"ewdb/internal/config"
)

func TestRebootUnmappedArgIsUnavailableUnderTestListener(t *testing.T) {
	d := &Deps{Config: &config.Config{Shell: "/bin/sh", Commands: map[string]string{}}, SystemPath: "/usr/bin:/bin"}
	var s closeRecorder
	reboot(&s, d, "recovery")
	if got := s.String(); got != "reboot recovery is not available on this device\n" {
		t.Fatalf("got %q", got)
	}
}
