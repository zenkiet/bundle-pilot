package bundlefs

import (
	"io/fs"
	"syscall"
)

// ctime is when the file's inode last changed: unlike mtime, nothing can set it back.
func ctime(info fs.FileInfo) int64 {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return st.Ctimespec.Nano()
	}
	return 0
}
