# Adding `ewdbd` to a distribution

You ship one binary, `ewdbd`, and a config at `/etc/ewdbd.conf`. Your USB gadget script sets up the gadget and starts
the daemon.

Notes for specific distributions:

- [muOS](muos.md)
- [Base OS](base-os.md)

## Command line

`ewdbd` with no arguments, or only flags, runs the daemon:

- `-c CONFIG`, default `/etc/ewdbd.conf`
- `-launchers DIR`, default `/run/ewdbd/bin`

`ewdbd TOOL ARGS...` runs a bundled tool, for example `ewdbd screencap -p`. So does a link to `ewdbd` named `ewdb-TOOL`
or just `TOOL`. Any other name runs the daemon, so you can install it as `adbd` if your scripts expect that.

`ewdbd` logs to stderr.

## USB gadget

`ewdbd` needs one of:

- FunctionFS: a configfs gadget with an `ffs.adb` function mounted at `/dev/usb-ffs/adb`. `ewdbd` writes the descriptors
  to `ep0` itself. Bind the UDC after that, once `ep1` shows up.
- The old Android gadget: `/dev/android_adb` on vendor kernels.

Vendor and product IDs, serial and strings come from the gadget, not ewdbd. Minimal FunctionFS setup:

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

If `ewdbd` exits, restart it and bind the UDC again.

## Config

INI-style. If it's missing or doesn't parse, `ewdbd` logs a warning and runs on defaults.

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

Map the bundled tools under `[commands]`, or put links named after them on `PATH`. Otherwise `adb logcat` and friends
say "not available on this device".

Input device names under `[keys]` are what the kernel calls them. Check `/proc/bus/input/devices` on your hardware.

### [device]

| Key                                               | Default          | Meaning                                                                                                      |
|---------------------------------------------------|------------------|--------------------------------------------------------------------------------------------------------------|
| `name`                                            | `linux`          | `ro.product.name`                                                                                            |
| `model`                                           | `Linux handheld` | `ro.product.model`                                                                                           |
| `device`                                          | `handheld`       | `ro.product.device`                                                                                          |
| `name_command`, `model_command`, `device_command` |                  | Shell command whose first line replaces the value. Must exit 0 within 2 s with a non-empty line.             |
| `state`                                           | `/var/lib/ewdb`  | Writable directory. A valid `<state>/ewdbd.conf` replaces `/etc/ewdbd.conf`, for read-only root filesystems. |
| `usb`                                             | `auto`           | `ffs:DIR`, `android:PATH` or `auto`. `auto` tries `/dev/usb-ffs/adb`, then `/dev/android_adb`.               |
| `serial_file`                                     |                  | First line is `ro.serialno`.                                                                                 |
| `shell`                                           | `/bin/sh`        | Shell for `adb shell`, `exec:` and mapped actions.                                                           |

The `*_command` keys are for images that run on more than one device.

### [props]

Extra `getprop` entries. They override the built-in ones. Host tools ask for `ro.build.version.release`,
`ro.build.version.sdk` and `ro.product.manufacturer`, so set those.

### [commands]

Command name to shell text. The caller's arguments get appended.

These Android commands get a stub that prints `NAME: not available on this device` and exits 127 unless you map them or
they're on `PATH`: `am`, `pm`, `monkey`, `content`, `ime`, `uiautomator`, `run-as`, `cmd`, `settings`, `svc`, `wm`,
`dumpsys`, `setprop`, `screenrecord`, `logcat`, `getprop`, `input`, `screencap`, `bugreportz`.

Daemon actions use dotted names in the same section: `reboot`, `reboot.ARG` (`reboot.bootloader`, `reboot.recovery`,
...), `remount` and `root`. Output goes to the host. They're killed after 30 s.

Unmapped `reboot ARG` stops all processes, remounts disks read-only and asks the kernel to restart with ARG. Unmapped
`root` says it's already root when it is.

### [logcat]

`file`: the log to read. Default `/var/log/messages`.

### [bugreport]

`dir`: where the zip goes. Default `/tmp`. Every other key becomes `KEY.txt` in the zip, holding that command's output.
30 s per command, or `EWDB_BUGREPORT_TIMEOUT`.

### [input]

`touch`: the touch screen's input device name. `tap` and `swipe` use its raw coordinates.

`socket`: where `input --serve` listens. Default `/run/ewdb/input.sock`.

### [keys]

Android key name to device and Linux code: `BUTTON_A = retrogame_joypad BTN_SOUTH`. `DPAD_UP`, `KEYCODE_DPAD_UP` and
`19` all work. Supported: D-pad, `DPAD_CENTER`, A, B, X, Y, L1, R1, L2, R2, thumbs, start, select, mode, home, back,
menu, enter, volume, power.

For a hat D-pad give an axis and a value: `DPAD_UP = ANBERNIC-keys ABS_HAT0Y -1`. The press writes the value, waits,
and centres the axis. Out of range values and codes the device doesn't have are errors.

## Bundled tools

They work over SSH without the daemon. They read the config from `EWDB_CONFIG`, which the daemon sets, or
`/etc/ewdbd.conf`.

- `getprop [NAME [DEFAULT]]`: product properties, `ro.product.cpu.abi`, `ro.serialno` from `serial_file`, and
  `[props]`. Read-only.
- `logcat`: follows the `[logcat]` file. `-d`, `-c`, `-t N`, `-T N` and tag filters. Falls back to `/dev/kmsg` when the
  file doesn't exist, and won't clear it on `-c`.
- `bugreportz`: runs the `[bugreport]` commands into a zip and prints Android's `BEGIN:`, `PROGRESS:` and `OK:` lines.
- `input [SOURCE] tap X Y | swipe X1 Y1 X2 Y2 [MS] | keyevent KEY... | text STRING`: `tap`, `swipe` and `keyevent` write
  straight into the real `/dev/input/event*` device, so the frontend sees real button presses. `text` goes through
  `input --serve`, which owns a uinput keyboard. Start it at boot if you want `text`.
- `screencap [-p] [-d NAME|INDEX] [--screens name=CONNECTOR,...] [FILE]`: reads screens through DRM and stacks the lit
  ones top to bottom. Falls back to `/dev/fb0`. `--screens` names connectors in order, `-d` picks one. Raw RGBA unless
  `-p` or FILE ends in `.png`.
