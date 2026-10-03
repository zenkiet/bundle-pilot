package asset

import (
	"cmp"
	"compress/flate"
	"encoding/binary"
	"errors"
	"io"
)

// Span locates a zip entry's bytes: Raw of them at Off in the file, Size once decoded.
// Framable means the deflate stream fills Raw exactly, so it can go out as gzip unchanged.
type Span struct {
	Off, Raw, Size    int64
	CRC               uint32
	Deflate, Framable bool
}

// Open reads the entry's decoded bytes: a stored entry as is, a deflated one inflated on demand.
func (s Span) Open(ra io.ReaderAt) io.ReadSeekCloser {
	if !s.Deflate {
		return nopCloser{io.NewSectionReader(exact{ra}, s.Off, s.Raw)}
	}
	return &inflater{ra: ra, s: s}
}

// inflater keeps one stream going forward; seeking back restarts it, which only ranges and
// ServeContent's size probe ask for.
type inflater struct {
	ra      io.ReaderAt
	s       Span
	r       io.ReadCloser
	pos, at int64
}

func (z *inflater) Seek(off int64, whence int) (int64, error) {
	switch whence {
	case io.SeekCurrent:
		off += z.pos
	case io.SeekEnd:
		off += z.s.Size
	}
	if off < 0 {
		return 0, errors.New("asset: negative position")
	}
	z.pos = off
	return off, nil
}

func (z *inflater) Read(p []byte) (int, error) {
	if z.pos >= z.s.Size {
		return 0, io.EOF
	}
	if z.r == nil || z.pos < z.at {
		_ = z.Close()
		z.r, z.at = flate.NewReader(io.NewSectionReader(z.ra, z.s.Off, z.s.Raw)), 0
	}
	if z.at < z.pos {
		n, err := io.CopyN(io.Discard, z.r, z.pos-z.at)
		if z.at += n; err != nil {
			return 0, short(err)
		}
	}
	n, err := z.r.Read(p[:min(int64(len(p)), z.s.Size-z.pos)])
	z.pos += int64(n)
	z.at += int64(n)
	if err == io.EOF && z.pos == z.s.Size {
		err = nil
	}
	return n, short(err)
}

func (z *inflater) Close() error {
	if z.r == nil {
		return nil
	}
	err := z.r.Close()
	z.r = nil
	return err
}

const frameLen = 18 // gzip header and trailer

var gzipHeader = []byte{0x1f, 0x8b, 8, 0, 0, 0, 0, 0, 0, 0xff}

// frame is a deflate span between a gzip header and a CRC-32/size trailer: gzip, not recompressed.
func frame(ra io.ReaderAt, s Span) io.ReadSeekCloser {
	tail := binary.LittleEndian.AppendUint32(binary.LittleEndian.AppendUint32(nil, s.CRC), uint32(s.Size))
	return nopCloser{io.NewSectionReader(frameAt{exact{ra}, s, tail}, 0, s.Raw+frameLen)}
}

type frameAt struct {
	ra   io.ReaderAt
	s    Span
	tail []byte
}

// ReadAt never steps from a short read of the span into the trailer.
func (f frameAt) ReadAt(p []byte, off int64) (n int, err error) {
	for len(p) > 0 {
		var m int
		switch body := off - int64(len(gzipHeader)); {
		case body < 0:
			m = copy(p, gzipHeader[off:])
		case body < f.s.Raw:
			if m, err = f.ra.ReadAt(p[:min(int64(len(p)), f.s.Raw-body)], f.s.Off+body); err != nil {
				return n + m, err
			}
		case body-f.s.Raw < int64(len(f.tail)):
			m = copy(p, f.tail[body-f.s.Raw:])
		default:
			return n, io.EOF
		}
		n, off, p = n+m, off+int64(m), p[m:]
	}
	return n, nil
}

// exact fails a short read: a zip cut short in place says EOF where an entry has more.
type exact struct{ io.ReaderAt }

func (e exact) ReadAt(p []byte, off int64) (int, error) {
	n, err := e.ReaderAt.ReadAt(p, off)
	if n < len(p) {
		return n, short(cmp.Or(err, io.EOF))
	}
	return n, nil
}

// short turns an early end into io.ErrUnexpectedEOF, so a truncated zip never reads as complete.
func short(err error) error {
	if err == io.EOF {
		return io.ErrUnexpectedEOF
	}
	return err
}

type nopCloser struct{ io.ReadSeeker }

func (nopCloser) Close() error { return nil }
