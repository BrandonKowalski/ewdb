package services

import "syscall"

func statTimes(st *syscall.Stat_t) (atime, mtime, ctime int64) {
	return int64(st.Atim.Sec), int64(st.Mtim.Sec), int64(st.Ctim.Sec)
}
