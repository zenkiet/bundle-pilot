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

	"github.com/andybalholm/brotli"
	"golang.org/x/net/html"

	"github.com/zenkiet/bundle-pilot/internal/domain"
	"github.com/zenkiet/bundle-pilot/internal/pkg/asset"
)

const (
	manifestFile  = "/bundle.sha256"
	signatureFile = "/bundle.sha256.sig"
	maxKept       = 8 << 20
)

var (
	// kept are read into memory at load; every other byte is served from the zip.
	kept = map[string]bool{"/index.html": true, "/ngsw.json": true, manifestFile: true, signatureFile: true}
	// textual files deserve a .br sidecar: without one they go out as gzip at best.
	textual = map[string]bool{".js": true, ".mjs": true, ".css": true, ".html": true, ".json": true, ".svg": true, ".txt": true}
)

// zipped is a loaded bundle, the open zip it serves from and that zip's state at load.
type zipped struct {
	bundle *domain.Bundle
	f      *os.File
	info   fs.FileInfo
	issues []string
}

// intact reports the zip was not rewritten in place since it loaded: a rename or a delete
// leaves the open file as it was, an in-place write would serve wrong bytes.
func (z zipped) intact() bool {
	info, err := z.f.Stat()
	return err == nil && info.Size() == z.info.Size() && info.ModTime().Equal(z.info.ModTime())
}

// loadBundles loads the zips side by side. A loaded zip stays open, owned by its bundle, and
// closes when its last snapshot is collected.
func loadBundles(fsys fs.FS, pub ed25519.PublicKey, vs []version) ([]zipped, []error) {
	out, errs := make([]zipped, len(vs)), make([]error, len(vs))
	slots := make(chan struct{}, runtime.GOMAXPROCS(0))
	var wg sync.WaitGroup
	for i, v := range vs {
		wg.Go(func() {
			slots <- struct{}{}
			defer func() { <-slots }()
			f, err := fsys.Open(v.path)
			if err != nil {
				errs[i] = err
				return
			}
			osf, ok := f.(*os.File)
			if !ok {
				_ = f.Close()
				errs[i] = errors.New("zips must be files on disk")
				return
			}
			if out[i], errs[i] = loadZip(osf, pub, v.name); errs[i] != nil {
				_ = osf.Close()
			}
		})
	}
	wg.Wait()
	return out, errs
}

// loadZip serves a zip in place. One pass over every entry checks CRC-32 and size against
// the central directory and takes the SHA-256 the ETag and the signature need, keeping no
// bytes; only index.html, stamped with the version, and ngsw.json are held in memory.
func loadZip(f *os.File, pub ed25519.PublicKey, name string) (zipped, error) {
	info, err := f.Stat()
	if err != nil {
		return zipped{}, err
	}
	zr, err := zip.NewReader(f, info.Size())
	if err != nil {
		return zipped{}, err
	}
	files, err := scan(f, zr, info.Size())
	if err != nil {
		return zipped{}, err
	}
	index := files["/index.html"]
	if index == nil {
		return zipped{}, domain.ErrNoIndex
	}
	if pub != nil {
		if err := verify(pub, files); err != nil {
			return zipped{}, err
		}
	}
	z := zipped{f: f, info: info}
	assets := make(map[string]*asset.Asset, len(files))
	dec := brotli.NewReader(nil) // reused: a fresh decoder per sidecar doubles the garbage
	bare := 0
	for rel, s := range files {
		if base, ok := strings.CutSuffix(rel, ".br"); ok && files[base] != nil {
			continue // a sidecar: the br encoding of base
		}
		var br *asset.Span
		if side := files[rel+".br"]; side != nil {
			if sum, err := decodedSum(dec, f, side, s.Size); err == nil && sum == s.sum {
				br = &side.Span
			} else {
				z.issues = append(z.issues, fmt.Sprintf("bundle %s: %s.br does not decode to %[2]s, ignored", name, rel[1:]))
			}
		}
		if br == nil && textual[path.Ext(rel)] && s.Size > 1<<10 && rel != "/index.html" {
			bare++
		}
		assets[rel] = asset.FromZip(rel, f, s.Span, s.sum, s.head, br)
	}
	if bare > 0 {
		z.issues = append(z.issues, fmt.Sprintf("bundle %s: %d text files have no .br, so they go out as gzip: precompress them in CI", name, bare))
	}
	if ngsw := files["/ngsw.json"]; ngsw != nil {
		assets["/ngsw.json"] = asset.New("/ngsw.json", ngsw.body, gzipped(ngsw.body))
	}
	base, link := indexMeta(index.body)
	if z.bundle, err = domain.NewBundle(name, base, assets, stamp(index.body, name)); err != nil {
		return zipped{}, err
	}
	z.bundle.ZipBytes, z.bundle.ModTime, z.bundle.Link = info.Size(), info.ModTime(), link
	if !z.intact() {
		return zipped{}, errChanged
	}
	return z, nil
}

// scanned is a zip entry checked at load.
type scanned struct {
	asset.Span
	sum  [32]byte
	head []byte
	body []byte
}

// scan reads every entry once and refuses what cannot be served in place safely: other
// methods, encryption, symlinks, duplicates, spans outside the file and zip bombs.
func scan(f *os.File, zr *zip.Reader, size int64) (map[string]*scanned, error) {
	files := make(map[string]*scanned, len(zr.File))
	var total uint64
	for _, zf := range zr.File {
		rel := path.Clean("/" + zf.Name)
		if strings.HasSuffix(zf.Name, "/") || strings.Contains(rel, "/.") {
			continue
		}
		off, err := zf.DataOffset()
		raw, n := int64(zf.CompressedSize64), int64(zf.UncompressedSize64)
		total += zf.UncompressedSize64
		switch {
		case zf.Method != zip.Store && zf.Method != zip.Deflate:
			return nil, fmt.Errorf("%s: compression method %d, want stored or deflated", rel, zf.Method)
		case zf.Flags&1 != 0 || zf.Mode()&fs.ModeSymlink != 0:
			return nil, fmt.Errorf("%s: encrypted entries and symlinks are refused", rel)
		case files[rel] != nil:
			return nil, fmt.Errorf("%s is zipped twice", rel)
		case total > 64*uint64(size):
			return nil, errors.New("expands to more than 64 times its size")
		case err != nil || off < 0 || off+raw > size || zf.Method == zip.Store && raw != n:
			return nil, fmt.Errorf("%s: entry outside the zip", rel)
		}
		s := &scanned{Span: asset.Span{Off: off, Raw: raw, Size: n, CRC: zf.CRC32, Deflate: zf.Method == zip.Deflate}}
		if err := s.read(f, kept[rel]); err != nil {
			return nil, fmt.Errorf("%s: %w", rel, err)
		}
		files[rel] = s
	}
	return files, nil
}

// read decodes the entry once. A deflate stream that ends short of its span cannot be framed
// as gzip, so that entry only goes out inflated.
func (s *scanned) read(ra io.ReaderAt, keep bool) error {
	if keep && s.Size > maxKept {
		return fmt.Errorf("larger than %d MiB", maxKept>>20)
	}
	in := &counter{r: io.NewSectionReader(ra, s.Off, s.Raw)}
	buf := bufio.NewReader(in)
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
	s.Framable = s.Deflate && in.n-int64(buf.Buffered()) == s.Raw
	return nil
}

// decodedSum hashes a .br sidecar once decompressed, so it pairs only with the file it encodes.
func decodedSum(dec *brotli.Reader, ra io.ReaderAt, s *scanned, limit int64) ([32]byte, error) {
	r := s.Open(ra)
	defer func() { _ = r.Close() }()
	if err := dec.Reset(r); err != nil {
		return [32]byte{}, err
	}
	h := sha256.New()
	_, err := io.Copy(h, io.LimitReader(dec, limit+1))
	return [32]byte(h.Sum(nil)), err
}

type counter struct {
	r io.Reader
	n int64
}

func (c *counter) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

// prefix keeps the first 512 bytes written, enough to sniff a content type.
type prefix []byte

func (p *prefix) Write(b []byte) (int, error) {
	if n := 512 - len(*p); n > 0 {
		*p = append(*p, b[:min(n, len(b))]...)
	}
	return len(b), nil
}

// verify checks bundle.sha256 (sha256sum format) against the scanned files and its Ed25519
// signature in bundle.sha256.sig against pub.
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
		if line = strings.TrimSpace(line); line == "" {
			continue
		}
		hx, name, _ := strings.Cut(line, " ")
		want, err := hex.DecodeString(hx)
		if err != nil || len(want) != sha256.Size || name == "" {
			return fmt.Errorf("bundle.sha256: bad line %q", line)
		}
		rel := path.Clean("/" + strings.TrimPrefix(strings.TrimLeft(name, " *"), "./"))
		switch s := files[rel]; {
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

// stamp puts <meta name="bundle-pilot:bundle"> in <head>, so the app knows its version; the
// zip keeps the raw page OTA manifests hash. gzip only: the page revalidates, so br would
// save ~2.5 KB once per release.
func stamp(index []byte, version string) *asset.Asset {
	if i := bytes.Index(index, []byte("<head")); i >= 0 {
		if n := bytes.IndexByte(index[i:], '>'); n >= 0 {
			i += n + 1
			index = slices.Concat(index[:i], []byte(`<meta name="bundle-pilot:bundle" content="`+html.EscapeString(version)+`">`), index[i:])
		}
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

// indexMeta reads <base href> ("/" without one) and a Link header preloading up to
// 12 module scripts, modulepreloads and stylesheets.
func indexMeta(index []byte) (base, link string) {
	base = "/"
	var links []string
	z := html.NewTokenizer(bytes.NewReader(index))
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}
		if tt != html.StartTagToken && tt != html.SelfClosingTagToken {
			continue
		}
		name, hasAttr := z.TagName()
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
		case "link":
			switch attr["rel"] {
			case "modulepreload":
				links = append(links, attr["href"]+">; rel=modulepreload")
			case "stylesheet":
				links = append(links, attr["href"]+">; rel=preload; as=style")
			}
		case "script":
			if attr["type"] == "module" && attr["src"] != "" {
				links = append(links, attr["src"]+">; rel=modulepreload")
			}
		}
	}
	links = links[:min(len(links), 12)]
	for i, l := range links {
		if !strings.HasPrefix(l, "/") && !strings.Contains(l, "://") {
			l = strings.TrimSuffix(base, "/") + "/" + l
		}
		links[i] = "<" + l
	}
	return base, strings.Join(links, ", ")
}
