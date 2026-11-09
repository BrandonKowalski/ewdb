# e-waste debug bridge

`ewdbd` is a drop-in replacement for `adbd` on Linux-based retro handhelds.

It works with the stock `adb` from Android's `platform-tools`.

## Why

The `adbd` that handheld distributions ship was built for Android. It expects `getprop`, `logcat`, `screencap`, `input`
and Android paths. When they aren't there, commands hang, exit codes get lost, or the device drops off USB.

`ewdbd` speaks adb protocol version `0x01000001` (current), and maps Android commands to Linux equivalents.

## Compared to stock `adbd`

RG SP (H700, Base OS 1.4.0), USB 2.0, macOS host, 64 MB file, average of 3 runs.

| Test            | `ewdbd`   | `adbd`   |
|-----------------|-----------|----------|
| shell latency   | 22.1 ms   | 22.5 ms  |
| exit codes      | yes       | no       |
| push to /tmp    | 10.7 MB/s | 5.1 MB/s |
| push to SD      | 10.4 MB/s | 4.5 MB/s |
| pull from /tmp  | 13.5 MB/s | 6.1 MB/s |
| pull from SD    | 13.6 MB/s | 6.0 MB/s |
| exec-out        | 11.6 MB/s | broken   |
| shell stdout    | 11.1 MB/s | 5.6 MB/s |
| push 500 x 4 KB | 0.40 s    | 0.35 s   |
| pull 500 x 4 KB | 0.50 s    | 0.55 s   |
| RAM idle        | 3.8 MB    | 0.5 MB   |
| RAM peak        | 8.2 MB    | 0.5 MB   |

## What works

| Host command                                                                 | On `ewdbd`                                                                                                             |
|------------------------------------------------------------------------------|------------------------------------------------------------------------------------------------------------------------|
| `devices -l`, `get-state`, `get-serialno`, `get-devpath`                     | Works. Name, model and device come from the config.                                                                    |
| `shell`                                                                      | Works. Separate stdout and stderr, real exit codes. pty with `-t` or an interactive shell, none with `-T`. `-x` works. |
| `exec-out`, `exec-in`                                                        | Works.                                                                                                                 |
| `push`, `pull`, `ls`                                                         | Works, including directories and `--sync`. No `-z`; the host falls back to uncompressed.                               |
| `forward`                                                                    | `tcp:`, `localabstract:`, `localfilesystem:`, `dev:`. `--list`, `--remove`, `--remove-all`, `--no-rebind`.             |
| `reverse`                                                                    | `tcp:` (127.0.0.1 on the device), `localabstract:`, `localfilesystem:`.                                                |
| `shell getprop`, `logcat`                                                    | Works.                                                                                                                 |
| `bugreport`                                                                  | Works. You get a zip.                                                                                                  |
| `exec-out screencap -p`                                                      | Works, DRM or fbdev. `framebuffer:` works too.                                                                         |
| `shell input tap`, `swipe`, `keyevent`, `text`                               | Works. `text` needs `input --serve` running.                                                                           |
| `reboot`, `reboot ARG`                                                       | Runs what the distribution mapped, or reboots.                                                                         |
| `reconnect`                                                                  | Works.                                                                                                                 |
| `root`, `remount`                                                            | Runs what the distribution mapped, or says it's not available.                                                         |
| `unroot`, `disable-verity`, `enable-verity`, `sideload`, `backup`, `restore` | Not available. Says so.                                                                                                |
| `install`, `uninstall`                                                       | Not available. There's no package manager.                                                                             |
| `jdwp`                                                                       | Empty list.                                                                                                            |
| `tcpip`, `connect`                                                           | No Wi-Fi adb, on purpose. `ewdbd` only serves USB. `tcpip` says so and the USB session keeps going.                    |

## Tested devices

- Anbernic RG DS Plus on dsOS
- Anbernic RG35XX SP and RG SP on Base OS 1.4.0
- TrimUI Smart Pro on muOS 2508.4

## Building

`ewdbd` does not use cgo. Go 1.26 required.

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o ewdbd ./cmd/ewdbd
CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -o ewdbd ./cmd/ewdbd
```

arm64 or 32-bit armv7. Runs on Linux 3.2 or newer, any libc.

## Installing

To add `ewdbd` to a distribution, or swap it in on one by hand, see [docs/distributions](docs/distributions/README.md).

## License

Apache-2.0. See [LICENSE](LICENSE) and [NOTICE](NOTICE).
