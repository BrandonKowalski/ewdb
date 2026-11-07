package services

import "syscall"

func statTimes(st *syscall.Stat_t) (atime, mtime, ctime int64) {
	return int64(st.Atimespec.Sec), int64(st.Mtimespec.Sec), int64(st.Ctimespec.Sec)
}
