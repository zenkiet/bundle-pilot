package bundlefs

import (
	"archive/zip"
	"bytes"
	"cmp"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"runtime"
	"slices"
	"strings"
	"sync"

	"golang.org/x/net/html"
	"golang.org/x/sync/errgroup"

	"github.com/zenkiet/bundle-pilot/internal/domain"
	"github.com/zenkiet/bundle-pilot/internal/pkg/asset"
)

var (
	gzipMagic   = []byte{0x1f, 0x8b}
	compressors = sync.Pool{New: func() any { return &compressor{zw: gzip.NewWriter(nil)} }}
)

const (
	maxWorkers    = 8
	manifestFile  = "/bundle.sha256"
	signatureFile = "/bundle.sha256.sig"
)

// item is one file of a bundle. gzip, when set, returns a gzip body built from
// the zip's own deflate stream, so nothing is recompressed at load.
type item struct {
	size int64
	open func() ([]byte, error)
	gzip func() []byte
}

type task struct {
	bundle int
	rel    string
	size   int64
	sum    [32]byte
	slot   *slot
	err    error
}

type slot struct{ blob *asset.Blob }

// loadBundles feeds every file of every version, largest first, to one worker
// pool. The first worker to hash a content claims it and compresses it; later
// copies only point at its slot, so identical files cost one read and one hash.
func loadBundles(fsys fs.FS, pub ed25519.PublicKey, vs []version, known map[[32]byte]*asset.Blob) ([]*domain.Bundle, []error) {
	errs := make([]error, len(vs))
	items := make([]map[string]item, len(vs))
	files := make([]map[string]*asset.Asset, len(vs))
	var tasks []*task
	for i, v := range vs {
		var closer io.Closer
		if items[i], closer, errs[i] = open(fsys, v); closer != nil {
			defer func() { _ = closer.Close() }()
		}
		if errs[i] == nil && items[i]["/index.html"].open == nil {
			errs[i] = domain.ErrNoIndex
		}
		if errs[i] != nil {
			continue
		}
		files[i] = map[string]*asset.Asset{}
		for rel, it := range items[i] {
			if !sidecar(rel, items[i]) {
				tasks = append(tasks, &task{bundle: i, rel: rel, size: it.size})
			}
		}
	}
	slices.SortFunc(tasks, func(a, b *task) int { return cmp.Compare(b.size, a.size) })

	var mu sync.Mutex
	slots := make(map[[32]byte]*slot, len(known)+len(tasks))
	for sum, b := range known {
		slots[sum] = &slot{b}
	}
	var g errgroup.Group
	g.SetLimit(min(runtime.GOMAXPROCS(0), maxWorkers))
	for _, t := range tasks {
		g.Go(func() error {
			its := items[t.bundle]
			it := its[t.rel]
			body, err := it.open()
			if err != nil {
				t.err = err
				return nil
			}
			t.sum = sha256.Sum256(body)
			var gz, br []byte
			if side, ok := its[t.rel+".gz"]; ok {
				if gz, _ = side.open(); !bytes.HasPrefix(gz, gzipMagic) {
					gz = nil
				}
			}
			if side, ok := its[t.rel+".br"]; ok {
				br, _ = side.open()
			}
			// A zip's own stream is just another encoding of the same bytes, so it
			// stays out of the key: zips built at different levels still share a blob.
			key := contentKey(t.sum, gz, br)
			if gz == nil && it.gzip != nil {
				gz = it.gzip()
			}
			mu.Lock()
			s, seen := slots[key]
			if !seen {
				s = &slot{}
				slots[key] = s
			}
			mu.Unlock()
			if t.slot = s; seen {
				return nil
			}
			if gz == nil && asset.Compressible(t.rel, body) {
				z := compressors.Get().(*compressor)
				gz = z.gzip(body)
				compressors.Put(z)
			}
			s.blob = asset.NewBlob(key, body, gz, br)
			return nil
		})
	}
	_ = g.Wait()

	for _, t := range tasks {
		switch {
		case errs[t.bundle] != nil:
		case t.err != nil:
			errs[t.bundle] = t.err
		default:
			files[t.bundle][t.rel] = asset.New(t.rel, t.slot.blob)
		}
	}
	bundles := make([]*domain.Bundle, len(vs))
	for i, v := range vs {
		if errs[i] != nil {
			continue
		}
		if pub != nil {
			if errs[i] = verify(pub, items[i], tasks, i); errs[i] != nil {
				continue
			}
		}
		base, link := indexMeta(files[i]["/index.html"].Body())
		if bundles[i], errs[i] = domain.NewBundle(v.name, base, files[i]); errs[i] == nil {
			bundles[i].ZipBytes, bundles[i].ModTime, bundles[i].Link = v.size, v.mtime, link
		}
	}
	return bundles, errs
}

// open lists a zip's entries and reads them straight from the archive, which
// stays open until the load finishes; directory and dot entries are skipped.
func open(fsys fs.FS, v version) (map[string]item, io.Closer, error) {
	f, err := fsys.Open(v.path)
	if err != nil {
		return nil, nil, err
	}
	ra, ok := f.(io.ReaderAt)
	if !ok {
		err = errors.New("zip needs random access")
	}
	var zr *zip.Reader
	if err == nil {
		zr, err = zip.NewReader(ra, v.size)
	}
	if err != nil {
		_ = f.Close()
		return nil, nil, err
	}
	items := make(map[string]item, len(zr.File))
	for _, zf := range zr.File {
		rel := path.Clean("/" + zf.Name)
		if strings.HasSuffix(zf.Name, "/") || strings.Contains(rel, "/.") {
			continue
		}
		it := item{size: int64(zf.UncompressedSize64), open: func() ([]byte, error) {
			rc, err := zf.Open()
			if err != nil {
				return nil, err
			}
			b, err := io.ReadAll(rc)
			_ = rc.Close()
			return b, err
		}}
		if zf.Method == zip.Deflate {
			it.gzip = func() []byte {
				r, err := zf.OpenRaw()
				if err != nil {
					return nil
				}
				raw, err := io.ReadAll(r)
				if err != nil {
					return nil
				}
				return gzipFrame(raw, zf.CRC32, zf.UncompressedSize64)
			}
		}
		items[rel] = it
	}
	return items, f, nil
}

// gzipFrame wraps a raw deflate stream as gzip; zip and gzip share the CRC-32
// of the uncompressed data.
func gzipFrame(raw []byte, crc uint32, size uint64) []byte {
	b := make([]byte, 0, len(raw)+18)
	b = append(b, 0x1f, 0x8b, 8, 0, 0, 0, 0, 0, 0, 0xff)
	b = append(b, raw...)
	b = binary.LittleEndian.AppendUint32(b, crc)
	return binary.LittleEndian.AppendUint32(b, uint32(size))
}

// verify checks bundle.sha256 (sha256sum format) against every loaded file
// and its Ed25519 signature in bundle.sha256.sig against pub.
func verify(pub ed25519.PublicKey, items map[string]item, tasks []*task, bundle int) error {
	m, ok := items[manifestFile]
	sg, ok2 := items[signatureFile]
	if !ok || !ok2 {
		return errors.New("bundle.sha256 and bundle.sha256.sig are required")
	}
	text, err := m.open()
	if err != nil {
		return err
	}
	sig, err := sg.open()
	if err != nil {
		return err
	}
	if hx := strings.TrimSpace(string(sig)); len(hx) == 2*ed25519.SignatureSize {
		sig, _ = hex.DecodeString(hx)
	}
	if !ed25519.Verify(pub, text, sig) {
		return errors.New("bundle.sha256.sig does not match bundle.sha256")
	}
	sums := map[string][32]byte{}
	for _, t := range tasks {
		if t.bundle == bundle {
			sums[t.rel] = t.sum
		}
	}
	listed := map[string]bool{manifestFile: true, signatureFile: true}
	for line := range strings.Lines(string(text)) {
		if line = strings.TrimSpace(line); line == "" {
			continue
		}
		hx, name, _ := strings.Cut(line, " ")
		want, err := hex.DecodeString(hx)
		if err != nil || len(want) != sha256.Size || name == "" {
			return fmt.Errorf("bundle.sha256: bad line %q", line)
		}
		rel := path.Clean("/" + strings.TrimPrefix(strings.TrimLeft(name, " *"), "./"))
		got, ok := sums[rel]
		if !ok {
			it, ok := items[rel]
			if !ok {
				return fmt.Errorf("%s is listed in bundle.sha256 but missing", rel)
			}
			b, err := it.open()
			if err != nil {
				return err
			}
			got = sha256.Sum256(b)
		}
		if got != [sha256.Size]byte(want) {
			return fmt.Errorf("%s does not match bundle.sha256", rel)
		}
		listed[rel] = true
	}
	for rel := range items {
		if !listed[rel] {
			return fmt.Errorf("%s is not listed in bundle.sha256", rel)
		}
	}
	return nil
}

// contentKey identifies a blob by its body and any precompressed sidecars, so a
// sidecar added or fixed later yields a new blob instead of reusing a stale one.
func contentKey(sum [32]byte, gz, br []byte) [32]byte {
	if gz == nil && br == nil {
		return sum
	}
	h := sha256.New()
	_, _ = h.Write(sum[:])
	for _, enc := range [...][]byte{gz, br} {
		es := sha256.Sum256(enc)
		_, _ = h.Write(es[:])
	}
	h.Sum(sum[:0])
	return sum
}

func sidecar(rel string, items map[string]item) bool {
	for _, ext := range [...]string{".gz", ".br"} {
		if base, ok := strings.CutSuffix(rel, ext); ok {
			if _, ok := items[base]; ok {
				return true
			}
		}
	}
	return false
}

type compressor struct {
	buf bytes.Buffer
	zw  *gzip.Writer
}

func (z *compressor) gzip(body []byte) []byte {
	z.buf.Reset()
	z.zw.Reset(&z.buf)
	if _, err := z.zw.Write(body); err != nil || z.zw.Close() != nil {
		return nil
	}
	return bytes.Clone(z.buf.Bytes())
}

// baseHrefOf reads <base href> from index.html; without one the app mounts at /.
// indexMeta reads <base href> and what index.html preloads (module scripts,
// modulepreload links, stylesheets), joined as a Link header value.
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
	for i, l := range links[:min(len(links), 12)] {
		if !strings.HasPrefix(l, "/") && !strings.Contains(l, "://") {
			l = strings.TrimSuffix(base, "/") + "/" + l
		}
		links[i] = "<" + l
	}
	return base, strings.Join(links[:min(len(links), 12)], ", ")
}
