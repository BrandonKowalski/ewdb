# ewdb design

Written 2026-11-07.

## Intent

The stock `adb` on a developer's computer should work against a Linux retro handheld
the way it works against a phone. Today dsOS runs Buildroot's `adbd`, a 2013 snapshot
of Android 4.2.2. It speaks an old protocol, assumes Android underneath, and fails in
ways that surprise people: lost exit codes, a pty on every shell, no Wi-Fi adb, a
`tcpip` command that kills the daemon, and most Android shell commands missing.

ewdb, the e-waste debug bridge, replaces the device side only. Its daemon,
`ewdbd`, speaks the current adb
protocol to an unmodified host `adb`, and a small set of device tools gives the
Android commands people reach for a Linux meaning (`screencap` reads the DRM
screens, `logcat` shows the system log, `input` taps the touch screen). It is built
for Linux handhelds in general, with dsOS on the RG DS+ as the first target.

## Decisions made by the owner

- A drop-in replacement for the device side. The host keeps the normal `adb`
  from Android platform-tools, unmodified.
- Aimed at Linux based retro handhelds in general, not only dsOS.
- No Wi-Fi adb in v1 (2026-11-07, reversing an earlier yes the same day). The
  daemon listens on USB only. If Wi-Fi adb comes back later, only hosts that ran
  `adb pair` with a code from the device may use it; USB connections are not enough.
- License: Apache-2.0, the same as AOSP adb, so AOSP code may be ported with its
  notices kept.
- Language: Go (confirmed after the designer proposed it).
- Name: ewdb, for e-waste debug bridge. The daemon is `ewdbd`.
- A sidecar config file with a section that maps adb commands to Linux commands,
  because what each distribution has varies so much.
- Two ways to give the device model in the config: a fixed `model`, and a
  `model_command` whose output is the model, for distributions that run on several
  devices.
- Built to run on very old systems: no dependency on the device's glibc version.
- The scope below, sorted into "yes, core", "yes, mapped", "no, fail clearly" and
  "later", was approved on 2026-11-07 as proposed.
- `root`/`unroot`, `install`/`uninstall` and `sideload` were not worth testing
  against the old daemon. They are not v1 features (see Non-goals).

## Survey of the current daemon

Run on 2026-11-07 against dsOS with patched `adbd` 4.2.2 and platform-tools
37.0.0. The patches already in `buildroot/patches/android-tools` rename the device,
add `exec:`, and fix the `framebuffer:` service.

| Works | Broken |
| --- | --- |
| `devices -l`, `get-state`, `get-serialno`, `get-devpath` | `shell` exit codes always 0 |
| `shell`, `shell -x`, `exec-out` | `shell` merges stderr, returns `\r\n`, refuses `-T` |
| `push`, `pull` (files, directories, `--sync`, `-z`, `-a`) | `tcpip` prints a message, then adbd exits and USB drops |
| `forward` (once loopback is up) | `connect`: nothing listens on TCP |
| `reboot`, `reboot bootloader`, `reboot recovery` (all reboot normally) | `reverse`: unsupported |
| `screencap` (dsOS tool) | `remount` prints "remount failed: Success" |
| | `disable-verity`, `enable-verity`: "closed" |
| | `jdwp` hangs |
| | `bugreport` wants `bugreportz` |
| | `usb` is a stub |

Shell commands not present: `getprop`, `setprop`, `pm`, `am`, `monkey`, `input`,
`wm`, `dumpsys`, `settings`, `cmd`, `screenrecord`, `logcat`, `run-as`, `content`,
`ime`, `uiautomator`, `toybox`. `svc` is BusyBox's, unrelated to Android's.
`/sdcard` and `/data/local/tmp` do not exist.

The survey also found a dsOS bug outside adb: nothing brings up `lo`, so 127.0.0.1
does not exist. That is what broke `forward`. It is fixed as part of this work (see
Plan order), and it matters to the new daemon too.

## Goals

### Core: the daemon

- Current adb wire protocol, version and `maxdata` as current platform-tools expect,
  with a `features` list in the `CNXN` banner that only names what is implemented.
  The host picks behaviour from that list, so an honest list is how unsupported
  things fail cleanly instead of hanging.
- Transport: USB through FunctionFS (the existing configfs gadget, same
  `18d1:4ee7` IDs and serial). No TCP listener.
- Banner properties `ro.product.name`, `ro.product.model`, `ro.product.device`
  from configuration (dsOS: `dsos`, `RG DS Plus`, `rgds-plus`). The model comes
  from `model_command` when it is set, exits 0 and prints a non-empty first line;
  otherwise from `model`. The same `name_command` and `device_command` pair exists
  for the other two (designer's call, for symmetry).
- Shell protocol v2 (`shell_v2` feature): separate stdout and stderr, real exit
  codes, pty only when asked (`-t`) or interactive, no pty with `-T` or a command,
  window size changes passed to the pty.
- `exec:` (raw, no pty), as `exec-out` uses.
- File sync: `push`, `pull`, `ls`, `stat`, including the v2 stat and list forms the
  host uses when the matching features are advertised. Compression (`-z`) is
  optional; if it is not implemented the feature is not advertised and the host
  falls back.
- `forward` and `reverse` for `tcp:` and `localabstract:`/`localfilesystem:` unix
  sockets, with `--list`, `--remove`, `--remove-all`, `--no-rebind`.
- `tcpip PORT` answers that Wi-Fi adb is not available and leaves the USB
  session working. `usb` answers that the daemon is already on USB. No features
  for TLS, pairing or mDNS are advertised.
- `reboot`, `reboot bootloader`, `reboot recovery`, `remount` and `root` run the
  command the commands section maps them to (see Command mapping). Unmapped,
  `reboot` and its variants do a normal reboot and the others fail clearly. dsOS
  maps `reboot.bootloader` to its lid-closed U-Boot path, which needs a way for SPL
  to be told (see Open items).
- `framebuffer:` served by running `screencap` (raw form), as Android does.
- AOSP's `packages/modules/adb` is the protocol reference for everything above.
- Clear, immediate errors for Android-only services: `jdwp` (empty list),
  `disable-verity`/`enable-verity`, `remount`, `root`/`unroot` ("adbd is already
  running as root" when it is), `sideload`, `install` paths (`abb_exec`, `cmd
  package`). No hangs.
- Survives host disconnects, `tcpip`/`usb` switches and USB unplug/replug without
  exiting. If it does exit, whatever starts it restarts it and rebinds the UDC.

### Command mapping

The sidecar config file has a commands section that maps the commands adb users
expect to the Linux commands a distribution has. What is available varies too
much between handheld distributions for one built-in answer. Example for dsOS:

```
[commands]
logcat = logread -f
screencap = ewdb-screencap
getprop = ewdb-getprop
input = ewdb-input
bugreportz = /usr/libexec/dsos/bugreport
reboot.bootloader = /usr/libexec/dsos/reboot-recovery
```

A systemd distribution might map `logcat` to `journalctl -f`, and a Wayland one
`screencap` to `grim -`.

- Every shell and `exec:` the daemon starts gets a private directory at the front
  of its `PATH`. Each mapped name there is a small launcher that runs the
  configured command with the caller's arguments appended. This covers
  `adb shell logcat`, typing `logcat` in an interactive `adb shell`, and host
  commands that run things behind the scenes (`adb logcat`, `adb bugreport`, the
  `framebuffer:` service). The system's own `PATH` is unchanged, and SSH sessions
  do not see the mappings.
- Daemon-level actions (`reboot` and its variants, `remount`, `root`) look up the
  same section, under dotted names for variants.
- Android-only commands with no mapping (`pm`, `am`, `settings`, and the rest of
  Non-goals) get a launcher that prints "<name>: not available on this device" and
  exits non-zero, instead of the shell's "not found".
- The file ships in the image. Where the root filesystem is read-only, a copy in
  the state directory on the card overrides it, so a user can remap without
  rebuilding. (Designer's call.)

### Default tools

ewdb ships default implementations for the commands that have no common Linux
equivalent. They are ordinary programs named `ewdb-<command>`, usable from SSH
too, and they do not depend on the daemon. A distribution maps to them or to
something else.

- `screencap`: already exists in `dsos-tools` in C. Port it to Go here as
  `ewdb-screencap` (it reads DRM connectors named in the config file, with the
  old-kernel fallbacks above), and remove the C version from dsOS.
- `logcat`: the system log. Needs a log collector on the device that keeps kernel
  messages, boot script output and frontend output (today `launch_frontend.sh`
  output goes to `/dev/null`). Supports `-d` (dump and exit), following by default,
  `-c` (clear), and simple tag or priority filtering where the source carries it.
  Android's `-v` formats and `-b` buffers are not required. Where the configured
  file does not exist (no syslog daemon), `ewdb-logcat` reads the kernel log from
  `/dev/kmsg` instead, says so on stderr, and leaves it alone on `-c`.
- `bugreport`: `bugreportz` with Android's progress and `OK:<path>` protocol, so
  plain `adb bugreport` produces a zip. Contents: OS version, kernel version and
  command line, `dmesg`, the log, `ps`, mounts, `df`, input devices, DRM
  connectors, power supply state, network state with secrets removed, and the boot
  timing record dsOS already writes.
- `input`: `tap X Y`, `swipe X1 Y1 X2 Y2 [ms]`, `keyevent KEY...`, `text STRING`.
  `tap`, `swipe` and `keyevent` write `input_event` records straight into the real
  `/dev/input/eventN` node of the target device, found by its name
  (`EVIOCGNAME`, first match in sorted `event*` order, as Slot picks it). The
  kernel delivers injected events to every reader of that device, so a frontend
  that already has the node open sees them as the real controls, with no server
  involved. Touch coordinates are the panel's raw range from `EVIOCGABS`
  (`Goodix Capacitive TouchScreen` in dsOS); the multi-touch, `BTN_TOUCH` and
  `ABS_X`/`ABS_Y` events are sent only where the device reports them. `keyevent`
  accepts Android key names and numbers for the keys a handheld has (D-pad,
  A/B/X/Y, shoulders, start, select, home, back, volume, power); the `[keys]`
  section maps each to a device name and a Linux code, and a code the device does
  not have is an error. An entry can instead name an axis and a value, as in
  `DPAD_UP = ANBERNIC-keys ABS_HAT0Y -1`, for a D-pad that reports a hat: the key
  press writes that value and `SYN_REPORT`, waits as for a key, then writes 0 and
  `SYN_REPORT`; an axis the device does not have is an error too. Concurrent
  invocations are serialized with an advisory `flock` on the event node. Only
  `text` needs a persistent device:
  `ewdb-input --serve` owns a uinput keyboard (`ewdb keyboard`, limited to the
  keys `text` types plus shift) and runs `text` requests over a unix socket;
  without it `text` fails and the other commands are unaffected. If another
  process holds an `EVIOCGRAB` on a device, the kernel does not deliver
  injected events to other readers.
- `getprop`: prints the properties the daemon advertises plus the ones host tools
  ask for first (`ro.build.version.release`, `ro.build.version.sdk`,
  `ro.product.cpu.abi`, `ro.serialno`, `ro.product.manufacturer`, and similar),
  from the same configuration. Read-only; `setprop` is not provided.
- `/sdcard` links to the card mount, and `/data/local/tmp` to a writable
  temporary directory, so pasted commands work.

### Later (not v1)

- Wi-Fi adb: `tcpip`/`usb`, TLS connect, `adb pair` pairing and mDNS
  advertising, compatible with platform-tools, pairing required for every host.
- `screenrecord`.
- `install`/`uninstall` meaning "put a game or app on the card".
- A device settings command (brightness, volume, Wi-Fi) in place of Android's
  `settings`, `svc`, `wm` and `dumpsys`.
- A writable overlay so `remount` means something on dev images.

## Non-goals

- Running Android apps or anything that needs the Android framework: `pm`, `am`,
  `monkey`, `content`, `ime`, `uiautomator`, `run-as`, `cmd`, `jdwp` debugging.
- `sync` (needs an Android build tree on the host).
- Changes to the host `adb`.
- `root`/`unroot` privilege switching. The daemon runs as root, as dsOS's does
  today.
- dm-verity.

## Anti-goals

These would make the project a failure even if the feature list works:

- A shell or forward that a stock platform-tools `adb` handles differently from a
  phone in ways a script would notice (exit codes, stream separation, line
  endings).
- Anything that hangs. Every unsupported request fails fast with a message.
- The daemon listening on any network interface.
- The daemon taking the device off USB (the current `tcpip` behaviour).
- Slowing boot. The daemon must be serving USB within the same window adbd is
  today (about 2.6 s from kernel start on dsOS).
- dsOS-only assumptions baked into the daemon or tools. Board specifics live in
  configuration.

## Constraints

- Root filesystem is read-only squashfs. Any persistent state lives on the card, next to
  `wifi.conf` and `authorized_keys` in `/mnt/sdcard/dsOS/` on dsOS. The location
  is configurable for other distributions.
- dsOS builds with Buildroot 2026.02.3 in Docker on an arm64 Mac (`make image`,
  `make image-dev`, `make test`), and the daemon replaces `BR2_PACKAGE_ANDROID_TOOLS_ADBD`
  in `dsos_rgds_plus_defconfig`. The `buildroot/patches/android-tools` patches are
  then removed.
- RK3568, 981 MB RAM, aarch64. Other targets: arm64 and 32-bit arm (armv7)
  handhelds, many with old vendor kernels and old glibc, some with musl.
- Every shipped binary is static with no dynamic loader and no libc dependency:
  pure Go, `CGO_ENABLED=0`. This rules out C tools and cgo libraries, so
  `screencap` is ported to Go. Go 1.26 needs Linux 3.2 or newer, which is the
  floor.
- Old kernels: USB through the legacy Android gadget device (`/dev/android_adb`)
  as well as FunctionFS, FunctionFS descriptors in the v1 format when the v2 one
  is refused, `screencap` falling back from `GETFB2` to `GETFB` and then to
  `/dev/fb0`, and the `text` keyboard falling back from `UI_DEV_SETUP` to the older
  `uinput_user_dev` write.
- The handheld has no UI of its own for prompts. The frontend, Slot (`../slot`,
  running from `/mnt/sdcard/System/slot`), owns the screens. Slot is the
  acceptance target for anything a frontend has to see, such as `input`.
- Repo conventions: no code comments, single-line commit subjects, no emojis or em
  dashes anywhere, tests under `tests/` run by `make test` in Docker.
- Licensing: dsOS keeps a `LICENSES.md` that `tools/check-licenses.sh` checks
  against the image. Reading AOSP for protocol details is fine (Apache-2.0);
  copying AOSP code brings its notice requirements with it.

## How it gets made

Calls made by the designer, not the owner. The owner may overrule any of them.

- **Go** (owner confirmed). One static binary per architecture with no runtime
  dependencies is the easiest thing to drop onto other handheld distributions, and
  memory safety matters for a root daemon. Buildroot builds it with its
  `golang-package` infrastructure. Its standard library also covers the TLS, RSA
  and HKDF a later Wi-Fi adb would need.
- **Home: its own repository**, sibling to dsOS, since it targets more than dsOS.
  dsOS gets a Buildroot package that builds it from a pinned revision. Until it
  has a public home, a local path works the way `dsos-tools` does.
- **Default tools as one multi-call binary or separate binaries**, named
  `ewdb-<command>`, builder's choice.
- **Config file:** `/etc/ewdbd.conf`, INI-style as in the example, one per device.
  Besides `[commands]` it holds product strings, DRM connector names, touch and
  gamepad device names, the state directory and the USB gadget path. dsOS ships it
  in the image.
- **Authentication:** USB connections are trusted as today (physical access), with
  no `AUTH` exchange.
- **Logging:** a small collector started first in `rcS`, reading `/dev/kmsg` and a
  socket that boot scripts and the frontend launcher write to, keeping a ring buffer
  in RAM with an optional copy on the card. BusyBox `syslogd` is an acceptable
  substitute if it meets the `logcat` needs above.
- **Supervision:** the dsOS boot script starts the daemon from a small loop that
  restarts it and rebinds the UDC if it exits.

## Plan order

1. Bring up `lo` in `rcS`, with a test.
2. Daemon core over USB: banner, `features`, shell v2, `exec:`, sync, `forward`,
   `reverse`, clear failures. Replaces adbd 4.2.2 in the dev image first.
3. Device tools: `getprop`, `input`, `logcat` with its collector, `bugreportz`,
   the `/sdcard` and `/data/local/tmp` links.
4. `reboot bootloader` mapping, supervision, release image.

Each step ends with the survey re-run against the device and its rows moved from
"Broken" to "Works".

## Testing

- Unit tests for the wire format, shell v2 packets and sync.
- An end-to-end test that drives the daemon with a real platform-tools `adb` on
  the build host: shell exit codes and streams, push and pull, forward and
  reverse, and the clear failures. The daemon may take a test-only local transport
  for this (for example a TCP listener bound to 127.0.0.1 that only test builds
  enable), since the shipped daemon has no network listener.
- The on-device survey from this document as a script (`tests/` or `tools/`), run
  by the owner or over adb, with the commands that reboot or drop the connection
  behind a flag.

## Open items for the builder

- How `reboot bootloader` tells SPL to take the lid-closed path on dsOS (a flag in
  a register that survives reset, or a file on the boot partition that SPL reads).
- Whether `localabstract:` forwarding is worth it in v1 for tools that expect it,
  or `tcp:` alone is enough.
- Exact `features` list to advertise, decided by what is implemented.
