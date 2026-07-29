# edge-gateway

Serves several versions of a static web bundle and picks one per user, so a
frontend can match the backend version it is talking to. Every file lives in
RAM once per unique content (identical files across bundles are shared),
pre-compressed, with strong ETags, conditional requests and ranges handled by
the standard library. Works with
any bundler that emits content-hashed filenames (Angular, Vite, webpack,
SvelteKit).

## Layout

```
dist/
  config.json         default bundle, selection rules, backend table
  versions/
    4.78.0.zip        one zip per bundle, index.html at the root of the zip
    4.79.0.zip          index.html: <base href> decides the mount path
                        main-ABCD1234.js.br: optional precompressed sidecars (.br, .gz)
```

One zip is one version: `mv` replaces it atomically, every entry carries a
CRC, and its deflate streams are served as `gzip` directly, so nothing is
compressed at load. Build it with a fixed tool and level (`zip -9 -r -X -D`)
so identical files across versions share one copy in RAM; store images and
fonts (`zip -0`) rather than deflating them. Directory and dot entries inside
the zip are ignored, and so is anything in `versions/` that is not a `.zip`.

Deploy a new version as `dist/versions/.tmp-<v>.zip` then `mv` it to
`dist/versions/<v>.zip`: dot files are ignored and the rename is atomic. The
gateway checks every 5 seconds and reloads only the zips whose size or mtime
changed; a bundle that fails to load (read error, or replaced while being
read) is retried with a backoff up to a minute, and its previous load keeps
serving meanwhile. Edit `config.json` the same way (write a temp file, then
`mv`).

Precompressed sidecars (`x.js.br`, `x.js.gz`) are served instead of compressing
at load time; everything else compressible is gzipped once per unique content.

```sh
cd build/browser   # the app's build output, before zipping
find . -type f \( -name '*.js' -o -name '*.css' -o -name '*.html' -o -name '*.svg' -o -name '*.json' -o -name '*.txt' \) \
  -print0 | xargs -0 -P8 -n1 brotli -q 11 -k
```

## config.json

```json
{
  "default": "4.79.0",
  "backend": {
    "07.29.2026": "4.78.0",
    "07.31.2026": "4.79.0"
  },
  "rules": [
    { "id": "legacy-app",    "when": { "app": "<2.0.0" },            "bundle": "4.73.0" },
    { "id": "qa-team",       "when": { "user": ["u-101", "u-102"] }, "bundle": "4.80.0" },
    { "id": "store-2020210", "when": { "storeID": 2020210 },         "bundle": "4.80.0" }
  ]
}
```

Every key is optional; `config.schema.json` in this repo gives editors
completion (`"$schema": "./config.schema.json"` next to a copy of it).
`POST /__gateway/data` decides a bundle from the facts the frontend sends:

1. the first rule, top to bottom, whose `when` entries all match;
2. else `backend`: the entry with the largest key `<=` the `backend` fact, or
   the oldest entry when the fact predates them all;
3. else `default`, or the newest bundle when none is set.

`when` values: a string, number or boolean matches exactly (numbers by their
literal text, so `2020210` equals `"2020210"`); a list matches any element;
`<`, `<=`, `>`, `>=` compare against a version or a date, and
space-separated parts must all hold (`">=2.0.0 <3.0.0"`); `"*"` matches any
present value; `"10%"` matches a stable 10% of the values (hashed, so raising
the number only adds users). A fact of another shape, or a missing fact,
never matches. Fact names are case-insensitive. Put hard compatibility limits
first, then users, stores and tenants.

A rule may carry `"note"` (free text, JSON has no comments) and `"until"`:
`"2026-10-31"` keeps it active through the end of that day in UTC, an RFC
3339 time gives an exact instant. Expiry applies at the next `/data` call;
expired rules are reported so they get deleted.

Versions are semantic versions of up to three numeric parts (`4.100.0 > 4.90.0`,
`1.0.0-rc.1 < 1.0.0`, an optional `v` prefix is ignored);
`backend` keys must all be dates or all dotted versions.

Dates follow `"dateFormat"`, `MM.dd.YYYY` by default, for backend keys,
comparisons and posted facts alike. Write it with `YYYY`, `MM` (two digits)
or `M` (one or two), `dd` or `d`, separated by `.`, `-` or `/`; `M` and `d`
need a separator next to them. Examples: `dd/MM/YYYY`, `YYYY-MM-DD`,
`YYYYMMDD`, `M.d.YYYY` (accepts `7.29.2026` and `07.29.2026`). A value shaped
like a date that does not exist, such as `02.30.2026`, never matches.

On every load the file is decoded strictly: unknown keys (`"bundel"`),
duplicate keys, trailing commas and invalid comparisons are rejected with a
line and column, and the previous config keeps serving. A rule whose bundle
is not on disk is inactive until that bundle is deployed. Logs and
`/__gateway/status` also warn about a backend table that gets older as the
backend gets newer, dates far in the future, and rules that repeat an
earlier rule's conditions.

Facts are claimed by the browser, so rules route users, they do not grant
access: anyone can send `storeID` 2020210, just as anyone can use
`?bundle=`. Never rely on a bundle to hide features or data.

## Bundle source

`versions/` is filled from wherever `"source"` in `config.json` points. Without
it, or with `{"type": "local"}`, the zips are the files you put there. With an
S3-compatible bucket (AWS S3, Cloudflare R2, MinIO, B2, Spaces) the gateway
mirrors the bucket's zips into `versions/` and everything else stays the same:

```json
{
  "source": {
    "type": "s3",
    "bucket": "bundles",
    "prefix": "prod/",
    "endpoint": "https://<ACCOUNT_ID>.r2.cloudflarestorage.com",
    "region": "auto",
    "accessKeyId": "...",
    "secretAccessKey": "...",
    "poll": "30s"
  }
}
```

`bucket` and `region` are required. Leave both keys out to use the AWS
credential chain (environment, shared config, IAM role, IRSA, SSO), which is
the way to go on AWS compute. Leave
`endpoint` out for AWS (`https://<bucket>.s3.<region>.amazonaws.com`); R2 uses
`https://<ACCOUNT_ID>.r2.cloudflarestorage.com` with region `auto`, MinIO
`http://host:9000`. `prefix` is the folder holding the zips (`prod/4.80.0.zip`
becomes `versions/4.80.0.zip`); nested keys and non-zip objects are ignored.
`poll` (default `30s`, at least `5s`) is how often the bucket is listed.

Every poll compares size and modification time with the local files,
downloads what changed through a temp file and an atomic rename, and deletes
local zips the bucket no longer has, so the bucket is the source of truth.
A listing that fails, or lists no zips at all, changes nothing on disk, and
the bundles already loaded keep serving. Sync failures show up in
`/__gateway/status` under `issues` (`source s3://bundles/prod/: operation
error S3: ListObjectsV2, ... api error AccessDenied: ...`) and in the log when
the state changes. Editing `"source"`
applies at the next reload without a restart; the first sync of a new bucket
runs before that load, so a fresh cache boots straight from the bucket.

The gateway only needs to list and read: on R2 create an API token with
**Object Read only** limited to the bucket; on AWS grant `s3:ListBucket` on the
bucket and `s3:GetObject` on `<prefix>/*` to the instance role, or to a user
whose keys go in `config.json`. Give CI a separate token that may write. Keys
in `config.json` mean keeping that file `0600`, out of git and out of chat. With a bucket source the container must be able to write
`versions/`: mount `dist/` read-write. Upload a new zip first and change
`default` or the rules at least one poll later, so every replica already has
the zip when the switch lands. `gateway check` syncs the same way before
checking.

## Configuration

| Env | Default | Purpose |
|---|---|---|
| `PORT` | `8080` | Listen port |
| `DIST` | `./dist` | Root holding `versions/` and `config.json`; with a bucket source, `versions/` is the local cache and must be writable |
| `LOG_LEVEL` | `info` | `debug` also logs every bundle switch decided by `/data` |
| `BUNDLE_PUBKEY` | off | Ed25519 public key (hex or base64); every bundle must then be signed, see below |

The gateway serves bundles only; route API paths to their backend at the
ingress. On shutdown, requests get 10 seconds to finish before open
connections are closed.

## Endpoints

| Path | Behaviour |
|---|---|
| `<base href>**` | Bundle chosen by `?bundle=`, then cookie `bundle`, then default. Hashed files resolve across all bundles so open sessions never 404 after a switch. Navigations (`Accept: text/html`) and paths without an extension fall back to `index.html`. |
| `POST /__gateway/data` | Body: a flat JSON object of facts (`Content-Type: application/json`, at most 4 KiB and 32 facts), e.g. `{"backend":"07.30.2026","storeID":2020210}`. Returns `{"bundle":"4.80.0","via":"rule:store-2020210"}`, where `via` is `rule:<id>`, `backend:<key>` or `default`. A rule or backend decision sets cookie `bundle` for 7 days (refreshed on every call); a default decision clears it. Cross-origin requests are refused. Send `X-Dry-Run: 1` to see the decision without cookies or counters. |
| `/__gateway/status` | Default and its source, base path, bundle `source` (`local` or `s3://bucket/prefix/`), `loaded_at`, reload count and last error, `errors` (a rejected config, refused bundles), RAM held (`resident_bytes`), bundles with file counts and sizes, rules (with `active`), backend table, decisions counted by `via`, load issues. Keep it off the public internet at the ingress. |
| `/healthz` | 200 |

Cache policy: hashed files are `public, max-age=31536000, immutable`;
everything else is `private, no-cache` with an ETag. Responses carry
`X-Bundle-Version` (the bundle that owns the file served). A CDN in front
caches the hashed files at the edge and passes everything else through; that
is the largest latency win available, the gateway itself spends about 10µs
of CPU per request.

## Signing bundles

With `BUNDLE_PUBKEY` set, every bundle must contain `bundle.sha256`
(`sha256sum` format, one line per file) and `bundle.sha256.sig` (its Ed25519
signature, raw or hex). A bundle whose manifest is missing, unsigned, or lists
a file that changed, is refused and its previous load keeps serving. The same
hashes serve as checksums for mobile live updates.

```sh
openssl genpkey -algorithm ed25519 -out bundle-key.pem           # once, keep private
openssl pkey -in bundle-key.pem -pubout -outform DER | tail -c 32 | xxd -p -c 64   # BUNDLE_PUBKEY
cd build/browser   # the app's build output, before zipping
find . -type f ! -name 'bundle.sha256*' -exec sha256sum {} + > bundle.sha256
openssl pkeyutl -sign -inkey /path/to/bundle-key.pem -rawin -in bundle.sha256 -out bundle.sha256.sig
zip -9 -r -X -D ../4.80.0.zip .
```

## Checking a deploy

`gateway check [DIST]` loads the dist directory exactly as the server would,
prints every issue and exits 1 when `config.json` is rejected or a zip is
refused (unreadable, unsigned, or without `index.html` at its root). Run it in CI or before
copying a hand edit into place.

## Frontend integration

Post every fact the app knows, and reload when the answer differs from the
version it was built as (`environment.version` must equal the zip's name
without `.zip`). Call it at boot, while the splash screen is up, with the
backend version plus the facts remembered from the last login, and again
after login, logout or a store switch. Skip it when the URL pins a bundle
with `?bundle=`, or the pin and the rules keep reloading each other.

```ts
const PIN = new URLSearchParams(location.search).has('bundle');

export async function syncBundle(facts: Record<string, string | number | boolean>) {
  if (PIN) return;
  const res = await fetch('/__gateway/data', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(facts),
  }).catch(() => null);
  if (!res?.ok) return;
  const { bundle } = await res.json();
  if (bundle === environment.version) return sessionStorage.removeItem('gw.target');
  if (sessionStorage.getItem('gw.target') === bundle) return; // one reload per target per tab
  sessionStorage.setItem('gw.target', bundle);
  // Angular service worker: let it fetch the new ngsw.json first, or the reload serves the cached index.
  // await swUpdate.checkForUpdate(); await swUpdate.activateUpdate();
  location.reload();
}

// boot:  syncBundle({ backend: posVersion, app: shellVersion, ...JSON.parse(localStorage.getItem('gw.facts') ?? '{}') })
// login: localStorage.setItem('gw.facts', JSON.stringify({ tenant, storeID, user })), then syncBundle again with everything
```

Send ids as strings when they can exceed 2^53. Returning users get their
bundle from the cookie on the first request. Runtime config files such as `environments/env.js` are served from
the selected bundle; keep one copy per version.

## Structure

```
cmd/gateway                    wiring: config -> bundlefs -> usecase -> handler -> http.Server
internal/config                env parsing and validation
internal/domain                Bundle, Snapshot, Config (rules, backend table): selection, no I/O
internal/usecase               Catalog: reload, watch, atomic snapshot swap (declares the Source port)
internal/infrastructure        bundlefs (zip loader, dedup, compression), mirror (bucket sync, s3 strategy)
internal/handler               HTTP routes, cookies, ngsw.json stamping
internal/pkg                   asset (in-memory static serving), version (comparison)
```

## Run

```
make dev          # go run ./cmd/gateway
make build && bin/gateway check dist
make lint         # golangci-lint, including layer rules
docker compose up --build
```
