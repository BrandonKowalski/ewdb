package ewdb

import (
	"debug/elf"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestBinariesAreStatic(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping build check in short mode")
	}
	entries, err := os.ReadDir("cmd")
	if err != nil {
		t.Fatal(err)
	}
	targets := []struct {
		name string
		env  []string
	}{
		{"arm64", []string{"GOARCH=arm64"}},
		{"arm", []string{"GOARCH=arm", "GOARM=7"}},
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		for _, tg := range targets {
			name := e.Name()
			tg := tg
			t.Run(name+"/"+tg.name, func(t *testing.T) {
				out := filepath.Join(t.TempDir(), name)
				cmd := exec.Command("go", "build", "-o", out, "./cmd/"+name)
				cmd.Env = append(os.Environ(), "GOOS=linux", "CGO_ENABLED=0")
				cmd.Env = append(cmd.Env, tg.env...)
				if b, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("build: %v\n%s", err, b)
				}
				f, err := elf.Open(out)
				if err != nil {
					t.Fatal(err)
				}
				defer f.Close()
				for _, p := range f.Progs {
					if p.Type == elf.PT_INTERP {
						t.Fatal("has PT_INTERP")
					}
				}
				libs, err := f.ImportedLibraries()
				if err != nil {
					t.Fatal(err)
				}
				if len(libs) != 0 {
					t.Fatalf("imported libraries: %v", libs)
				}
			})
		}
	}
}
