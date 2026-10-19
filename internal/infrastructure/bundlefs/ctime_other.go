//go:build !linux && !darwin

package bundlefs

import "io/fs"

// ponytail: so on Windows a zip rewritten in place, its mtime put back, goes unnoticed until restart.
func ctime(fs.FileInfo) int64 { return 0 }
