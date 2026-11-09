# Base OS

Tested on the Anbernic RG35XX SP and RG SP with Base OS 1.4.0.

Base OS keeps its daemon at `/usr/sbin/adbd` and starts it from its own USB script. Put `ewdbd` in its place.

## Install by hand

Copy `ewdbd` and your `ewdbd.conf` over SSH, then on the device:

```sh
mv /usr/sbin/adbd /usr/sbin/adbd.stock
cp ewdbd /usr/sbin/ewdbd
ln -s /usr/sbin/ewdbd /usr/sbin/adbd
cp ewdbd.conf /etc/ewdbd.conf
reboot
```

To go back, move `adbd.stock` back and reboot.

## Config

The D-pad on these is a hat on `ANBERNIC-keys`:

```ini
[keys]
DPAD_UP = ANBERNIC-keys ABS_HAT0Y -1
DPAD_DOWN = ANBERNIC-keys ABS_HAT0Y 1
DPAD_LEFT = ANBERNIC-keys ABS_HAT0X -1
DPAD_RIGHT = ANBERNIC-keys ABS_HAT0X 1
```

The screen is fbdev, so `screencap` reads `/dev/fb0`.
