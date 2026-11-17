package bundlefs

import (
	"archive/zip"
	"bufio"
	"bytes"
	"compress/flate"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"io/fs"
	"os"
	"path"
	"runtime"
	"slices"
	"strings"
	"sync"

	"golang.org/x/net/html"

	"github.com/zenkiet/bundle-pilot/internal/domain"
	"github.com/zenkiet/bundle-pilot/internal/pkg/asset"
)

const (
	manifestFile  = "/bundle.sha256"
	signatureFile = "/bundle.sha256.sig"
	maxKept       = 8 << 20
	maxFiles      = 1 << 16
)

var (
	// kept files are read into memory; the rest is served from the zip.
	kept = map[string]bool{
		"/index.html": true, "/ngsw.json": true, manifestFile: true, signatureFile: true,
	}
	// textual files deserve a .br sidecar: without one they go out as gzip at best.
	textual = map[string]bool{
		".js": true, ".mjs": true, ".css": true, ".html": true, ".json": true, ".svg": true, ".txt": true,
	}
	errTooMany = refuse("too_many_files", fmt.Errorf("more than %d files", maxFiles))
)

// refuse keeps a reason err already carries.
func refuse(reason string, err error) error {
	if domain.RefusalReason(err) != "" {
		return err
	}
	return &domain.RefusedError{Reason: reason, Err: err}
}

type zipped struct {
	bundle *domain.Bundle
	f      *os.File
	fp     [2]int64
	issues []domain.Issue
}

// loadBundles keeps each loaded zip open, owned by its bundle, until its last snapshot is
// garbage collected.
func loadBundles(root *os.Root, pub ed25519.PublicKey, zips []zipFile) ([]zipped, []error) {
	out, errs := make([]zipped, len(zips)), make([]error, len(zips))
	slots := make(chan struct{}, runtime.GOMAXPROCS(0))
	var wg sync.WaitGroup
	for i, file := range zips {
		wg.Go(func() {
			slots <- struct{}{}
			defer func() { <-slots }()
			f, err := root.Open(path.Join(versionsDir, file.name+".zip"))
			if err != nil {
				errs[i] = err
				return
			}
			out[i], errs[i] = loadZip(f, pub, file.name)
			if errs[i] == nil {
				out[i].bundle.SHA256, errs[i] = digest(f, out[i].bundle.ZipBytes)
			}
			if errs[i] != nil {
				_ = f.Close()
			}
		})
	}
	wg.Wait()
	return out, errs
}

// digest runs once per changed zip, not on every reload.
func digest(f *os.File, size int64) (string, error) {
	h := sha256.New()
	if _, err := io.Copy(h, io.NewSectionReader(f, 0, size)); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// loadZip checks every entry in one pass, which also takes the SHA-256 the ETag and the
// signature need.
func loadZip(f *os.File, pub ed25519.PublicKey, name string) (zipped, error) {
	info, err := f.Stat()
	if err != nil {
		return zipped{}, err
	}
	// archive/zip keeps about 200 bytes for each entry it lists, before maxFiles can be checked,
	// so it reads at most 16 MiB: a 256 MiB zip of empty entries would otherwise take 600 MiB.
	zr, err := zip.NewReader(&capped{ReaderAt: f, left: maxFiles * 256}, info.Size())
	if err != nil {
		return zipped{}, refuse("damaged", err) // not a zip, or cut short
	}
	if len(zr.File) > maxFiles {
		return zipped{}, errTooMany
	}
	files, err := scan(f, zr, info.Size())
	if err != nil {
		return zipped{}, refuse("damaged", err)
	}
	index := files["/index.html"]
	if index == nil {
		return zipped{}, domain.ErrNoIndex
	}
	if pub != nil {
		if err := verify(pub, files); err != nil {
			return zipped{}, refuse("signature", err)
		}
	}
	z := zipped{f: f, fp: fingerprint(info)}
	assets := make(map[string]*asset.Asset, len(files))
	bare := 0
	for rel, s := range files {
		if base, ok := strings.CutSuffix(rel, ".br"); ok && files[base] != nil {
			continue // a sidecar: the br encoding of base
		}
		var br *asset.Span
		if side := files[rel+".br"]; side != nil {
			br = &side.Span
		}
		if br == nil && textual[path.Ext(rel)] && s.Size > 1<<10 && rel != "/index.html" {
			bare++
		}
		assets[rel] = asset.FromZip(rel, f, s.Span, s.sum, s.head, br)
	}
	if bare > 0 {
		z.issues = append(z.issues, domain.Issue{
			Code:   "no_brotli",
			Text:   fmt.Sprintf("bundle %s: %d text files have no .br, so they go out as gzip: precompress them in CI", name, bare),
			Bundle: name,
		})
	}
	if ngsw := files["/ngsw.json"]; ngsw != nil {
		assets["/ngsw.json"] = asset.New("/ngsw.json", ngsw.body, gzipped(ngsw.body))
	}
	base, link, meta, head := indexMeta(index.body)
	if head == 0 {
		z.issues = append(z.issues, domain.Issue{
			Code:   "no_head",
			Text:   fmt.Sprintf("bundle %s: index.html has no <head>, so it carries no version and the SDK never switches it", name),
			Bundle: name,
		})
	}
	z.bundle, err = domain.NewBundle(name, base, meta, assets, stamp(index.body, name, head))
	if err != nil {
		return zipped{}, err
	}
	z.bundle.Zip, z.bundle.ZipBytes, z.bundle.ModTime, z.bundle.Link = f, info.Size(), info.ModTime(), link
	return z, nil
}

type capped struct {
	io.ReaderAt
	left int64
}

func (c *capped) ReadAt(p []byte, off int64) (int, error) {
	c.left -= int64(len(p))
	if c.left < 0 {
		return 0, errTooMany
	}
	return c.ReaderAt.ReadAt(p, off)
}

type scanned struct {
	asset.Span
	sum  [32]byte
	head []byte
	body []byte
}

// scan skips directories and dot files, except under /.well-known/ (app links). Errors without
// a reason mean a damaged zip.
func scan(f *os.File, zr *zip.Reader, size int64) (map[string]*scanned, error) {
	files := make(map[string]*scanned, len(zr.File))
	var total uint64
	for _, zf := range zr.File {
		rel := path.Clean("/" + zf.Name)
		if strings.HasSuffix(zf.Name, "/") || strings.Contains("/"+strings.TrimPrefix(rel, "/.well-known/"), "/.") {
			continue
		}
		off, err := zf.DataOffset()
		raw, n := int64(zf.CompressedSize64), int64(zf.UncompressedSize64)
		total += zf.UncompressedSize64
		switch {
		case zf.Method != zip.Store && zf.Method != zip.Deflate:
			return nil, refuse("unsupported", fmt.Errorf("%s: compression method %d, want stored or deflated", rel, zf.Method))
		case zf.Flags&1 != 0 || zf.Mode()&fs.ModeSymlink != 0:
			return nil, refuse("unsupported", fmt.Errorf("%s: encrypted entries and symlinks are refused", rel))
		case files[rel] != nil:
			return nil, fmt.Errorf("%s is zipped twice", rel)
		case total > 64*uint64(size):
			return nil, refuse("too_large", errors.New("expands to more than 64 times its size"))
		case err != nil || off < 0 || off+raw > size || (zf.Method == zip.Store && raw != n):
			return nil, fmt.Errorf("%s: entry outside the zip", rel)
		}
		span := asset.Span{Off: off, Raw: raw, Size: n, CRC: zf.CRC32, Deflate: zf.Method == zip.Deflate}
		s := &scanned{Span: span}
		if err := s.read(f, kept[rel]); err != nil {
			return nil, fmt.Errorf("%s: %w", rel, err)
		}
		files[rel] = s
	}
	return files, nil
}

// read marks a deflate stream that ends short of its span as not framable: it cannot go out as
// gzip, only inflated.
func (s *scanned) read(ra io.ReaderAt, keep bool) error {
	if keep && s.Size > maxKept {
		return refuse("too_large", fmt.Errorf("larger than %d MiB", maxKept>>20))
	}
	span := io.NewSectionReader(ra, s.Off, s.Raw)
	buf := bufio.NewReader(span)
	var r io.Reader = buf
	if s.Deflate {
		r = flate.NewReader(buf)
	}
	sum, crc, head := sha256.New(), crc32.NewIEEE(), &prefix{}
	w := io.MultiWriter(sum, crc, head)
	var body bytes.Buffer
	if keep {
		w = io.MultiWriter(w, &body)
	}
	n, err := io.Copy(w, io.LimitReader(r, s.Size+1))
	switch {
	case err != nil:
		return err
	case n != s.Size || crc.Sum32() != s.CRC:
		return errors.New("checksum mismatch, the zip is damaged")
	}
	s.sum, s.head, s.body = [32]byte(sum.Sum(nil)), *head, body.Bytes()
	read, _ := span.Seek(0, io.SeekCurrent)
	consumed := read - int64(buf.Buffered()) // bytes the inflater took from the span
	s.Framable = s.Deflate && consumed == s.Raw
	return nil
}

// prefix keeps 512 bytes, enough to sniff a content type.
type prefix []byte

func (p *prefix) Write(b []byte) (int, error) {
	if n := 512 - len(*p); n > 0 {
		*p = append(*p, b[:min(n, len(b))]...)
	}
	return len(b), nil
}

// verify checks bundle.sha256, in sha256sum format, and its Ed25519 signature.
func verify(pub ed25519.PublicKey, files map[string]*scanned) error {
	m, sg := files[manifestFile], files[signatureFile]
	if m == nil || sg == nil {
		return errors.New("bundle.sha256 and bundle.sha256.sig are required")
	}
	sig := sg.body
	if hx := strings.TrimSpace(string(sig)); len(hx) == 2*ed25519.SignatureSize {
		sig, _ = hex.DecodeString(hx)
	}
	if !ed25519.Verify(pub, m.body, sig) {
		return errors.New("bundle.sha256.sig does not match bundle.sha256")
	}
	listed := map[string]bool{manifestFile: true, signatureFile: true}
	for line := range strings.Lines(string(m.body)) {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		hx, name, _ := strings.Cut(line, " ")
		want, err := hex.DecodeString(hx)
		if err != nil || len(want) != sha256.Size || name == "" {
			return fmt.Errorf("bundle.sha256: bad line %q", line)
		}
		rel := path.Clean("/" + strings.TrimLeft(name, " *")) // "<hash>  ./x" or "<hash> *x"
		s := files[rel]
		switch {
		case s == nil:
			return fmt.Errorf("%s is listed in bundle.sha256 but missing", rel)
		case s.sum != [sha256.Size]byte(want):
			return fmt.Errorf("%s does not match bundle.sha256", rel)
		}
		listed[rel] = true
	}
	for rel := range files {
		if !listed[rel] {
			return fmt.Errorf("%s is not listed in bundle.sha256", rel)
		}
	}
	return nil
}

// stamp tells the app its version; the zip keeps the raw page, which OTA manifests hash. Only
// gzip: the page revalidates, so brotli would save about 2.5 KB once per release.
func stamp(index []byte, version string, head int) *asset.Asset {
	if head > 0 {
		meta := []byte(`<meta name="bundle-pilot:bundle" content="` + html.EscapeString(version) + `">`)
		index = slices.Concat(index[:head], meta, index[head:])
	}
	return asset.New("/index.html", index, gzipped(index))
}

func gzipped(b []byte) []byte {
	var buf bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	_, _ = zw.Write(b)
	_ = zw.Close()
	return buf.Bytes()
}

// indexMeta returns head as the offset just after <head>, 0 without one.
func indexMeta(index []byte) (base string, links []string, meta map[string]string, head int) {
	base, meta = "/", map[string]string{}
	z := html.NewTokenizer(bytes.NewReader(index))
	for off := 0; ; {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}
		off += len(z.Raw()) // tokens are contiguous, so this is the end of the current one
		if tt != html.StartTagToken && tt != html.SelfClosingTagToken {
			continue
		}
		name, hasAttr := z.TagName()
		if head == 0 && string(name) == "head" { // lower-cased; never <header> or a comment
			head = off
		}
		if !hasAttr {
			continue
		}
		attr := map[string]string{}
		for more := true; more; {
			var k, v []byte
			k, v, more = z.TagAttr()
			attr[string(k)] = string(v)
		}
		switch string(name) {
		case "base":
			if h := attr["href"]; h != "" {
				base = h
			}
		case "meta":
			if key, ok := strings.CutPrefix(attr["name"], "bundle-pilot:"); ok {
				meta[key] = attr["content"]
			}
		case "link":
			switch attr["rel"] {
			case "modulepreload":
				links = append(links, attr["href"]+">; rel=modulepreload")
			case "stylesheet":
				l := attr["href"] + ">; rel=preload; as=style"
				if _, ok := attr["crossorigin"]; ok {
					l += "; crossorigin" // a preload in another CORS mode is fetched again, not reused
				}
				links = append(links, l)
			}
		case "script":
			if attr["type"] == "module" && attr["src"] != "" {
				links = append(links, attr["src"]+">; rel=modulepreload")
			}
		}
	}
	return base, links[:min(len(links), 12)], meta, head
}
