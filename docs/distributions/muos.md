# muOS

Tested on a TrimUI Smart Pro with muOS 2508.4.

muOS starts `/usr/bin/adbd` from `/opt/muos/script/system/usb_gadget.sh`. That script also watches `pidof adbd` and
restarts the daemon and rebinds the UDC when it's gone. So `ewdbd` has to run under the name `adbd`.

## Install by hand

Copy `ewdbd` (arm64) and your `ewdbd.conf` over, then on the device:

```sh
mv /usr/bin/adbd /usr/bin/adbd.stock
cp ewdbd /usr/bin/ewdbd
ln -s /usr/bin/ewdbd /usr/bin/adbd
cp ewdbd.conf /etc/ewdbd.conf
killall adbd
```

The watchdog starts `ewdbd` within a few seconds.

To go back, put `adbd.stock` back in place and kill `adbd` again.

## Logs

The watchdog starts `adbd` with no output. To see `ewdbd`'s log:

```sh
/opt/muos/script/system/usb_gadget.sh stop
killall adbd
/usr/bin/adbd > /tmp/ewdbd.log 2>&1 &
```

Bind the UDC by hand, then `usb_gadget.sh start` when you're done. `stop` only pauses the watchdog.

## Known issue

The first switch from the stock `adbd` to `ewdbd` can leave the host silent until you rebind the UDC. After that,
restarts reconnect in about 3 s.
