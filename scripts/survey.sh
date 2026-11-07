#!/bin/sh

DISRUPTIVE=0
for a in "$@"; do
	case "$a" in
	--disruptive) DISRUPTIVE=1 ;;
	*)
		echo "usage: SERIAL=<serial> $0 [--disruptive]" >&2
		exit 2
		;;
	esac
done

if [ -z "$SERIAL" ]; then
	DEVS=$(adb devices | awk 'NR > 1 && $2 == "device" { print $1 }')
	if [ -n "$DEVS" ] && [ "$(printf '%s\n' "$DEVS" | wc -l | tr -d ' ')" -eq 1 ]; then
		SERIAL=$DEVS
	else
		echo "usage: SERIAL=<serial> sh $0 [--disruptive]" >&2
		echo "exactly one attached device is needed when SERIAL is unset" >&2
		exit 2
	fi
fi

TMP=$(mktemp -d "${TMPDIR:-/tmp}/ewdb-survey.XXXXXX") || exit 2
RTAG=/tmp/ewdb-survey-$$
FAILS=0

cleanup() {
	if [ -f "$TMP/fwds" ]; then
		while read -r fp; do
			perl -e 'alarm shift; exec @ARGV' 15 adb -s "$SERIAL" forward --remove "tcp:$fp" >/dev/null 2>&1
		done <"$TMP/fwds"
	fi
	perl -e 'alarm shift; exec @ARGV' 15 adb -s "$SERIAL" shell "rm -rf $RTAG*" >/dev/null 2>&1
	rm -rf "$TMP"
}
trap cleanup EXIT
trap 'exit 130' INT TERM

A() {
	perl -e 'alarm shift; exec @ARGV' 15 adb -s "$SERIAL" "$@"
}

row() {
	name=$1
	shift
	reason=$("$@" 2>&1)
	rc=$?
	case $rc in
	0) printf '%-28s PASS\n' "$name" ;;
	2) printf '%-28s SKIP\n' "$name" ;;
	*)
		FAILS=$((FAILS + 1))
		printf '%-28s FAIL %s\n' "$name" "$(printf '%s\n' "$reason" | head -n 1)"
		;;
	esac
}

fail() {
	printf '%s\n' "$*"
	exit 1
}

t_devices() {
	out=$(A devices -l 2>&1) || fail "exit $?: $out"
	printf '%s\n' "$out" | grep -qF "$SERIAL" || fail "serial not listed: $out"
}

t_get_state() {
	out=$(A get-state 2>&1) || fail "exit $?: $out"
	[ "$out" = device ] || fail "state $out"
}

t_get_serialno() {
	out=$(A get-serialno 2>&1) || fail "exit $?: $out"
	[ -n "$out" ] || fail "empty"
}

t_get_devpath() {
	out=$(A get-devpath 2>&1) || fail "exit $?: $out"
	[ -n "$out" ] || fail "empty"
}

t_shell_true() {
	out=$(A shell true 2>&1) || fail "exit $?: $out"
}

t_shell_exit() {
	A shell 'exit 7' >/dev/null 2>&1
	rc=$?
	[ $rc -eq 7 ] || fail "exit code $rc, want 7"
}

t_shell_streams() {
	A shell 'echo out; echo err >&2' >"$TMP/so" 2>"$TMP/se"
	[ "$(cat "$TMP/so")" = out ] || fail "stdout $(cat "$TMP/so")"
	[ "$(cat "$TMP/se")" = err ] || fail "stderr $(cat "$TMP/se")"
}

t_shell_raw() {
	out=$(A shell -x 'echo raw' 2>&1) || fail "exit $?: $out"
	[ "$out" = raw ] || fail "got $out"
}

t_shell_T() {
	out=$(A shell -T 'echo notty' 2>&1) || fail "exit $?: $out"
	[ "$out" = notty ] || fail "got $out"
}

t_exec_out() {
	A exec-out 'printf "a\nb\n"' >"$TMP/eo" 2>"$TMP/ee" || fail "exit $?: $(cat "$TMP/ee")"
	[ "$(od -An -c "$TMP/eo" | tr -d ' ')" = 'a\nb\n' ] || fail "bytes $(od -An -c "$TMP/eo")"
}

t_push_pull() {
	dd if=/dev/urandom of="$TMP/blob" bs=1024 count=300 2>/dev/null
	want=$(shasum -a 256 "$TMP/blob" | awk '{print $1}')
	out=$(A push "$TMP/blob" "$RTAG-blob" 2>&1) || fail "push exit $?: $out"
	out=$(A pull "$RTAG-blob" "$TMP/back" 2>&1) || fail "pull exit $?: $out"
	got=$(shasum -a 256 "$TMP/back" | awk '{print $1}')
	[ -n "$want" ] || fail "no local hash"
	[ "$want" = "$got" ] || fail "hash mismatch after pull-back"
	rhash=$(A shell "sha256sum $RTAG-blob" 2>/dev/null | awk '{print $1}')
	if [ -n "$rhash" ] && [ "$rhash" != "$want" ]; then
		fail "device sha256sum $rhash, want $want"
	fi
}

t_push_dir() {
	mkdir -p "$TMP/d/sub"
	echo one >"$TMP/d/one"
	echo two >"$TMP/d/sub/two"
	out=$(A push "$TMP/d" "$RTAG-dir" 2>&1) || fail "push exit $?: $out"
	mkdir -p "$TMP/dback"
	out=$(A pull "$RTAG-dir" "$TMP/dback" 2>&1) || fail "pull exit $?: $out"
	diff -r "$TMP/d" "$TMP/dback/$(basename "$RTAG-dir")" >/dev/null 2>&1 ||
		diff -r "$TMP/d" "$TMP/dback" >/dev/null 2>&1 ||
		fail "tree differs after pull-back"
}

t_pull_missing() {
	A pull "$RTAG-nonexistent" "$TMP/none" >/dev/null 2>&1
	[ $? -ne 0 ] || fail "pull of a missing file succeeded"
}

add_forward() {
	port=$(A forward tcp:0 tcp:22 2>&1) || fail "forward exit $?: $port"
	case "$port" in
	'' | *[!0-9]*) fail "forward printed $port" ;;
	esac
	echo "$port" >>"$TMP/fwds"
}

t_forward() {
	add_forward
	banner=$(perl -e 'alarm shift; exec @ARGV' 8 nc -w 3 127.0.0.1 "$port" </dev/null 2>/dev/null | head -n 1)
	A forward --remove "tcp:$port" >/dev/null 2>&1 || fail "forward --remove failed"
	case "$banner" in
	SSH-*) return 0 ;;
	*) fail "no SSH banner on device 127.0.0.1:22 (got '$banner')" ;;
	esac
}

t_forward_list() {
	add_forward
	out=$(A forward --list 2>&1) || fail "list exit $?: $out"
	printf '%s\n' "$out" | grep -qF "tcp:$port" || fail "forward not listed: $out"
	A forward --remove "tcp:$port" >/dev/null 2>&1 || fail "remove failed"
	out=$(A forward --list 2>&1)
	if printf '%s\n' "$out" | grep -qF "tcp:$port"; then
		fail "forward still listed after remove"
	fi
}

t_reverse_list() {
	out=$(A reverse --list 2>&1) || fail "exit $?: $out"
}

t_getprop() {
	out=$(A shell getprop ro.product.model 2>&1) || fail "exit $?: $out"
	[ -n "$out" ] || fail "empty model"
}

t_logcat() {
	A logcat -d >"$TMP/log" 2>"$TMP/loge" || fail "exit $?: $(cat "$TMP/loge")"
	[ -s "$TMP/log" ] || fail "logcat -d printed nothing"
}

t_bugreport() {
	mkdir -p "$TMP/br"
	out=$(perl -e 'alarm shift; exec @ARGV' 60 adb -s "$SERIAL" bugreport "$TMP/br" 2>&1) || fail "exit $?: $out"
	set -- "$TMP"/br/*.zip
	[ -f "$1" ] || fail "no zip in output dir: $out"
	[ -s "$1" ] || fail "zip is empty"
	[ "$(dd if="$1" bs=2 count=1 2>/dev/null)" = PK ] || fail "not a zip"
}

t_screencap() {
	A exec-out screencap -p >"$TMP/shot.png" 2>"$TMP/shote" || fail "exit $?: $(cat "$TMP/shote")"
	sig=$(dd if="$TMP/shot.png" bs=8 count=1 2>/dev/null | xxd -p)
	[ "$sig" = 89504e470d0a1a0a ] || fail "not a PNG (first bytes $sig)"
}

t_input() {
	out=$(A shell input keyevent DPAD_DOWN 2>&1) || fail "exit $?: $out"
}

t_tcpip() {
	out=$(A tcpip 5555 2>&1)
	case "$out" in
	*"Wi-Fi adb is not available on this device"*) ;;
	*) fail "unexpected message: $out" ;;
	esac
	A shell true >/dev/null 2>&1 || fail "shell failed after tcpip"
}

t_usb() {
	out=$(A usb 2>&1)
	case "$out" in
	*"already running on USB"*) ;;
	*) fail "unexpected message: $out" ;;
	esac
	A shell true >/dev/null 2>&1 || fail "shell failed after usb"
}

t_remount() {
	out=$(A remount 2>&1) || fail "exit $?: $out"
}

t_verity() {
	out=$(A disable-verity 2>&1) || fail "exit $?: $out"
}

t_jdwp() {
	A jdwp >/dev/null 2>&1
	rc=$?
	[ $rc -ne 142 ] || fail "jdwp hung until the alarm"
}

wait_back() {
	end=$(($(date +%s) + 90))
	st=none
	sleep 3
	while [ "$(date +%s)" -lt "$end" ]; do
		left=$((end - $(date +%s)))
		[ "$left" -ge 1 ] || left=1
		perl -e 'alarm shift; exec @ARGV' "$left" adb -s "$SERIAL" wait-for-device >/dev/null 2>&1
		st=$(A get-state 2>&1)
		case "$st" in
		device | recovery) return 0 ;;
		esac
		sleep 1
	done
	fail "device did not return within 90 s (last state: $st)"
}

t_reboot() {
	A reboot >/dev/null 2>&1
	wait_back
}

t_reboot_bootloader() {
	A reboot bootloader >/dev/null 2>&1
	wait_back
}

t_reboot_recovery() {
	A reboot recovery >/dev/null 2>&1
	wait_back
}

t_skip() {
	return 2
}

printf 'survey of %s\n' "$SERIAL"

row "devices -l" t_devices
row "get-state" t_get_state
row "get-serialno" t_get_serialno
row "get-devpath" t_get_devpath
row "shell true" t_shell_true
row "shell exit code" t_shell_exit
row "shell stdout/stderr split" t_shell_streams
row "shell -x" t_shell_raw
row "shell -T" t_shell_T
row "exec-out" t_exec_out
row "push/pull file" t_push_pull
row "push/pull directory" t_push_dir
row "pull missing file" t_pull_missing
row "forward tcp:0 tcp:22 (ssh)" t_forward
row "forward --list/--remove" t_forward_list
row "reverse --list" t_reverse_list
row "getprop ro.product.model" t_getprop
row "logcat -d" t_logcat
row "bugreport" t_bugreport
row "screencap -p" t_screencap
row "input keyevent DPAD_DOWN" t_input
row "tcpip 5555" t_tcpip
row "usb" t_usb
row "jdwp" t_jdwp

if [ "$DISRUPTIVE" -eq 1 ]; then
	row "remount" t_remount
	row "disable-verity" t_verity
	row "reboot" t_reboot
	row "reboot bootloader" t_reboot_bootloader
	row "reboot recovery" t_reboot_recovery
else
	row "remount" t_skip
	row "disable-verity" t_skip
	row "reboot" t_skip
	row "reboot bootloader" t_skip
	row "reboot recovery" t_skip
fi

[ "$FAILS" -eq 0 ]
