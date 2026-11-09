package services

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"

	"ewdb/internal/session"
)

const (
	syncDataMax = 64 * 1024
	syncPathMax = 1024
)

func syncID(s string) uint32 {
	return uint32(s[0]) | uint32(s[1])<<8 | uint32(s[2])<<16 | uint32(s[3])<<24
}

var (
	idLstatV1 = syncID("STAT")
	idStatV2  = syncID("STA2")
	idLstatV2 = syncID("LST2")
	idListV1  = syncID("LIST")
	idListV2  = syncID("LIS2")
	idDentV1  = syncID("DENT")
	idDentV2  = syncID("DNT2")
	idSendV1  = syncID("SEND")
	idRecvV1  = syncID("RECV")
	idDone    = syncID("DONE")
	idData    = syncID("DATA")
	idOkay    = syncID("OKAY")
	idFail    = syncID("FAIL")
	idQuit    = syncID("QUIT")
)

func syncHandler() session.Handler {
	return func(ctx context.Context, s *session.Stream, arg string) {
		serveSync(ctx, s)
	}
}

type statInfo struct {
	dev, ino            uint64
	mode, nlink         uint32
	uid, gid            uint32
	size                uint64
	atime, mtime, ctime int64
}

func infoFrom(st *syscall.Stat_t) statInfo {
	a, m, c := statTimes(st)
	return statInfo{
		dev:   uint64(st.Dev),
		ino:   uint64(st.Ino),
		mode:  uint32(st.Mode),
		nlink: uint32(st.Nlink),
		uid:   st.Uid,
		gid:   st.Gid,
		size:  uint64(st.Size),
		atime: a,
		mtime: m,
		ctime: c,
	}
}

func wireErrno(err error) uint32 {
	var en syscall.Errno
	if errors.As(err, &en) {
		return uint32(en)
	}
	return uint32(syscall.EIO)
}

func strerror(err error) string {
	var en syscall.Errno
	if errors.As(err, &en) {
		err = en
	}
	s := err.Error()
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func appendU32(b []byte, v uint32) []byte { return binary.LittleEndian.AppendUint32(b, v) }
func appendU64(b []byte, v uint64) []byte { return binary.LittleEndian.AppendUint64(b, v) }

func appendStat2(b []byte, id uint32, errno uint32, si statInfo) []byte {
	b = appendU32(b, id)
	b = appendU32(b, errno)
	b = appendU64(b, si.dev)
	b = appendU64(b, si.ino)
	b = appendU32(b, si.mode)
	b = appendU32(b, si.nlink)
	b = appendU32(b, si.uid)
	b = appendU32(b, si.gid)
	b = appendU64(b, si.size)
	b = appendU64(b, uint64(si.atime))
	b = appendU64(b, uint64(si.mtime))
	b = appendU64(b, uint64(si.ctime))
	return b
}

type syncConn struct {
	ctx context.Context
	br  *bufio.Reader
	bw  *bufio.Writer
	buf []byte
	eof bool
}

func serveSync(ctx context.Context, rw io.ReadWriteCloser) {
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		select {
		case <-ctx.Done():
			rw.Close()
		case <-stop:
		}
	}()
	c := &syncConn{
		ctx: ctx,
		br:  bufio.NewReaderSize(rw, 2*syncDataMax),
		bw:  bufio.NewWriterSize(rw, 2*syncDataMax),
		buf: make([]byte, 8+syncDataMax),
	}
	defer c.bw.Flush()
	for c.ctx.Err() == nil {
		if !c.request() {
			return
		}
		if c.bw.Flush() != nil {
			return
		}
	}
}

func (c *syncConn) readU32Pair() (uint32, uint32, bool) {
	var h [8]byte
	if _, err := io.ReadFull(c.br, h[:]); err != nil {
		c.eof = err == io.EOF
		return 0, 0, false
	}
	return binary.LittleEndian.Uint32(h[0:4]), binary.LittleEndian.Uint32(h[4:8]), true
}

func (c *syncConn) fail(msg string) {
	var h [8]byte
	binary.LittleEndian.PutUint32(h[0:4], idFail)
	binary.LittleEndian.PutUint32(h[4:8], uint32(len(msg)))
	c.bw.Write(h[:])
	c.bw.WriteString(msg)
	c.bw.Flush()
}

func (c *syncConn) failErr(prefix string, err error) {
	c.fail(prefix + ": " + strerror(err))
}

func (c *syncConn) status(id, v uint32) {
	var h [8]byte
	binary.LittleEndian.PutUint32(h[0:4], id)
	binary.LittleEndian.PutUint32(h[4:8], v)
	c.bw.Write(h[:])
}

func (c *syncConn) request() bool {
	id, n, ok := c.readU32Pair()
	if !ok {
		if c.ctx.Err() == nil && !c.eof {
			c.fail("command read failure")
		}
		return false
	}
	if n > syncPathMax {
		c.fail("path too long")
		return false
	}
	nameBuf := make([]byte, n)
	if _, err := io.ReadFull(c.br, nameBuf); err != nil {
		if c.ctx.Err() == nil {
			c.fail("filename read failure")
		}
		return false
	}
	path := string(nameBuf)
	if i := strings.IndexByte(path, 0); i >= 0 {
		path = path[:i]
	}
	switch id {
	case idLstatV1:
		return c.statV1(path)
	case idLstatV2, idStatV2:
		return c.statV2(id, path)
	case idListV1:
		return c.list(path, false)
	case idListV2:
		return c.list(path, true)
	case idSendV1:
		return c.send(path)
	case idRecvV1:
		return c.recv(path)
	case idQuit:
		return false
	default:
		c.fail(fmt.Sprintf("unknown command %08x", id))
		return false
	}
}

func (c *syncConn) statV1(path string) bool {
	var st syscall.Stat_t
	var si statInfo
	if syscall.Lstat(path, &st) == nil {
		si = infoFrom(&st)
	}
	var b []byte
	b = appendU32(b, idLstatV1)
	b = appendU32(b, si.mode)
	b = appendU32(b, uint32(si.size))
	b = appendU32(b, uint32(si.mtime))
	_, err := c.bw.Write(b)
	return err == nil
}

func (c *syncConn) statV2(id uint32, path string) bool {
	var st syscall.Stat_t
	var err error
	if id == idStatV2 {
		err = syscall.Stat(path, &st)
	} else {
		err = syscall.Lstat(path, &st)
	}
	var si statInfo
	var errno uint32
	if err != nil {
		errno = wireErrno(err)
	} else {
		si = infoFrom(&st)
	}
	_, werr := c.bw.Write(appendStat2(nil, id, errno, si))
	return werr == nil
}

func (c *syncConn) list(path string, v2 bool) bool {
	names := []string{".", ".."}
	if d, err := os.Open(path); err == nil {
		rest, _ := d.Readdirnames(-1)
		d.Close()
		names = append(names, rest...)
	} else {
		names = nil
	}
	for _, name := range names {
		if c.ctx.Err() != nil {
			return false
		}
		var st syscall.Stat_t
		lerr := syscall.Lstat(path+"/"+name, &st)
		var b []byte
		if v2 {
			var si statInfo
			var errno uint32
			if lerr != nil {
				errno = wireErrno(lerr)
			} else {
				si = infoFrom(&st)
			}
			b = appendStat2(b, idDentV2, errno, si)
		} else {
			if lerr != nil {
				continue
			}
			si := infoFrom(&st)
			b = appendU32(b, idDentV1)
			b = appendU32(b, si.mode)
			b = appendU32(b, uint32(si.size))
			b = appendU32(b, uint32(si.mtime))
		}
		b = appendU32(b, uint32(len(name)))
		b = append(b, name...)
		if _, err := c.bw.Write(b); err != nil {
			return false
		}
	}
	var done []byte
	done = appendU32(done, idDone)
	size := 20
	if v2 {
		size = 76
	}
	done = append(done, make([]byte, size-4)...)
	_, err := c.bw.Write(done)
	return err == nil
}

func (c *syncConn) drain() {
	buf := c.buf
	for {
		id, n, ok := c.readU32Pair()
		if !ok || id != idData || n > syncDataMax {
			return
		}
		if _, err := io.ReadFull(c.br, buf[:n]); err != nil {
			return
		}
	}
}

func mkdirs(dir string) error {
	return os.MkdirAll(dir, 0o755)
}

func setMtime(path string, sec uint32) {
	t := unix.NsecToTimespec(int64(sec) * 1e9)
	ts := []unix.Timespec{t, t}
	unix.UtimesNanoAt(unix.AT_FDCWD, path, ts, unix.AT_SYMLINK_NOFOLLOW)
}

func (c *syncConn) send(spec string) bool {
	comma := strings.LastIndexByte(spec, ',')
	if comma < 0 {
		c.fail("missing , in ID_SEND_V1")
		return false
	}
	path := spec[:comma]
	m, err := strconv.ParseUint(spec[comma+1:], 0, 32)
	if err != nil {
		c.fail("bad mode")
		return false
	}
	mode := uint32(m)

	var st syscall.Stat_t
	doUnlink := syscall.Lstat(path, &st) != nil ||
		st.Mode&syscall.S_IFMT == syscall.S_IFREG ||
		(st.Mode&syscall.S_IFMT == syscall.S_IFLNK && mode&syscall.S_IFMT != syscall.S_IFLNK)
	if doUnlink {
		syscall.Unlink(path)
	}

	var ts uint32
	var ok bool
	if mode&syscall.S_IFMT == syscall.S_IFLNK {
		ts, ok = c.sendLink(path)
	} else {
		mode &= 0o777
		ts, ok = c.sendFile(path, os.FileMode(mode), doUnlink)
	}
	if !ok {
		return false
	}
	setMtime(path, ts)
	return true
}

func (c *syncConn) sendFile(path string, mode os.FileMode, doUnlink bool) (uint32, bool) {
	flags := os.O_WRONLY | os.O_CREATE | os.O_EXCL
	f, err := os.OpenFile(path, flags, mode)
	if err != nil && errors.Is(err, syscall.ENOENT) {
		if merr := mkdirs(filepath.Dir(path)); merr != nil {
			c.failErr("secure_mkdirs() failed", merr)
			return 0, c.abortSend(path, doUnlink)
		}
		f, err = os.OpenFile(path, flags, mode)
	}
	if err != nil && errors.Is(err, syscall.EEXIST) {
		f, err = os.OpenFile(path, os.O_WRONLY, mode)
	}
	if err != nil {
		c.failErr("couldn't create file", err)
		return 0, c.abortSend(path, doUnlink)
	}
	f.Chmod(mode)

	for {
		id, n, ok := c.readU32Pair()
		if !ok {
			f.Close()
			return 0, c.abortSend(path, doUnlink)
		}
		if id == idDone {
			if err := f.Close(); err != nil {
				c.failErr("write failed", err)
				return 0, c.abortSend(path, doUnlink)
			}
			c.status(idOkay, 0)
			return n, true
		}
		if id != idData {
			c.fail("invalid data message")
			f.Close()
			return 0, c.abortSend(path, doUnlink)
		}
		if n > syncDataMax {
			c.fail("oversize data message")
			f.Close()
			return 0, c.abortSend(path, doUnlink)
		}
		_, err := io.ReadFull(c.br, c.buf[:n])
		if err == nil {
			_, err = f.Write(c.buf[:n])
		}
		if err != nil {
			if c.ctx.Err() == nil && !isReadSide(err) {
				c.failErr("write failed", err)
				f.Close()
				c.drain()
			} else {
				f.Close()
			}
			return 0, c.abortSend(path, doUnlink)
		}
	}
}

func isReadSide(err error) bool {
	return errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.ErrClosedPipe)
}

func (c *syncConn) abortSend(path string, doUnlink bool) bool {
	if doUnlink {
		syscall.Unlink(path)
	}
	return false
}

func (c *syncConn) sendLink(path string) (uint32, bool) {
	id, n, ok := c.readU32Pair()
	if !ok {
		return 0, false
	}
	if id != idData {
		c.fail("invalid data message: expected ID_DATA")
		return 0, false
	}
	if n > syncDataMax {
		c.fail("oversize data message")
		return 0, false
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(c.br, buf); err != nil {
		return 0, false
	}
	if i := strings.IndexByte(string(buf), 0); i >= 0 {
		buf = buf[:i]
	}
	target := string(buf)

	if cur, err := os.Readlink(path); err != nil || cur != target {
		syscall.Unlink(path)
		err := syscall.Symlink(target, path)
		if errors.Is(err, syscall.ENOENT) {
			if merr := mkdirs(filepath.Dir(path)); merr != nil {
				c.failErr("secure_mkdirs failed", merr)
				return 0, false
			}
			err = syscall.Symlink(target, path)
		}
		if err != nil {
			c.failErr("symlink failed", err)
			return 0, false
		}
	}

	id, n, ok = c.readU32Pair()
	if !ok {
		return 0, false
	}
	if id != idDone {
		c.fail("invalid data message: expected ID_DONE")
		return 0, false
	}
	c.status(idOkay, 0)
	return n, true
}

func (c *syncConn) recv(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		c.failErr("open failed", err)
		return false
	}
	defer f.Close()
	buf := c.buf
	binary.LittleEndian.PutUint32(buf[0:4], idData)
	for {
		if c.ctx.Err() != nil {
			return false
		}
		n, err := f.Read(buf[8:])
		if n > 0 {
			binary.LittleEndian.PutUint32(buf[4:8], uint32(n))
			if _, werr := c.bw.Write(buf[:8+n]); werr != nil {
				return false
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			c.failErr("read failed", err)
			return false
		}
	}
	c.status(idDone, 0)
	return true
}
