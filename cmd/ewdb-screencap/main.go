package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"ewdb/internal/capture"
)

type connector struct {
	Name string
	Lit  bool
}

type source interface {
	Connectors() ([]connector, error)
	Grab(name, label string) (capture.Image, error)
	Close()
}

type backend struct {
	openDRM func() (source, error)
	grabFB  func() (capture.Image, error)
}

type screen struct {
	name      string
	connector string
}

const usageText = `usage: screencap [-p] [-d NAME|INDEX] [--screens name=CONNECTOR,...] [FILE]
  -p         write PNG, also chosen by a FILE ending in .png
  -d         one screen, by --screens name or by index; all stacked by default
  --screens  name the connectors to capture, top to bottom, for example top=DSI-2,bottom=DSI-1
  without -p the output is raw: width, height, format 1, then RGBA pixels
`

func parseScreens(s string) ([]screen, error) {
	var out []screen
	for _, part := range strings.Split(s, ",") {
		name, conn, ok := strings.Cut(part, "=")
		if !ok || name == "" || conn == "" {
			return nil, fmt.Errorf("bad --screens entry %q", part)
		}
		out = append(out, screen{name, conn})
	}
	return out, nil
}

func pick(list []screen, want string) ([]screen, bool) {
	for _, s := range list {
		if s.name == want {
			return []screen{s}, true
		}
	}
	if i, err := strconv.Atoi(want); err == nil && i >= 0 && i < len(list) {
		return list[i : i+1], true
	}
	return nil, false
}

func run(args []string, stdout, stderr io.Writer, be backend) int {
	fs := flag.NewFlagSet("screencap", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var (
		asPNG   bool
		want    string
		screens string
		help    bool
	)
	fs.BoolVar(&asPNG, "p", false, "")
	fs.StringVar(&want, "d", "", "")
	fs.StringVar(&screens, "screens", "", "")
	fs.BoolVar(&help, "h", false, "")
	if err := fs.Parse(args); err != nil || fs.NArg() > 1 {
		fmt.Fprint(stderr, usageText)
		return 2
	}
	if help {
		fmt.Fprint(stdout, usageText)
		return 0
	}
	path := fs.Arg(0)
	if len(path) > 4 && strings.EqualFold(path[len(path)-4:], ".png") {
		asPNG = true
	}
	var named []screen
	if screens != "" {
		var err error
		if named, err = parseScreens(screens); err != nil {
			fmt.Fprintf(stderr, "screencap: %v\n", err)
			return 2
		}
	}

	var imgs []capture.Image
	src, err := be.openDRM()
	drmTried := err == nil
	if err != nil {
		fmt.Fprintf(stderr, "screencap: drm: %v\n", err)
	}
	if drmTried {
		code, got, lit := captureDRM(src, named, want, stderr)
		src.Close()
		if code != 0 {
			return code
		}
		imgs = got
		if lit == 0 {
			drmTried = false
		}
	}
	if !drmTried {
		img, err := be.grabFB()
		if err != nil {
			fmt.Fprintf(stderr, "screencap: %v\n", err)
			return 1
		}
		imgs = []capture.Image{img}
	}
	if len(imgs) == 0 {
		return 1
	}
	img := capture.Stack(imgs)

	out := stdout
	var f *os.File
	if path != "" {
		f, err = os.Create(path)
		if err != nil {
			fmt.Fprintf(stderr, "screencap: %v\n", err)
			return 1
		}
		out = f
	}
	if asPNG {
		err = capture.WritePNG(out, img)
	} else {
		err = capture.WriteRaw(out, img)
	}
	if f != nil {
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}
	if err != nil {
		fmt.Fprintf(stderr, "screencap: write failed: %v\n", err)
		return 1
	}
	return 0
}

func captureDRM(src source, named []screen, want string, stderr io.Writer) (int, []capture.Image, int) {
	conns, err := src.Connectors()
	if err != nil {
		fmt.Fprintf(stderr, "screencap: drm: %v\n", err)
		return 0, nil, 0
	}
	lit := map[string]bool{}
	anyLit := 0
	var list []screen
	for _, c := range conns {
		lit[c.Name] = c.Lit
		if c.Lit {
			anyLit++
		}
		if named == nil && c.Lit {
			list = append(list, screen{c.Name, c.Name})
		}
	}
	if named != nil {
		list = named
	}
	if want != "" {
		var ok bool
		if list, ok = pick(list, want); !ok {
			fmt.Fprintf(stderr, "screencap: no screen %q\n", want)
			fmt.Fprint(stderr, usageText)
			return 2, nil, anyLit
		}
	}
	var imgs []capture.Image
	for _, s := range list {
		if !lit[s.connector] {
			fmt.Fprintf(stderr, "screencap: the %s screen is off\n", s.name)
			continue
		}
		img, err := src.Grab(s.connector, s.name)
		if err != nil {
			fmt.Fprintf(stderr, "screencap: %v\n", err)
			continue
		}
		imgs = append(imgs, img)
	}
	return 0, imgs, anyLit
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, newBackend()))
}

var errNoDRM = errors.New("no DRM card")
