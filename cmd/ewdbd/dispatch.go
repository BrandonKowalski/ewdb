package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"

	"ewdb/internal/tools/bugreportz"
	"ewdb/internal/tools/getprop"
	"ewdb/internal/tools/input"
	"ewdb/internal/tools/logcat"
	"ewdb/internal/tools/screencap"
)

type toolMain func(args []string, stdout, stderr io.Writer) int

var tools = map[string]toolMain{
	"bugreportz": bugreportz.Main,
	"getprop":    getprop.Main,
	"input":      input.Main,
	"logcat":     logcat.Main,
	"screencap":  screencap.Main,
}

const toolPrefix = "ewdb-"

type daemonOpts struct {
	conf      string
	launchers string
}

func selectTool(argv []string) (string, []string, bool) {
	base := filepath.Base(argv[0])
	if name, ok := strings.CutPrefix(base, toolPrefix); ok {
		if tools[name] != nil {
			return name, argv[1:], true
		}
		return "", nil, true
	}
	if tools[base] != nil {
		return base, argv[1:], true
	}
	if len(argv) > 1 && tools[argv[1]] != nil {
		return argv[1], argv[2:], true
	}
	return "", nil, false
}

func versionString(bi *debug.BuildInfo, ok bool) string {
	if !ok {
		return "unknown"
	}
	var rev, when string
	dirty := false
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.time":
			when = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if rev == "" {
		return strings.TrimSpace(bi.Main.Version + " " + bi.GoVersion)
	}
	if len(rev) > 12 {
		rev = rev[:12]
	}
	if dirty {
		rev += "-dirty"
	}
	return strings.Join(strings.Fields(rev+" "+when+" "+bi.GoVersion), " ")
}

var readBuildInfo = debug.ReadBuildInfo

func usage(w io.Writer, prog string, fs *flag.FlagSet) {
	names := make([]string, 0, len(tools))
	for n := range tools {
		names = append(names, n)
	}
	sort.Strings(names)
	fmt.Fprintf(w, "usage: %s [-c CONFIG] [-launchers DIR]\n", prog)
	fmt.Fprintf(w, "       %s TOOL [ARGS...]\n", prog)
	fmt.Fprintf(w, "       %s<TOOL> [ARGS...]\n", toolPrefix)
	fmt.Fprintf(w, "tools: %s\n", strings.Join(names, ", "))
	fmt.Fprintln(w, "with no arguments other than flags it runs the adb daemon")
	fs.SetOutput(w)
	fs.PrintDefaults()
}

func dispatch(argv []string, stdout, stderr io.Writer) (int, *daemonOpts) {
	prog := filepath.Base(argv[0])
	fs := flag.NewFlagSet(prog, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	opts := &daemonOpts{}
	showVersion := fs.Bool("version", false, "print version and exit")
	fs.StringVar(&opts.conf, "c", "/etc/ewdbd.conf", "config path")
	fs.StringVar(&opts.launchers, "launchers", "/run/ewdbd/bin", "directory for command launchers")
	registerFlags(fs)

	name, args, explicit := selectTool(argv)
	if name != "" {
		return tools[name](args, stdout, stderr), nil
	}
	if explicit {
		fmt.Fprintf(stderr, "%s: unknown tool %q\n", prog, strings.TrimPrefix(prog, toolPrefix))
		usage(stderr, prog, fs)
		return 2, nil
	}
	if len(argv) > 1 && argv[1] == "help" {
		usage(stdout, prog, fs)
		return 0, nil
	}
	if err := fs.Parse(argv[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			usage(stdout, prog, fs)
			return 0, nil
		}
		fmt.Fprintln(stderr, err)
		usage(stderr, prog, fs)
		return 2, nil
	}
	if *showVersion {
		fmt.Fprintf(stdout, "%s %s\n", prog, versionString(readBuildInfo()))
		return 0, nil
	}
	if fs.NArg() > 0 {
		if fs.NArg() == len(argv)-1 {
			fmt.Fprintf(stderr, "%s: unknown tool %q\n", prog, fs.Arg(0))
		} else {
			fmt.Fprintf(stderr, "%s: unexpected argument %q after flags\n", prog, fs.Arg(0))
		}
		usage(stderr, prog, fs)
		return 2, nil
	}
	if err := checkFlags(); err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", prog, err)
		return 2, nil
	}
	return 0, opts
}
