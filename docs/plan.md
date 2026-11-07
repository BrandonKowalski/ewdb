# ewdb Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace dsOS's adbd 4.2.2 with ewdb, a static Go adb daemon and tool set for Linux handhelds that a stock platform-tools `adb` drives the way it drives a phone.

**Architecture:** A new repository, `/Users/btk/Developer/ewdb`, holds `ewdbd` (wire protocol, session, services, USB transports) and the default tools (`ewdb-getprop`, `ewdb-logcat`, `ewdb-bugreportz`, `ewdb-input`, `ewdb-screencap`), all pure Go and fully static. A sidecar INI file, `/etc/ewdbd.conf`, carries board details and a `[commands]` section that maps adb commands to Linux commands through launcher scripts placed first on the `PATH` of every shell the daemon starts. dsOS builds it as a Buildroot package, ships a config, and drops android-tools.

**Tech Stack:** Go 1.26 (Mac: go1.26.4; Buildroot 2026.02.3: 1.26.3), `CGO_ENABLED=0`, `github.com/creack/pty`, `golang.org/x/sys/unix`, platform-tools `adb` 37.0.0 for end-to-end tests, Buildroot `golang-package`, BusyBox `syslogd`/`klogd`/`logger`.

**Spec:** `docs/design.md`. Read it before starting. AOSP `packages/modules/adb` is the protocol reference wherever this plan says "as AOSP": `adb.h`, `adb.cpp`, `transport.cpp`, `daemon/shell_service.cpp`, `shell_protocol.h`, `file_sync_protocol.h`, `daemon/file_sync_service.cpp`, `daemon/services.cpp`, `daemon/usb_ffs.cpp`, `client/bugreport.cpp`, and frameworks `cmds/bugreportz`.

## Global Constraints

- Every shipped binary: `CGO_ENABLED=0`, no `PT_INTERP`, no `DT_NEEDED`, for `linux/arm64` and `linux/arm` with `GOARM=7`. Floor: Linux 3.2.
- Dependencies: only `github.com/creack/pty` and `golang.org/x/sys`. `vendor/` is committed (Buildroot builds a local site with `-mod=vendor`).
- License: Apache-2.0 for ewdb (`LICENSE` file at the repo root). Ported AOSP code keeps its notice in `NOTICE`.
- Module path `ewdb` (designer's call; rename when it gets a public home).
- No network listener in shipped builds. The TCP listener exists only under build tag `testlistener` and refuses non-loopback addresses. Device-side `reverse` and forward listeners bind 127.0.0.1 or abstract/filesystem unix sockets only.
- USB IDs and serial are set by the dsOS gadget script, unchanged (`18d1:4ee7`, serial from `dsos-serial`).
- Banner: `device::ro.product.name=<name>;ro.product.model=<model>;ro.product.device=<device>;features=<features>`. dsOS values: `dsos`, `RG DS Plus`, `rgds-plus`.
- Advertised features, exactly: `shell_v2,stat_v2,ls_v2,fixed_push_mkdir`. Nothing advertised that is not implemented.
- Protocol version `0x01000001`, our max payload `1048576`; reply `maxdata = min(host, ours)`.
- `ewdbd` must be serving USB within the window adbd 4.2.2 does today: adb answers about 2.6 s after kernel start on dsOS.
- Unsupported requests fail immediately with a message. Nothing hangs.
- No code comments anywhere, including tests and scripts. No emojis, no em dashes.
- Commits: single short subject line, no body, no `Co-Authored-By` or `Claude-Session` trailers (owner rule for dsOS, applied to ewdb as well; designer's call for ewdb). Weekday commits made between 09:00 and 17:30 get `GIT_AUTHOR_DATE` and `GIT_COMMITTER_DATE` after 17:30 that day and after `HEAD`'s date. `README.md` and `dsOS.iml` in dsOS stay out of commits; always `git add` explicit paths.
- dsOS tests are `tests/*.sh`, run by `make test` (Docker, arm64). ewdb tests are `go test ./...` on the Mac; end-to-end tests are `go test -tags e2e ./e2e/...` and need `adb` on `PATH`.

## Review Focus

1. USB unplug and replug, and `sleep-hooks` writing an empty `UDC` then restoring it, while a shell is open: `ewdbd` stays up and the host sees the device again. Test in Task 10 (FunctionFS event handling) and Task 18 Step 7 (on device).
2. Host vanishes mid-stream (`adb kill-server` during `push` or a long `shell`): the child process group is killed, streams are freed, no file descriptors leak. Test in Task 4 (`TestSessionResetClosesStreams`) and Task 6 (`TestShellHostGoneKillsChild`).
3. Binary and large transfers: `push`/`pull` of a 5 MiB random file and of names with spaces and UTF-8, `exec-out` of all 256 byte values. Tests in Task 7 and Task 6.
4. Interactive shell: Ctrl-C reaches the child through the pty, window resize applies, the shell exits when the host closes. Test in Task 6 (`TestInteractiveShellSignalsAndResize`).
5. Bad or missing config, including a malformed card override and a `model_command` that hangs: `ewdbd` still serves USB with defaults and logs why. Tests in Task 2 and Task 3.

## File Structure

ewdb repository (`/Users/btk/Developer/ewdb`):

| Path | Responsibility |
| --- | --- |
| `go.mod`, `vendor/`, `LICENSE`, `NOTICE` | module `ewdb`, Go 1.26, deps |
| `internal/config/` | INI parsing, defaults, card override, product strings with `*_command` |
| `internal/wire/` | message format, constants, banner |
| `internal/session/` | CNXN handshake, stream table, flow control, service dispatch, device-initiated streams |
| `internal/launchers/` | launcher directory for `[commands]` and Android stubs |
| `internal/services/` | `shell.go`, `exec.go`, `sync.go`, `forward.go`, `reverse.go`, `misc.go`, `framebuffer.go`, `registry.go` |
| `internal/usb/` | FunctionFS (v2 and v1 descriptors) and `/dev/android_adb` transports, Linux only |
| `internal/tcplisten/` | loopback TCP transport, `//go:build testlistener` |
| `internal/drm/`, `internal/fbdev/` | screen capture back ends |
| `internal/uinput/` | uinput device creation and evdev capability reading |
| `cmd/ewdbd/`, `cmd/ewdb-getprop/`, `cmd/ewdb-logcat/`, `cmd/ewdb-bugreportz/`, `cmd/ewdb-input/`, `cmd/ewdb-screencap/` | binaries |
| `e2e/` | `//go:build e2e` tests driving a real `adb` against `ewdbd` built with `testlistener` |
| `staticcheck_test.go` | builds every `cmd/` for linux/arm64 and linux/arm and checks the ELF is static |
| `scripts/survey.sh` | on-device survey from the spec, `--disruptive` for reboots |

dsOS repository changes:

| Path | Change |
| --- | --- |
| `buildroot/board/rgds-plus/rootfs/etc/init.d/rcS` | `lo` up; `syslogd`/`klogd` first; `ewdb-input --serve` before the frontend |
| `buildroot/board/rgds-plus/rootfs/usr/libexec/dsos/adb` | start `ewdbd` in a restart loop |
| `buildroot/board/rgds-plus/rootfs/usr/libexec/dsos/frontend` | frontend output to `logger -t frontend` |
| `buildroot/board/rgds-plus/rootfs/etc/ewdbd.conf` | dsOS config |
| `buildroot/board/rgds-plus/rootfs/sdcard`, `.../data/local/tmp` | symlinks |
| `buildroot/package/ewdb/` | `Config.in`, `ewdb.mk` |
| `buildroot/package/dsos-tools/` | drop `screencap.c`, `capture.h` |
| `buildroot/patches/android-tools/` | deleted, with `BR2_GLOBAL_PATCH_DIR` |
| `buildroot/configs/dsos_rgds_plus_defconfig`, `buildroot/Config.in`, `Makefile` | swap packages, mount `../ewdb` |
| `buildroot/board/rgds-plus/dts-patches/0004-reboot-mode.patch`, `uboot-patches/0004-...` | `reboot bootloader` |
| `tests/rcs-dry.sh`, `tests/adb.sh`, `tests/ewdb.sh` (replaces `tests/screencap.sh`), `tests/capture_test.c` (deleted), `tests/reboot-mode.sh` | tests |
| `LICENSES.md`, `docs/03-open-questions.md` | licences and adb notes |

## Flashing a dev image (procedure used by Tasks 18 and 19)

The device boots one of two squashfs slots (`/proc/cmdline` has `root=PARTLABEL=slot_a` or `slot_b`; `slot_a` is `mmcblk1p2`, `slot_b` is `mmcblk1p3`). Never write the running slot.

1. `make image-dev`. Outputs are in the Docker volume at `/output/rgds-plus-dev/images/`.
2. Build a falcon FIT for the target slot inside the container: `PATH=/output/rgds-plus-dev/host/bin:$PATH sh /src/tools/mkfalcon.sh /output/rgds-plus-dev/build/uboot-custom Image.gz rk3568-anbernic-rg-ds-plus.dtb "<APPEND line for that slot from extlinux.conf>" /s/falcon.itb`, run from `/output/rgds-plus-dev/images`, with the scratch directory mounted at `/s`. Copy `rootfs.squashfs` out too.
3. `adb push` both to `/tmp`, then on the device `dd` the rootfs to the target partition (`bs=1M conv=fsync`) and the FIT to `/dev/mmcblk1` at `seek=32768` (`bs=512 conv=fsync`), `sync`.
4. Verify with explicit byte counts (the image has no `stat`): `head -c <rootfs bytes> /dev/mmcblk1pN | sha256sum` and `dd if=/dev/mmcblk1 bs=512 skip=32768 count=<FIT bytes / 512> | sha256sum`, compared with `shasum -a 256` on the Mac. Remove the `/tmp` copies.
5. `adb shell 'sync; reboot'`, wait, and confirm `/proc/cmdline` names the target slot.

If `adb` is lost, SSH is `root@<wlan0 address>` (dropbear, key in `/mnt/sdcard/dsOS/authorized_keys`), and `/usr/libexec/dsos/adb` restarts the USB side. A power cycle recovers everything else.

---

### Task 1: dsOS: bring up loopback

**Files:**
- Modify: `buildroot/board/rgds-plus/rootfs/etc/init.d/rcS` (after `mount -t tmpfs tmpfs /tmp`)
- Test: `tests/rcs-dry.sh`

- [ ] **Step 1: Add the failing assertions to `tests/rcs-dry.sh`**, after the first plan is generated:

```sh
[ -n "$(line 'ip link set lo up')" ] || fail "loopback not brought up"
[ "$(line 'ip link set lo up')" -lt "$(line 'libexec/dsos/adb')" ] || fail "adb starts before loopback"
```

- [ ] **Step 2: Run** `make test T=tests/rcs-dry.sh`. Expected: `FAIL: loopback not brought up`.
- [ ] **Step 3: Add** `run ip link set lo up` to `rcS` directly after the `/tmp` mount.
- [ ] **Step 4: Run** `make test`. Expected: every test prints `PASS`.
- [ ] **Step 5: Commit** in dsOS: `git add buildroot/board/rgds-plus/rootfs/etc/init.d/rcS tests/rcs-dry.sh && git commit -m "Bring up loopback at boot"`.

### Task 2: ewdb scaffold and config

**Files:**
- Create: `/Users/btk/Developer/ewdb/go.mod`, `LICENSE` (Apache-2.0 text), `NOTICE`, `.gitignore`, `internal/config/config.go`, `internal/config/config_test.go`

**Interfaces:**
- Produces:
  - `type Config struct { Name, Model, Device string; State, USB, SerialFile, Shell string; Props []KV; Commands map[string]string; Sections map[string][]KV; Path string; Warnings []string }`
  - `type KV struct { Key, Value string }`
  - `func Parse(r io.Reader) (*Config, error)`
  - `func Load(path string) *Config` (never nil; problems go to `Warnings`)
  - `func (c *Config) Command(name string) (string, bool)`
  - `func (c *Config) Section(name string) []KV`

INI rules: `[section]`; `key = value` split at the first `=`, both trimmed; blank lines and lines starting with `#` or `;` ignored; last duplicate wins in `Commands`, all kept in `Props`/`Sections`; any other line is an error naming its line number. Sections used: `[device]` keys `name`, `name_command`, `model`, `model_command`, `device`, `device_command`, `state`, `usb`, `serial_file`, `shell`; `[props]`; `[commands]`; everything else kept in `Sections` for tools.

Defaults: name `linux`, model `Linux handheld`, device `handheld`, state `/var/lib/ewdb`, usb `auto`, shell `/bin/sh`.

`Load`: parse `path`; if `<state>/ewdbd.conf` exists, parse that instead (whole-file override, designer's call). A parse error in the override falls back to the base file; a parse error in the base falls back to defaults. Each fallback appends a warning. `*_command` runs through `sh -c` with a 2 s timeout; on exit 0 with a non-empty first line, that line wins; otherwise the plain key is used and a warning is added.

- [ ] **Step 1: `git init /Users/btk/Developer/ewdb`, `go mod init ewdb`, `go get github.com/creack/pty golang.org/x/sys`, `go mod vendor`.** Add `LICENSE` and `NOTICE`.
- [ ] **Step 2: Write failing tests** in `internal/config/config_test.go`:

```go
func TestParseSectionsAndDefaults(t *testing.T)
// input "[device]\nmodel = RG DS Plus\n[commands]\nlogcat = logread -f\n"
// c.Model == "RG DS Plus", c.Name == "linux", c.Device == "handheld"
// c.Commands["logcat"] == "logread -f"; c.Shell == "/bin/sh"
func TestParseRejectsBadLine(t *testing.T)
// "[device]\nnot a pair\n" -> error containing "line 2"
func TestModelCommandWins(t *testing.T)
// model = Fixed, model_command = echo From Command -> c.Model == "From Command"
func TestModelCommandFailureFallsBack(t *testing.T)
// model_command = exit 3 -> c.Model == "Fixed", one warning mentions model_command
func TestModelCommandTimeout(t *testing.T)
// model_command = sleep 10 -> Load returns within 3 s, c.Model == "Fixed"
func TestCardOverrideReplacesBase(t *testing.T)
// base sets state to a temp dir holding ewdbd.conf with model = Card -> c.Model == "Card", c.Path is the override
func TestMalformedOverrideFallsBackToBase(t *testing.T)
// override "garbage" -> base model kept, one warning
func TestMissingFileGivesDefaults(t *testing.T)
// Load("/nonexistent") -> c.Name == "linux", one warning
```

- [ ] **Step 3: Run** `go test ./internal/config/`. Expected: FAIL (package does not compile).
- [ ] **Step 4: Implement** `internal/config/config.go`.
- [ ] **Step 5: Run** `go test ./internal/config/`. Expected: PASS.
- [ ] **Step 6: Commit:** `git add -A . && git commit -m "Parse the ewdbd config"`.

### Task 3: wire format and static build check

**Files:**
- Create: `internal/wire/wire.go`, `internal/wire/wire_test.go`, `staticcheck_test.go`, `cmd/ewdbd/main.go` (stub that parses `-c` and exits 0)

**Interfaces:**
- Produces:
  - Constants `CNXN=0x4e584e43 OPEN=0x4e45504f OKAY=0x59414b4f CLSE=0x45534c43 WRTE=0x45545257 AUTH=0x48545541 STLS=0x534c5453 SYNC=0x434e5953`, `Version=0x01000001`, `VersionMin=0x01000000`, `MaxPayload=1048576`
  - `type Message struct { Command, Arg0, Arg1 uint32; Data []byte }`
  - `func Read(r io.Reader, max uint32) (Message, error)` (24-byte little-endian header: command, arg0, arg1, length, checksum, magic; rejects magic != command ^ 0xffffffff and length > max)
  - `func Write(w io.Writer, m Message, checksum bool) error` (checksum = byte sum of data when `checksum`, else 0; header and data in one `Write` call)
  - `func Banner(c *config.Config, features []string) []byte`
  - `var Features = []string{"shell_v2", "stat_v2", "ls_v2", "fixed_push_mkdir"}`

- [ ] **Step 1: Write failing tests:**

```go
func TestRoundTrip(t *testing.T)            // Write then Read gives the same Message
func TestRejectsBadMagic(t *testing.T)
func TestRejectsOversize(t *testing.T)      // length 1048577 with max 1048576
func TestChecksumIsByteSum(t *testing.T)    // data {1,2,250} -> header bytes 16..19 == 253 LE
func TestBanner(t *testing.T)
// == "device::ro.product.name=dsos;ro.product.model=RG DS Plus;ro.product.device=rgds-plus;features=shell_v2,stat_v2,ls_v2,fixed_push_mkdir"
```

and in `staticcheck_test.go`, `TestBinariesAreStatic`: for each directory under `cmd/`, for `GOARCH=arm64` and `GOARCH=arm GOARM=7`, run `go build -o <tmp> ./cmd/<name>` with `GOOS=linux CGO_ENABLED=0`, open with `debug/elf`, assert no `PT_INTERP` program header and `ImportedLibraries()` is empty. It skips under `-short`.

- [ ] **Step 2: Run** `go test ./...`. Expected: FAIL.
- [ ] **Step 3: Implement** `internal/wire`.
- [ ] **Step 4: Run** `go test ./...`. Expected: PASS.
- [ ] **Step 5: Commit:** `"Add the adb wire format"`.

### Task 4: session

**Files:**
- Create: `internal/session/session.go`, `internal/session/stream.go`, `internal/session/session_test.go`

**Interfaces:**
- Consumes: `wire`, `config`
- Produces:
  - `type Transport interface { Read() (wire.Message, error); Write(wire.Message) error; Close() error }`
  - `type Handler func(ctx context.Context, s *Stream, arg string)` and `type Registry struct{...}` with `func (r *Registry) Handle(prefix string, h Handler)`; dispatch picks the longest matching prefix; `arg` is the service name after the prefix
  - `type Stream` implements `io.ReadWriteCloser` plus `CloseWrite() error`; `Write` splits at `maxdata`, sends `WRTE`, and blocks until the matching `OKAY`; incoming `WRTE` data is queued and acknowledged with `OKAY` once `Read` has consumed it
  - `func Serve(ctx context.Context, t Transport, c *config.Config, r *Registry) error` (returns when the transport fails or the host sends a new `CNXN`, in which case it answers it and keeps going with fresh state)
  - `func (s *Session) Dial(ctx context.Context, service string) (*Stream, error)` (device-initiated `OPEN`, used by reverse), reachable from a handler as `s.Session()`

Behaviour, as AOSP: answer `CNXN` with `CNXN(Version, min(maxdata), banner)`, using a checksum only when the host version is below `0x01000001`. `OPEN(local, 0, name\0)`: an unknown service gets `CLSE(0, local)`; a known one gets `OKAY(ours, local)` and its handler runs in a goroutine. `CLSE` from the host cancels that stream's context. Ignore `AUTH` and `STLS` (never requested, since we do not send them).

- [ ] **Step 1: Write failing tests** with an in-memory `Transport` (channel pair):

```go
func TestHandshake(t *testing.T)                  // host CNXN maxdata 4096 -> reply CNXN arg0 0x01000001 arg1 4096, banner data
func TestUnknownServiceGetsClose(t *testing.T)    // OPEN(7,0,"nope:\x00") -> CLSE(0,7)
func TestEchoServiceFlowControl(t *testing.T)     // echo handler; host WRTE "hi" gets OKAY then WRTE "hi"; a second device WRTE is not sent before the host OKAYs the first
func TestWriteSplitsAtMaxdata(t *testing.T)       // maxdata 4096, handler writes 10000 bytes -> three WRTEs of 4096, 4096, 1808
func TestHostCloseCancelsHandler(t *testing.T)    // handler blocks on ctx.Done(); host CLSE -> handler returns within 1 s
func TestSessionResetClosesStreams(t *testing.T)  // transport Read returns error -> every open handler's ctx is done, Serve returns
func TestDialOpensToHost(t *testing.T)            // Dial("tcp:9000") sends OPEN(x,0,"tcp:9000\x00"); host OKAY -> stream usable
```

- [ ] **Step 2: Run** `go test ./internal/session/`. Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** `go test -race ./internal/session/`. Expected: PASS.
- [ ] **Step 5: Commit:** `"Run adb sessions over a transport"`.

### Task 5: test listener, daemon main, end-to-end harness

**Files:**
- Create: `internal/tcplisten/tcplisten.go` (`//go:build testlistener`), `cmd/ewdbd/main.go`, `cmd/ewdbd/listen_testlistener.go`, `cmd/ewdbd/listen_default.go`, `e2e/harness_test.go`, `e2e/connect_test.go`

**Interfaces:**
- Consumes: `session.Serve`, `config.Load`
- Produces:
  - `ewdbd` flags: `-c PATH` (default `/etc/ewdbd.conf`); under `testlistener` also `-listen ADDR`, which exits with status 2 and `ewdbd: -listen only accepts loopback addresses` for any non-loopback address.
  - e2e helpers: `func startDaemon(t *testing.T, conf string) (serial string)` builds `ewdbd` with `-tags testlistener` once per run, starts it on `127.0.0.1:<free port>` with a temp config, runs `adb connect`, and registers cleanup (`adb disconnect`, kill daemon); `func adb(t *testing.T, serial string, args ...string) (stdout, stderr string, code int)`.
  - `ewdbd` logs to stderr with `log` (dsOS sends it to syslog), including every config warning at start.

- [ ] **Step 1: Write failing e2e tests:**

```go
func TestDevicesShowsModel(t *testing.T)   // `adb devices -l` line for serial contains "product:dsos model:RG_DS_Plus device:rgds-plus"
func TestGetState(t *testing.T)            // `adb -s S get-state` == "device"
func TestListenRefusesNonLoopback(t *testing.T) // ewdbd -listen 0.0.0.0:5555 exits 2
func TestBadConfigStillServes(t *testing.T)     // config "garbage" -> devices -l shows model:Linux_handheld
```

- [ ] **Step 2: Run** `go test -tags e2e ./e2e/`. Expected: FAIL.
- [ ] **Step 3: Implement** `main.go` (load config, log warnings, build the registry, serve the transport in a loop) and the listener.
- [ ] **Step 4: Run** `go test -tags e2e ./e2e/` and `go test ./...`. Expected: PASS.
- [ ] **Step 5: Commit:** `"Serve adb over a loopback test listener"`.

### Task 6: launchers, shell and exec

**Files:**
- Create: `internal/launchers/launchers.go`, `internal/launchers/launchers_test.go`, `internal/services/registry.go`, `internal/services/shell.go`, `internal/services/exec.go`, `internal/services/shell_test.go`, `e2e/shell_test.go`

**Interfaces:**
- Consumes: `config.Config.Commands`, `session.Stream`
- Produces:
  - `func launchers.Build(dir string, commands map[string]string, systemPath string) error`: one executable `#!/bin/sh` script per mapped name: `PATH='<systemPath>'; export PATH; exec <command> "$@"`. One stub per name in `launchers.Android` that is neither mapped nor found on `systemPath`: prints `<name>: not available on this device` to stderr and exits 127. Rebuilds the directory from scratch.
  - `var launchers.Android = []string{"am", "pm", "monkey", "content", "ime", "uiautomator", "run-as", "cmd", "settings", "svc", "wm", "dumpsys", "setprop", "screenrecord", "logcat", "getprop", "input", "screencap", "bugreportz"}`
  - `func services.Register(r *session.Registry, d *Deps)` with `type Deps struct { Config *config.Config; LauncherDir string; SystemPath string }`; every later service task adds itself here.
  - `func services.Env(d *Deps, term string) []string`: `PATH=<LauncherDir>:<SystemPath>`, `HOME=/root` if it exists else `/`, `TERM=<term or dumb>`, `EWDB_CONFIG=<Config.Path>`.
  - Launcher dir: `/run/ewdbd/bin` on the device, a temp dir in tests. `main.go` builds it at start.

Shell service names, as AOSP: `shell[,ARG]*:COMMAND` where ARGs are `v2`, `raw`, `pty`, `TERM=...`. Type: explicit `raw`/`pty` wins, else `pty` when COMMAND is empty, else `raw`. Interactive: `<Shell>` with no arguments (not `-l`, because BusyBox `/etc/profile` resets `PATH`). Command: `<Shell> -c COMMAND`. With `v2`, framing is `id(1) len(4 LE) data`: stdin 0, stdout 1, stderr 2, exit 3 (one byte: exit status, or 128+signal), close-stdin 4, window size 5 (ASCII `ROWSxCOLS,XPIXxYPIX`). Raw v2 keeps stdout and stderr on separate pipes. Without `v2`, all output goes on the stream unframed. `exec:COMMAND` is raw with no framing and stderr joined to stdout. The child runs in its own process group (setsid); when the host closes the stream, send SIGHUP to the group, then SIGKILL after 1 s.

- [ ] **Step 1: Write failing tests:**

```go
func TestLauncherRunsMappedCommand(t *testing.T) // commands {"getprop": "echo mapped"} -> running dir/getprop a b prints "mapped a b"
func TestLauncherAvoidsSelfRecursion(t *testing.T) // commands {"date": "date"} with "date" on systemPath -> dir/date exits 0 within 1 s
func TestStubForUnmappedAndroidCommand(t *testing.T) // dir/pm -> stderr "pm: not available on this device", exit 127
func TestNoStubWhenSystemHasCommand(t *testing.T) // systemPath holds an executable "svc" -> no dir/svc
func TestShellArgsParse(t *testing.T) // "shell,v2,TERM=xterm,raw:ls" -> v2 true, raw, TERM xterm, command "ls"
```

and e2e:

```go
func TestShellExitCode(t *testing.T)      // `adb shell false` code 1; `adb shell exit 42` code 42
func TestShellSeparatesStderr(t *testing.T) // `adb shell 'echo out; echo err >&2'` -> stdout "out\n", stderr "err\n"
func TestShellNoCRLF(t *testing.T)        // `adb shell echo hi` stdout == "hi\n"
func TestShellT(t *testing.T)             // `adb shell -T echo hi` stdout == "hi\n", code 0
func TestExecOutBinary(t *testing.T)      // `adb exec-out` of a script printing bytes 0..255 -> exactly those 256 bytes
func TestShellUsesLaunchers(t *testing.T) // config [commands] getprop = echo mapped -> `adb shell getprop x` stdout "mapped x\n"
func TestShellHostGoneKillsChild(t *testing.T) // `adb shell 'sleep 300'` started, adb client killed -> no "sleep 300" process remains after 3 s
func TestInteractiveShellSignalsAndResize(t *testing.T) // `adb shell -t sh -c 'stty size; sleep 30'` under a pty sized 40x100 prints "40 100"; sending \x03 ends it within 2 s
```

- [ ] **Step 2: Run** `go test ./internal/... && go test -tags e2e ./e2e/`. Expected: FAIL.
- [ ] **Step 3: Implement** launchers, then shell and exec, with `creack/pty` for pty mode and `os/exec` pipes for raw mode.
- [ ] **Step 4: Run** both. Expected: PASS.
- [ ] **Step 5: Commit:** `"Add the shell, exec and command launchers"`.

### Task 7: file sync

**Files:**
- Create: `internal/services/sync.go`, `internal/services/sync_test.go`, `e2e/sync_test.go`

**Interfaces:**
- Consumes: `session.Stream`
- Produces: the `sync:` handler.

Request: 4-byte ASCII id plus a 4-byte LE length plus path (at most 1024 bytes). Implement, as AOSP `file_sync_protocol.h`:
- `STAT`: reply `STAT mode size mtime` (u32 each; zeros if missing).
- `STA2`/`LST2`: reply id plus packed `error u32, dev u64, ino u64, mode u32, nlink u32, uid u32, gid u32, size u64, atime i64, mtime i64, ctime i64`, `error` = errno.
- `LIST`: `DENT mode size mtime namelen name` per entry, then `DONE` with zeros.
- `LIS2`: `DNT2` per entry with the v2 stat fields plus `namelen` and name, ending as AOSP does.
- `SEND path,mode`: `DATA` chunks of at most 65536 bytes, `DONE mtime`; create parent directories; a symlink mode makes the data the link target; set mtime; reply `OKAY` (length 0) or `FAIL` with a message.
- `RECV`: `DATA` chunks of at most 65536 bytes, then `DONE`; or `FAIL` with a message.
- `QUIT`: end the stream.

- [ ] **Step 1: Write failing tests:** unit tests feeding requests through an in-memory stream (`TestStatMissingGivesZeros`, `TestSendCreatesParents`, `TestSendSymlink`, `TestRecvMissingFails` with `FAIL` text containing "No such file"), and e2e:

```go
func TestPushPullRoundTrip(t *testing.T) // 5 MiB random file, sha256 equal after push then pull
func TestPushPullOddNames(t *testing.T)  // "a b ü.txt" round trips
func TestPushDirectory(t *testing.T)     // dir with nested files -> `adb shell find` lists them
func TestPushSyncSkips(t *testing.T)     // second `adb push --sync` reports "0 files pushed, 1 skipped"
func TestPullPreservesMode(t *testing.T) // `adb pull -a` keeps 0755
func TestPullMissing(t *testing.T)       // `adb pull /nonexistent x` code 1, stderr mentions "No such file"
func TestLsV2(t *testing.T)              // `adb shell` unaffected; `adb ls /` lists "etc"
```

- [ ] **Step 2: Run** and see FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** and see PASS.
- [ ] **Step 5: Commit:** `"Add file sync"`.

### Task 8: forward and reverse

**Files:**
- Create: `internal/services/forward.go`, `internal/services/reverse.go`, `e2e/forward_test.go`

**Interfaces:**
- Consumes: `session.Session.Dial`
- Produces handlers:
  - `tcp:PORT` and `tcp:HOST:PORT`: dial (`HOST` defaults to `127.0.0.1`).
  - `localabstract:NAME`, `localfilesystem:PATH`: dial unix.
  - `dev:PATH`: open read-write.
  - `localreserved:`: refuse with `CLSE`.
  - `reverse:forward:[norebind:]REMOTE;LOCAL`, `reverse:killforward:REMOTE`, `reverse:killforward-all`, `reverse:list-forward`, with replies framed as AOSP (`OKAY`, or `FAIL` plus a 4-hex-digit length and message; `tcp:0` reports the bound port).

A reverse listener binds 127.0.0.1 (or a unix socket). Each accepted connection is copied both ways with a `Dial(LOCAL)` stream. Listeners close when their session ends.

- [ ] **Step 1: Write failing e2e tests:**

```go
func TestForwardTCP(t *testing.T)        // echo server on 127.0.0.1:P (stands in for the device side); adb forward tcp:L tcp:P; write "ping" to L reads "ping"
func TestForwardList(t *testing.T)       // `adb forward --list` contains "tcp:L tcp:P"
func TestReverseTCP(t *testing.T)        // host echo on H; adb reverse tcp:R tcp:H; connecting to 127.0.0.1:R (device side) echoes
func TestReverseListAndRemove(t *testing.T) // --list shows it; --remove-all empties it
func TestReverseNoRebind(t *testing.T)   // second reverse --no-rebind on R fails with a message, code 1
func TestForwardToClosedPort(t *testing.T) // forward to a closed port: client connection closes within 2 s, daemon still answers `adb shell true`
```

- [ ] **Step 2: Run** and see FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** and see PASS.
- [ ] **Step 5: Commit:** `"Add forward and reverse"`.

### Task 9: daemon actions, clear failures and framebuffer

**Files:**
- Create: `internal/services/misc.go`, `internal/services/framebuffer.go`, `internal/services/misc_test.go`, `e2e/misc_test.go`

**Interfaces:**
- Consumes: `config.Command`, `services.Env`
- Produces handlers. Each one writes its message to the stream, then closes it.

| Service | Behaviour |
| --- | --- |
| `reboot:ARG` | `sync`; if `reboot` (empty ARG) or `reboot.ARG` is mapped, run it with `sh -c`; else empty ARG runs the system `reboot`, and any other ARG calls `unix.Reboot(LINUX_REBOOT_CMD_RESTART2, ARG)` (Linux-only file; under `testlistener` it is replaced by a failure so tests never reboot the Mac) |
| `remount:` | mapped `remount` runs; else `remount is not available on this device\n` |
| `root:` | mapped `root` runs; else `adbd is already running as root\n` when uid 0 |
| `unroot:` | `unroot is not available on this device\n` |
| `tcpip:PORT` | `Wi-Fi adb is not available on this device\n` |
| `usb:` | `already running on USB\n` |
| `disable-verity:`, `enable-verity:` | `verity is not available on this device\n` |
| `jdwp`, `track-jdwp`, `track-app` | empty, then close |
| `sideload:`, `sideload-host:`, `abb:`, `abb_exec:`, `backup:`, `restore:` | `<service> is not available on this device\n` |
| `reconnect` | close every stream; wait for the host's new `CNXN` |
| `framebuffer:` | run `screencap` with `Env` (so the mapping applies); read the 12-byte header `w, h, format` (format must be 1, RGBA_8888); send the v1 header of 13 u32 LE (`version=1, bpp=32, size=w*h*4, width, height, red_offset=0, red_length=8, blue_offset=16, blue_length=8, green_offset=8, green_length=8, alpha_offset=24, alpha_length=8`, field order as AOSP `framebuffer_service.cpp`); copy exactly `size` bytes; close |

- [ ] **Step 1: Write failing tests:**

```go
func TestRebootRunsMapping(t *testing.T)   // e2e: [commands] reboot.bootloader = touch $MARK -> `adb reboot bootloader` creates MARK
func TestTcpipKeepsSession(t *testing.T)   // `adb tcpip 5555` prints the message; `adb shell true` still works
func TestUsbMessage(t *testing.T)
func TestRemountMessage(t *testing.T)      // stdout contains "not available"
func TestVerityMessage(t *testing.T)
func TestJdwpReturnsQuickly(t *testing.T)  // `adb jdwp` exits within 2 s
func TestFramebufferHeader(t *testing.T)   // unit: fake screencap printing 2x1 raw RGBA -> 52-byte header with size 8, then 8 bytes
```

- [ ] **Step 2: Run** and see FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** and see PASS.
- [ ] **Step 5: Commit:** `"Add daemon actions and clear failures"`.

### Task 10: USB transports

**Files:**
- Create: `internal/usb/ffs_linux.go`, `internal/usb/descriptors.go`, `internal/usb/android_linux.go`, `internal/usb/usb_test.go`; modify `cmd/ewdbd/main.go`

**Interfaces:**
- Produces:
  - `func usb.Open(spec string) (Endpoint, error)`, where `spec` is `auto`, `ffs:DIR` or `android:PATH`. `auto` tries `ffs:/dev/usb-ffs/adb`, then `android:/dev/android_adb`.
  - `type Endpoint interface { Next(ctx context.Context) (session.Transport, error) }`: each returned transport lives for one host connection; `Next` blocks until the host is there again.
  - `func DescriptorsV2() []byte`, `func DescriptorsV1() []byte`, `func Strings() []byte`

FunctionFS, as AOSP `daemon/usb_ffs.cpp`: interface class `0xff`, subclass `0x42`, protocol `1`, two bulk endpoints (OUT `ep1`, IN `ep2`), max packet 64 full speed, 512 high speed, 1024 super speed with a companion descriptor; strings `ADB Interface` (lang `0x0409`). Write v2 descriptors (`magic 3`, flags FS|HS|SS) to `ep0`; on `EINVAL`, write v1 (`magic 1`, FS and HS only). Then strings (`magic 2`). Keep `ep0` open for the life of the process: closing it unbinds the gadget, which is what took dsOS off USB under adbd 4.2.2. Read `ep0` events: on `ENABLE`, open `ep1`/`ep2` and hand out a transport; on `DISABLE`, `UNBIND` or an endpoint I/O error, close `ep1`/`ep2` and fail the current transport, then wait for the next `ENABLE`. Reads on `ep1` are a 24-byte header, then the payload.

`/dev/android_adb`: open, read the header, then the payload, write each message; on an I/O error, close and reopen every 500 ms.

- [ ] **Step 1: Write failing tests:** `TestDescriptorsV2Bytes` and `TestDescriptorsV1Bytes`, which compare against byte arrays built from AOSP's struct layout (write them out in the test from the field values above, little-endian, with lengths computed), and `TestStringsBytes`.
- [ ] **Step 2: Run** and see FAIL.
- [ ] **Step 3: Implement**, and wire `usb.Open(c.USB)` into `main.go` for non-`testlistener` builds.
- [ ] **Step 4: Run** `go test ./...`. Expected: PASS. Device behaviour is covered in Task 18.
- [ ] **Step 5: Commit:** `"Add FunctionFS and android_adb transports"`.

### Task 11: ewdb-getprop

**Files:**
- Create: `cmd/ewdb-getprop/main.go`, `cmd/ewdb-getprop/main_test.go`

**Interfaces:**
- Consumes: `config.Load(os.Getenv("EWDB_CONFIG") or "/etc/ewdbd.conf")`

Properties, in this order of precedence: `[props]`; then derived `ro.product.name`, `ro.product.model`, `ro.product.device`; `ro.serialno` (first line of `serial_file`); `ro.product.cpu.abi` (`arm64` gives `arm64-v8a`, `arm` gives `armeabi-v7a`, `amd64` gives `x86_64`). With no arguments, print `[key]: [value]` for every property, sorted. With `KEY`, print its value or an empty line. With `KEY DEFAULT`, print the default when the key is missing.

- [ ] **Step 1: Write failing tests:** `TestListsSorted`, `TestSingleKey` (`ro.product.model` gives `RG DS Plus`), `TestDefault` (`missing x` gives `x`), `TestPropsOverrideDerived`.
- [ ] **Step 2: Run, FAIL. Step 3: Implement. Step 4: Run, PASS.**
- [ ] **Step 5: Commit:** `"Add ewdb-getprop"`.

### Task 12: ewdb-logcat

**Files:**
- Create: `cmd/ewdb-logcat/main.go`, `cmd/ewdb-logcat/main_test.go`

The log file comes from `[logcat] file` (default `/var/log/messages`), in BusyBox `syslogd` format (`Nov  7 15:00:00 dsos user.info frontend: text`). Flags:
- `-d`: print and exit.
- `-c`: truncate the file.
- no `-d`: print, then follow (re-open on truncate).
- `-v X`, `-b X`: accepted and ignored.
- `-s`: accepted.
- positional `TAG` or `TAG:P`: keep only lines whose tag field is TAG, matching `TAG:` or `TAG[`.
- `*:S`: ignored.
- missing file: `ewdb-logcat: no log at PATH`, exit 1.

- [ ] **Step 1: Write failing tests:** `TestDumpExits`, `TestClearTruncates`, `TestTagFilter` (keeps `frontend:` and `frontend[12]:` lines, drops `kernel:`), `TestFollowSeesAppend` (a line appended after start shows within 1 s), `TestFollowSurvivesTruncate`, `TestMissingFile`.
- [ ] **Step 2: Run, FAIL. Step 3: Implement. Step 4: Run, PASS.**
- [ ] **Step 5: Commit:** `"Add ewdb-logcat"`.

### Task 13: ewdb-bugreportz

**Files:**
- Create: `cmd/ewdb-bugreportz/main.go`, `cmd/ewdb-bugreportz/main_test.go`, `e2e/bugreport_test.go`

Protocol, as frameworks `cmds/bugreportz` and AOSP `client/bugreport.cpp`:
- `-v`: print `1.2` to stderr.
- `-p`: print `BEGIN:<path>`, then `PROGRESS:<i>/<n>` after each item, then `OK:<path>`; or `FAIL:<message>`.
- no flag: the same without progress.

Items come from `[bugreport]` entries `name = shell command`; each command's combined output goes to `<name>.txt` in a zip, with a 30 s timeout per item (a timeout writes `timed out` into that file and moves on). Zip path: `[bugreport] dir` (default `/tmp`) plus `bugreport-<device>-<YYYY-MM-DD-HH-MM-SS>.zip`.

- [ ] **Step 1: Write failing tests:** `TestVersion`, `TestZipHasItems`, `TestProgressLines`, `TestItemTimeout` (with a 1 s test timeout), and e2e `TestAdbBugreport` (`adb bugreport <dir>` with the mapping `bugreportz = <built ewdb-bugreportz>`, exit 0, and the zip exists locally).
- [ ] **Step 2: Run, FAIL. Step 3: Implement. Step 4: Run, PASS.**
- [ ] **Step 5: Commit:** `"Add ewdb-bugreportz"`.

### Task 14: ewdb-input

**Files:**
- Create: `internal/uinput/uinput_linux.go`, `internal/uinput/codes.go`, `cmd/ewdb-input/main.go`, `cmd/ewdb-input/plan.go`, `cmd/ewdb-input/plan_test.go`

**Interfaces:**
- Produces:
  - `type Event struct { Device string; Type, Code uint16; Value int32; Delay time.Duration }`
  - `func Plan(args []string, keys map[string]KeyTarget, touch Touch) ([]Event, error)`: pure, testable
  - `type KeyTarget struct { Device string; Code uint16 }`, `type Touch struct { Device string; MaxX, MaxY int32 }`
  - `func uinput.Clone(eventPath string) (*Device, error)`: copies name, bus, vendor, product, version, event/key/abs bits, abs info and properties; uses `UI_DEV_SETUP`/`UI_ABS_SETUP`, falling back to writing `uinput_user_dev` on older kernels
  - `func uinput.Keyboard(name string) (*Device, error)`
  - `ewdb-input --serve`: reads `[input] clone` (comma list of device names), `[input] touch`, `[input] socket` (default `/run/ewdb/input.sock`) and `[keys]`; clones each device, creates `ewdb keyboard`, listens on the socket, and runs one plan per connection, replying `ok` or `error: <message>`. Client mode sends its argv joined by NUL. With no server: `ewdb-input: ewdb-input --serve is not running`, exit 1.

Commands, as Android `input`: an optional leading source (`keyboard`, `touchscreen`, `gamepad`, `dpad`, `touchpad`) is ignored.
- `tap X Y`: MT protocol B. Down: `ABS_MT_SLOT 0`, `ABS_MT_TRACKING_ID n`, `ABS_MT_POSITION_X/Y`, `BTN_TOUCH 1`, `ABS_X/Y` when present, `SYN`. Wait 50 ms. Up: `ABS_MT_TRACKING_ID -1`, `BTN_TOUCH 0`, `SYN`.
- `swipe X1 Y1 X2 Y2 [ms]`: default 300 ms, a move every 16 ms.
- `keyevent K...`: press and release each, 40 ms apart.
- `text S`: ASCII through `ewdb keyboard`, using a US layout with shift.

Out-of-range coordinates give `x out of range 0..MAX`. Key names accept `KEYCODE_X`, `X` or the Android number for: `HOME 3, BACK 4, DPAD_UP 19, DPAD_DOWN 20, DPAD_LEFT 21, DPAD_RIGHT 22, DPAD_CENTER 23, VOLUME_UP 24, VOLUME_DOWN 25, POWER 26, ENTER 66, MENU 82, BUTTON_A 96, BUTTON_B 97, BUTTON_X 99, BUTTON_Y 100, BUTTON_L1 102, BUTTON_R1 103, BUTTON_L2 104, BUTTON_R2 105, BUTTON_THUMBL 106, BUTTON_THUMBR 107, BUTTON_START 108, BUTTON_SELECT 109, BUTTON_MODE 110`. `[keys]` entries are `NAME = <device name> <LINUX_CODE_NAME>` (the last token is the code; `codes.go` holds the `KEY_*`/`BTN_*` names and numbers from `linux/input-event-codes.h` needed for these). An unmapped key gives `key NAME is not mapped on this device`.

- [ ] **Step 1: Write failing tests:** `TestPlanTap` (exact event list for `tap 10 20` with MaxX 1023), `TestPlanSwipeSteps` (300 ms gives 19 moves), `TestPlanKeyeventNames` (`KEYCODE_DPAD_UP`, `DPAD_UP` and `19` give the same events), `TestPlanSourceIgnored`, `TestPlanOutOfRange`, `TestPlanUnmappedKey`, `TestPlanText` (`Ab` gives shift press, a, shift release, b).
- [ ] **Step 2: Run, FAIL. Step 3: Implement. Step 4: Run, PASS.**
- [ ] **Step 5: Commit:** `"Add ewdb-input"`.

### Task 15: ewdb-screencap

**Files:**
- Create: `internal/drm/drm_linux.go`, `internal/fbdev/fbdev_linux.go`, `internal/capture/capture.go`, `internal/capture/capture_test.go`, `cmd/ewdb-screencap/main.go`

**Interfaces:**
- Produces:
  - `func capture.ToRGBA(format uint32, src []byte, pitch, w, h int) ([]byte, error)` for `XR24 AR24 XB24 AB24 RG24 BG24 RG16 BG16` (the same set and semantics as dsOS `buildroot/package/dsos-tools/src/capture.h`)
  - `func capture.WriteRaw(w io.Writer, img Image) error` (12-byte header `w, h, 1`, then RGBA)
  - `func capture.WritePNG(w io.Writer, img Image) error` (RGB, `image/png` with `png.BestSpeed`)
  - `func capture.Stack(imgs []Image) Image` (top to bottom, opaque black padding)
  - CLI: `ewdb-screencap [-p] [-d NAME|INDEX] [--screens name=CONNECTOR,...] [FILE]`; a `FILE` ending in `.png` implies `-p`; with no `--screens`, every connected connector in DRM order.

Back ends: DRM via ioctls (`GETRESOURCES`, `GETCONNECTOR`, `GETENCODER`, `GETPLANERESOURCES`, `GETPLANE` with universal planes, `GETFB2`, falling back to `GETFB` when `GETFB2` is `EINVAL`/`ENOTTY`; map with `PRIME_HANDLE_TO_FD` plus `mmap` and `DMA_BUF_IOCTL_SYNC`, falling back to `MODE_MAP_DUMB` and `mmap` of the card; refuse non-linear modifiers with `the NAME framebuffer is not linear (modifier 0x...)`; close GEM handles). With no DRM card, or no lit connector, `/dev/fb0` via `FBIOGET_VSCREENINFO`/`FBIOGET_FSCREENINFO` and `mmap`.

- [ ] **Step 1: Write failing tests** porting `tests/capture_test.c`'s cases from dsOS: `TestXR24` (2x2 with pitch 12 gives `102030ff ff0000ff 00ff00ff 0000ffff`), `TestRG16` (`00f8 1f00` gives `ff0000ff 0000ffff`), `TestUnknownFormat`, `TestPNGDecodes` (round trip through `image/png`), `TestRawHeader` (`2,2,1` then 16 bytes), `TestStack` (1024x768 plus 1024x768 gives 1024x1536).
- [ ] **Step 2: Run, FAIL. Step 3: Implement. Step 4: Run, PASS.**
- [ ] **Step 5: Commit:** `"Port screencap to Go"`.

### Task 16: survey script

**Files:**
- Create: `scripts/survey.sh`

Runs the spec's survey against `adb -s "$SERIAL"`, one line per command: `PASS`, `FAIL <first output line>` or `SKIP`. Every command gets a 15 s timeout (`perl -e 'alarm shift; exec @ARGV'`, since macOS has no `timeout`). It never runs `reconnect`, `root`, `unroot`, `install`, `uninstall` or `sideload`. `reboot`, `reboot bootloader` and `reboot recovery` run only with `--disruptive`, each followed by waiting up to 90 s for the device. PASS criteria are the e2e expectations from Tasks 5 to 13 (exit codes, stream separation, push/pull hashes, forward echo through device `127.0.0.1:22`'s SSH banner, `getprop ro.product.model`, `logcat -d` non-empty, `bugreport` zip exists, `screencap -p` decodes as PNG, `input keyevent DPAD_DOWN` exits 0, `tcpip` keeps the session).

- [ ] **Step 1: Write the script.**
- [ ] **Step 2: Run** `sh scripts/survey.sh` against the test daemon (`SERIAL=127.0.0.1:<port>`). Expected: device-only rows (`input`, `screencap`) show `FAIL` or `SKIP`, everything else `PASS`.
- [ ] **Step 3: Commit:** `"Add the device survey script"`.

### Task 17: dsOS: package ewdb

**Files:**
- Create: `buildroot/package/ewdb/Config.in`, `buildroot/package/ewdb/ewdb.mk`, `buildroot/board/rgds-plus/rootfs/etc/ewdbd.conf`, `tests/ewdb.sh`
- Modify: `Makefile`, `buildroot/Config.in`, `buildroot/configs/dsos_rgds_plus_defconfig`, `buildroot/board/rgds-plus/post-build.sh` (substitute `VERSION` into `/etc/ewdbd.conf`), `buildroot/package/dsos-tools/dsos-tools.mk`, `buildroot/package/dsos-tools/Config.in`, `LICENSES.md`
- Delete: `buildroot/patches/android-tools/`, `buildroot/package/dsos-tools/src/screencap.c`, `buildroot/package/dsos-tools/src/capture.h`, `tests/screencap.sh`, `tests/capture_test.c`

**Interfaces:**
- `Makefile`: `EWDB ?= $(abspath $(CURDIR)/../ewdb)`, plus `-v $(EWDB):/ewdb` in `RUN`.
- `ewdb.mk`: `golang-package`, `EWDB_SITE = /ewdb`, `EWDB_SITE_METHOD = local`, `EWDB_GOMOD = ewdb`, `EWDB_BUILD_TARGETS = cmd/ewdbd cmd/ewdb-getprop cmd/ewdb-logcat cmd/ewdb-bugreportz cmd/ewdb-input cmd/ewdb-screencap`, `EWDB_GO_ENV = CGO_ENABLED=0`, build mode `exe` (not PIE, which would add an interpreter), `EWDB_LICENSE = Apache-2.0`, `EWDB_LICENSE_FILES = LICENSE`. Installs to `/usr/bin`.
- defconfig: remove `BR2_PACKAGE_ANDROID_TOOLS`, `BR2_PACKAGE_ANDROID_TOOLS_ADBD`, `# BR2_PACKAGE_ANDROID_TOOLS_FASTBOOT is not set` and `BR2_GLOBAL_PATCH_DIR`; add `BR2_PACKAGE_EWDB=y`.
- `/etc/ewdbd.conf`:

```
[device]
name = dsos
model = RG DS Plus
device = rgds-plus
state = /mnt/sdcard/dsOS
usb = ffs:/dev/usb-ffs/adb
serial_file = /sys/kernel/config/usb_gadget/dsos/strings/0x409/serialnumber

[props]
ro.product.manufacturer = Anbernic
ro.build.version.release = <contents of VERSION, filled by post-build.sh>

[commands]
logcat = ewdb-logcat
getprop = ewdb-getprop
input = ewdb-input
screencap = ewdb-screencap --screens top=DSI-2,bottom=DSI-1
bugreportz = ewdb-bugreportz

[logcat]
file = /var/log/messages

[bugreport]
version = cat /etc/os-release /proc/version /proc/cmdline
dmesg = dmesg
log = cat /var/log/messages
ps = ps
mounts = cat /proc/mounts
df = df -h
input = cat /proc/bus/input/devices
power = cat /sys/class/power_supply/*/uevent
net = ip addr
boot = cat /mnt/sdcard/dsOS/bootlog.txt

[input]
clone = retrogame_joypad, adc-keys-back, gpio-keys-volume, rk805 pwrkey, Goodix Capacitive TouchScreen
touch = Goodix Capacitive TouchScreen

[keys]
```

  Fill `[keys]` from the real devices: `adb shell cat /proc/bus/input/devices`, and `evtest` (in the dev image) on each, pressing each button, for `DPAD_*`, `BUTTON_*`, `BACK`, `VOLUME_*`, `POWER`, `HOME`. Check the boot log path against `/usr/libexec/dsos/bootlog`.
- `tests/ewdb.sh` (run in Docker against `/output/rgds-plus`): the package selects; `/usr/bin/ewdbd` and the five tools are installed; `readelf -l` shows no `INTERP` and `readelf -d` no `NEEDED` for each; `/etc/ewdbd.conf` exists; no `adbd` in target; `[commands]` maps the five tools.

- [ ] **Step 1: Write `tests/ewdb.sh`**, and update `tests/adb.sh` to expect `ewdbd` in place of `adbd`.
- [ ] **Step 2: Run** `make test T="tests/ewdb.sh tests/adb.sh"`. Expected: FAIL.
- [ ] **Step 3: Make the changes listed above.** Replace the android-tools row in `LICENSES.md` with ewdb (Apache-2.0), creack/pty (MIT) and golang.org/x/sys (BSD-3-Clause), at the versions in `go.mod`.
- [ ] **Step 4: Run** `make configs && make image && make image-dev && make test`. Expected: images build, `check-licenses` passes, every test `PASS`.
- [ ] **Step 5: Commit:** `"Replace adbd with ewdb"`.

### Task 18: dsOS: boot integration and on-device check

**Files:**
- Modify: `buildroot/board/rgds-plus/rootfs/usr/libexec/dsos/adb`, `.../etc/init.d/rcS`, `.../usr/libexec/dsos/frontend`, `tests/adb.sh`, `tests/rcs-dry.sh`, `tests/frontend.sh`
- Create: `buildroot/board/rgds-plus/rootfs/sdcard` (symlink to `mnt/sdcard`), `buildroot/board/rgds-plus/rootfs/data/local/tmp` (symlink to `/tmp`)

Changes:
- `adb` script: replace `adbd >/dev/null 2>&1 &` with a restart loop. While true: run `ewdbd -c /etc/ewdbd.conf 2>&1 | logger -t ewdbd`; when it exits, and `UDC` is empty and the sleep script has not saved one in `/run/dsos/udc`, wait for `ep1` and write the UDC again; sleep 1 s; repeat. Keep the `DSOS_DRY` plan output style.
- `rcS`: `syslogd -O /var/log/messages -s 1024 -b 1` and `klogd` directly after `lo` comes up; `ewdb-input --serve &` before the frontend starts.
- `frontend`: launcher output to `logger -t frontend` in place of `/dev/null`.
- Device check (first flash of ewdb):

- [ ] **Step 1: Add the failing dry-run assertions:** `tests/adb.sh` (plan contains `ewdbd -c /etc/ewdbd.conf`, and a restart `while`); `tests/rcs-dry.sh` (`syslogd` before `libexec/dsos/adb`, `ewdb-input --serve` before `libexec/dsos/frontend`); `tests/frontend.sh` (`logger -t frontend`).
- [ ] **Step 2: Run** `make test`. Expected: those FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** `make test`. Expected: PASS.
- [ ] **Step 5: Flash** the dev image (procedure above) and confirm `adb devices -l` shows `model:RG_DS_Plus` within 3 s of the boot logo.
- [ ] **Step 6: Run** `SERIAL=60f4bb517ca2779f sh ../ewdb/scripts/survey.sh`. Expected: every non-disruptive row `PASS`. Check by eye with Slot running that `adb shell input keyevent DPAD_DOWN` moves Slot's selection and `adb shell input tap` on a visible item selects it (confirm with `adb exec-out screencap -p`).
- [ ] **Step 7: Check unplug, sleep and kill:** unplug and replug the cable during `adb shell sleep 60`, close and reopen the lid, and `adb shell killall ewdbd`. After each, `adb devices` shows the device again within 5 s.
- [ ] **Step 8: Commit:** `"Start ewdb at boot with logging and input"`.

### Task 19: dsOS: reboot bootloader goes to U-Boot

**Files:**
- Create: `buildroot/board/rgds-plus/dts-patches/0004-reboot-mode.patch`, `buildroot/board/rgds-plus/uboot-patches/0004-rk3568-spl-boot-mode-takes-u-boot.patch`, `tests/reboot-mode.sh`
- Modify: `docs/03-open-questions.md` (Falcon boot section)

Values: boot mode register `0xfdc20200` (pmugrf `0xfdc20000` + `0x200`, U-Boot `CONFIG_ROCKCHIP_BOOT_MODE_REG`), `BOOT_FASTBOOT = 0x5242C309`, `BOOT_NORMAL = 0`.
- The DTS patch adds a `reboot-mode` node (`compatible = "syscon-reboot-mode"; offset = <0x200>; mode-normal = <BOOT_NORMAL>; mode-bootloader = <BOOT_FASTBOOT>; mode-recovery = <BOOT_RECOVERY>; mode-loader = <BOOT_BL_DOWNLOAD>;`) under `&pmugrf`. `CONFIG_SYSCON_REBOOT_MODE=y` is already set.
- The U-Boot patch extends `board_spl_mmc_get_uboot_raw_sector` in the 0003 patch's code: if `readl(0xfdc20200) == 0x5242C309`, write `0` (so U-Boot proper does not start fastboot) and return `raw_sect` (the U-Boot path); otherwise keep the lid check.
- No `[commands]` mapping is needed: unmapped `reboot bootloader` calls `RESTART2` with `bootloader` (Task 9).

- [ ] **Step 1: Write `tests/reboot-mode.sh`:** the DTS patch has `syscon-reboot-mode` and `mode-bootloader = <BOOT_FASTBOOT>`; the U-Boot patch has `0xfdc20200` and `0x5242C309`. Then build with `make br-dev-linux-rebuild` and check the DTB with `fdtget` (`reboot-mode` under the pmugrf syscon).
- [ ] **Step 2: Run** `make test T=tests/reboot-mode.sh`. Expected: FAIL.
- [ ] **Step 3: Write both patches.**
- [ ] **Step 4: Run** `make image-dev && make test`. Expected: PASS.
- [ ] **Step 5: Flash and check:** after `adb reboot`, `/proc/device-tree/serial-number` is absent (falcon). After `adb reboot bootloader`, it is present (U-Boot proper adds it). A following plain `adb reboot` is falcon again (flag cleared). Run `sh ../ewdb/scripts/survey.sh --disruptive`.
- [ ] **Step 6: Update `docs/03-open-questions.md`:** the adb paragraph (ewdb replaces adbd 4.2.2; the `android-tools` patches and the `exec:`/framebuffer notes are gone) and the falcon section (`reboot bootloader` takes the U-Boot path).
- [ ] **Step 7: Run** `make image && make test`, then commit: `"Send reboot bootloader to U-Boot"`.
