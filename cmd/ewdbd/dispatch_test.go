package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSelectTool(t *testing.T) {
	cases := []struct {
		argv     []string
		name     string
		args     []string
		explicit bool
	}{
		{[]string{"ewdb-getprop", "ro.x"}, "getprop", []string{"ro.x"}, true},
		{[]string{"/usr/bin/ewdb-input", "--serve"}, "input", []string{"--serve"}, true},
		{[]string{"/run/ewdbd/bin/screencap", "-p"}, "screencap", []string{"-p"}, true},
		{[]string{"logcat"}, "logcat", []string{}, true},
		{[]string{"ewdb-getprop", "logcat"}, "getprop", []string{"logcat"}, true},
		{[]string{"/usr/bin/ewdbd", "bugreportz", "-p"}, "bugreportz", []string{"-p"}, true},
		{[]string{"ewdbd", "screencap"}, "screencap", []string{}, true},
		{[]string{"ewdbd"}, "", nil, false},
		{[]string{"ewdbd", "-c", "/etc/x.conf"}, "", nil, false},
		{[]string{"ewdbd", "ewdb-getprop"}, "", nil, false},
		{[]string{"ewdb-bogus"}, "", nil, true},
	}
	for _, c := range cases {
		name, args, explicit := selectTool(c.argv)
		if name != c.name || explicit != c.explicit || (name != "" && !reflect.DeepEqual(args, c.args)) {
			t.Errorf("selectTool(%q) = %q %q %v, want %q %q %v", c.argv, name, args, explicit, c.name, c.args, c.explicit)
		}
	}
}

func TestEveryToolIsRegistered(t *testing.T) {
	for _, n := range []string{"getprop", "logcat", "bugreportz", "input", "screencap"} {
		if tools[n] == nil {
			t.Errorf("tool %s missing", n)
		}
	}
	if len(tools) != 5 {
		t.Errorf("tools %d, want 5", len(tools))
	}
}

func dispatchOut(argv ...string) (int, *daemonOpts, string, string) {
	var o, e bytes.Buffer
	code, opts := dispatch(argv, &o, &e)
	return code, opts, o.String(), e.String()
}

func TestDaemonFlagsParse(t *testing.T) {
	code, opts, _, se := dispatchOut("/usr/bin/ewdbd", "-c", "/tmp/a.conf", "-launchers", "/tmp/l")
	if opts == nil {
		t.Fatalf("no daemon, code %d stderr %q", code, se)
	}
	if opts.conf != "/tmp/a.conf" || opts.launchers != "/tmp/l" {
		t.Fatalf("opts %+v", opts)
	}
	_, opts, _, _ = dispatchOut("ewdbd")
	if opts == nil || opts.conf != "/etc/ewdbd.conf" || opts.launchers != "/run/ewdbd/bin" {
		t.Fatalf("defaults %+v", opts)
	}
}

func TestHelpDoesNotStartDaemon(t *testing.T) {
	for _, h := range []string{"help", "-h", "--help"} {
		code, opts, so, _ := dispatchOut("/usr/bin/ewdbd", h)
		if opts != nil || code != 0 {
			t.Fatalf("%s: code %d opts %v", h, code, opts)
		}
		if !strings.HasPrefix(so, "usage: ewdbd [-c CONFIG]") || !strings.Contains(so, "screencap") {
			t.Fatalf("%s: usage %q", h, so)
		}
	}
}

func TestUsageNamesInvokedProgram(t *testing.T) {
	_, _, so, _ := dispatchOut("/opt/bin/mydbd", "help")
	if !strings.HasPrefix(so, "usage: mydbd ") || !strings.Contains(so, "\n       mydbd TOOL [ARGS...]\n") {
		t.Fatalf("usage %q", so)
	}
}

func TestUnknownArgumentsDoNotStartDaemon(t *testing.T) {
	cases := []struct {
		argv []string
		msg  string
	}{
		{[]string{"ewdbd", "bogus"}, `ewdbd: unknown tool "bogus"`},
		{[]string{"ewdbd", "-c", "/x", "getprop"}, `ewdbd: unexpected argument "getprop" after flags`},
		{[]string{"ewdbd", "-nope"}, "flag provided but not defined: -nope"},
		{[]string{"ewdb-bogus", "x"}, `ewdb-bogus: unknown tool "bogus"`},
	}
	for _, c := range cases {
		code, opts, so, se := dispatchOut(c.argv...)
		if opts != nil || code != 2 || so != "" {
			t.Fatalf("%q: code %d opts %v stdout %q", c.argv, code, opts, so)
		}
		if !strings.HasPrefix(se, c.msg+"\n") || !strings.Contains(se, "usage: ") {
			t.Fatalf("%q: stderr %q", c.argv, se)
		}
	}
}

func TestDispatchRunsTool(t *testing.T) {
	conf := filepath.Join(t.TempDir(), "ewdbd.conf")
	if err := os.WriteFile(conf, []byte("[device]\nmodel = RG DS Plus\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("EWDB_CONFIG", conf)
	for _, argv := range [][]string{
		{"/usr/bin/ewdbd", "getprop", "ro.product.model"},
		{"/usr/bin/ewdb-getprop", "ro.product.model"},
		{"getprop", "ro.product.model"},
	} {
		code, opts, so, se := dispatchOut(argv...)
		if opts != nil || code != 0 || so != "RG DS Plus\n" || se != "" {
			t.Fatalf("%q: code %d opts %v stdout %q stderr %q", argv, code, opts, so, se)
		}
	}
	code, _, so, _ := dispatchOut("ewdbd", "bugreportz", "-v")
	if code != 0 || so != "" {
		t.Fatalf("bugreportz -v: code %d stdout %q", code, so)
	}
}
