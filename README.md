# e-waste debug bridge (ewdb)

ewdb replaces the device side of adb (`adbd`) on Linux retro handhelds. The host keeps the stock `adb` from Android
platform-tools, unmodified, and it behaves the way it does with a phone.

Plug the handheld into your computer, and you can open a shell, copy files both ways, take screenshots, press buttons,
read the system log and grab a bug report, all with ordinary `adb` commands.

## Why

The `adbd` builds that Linux handheld distributions ship were made for Android. They expect `getprop`, `logcat`,
`screencap`, `input` and Android paths to exist. When they don't, commands hang, lose their exit codes or take the
device off USB.

ewdb speaks the current adb protocol and maps Android commands onto Linux equivalents. It ships its own
`getprop`, `logcat`, `bugreportz`, `input` and `screencap`, and anything Android-only fails right away with a message
saying it is not available.

## What works

| Host command                                                                     | On ewdb                                                                                                                                                                         |
|----------------------------------------------------------------------------------|---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `adb devices -l`, `get-state`, `get-serialno`, `get-devpath`                     | Works. Product name, model and device come from the config.                                                                                                                     |
| `adb shell`                                                                      | Works. Separate stdout and stderr and real exit codes. A pty only with `-t` or an interactive shell, none with `-T` or a command. Window size changes reach the pty. `-x` works. |
| `adb exec-out`, `adb exec-in`                                                    | Works, raw, no pty.                                                                                                                                                             |
| `adb push`, `pull`, `ls`                                                         | Works, including directories and `--sync`. Compression (`-z`) is not advertised, so the host falls back to uncompressed.                                                        |
| `adb forward`                                                                    | Works for `tcp:`, `localabstract:`, `localfilesystem:` and `dev:`, with `--list`, `--remove`, `--remove-all` and `--no-rebind`.                                                 |
| `adb reverse`                                                                    | Works for `tcp:` (bound to 127.0.0.1 on the device), `localabstract:` and `localfilesystem:`.                                                                                   |
| `adb shell getprop`, `adb logcat`                                                | Works with the `getprop` and `logcat` tools.                                                                                                                                    |
| `adb bugreport`                                                                  | Works with the `bugreportz` tool and produces a zip.                                                                                                                            |
| `adb exec-out screencap -p`                                                      | Works with the `screencap` tool. The `framebuffer:` service, which some host tools use to grab the screen, runs `screencap` too.                                                |
| `adb shell input tap`, `swipe`, `keyevent`, `text`                               | Works with the `input` tool. `text` needs `input --serve` running.                                                                                                              |
| `adb reboot`                                                                     | Runs the `reboot` mapping, or the system's `reboot` command.                                                                                                                    |
| `adb reboot ARG` (`bootloader`, `recovery`, ...)                                 | Runs the `reboot.ARG` mapping. Unmapped, ewdbd stops all processes, remounts disks read-only and asks the kernel to restart with ARG.                                           |
| `adb reconnect`                                                                  | Works. The daemon drops the connection and the host connects again.                                                                                                             |
| `adb root`                                                                       | Runs the `root` mapping. Unmapped: "adbd is already running as root" when it is, otherwise "root is not available on this device".                                              |
| `adb remount`                                                                    | Runs the `remount` mapping. Unmapped: "remount is not available on this device".                                                                                                |
| `adb unroot`, `disable-verity`, `enable-verity`, `sideload`, `backup`, `restore` | Not available. Each answers at once with a message saying so.                                                                                                                   |
| `adb install`, `uninstall`                                                       | Not available. There is no package manager; `pm` and `cmd` are stubs.                                                                                                           |
| `adb jdwp`                                                                       | Returns an empty list.                                                                                                                                                          |
| `adb usb`                                                                        | Answers "already running on USB".                                                                                                                                               |
| `adb tcpip`, `adb connect` (Wi-Fi adb)                                           | Not supported, on purpose. `tcpip` answers "Wi-Fi adb is not available on this device" and the USB session keeps working.                                                      |

Wi-Fi adb is left out on purpose: ewdbd has no network listener and serves USB only.

## Tested devices

- Anbernic RG DS Plus on dsOS: RK3568, Linux 7.2, DRM, two screens.
- Anbernic RG35XX SP and RG SP on Base OS 1.4.0: Allwinner H700, Linux 4.9, fbdev.

## Installing on a handheld

There is no install script yet. If your distribution already ships an `adbd` that its own USB script starts (Base OS
keeps it at `/usr/sbin/adbd`), you can swap in ewdb by hand over SSH. Copy `ewdbd` and your `ewdbd.conf` to the
handheld, then:

### Building

ewdbd is one static binary written in pure Go. Building it takes Go 1.26:

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o ewdbd ./cmd/ewdbd
CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -o ewdbd ./cmd/ewdbd
```

The first line builds for arm64 handhelds, the second for 32-bit armv7 ones. The result runs on Linux 3.2 or newer.

## Configuring

ewdbd reads `/etc/ewdbd.conf`, an INI-style file. If the file is missing or does not parse, the daemon logs a warning
and runs on defaults.

Here is a small config. It names the device, hooks up the bundled tools, and maps a few buttons so
`adb shell input keyevent` works:

```ini
[device]
name = mydistro
model = RG35XX SP
device = rg35xxsp

[props]
ro.product.manufacturer = Anbernic
ro.build.version.release = 1.0
ro.build.version.sdk = 0

[commands]
getprop = ewdbd getprop
logcat = ewdbd logcat
input = ewdbd input
screencap = ewdbd screencap
bugreportz = ewdbd bugreportz

[keys]
DPAD_UP = ANBERNIC-keys ABS_HAT0Y -1
DPAD_DOWN = ANBERNIC-keys ABS_HAT0Y 1
DPAD_LEFT = ANBERNIC-keys ABS_HAT0X -1
DPAD_RIGHT = ANBERNIC-keys ABS_HAT0X 1
```

List the bundled tools under `[commands]` (or put links named after them on `PATH`). If you don't, `adb shell logcat`
and the others get the "not available on this device" stub. The device names under `[keys]` are input device names as
the kernel reports them; `/proc/bus/input/devices` lists them. They differ between handhelds, so check yours.

### Reference

#### [device]

| Key                                               | Default          | Meaning                                                                                                                                                                              |
|---------------------------------------------------|------------------|--------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `name`                                            | `linux`          | `ro.product.name`                                                                                                                                                                    |
| `model`                                           | `Linux handheld` | `ro.product.model`                                                                                                                                                                   |
| `device`                                          | `handheld`       | `ro.product.device`                                                                                                                                                                  |
| `name_command`, `model_command`, `device_command` |                  | Shell command whose first output line replaces the fixed value. It must exit 0 within 2 seconds and print a non-empty line, otherwise the fixed value stays and a warning is logged. |
| `state`                                           | `/var/lib/ewdb`  | Writable state directory. If `<state>/ewdbd.conf` exists and parses, it replaces the whole of `/etc/ewdbd.conf`, so users can remap on a read-only root filesystem.                  |
| `usb`                                             | `auto`           | `ffs:DIR`, `android:PATH` or `auto`. `auto` tries `/dev/usb-ffs/adb`, then `/dev/android_adb`. A bad or failing value falls back to `auto`.                                          |
| `serial_file`                                     |                  | File whose first line is `ro.serialno` in `getprop`.                                                                                                                                 |
| `shell`                                           | `/bin/sh`        | Shell for `adb shell`, `exec:` and mapped actions.                                                                                                                                   |

#### [props]

Any `key = value` here is added to `getprop` output, overriding the built-in properties. Host tools ask for
`ro.build.version.release`, `ro.build.version.sdk` and `ro.product.manufacturer`, so set those.

#### [commands]

Maps command names to shell commands. The value is shell text, and the caller's arguments are appended to it.

Android commands that are neither mapped nor found on the system `PATH` get a stub that prints
`NAME: not available on this device` and exits 127. The list is `am`, `pm`, `monkey`, `content`, `ime`, `uiautomator`,
`run-as`, `cmd`, `settings`, `svc`, `wm`, `dumpsys`, `setprop`, `screenrecord`, `logcat`, `getprop`, `input`,
`screencap` and `bugreportz`.

Daemon actions look up dotted names in the same section: `reboot`, `reboot.bootloader`, `reboot.recovery` (any
`reboot.ARG`), `remount` and `root`. Their output goes back to the host, and they are stopped after 30 seconds.

#### [logcat]

`file`: the log `logcat` reads. Default `/var/log/messages`.

#### [bugreport]

`dir`: where the zip is written. Default `/tmp`. Every other key is one entry in the zip, `KEY.txt`, holding the output
of its command. Each command gets 30 seconds, or the duration in `EWDB_BUGREPORT_TIMEOUT`.

#### [input]

`touch`: the touch screen's input device name, as the kernel reports it. `tap` and `swipe` use the panel's raw
coordinate range. `socket`: the socket `input --serve` listens on. Default `/run/ewdb/input.sock`.

#### [keys]

Maps Android key names (`DPAD_UP`, `KEYCODE_DPAD_UP` and key numbers such as `19` all work) to a device name and a Linux
code: `BUTTON_A = retrogame_joypad BTN_SOUTH`. The supported names are the D-pad, `DPAD_CENTER`, A, B, X, Y, L1, R1, L2,
R2, the thumb buttons, start, select, mode, home, back, menu, enter, volume and power.

For a D-pad that reports a hat, give an axis and a value instead: `DPAD_UP = ANBERNIC-keys ABS_HAT0Y -1`. The key press
writes that value, waits, then returns the axis to the centre of its range. A value outside the device's range, or a
code or axis the device lacks, is an error.

### The bundled tools

The tools work from SSH as well, without the daemon running. They read the config named by `EWDB_CONFIG`, which the
daemon sets for everything it starts, and fall back to `/etc/ewdbd.conf`.

- `getprop [NAME [DEFAULT]]`: prints the product properties from the config, `ro.product.cpu.abi`, `ro.serialno` from
  `serial_file`, and everything in `[props]`. Read-only.
- `logcat`: shows the file named in `[logcat]` and follows it. Supports `-d`, `-c`, `-t N`, `-T N` and tag filters. When
  that file does not exist, it shows the kernel log from `/dev/kmsg` instead, says so on stderr, and leaves it alone on
  `-c`.
- `bugreportz`: runs each command in `[bugreport]`, stores the output in a zip and prints Android's `BEGIN:`,
  `PROGRESS:` and `OK:` lines, so `adb bugreport` works.
- `input [SOURCE] tap X Y | swipe X1 Y1 X2 Y2 [MS] | keyevent KEY... | text STRING`: `tap`, `swipe` and `keyevent` write
  events straight into the real `/dev/input/event*` device, so a frontend that already has it open sees them as the real
  controls. `text` goes through `input --serve`, which owns a uinput keyboard and takes requests on a unix socket.
  Without it, `text` fails and the other commands are unaffected.
- `screencap [-p] [-d NAME|INDEX] [--screens name=CONNECTOR,...] [FILE]`: reads the screens through DRM and stacks lit
  screens top to bottom. With no DRM card or no lit screen it reads `/dev/fb0`. `--screens` names connectors in order,
  `-d` picks one screen by name or index. Without `-p` (or a FILE ending in `.png`) the output is raw RGBA.

## For distribution maintainers

### USB gadget setup

ewdbd needs one of these:

- FunctionFS: a configfs gadget with an `ffs.adb` function, mounted at `/dev/usb-ffs/adb` (or the `ffs:DIR` you
  configure). ewdbd opens `ep0` and writes the descriptors and strings itself. The UDC can only be bound after that,
  once `ep1` exists, so the init script waits for it.
- The legacy Android gadget: `/dev/android_adb` on old vendor kernels.

The vendor and product IDs, serial number and strings shown to the host come from the gadget, not from ewdbd. A minimal
FunctionFS bring-up:

```sh
G=/sys/kernel/config/usb_gadget/g1
grep -q configfs /proc/mounts || mount -t configfs none /sys/kernel/config
mkdir -p $G/strings/0x409 $G/configs/b.1/strings/0x409 $G/functions/ffs.adb
echo 0x18d1 > $G/idVendor
echo 0x4ee7 > $G/idProduct
echo 0123456789ABCDEF > $G/strings/0x409/serialnumber
echo "My Distro" > $G/strings/0x409/manufacturer
echo "My Handheld" > $G/strings/0x409/product
echo adb > $G/configs/b.1/strings/0x409/configuration
ln -sf $G/functions/ffs.adb $G/configs/b.1/
mkdir -p /dev/usb-ffs/adb
mount -t functionfs adb /dev/usb-ffs/adb
ewdbd -c /etc/ewdbd.conf &
while [ ! -e /dev/usb-ffs/adb/ep1 ]; do sleep 0.1; done
ls /sys/class/udc | head -n 1 > $G/UDC
```

If ewdbd exits, restart it and bind the UDC again.

### Command line

`ewdbd` with no arguments, or only daemon flags, runs the daemon. The flags are `-c CONFIG` (default `/etc/ewdbd.conf`)
and `-launchers DIR` (default `/run/ewdbd/bin`). `ewdbd TOOL ARGS...` runs a tool, for example `ewdbd screencap -p`. A
link to `ewdbd` named `ewdb-TOOL`, or named after the tool itself, runs that tool. `ewdbd help`, `-h` and `--help` print
the usage.

## Design

ewdb has two aims: a stock platform-tools `adb` should treat a handheld the way it treats a phone, and the daemon has to
run on very old systems.

### One static binary

ewdbd is pure Go built with `CGO_ENABLED=0`, so it has no dynamic loader and no dependency on
the device's libc. The same file runs on glibc, musl and old vendor images, on Linux 3.2 or newer.

### The current adb protocol

ewdbd announces protocol version `0x01000001` with a maximum payload of 1 MiB, lowered if
the host asks for less. Its connect banner lists only the features it implements: `shell_v2`, `stat_v2`, `ls_v2` and
`fixed_push_mkdir`. The host picks its behavior from that list, so an honest list is how unsupported things fail
cleanly instead of hanging.

### Sessions and flow control

A session starts with the host's `CNXN` and carries many streams, each with its own ID.
Outgoing data is split at the negotiated payload size, and each write waits for the host's `OKAY` before the next one
goes out. Incoming data is acknowledged once it has been consumed. When the host goes away, every stream closes and
the processes it started are stopped.

### Built-in tools

`getprop`, `logcat`, `bugreportz`, `input` and `screencap` live in the same binary and are picked
by name, BusyBox style.

The full design notes are in [docs/design.md](docs/design.md).

## License

Apache-2.0. See [LICENSE](LICENSE) and [NOTICE](NOTICE).
