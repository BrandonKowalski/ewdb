# ewdb

ewdb, the e-waste debug bridge, replaces the device side of adb (`adbd`) on Linux retro handhelds. The host keeps the
stock `adb` from Android platform-tools, unmodified, and it behaves the way it does with a phone.

## Why

The `adbd` builds that Linux handheld distributions ship were written for Android. They expect `getprop`, `logcat`,
`screencap`, `input` and Android paths to exist, and when they don't, commands hang, lose their exit codes or take the
device off USB.

ewdb speaks the current adb protocol and maps the Android commands people use onto Linux equivalents. It ships its own
`getprop`, `logcat`, `bugreportz`, `input` and `screencap`, and anything Android-only fails right away with a message
saying it is not available.

It is one static binary written in pure Go (`CGO_ENABLED=0`). It has no dependency on the device's libc and runs on
Linux 3.2 or newer, on arm64 and armv7.

## What works

| Host command                                                                     | On ewdb                                                                                                                                                                                              |
|----------------------------------------------------------------------------------|------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `adb devices -l`, `get-state`, `get-serialno`, `get-devpath`                     | Works. Product name, model and device come from the config.                                                                                                                                          |
| `adb shell`                                                                      | Works. Separate stdout and stderr and real exit codes (shell protocol v2). A pty only with `-t` or an interactive shell, none with `-T` or a command. Window size changes reach the pty. `-x` works. |
| `adb exec-out`, `adb exec-in`                                                    | Works, raw, no pty.                                                                                                                                                                                  |
| `adb push`, `pull`, `ls`                                                         | Works, including directories, `--sync` and the v2 stat and list forms. Compression (`-z`) is not advertised, so the host falls back to uncompressed.                                                 |
| `adb forward`                                                                    | Works for `tcp:`, `localabstract:`, `localfilesystem:` and `dev:`, with `--list`, `--remove`, `--remove-all` and `--no-rebind`.                                                                      |
| `adb reverse`                                                                    | Works for `tcp:` (bound to 127.0.0.1 on the device), `localabstract:` and `localfilesystem:`.                                                                                                        |
| `adb shell getprop`                                                              | Works with the `getprop` tool.                                                                                                                                                                       |
| `adb logcat`                                                                     | Works with the `logcat` tool.                                                                                                                                                                        |
| `adb bugreport`                                                                  | Works with the `bugreportz` tool and produces a zip.                                                                                                                                                 |
| `adb exec-out screencap -p`                                                      | Works with the `screencap` tool. The `framebuffer:` service, which some host tools use to grab the screen, runs `screencap` too.                                                                     |
| `adb shell input tap`, `swipe`, `keyevent`, `text`                               | Works with the `input` tool. `text` needs `input --serve` running.                                                                                                                                   |
| `adb reboot`                                                                     | Runs the `reboot` mapping, or the system's `reboot` command.                                                                                                                                         |
| `adb reboot ARG` (`bootloader`, `recovery`, ...)                                 | Runs the `reboot.ARG` mapping. Unmapped, ewdbd stops all processes, remounts disks read-only and asks the kernel to restart with ARG.                                                                |
| `adb reconnect`                                                                  | Works. The daemon drops the connection and the host connects again.                                                                                                                                  |
| `adb root`                                                                       | Runs the `root` mapping. Unmapped: "adbd is already running as root" when it is, otherwise "root is not available on this device".                                                                   |
| `adb remount`                                                                    | Runs the `remount` mapping. Unmapped: "remount is not available on this device".                                                                                                                     |
| `adb unroot`, `disable-verity`, `enable-verity`, `sideload`, `backup`, `restore` | Not available. Each answers at once with a message saying so.                                                                                                                                        |
| `adb install`, `uninstall`                                                       | Not available. There is no package manager; `pm` and `cmd` are stubs.                                                                                                                                |
| `adb jdwp`                                                                       | Returns an empty list.                                                                                                                                                                               |
| `adb usb`                                                                        | Answers "already running on USB".                                                                                                                                                                    |
| `adb tcpip`, `adb connect` (Wi-Fi adb)                                           | Not supported, on purpose. ewdbd has no network listener and serves USB only. `tcpip` answers "Wi-Fi adb is not available on this device" and the USB session keeps working.                         |

There is no `AUTH` exchange. USB connections are trusted, as with the `adbd` builds handhelds already ship.

## One binary, several tools

`ewdbd` is a multi-call binary:

- `ewdbd` with no arguments, or only daemon flags, runs the daemon. The flags are `-c CONFIG` (default
  `/etc/ewdbd.conf`) and `-launchers DIR` (default `/run/ewdbd/bin`).
- `ewdbd TOOL ARGS...` runs a tool, for example `ewdbd screencap -p`.
- A link to `ewdbd` named `ewdb-TOOL`, or named after the tool itself, runs that tool.
- `ewdbd help`, `-h` and `--help` print the usage.

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

## Configuration

ewdbd reads `/etc/ewdbd.conf`, an INI-style file. If the file is missing or
does not parse, the daemon logs a warning and runs on defaults.

### [device]

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

### [props]

Any `key = value` here is added to `getprop` output, overriding the built-in properties. Host tools ask for
`ro.build.version.release`, `ro.build.version.sdk` and `ro.product.manufacturer`, so set those.

### [commands]

Maps command names to shell commands. The value is shell text.

When the daemon starts, it fills a launcher directory (`-launchers`, default `/run/ewdbd/bin`) and puts it first on
`PATH` for every shell and `exec:` it starts. Each mapped name there is a small script that runs the mapping with the
caller's arguments appended, with the system `PATH` restored. So `adb shell logcat`, typing `logcat` in an interactive
shell, `adb logcat`, `adb bugreport` and the `framebuffer:` service all reach the mapping. SSH sessions and the system
`PATH` are not changed.

Android commands that are neither mapped nor found on the system `PATH` get a stub that prints
`NAME: not available on this device` and exits 127. The list is `am`, `pm`, `monkey`, `content`, `ime`, `uiautomator`,
`run-as`, `cmd`, `settings`, `svc`, `wm`, `dumpsys`, `setprop`, `screenrecord`, `logcat`, `getprop`, `input`,
`screencap` and `bugreportz`. The bundled tools are on that list, so map them (or put links named after them on `PATH`)
or they become stubs too.

Daemon actions look up dotted names in the same section: `reboot`, `reboot.bootloader`, `reboot.recovery` (any
`reboot.ARG`), `remount` and `root`. Their output goes back to the host, and they are stopped after 30 seconds.

The launcher directory must either not exist or carry ewdbd's marker file. ewdbd will not replace a directory it did not
create; it logs why and runs without launchers.

### [logcat]

`file`: the log `logcat` reads. Default `/var/log/messages`.

### [bugreport]

`dir`: where the zip is written. Default `/tmp`. Every other key is one entry in the zip, `KEY.txt`, holding the output
of its command. Each command gets 30 seconds, or the duration in `EWDB_BUGREPORT_TIMEOUT`.

### [input]

`touch`: the touch screen's input device name, as the kernel reports it. `tap` and `swipe` use the panel's raw
coordinate range. `socket`: the socket `input --serve` listens on. Default `/run/ewdb/input.sock`.

### [keys]

Maps Android key names (`DPAD_UP`, `KEYCODE_DPAD_UP` and key numbers such as `19` all work) to a device name and a Linux
code: `BUTTON_A = retrogame_joypad BTN_SOUTH`. The supported names are the D-pad, `DPAD_CENTER`, A, B, X, Y, L1, R1, L2,
R2, the thumb buttons, start, select, mode, home, back, menu, enter, volume and power.

For a D-pad that reports a hat, give an axis and a value instead: `DPAD_UP = ANBERNIC-keys ABS_HAT0Y -1`. The key press
writes that value, waits, then returns the axis to the centre of its range. A value outside the device's range, or a
code or axis the device lacks, is an error.

### Example

This example is based on the dsOS configuration, with `dir`, `shell`, `socket` and a `model_command` added to show every
key. When `model_command` succeeds, its output wins over `model`.

```ini
[device]
name = dsos
model = RG DS Plus
model_command = tr -d '\000' < /proc/device-tree/model
device = rgds-plus
state = /mnt/sdcard/dsOS
usb = ffs:/dev/usb-ffs/adb
serial_file = /sys/kernel/config/usb_gadget/dsos/strings/0x409/serialnumber
shell = /bin/sh

[props]
ro.product.manufacturer = Anbernic
ro.build.version.release = 1.0
ro.build.version.sdk = 0

[commands]
logcat = ewdbd logcat
getprop = ewdbd getprop
input = ewdbd input
screencap = ewdbd screencap --screens top=DSI-2,bottom=DSI-1
bugreportz = ewdbd bugreportz
reboot.bootloader = /usr/libexec/dsos/bootflag set && reboot

[logcat]
file = /var/log/messages

[bugreport]
dir = /tmp
version = cat /etc/os-release /proc/version /proc/cmdline
dmesg = dmesg
log = cat /var/log/messages
ps = ps
mounts = cat /proc/mounts
df = df -h
input = cat /proc/bus/input/devices
power = cat /sys/class/power_supply/*/uevent
net = ip addr
drm = grep . /sys/class/drm/*/status

[input]
touch = Goodix Capacitive TouchScreen
socket = /run/ewdb/input.sock

[keys]
DPAD_UP = retrogame_joypad BTN_DPAD_UP
DPAD_DOWN = retrogame_joypad BTN_DPAD_DOWN
DPAD_LEFT = retrogame_joypad BTN_DPAD_LEFT
DPAD_RIGHT = retrogame_joypad BTN_DPAD_RIGHT
BUTTON_A = retrogame_joypad BTN_SOUTH
BUTTON_B = retrogame_joypad BTN_EAST
BUTTON_X = retrogame_joypad BTN_NORTH
BUTTON_Y = retrogame_joypad BTN_WEST
BUTTON_L1 = retrogame_joypad BTN_TL
BUTTON_R1 = retrogame_joypad BTN_TR
BUTTON_START = retrogame_joypad BTN_START
BUTTON_SELECT = retrogame_joypad BTN_SELECT
VOLUME_UP = gpio-keys-volume KEY_VOLUMEUP
VOLUME_DOWN = gpio-keys-volume KEY_VOLUMEDOWN
POWER = rk805 pwrkey KEY_POWER
```

On a device whose D-pad is a hat, the D-pad lines would read `DPAD_UP = ANBERNIC-keys ABS_HAT0Y -1`,
`DPAD_DOWN = ANBERNIC-keys ABS_HAT0Y 1`, and the same with `ABS_HAT0X` for left and right.

## USB setup

ewdbd needs one of these:

- FunctionFS: a configfs gadget with an `ffs.adb` function, mounted at `/dev/usb-ffs/adb` (or the `ffs:DIR` you
  configure). ewdbd opens `ep0` and writes the descriptors and strings itself, falling back to the v1 descriptor format
  on kernels that refuse v2. The UDC can only be bound after that, once `ep1` exists, so the init script waits for it.
- The legacy Android gadget: `/dev/android_adb` on old vendor kernels.

ewdbd keeps `ep0` open for its whole life, so the function stays bound across host disconnects. When a session ends and
the host does not start a new one, ewdbd toggles the UDC's `soft_connect` so the host sees a fresh device. Unplugging
and replugging, `adb reconnect` and `adb kill-server` all leave the daemon running. The vendor and product IDs, serial
number and strings shown to the host come from the gadget, not from ewdbd.

A minimal FunctionFS bring-up:

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

If ewdbd exits, restart it and bind the UDC again. dsOS runs it from a small loop that does both.

## Tested devices

- Anbernic RG DS Plus on dsOS: RK3568, Linux 7.2, DRM, two screens.
- Anbernic RG35XX SP on Base OS 1.4.0: Allwinner H700, Linux 4.9, fbdev.

Known issue: on macOS hosts, large pulls and big `exec-out` transfers occasionally fail because the host ends a USB read
early. adb reconnects within a few seconds. Pushes are unaffected.

## License

Apache-2.0. See [LICENSE](LICENSE) and [NOTICE](NOTICE).
