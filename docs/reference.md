# Bundle Pilot reference

The technical manual: layout on disk, the config contract, bundle sources, endpoints, signing and the client snippet. The [README](../README.md) explains why the project exists and how it works.

Bundle Pilot serves several versions of a static web bundle and picks one per user, so a
frontend can match the backend version it is talking to. Files are served
straight from the zips, which the OS page cache keeps warm, so RAM stays at a
few MB however many versions are deployed; strong ETags, conditional requests
and ranges come from the standard library. Works with
any bundler that emits content-hashed filenames (Angular, Vite, webpack,
SvelteKit).

## Layout

```
dist/
  config.pb           default bundle, selection rules, backend table (protobuf, see below)
  history/            every saved config, newest 100, secrets masked (see History)
  events.jsonl        the activity feed, newest 500 (see ListEvents)
  versions/
    4.78.0.zip        one zip per bundle, index.html at the root of the zip
    4.79.0.zip          index.html: <base href> decides the mount path
                        main-ABCD1234.js.br: optional brotli sidecar, stored
```

Every bundle serves under one mount path, the default's `<base href>`: a bundle
built for another base is not served (rules and the backend table skip it, and
the status says why), since its pages would load there and 404 on their files.
Build every version with the same base href.

One zip is one version and the gateway serves it in place: a deflated entry
goes out as `gzip` with its stream framed, a stored `x.br` next to `x` as
`br`, and anything else is inflated on demand. Byte ranges work on stored
entries and on deflated ones up to 1 MiB; a larger deflated entry goes out
whole, since a range would inflate everything before it, so store media that
needs ranges (`zip -0`). At load every entry is read
once to check its CRC and size and take the SHA-256 behind its ETag; only
`index.html` (stamped with the version) and `ngsw.json` are kept in memory.
A zip stays open while a snapshot uses it, so **replace zips only by rename**:
a write in place goes out as shifted, unchecked bytes until the next check,
about 5 s, which then loads the version again or drops it if it no longer
checks out; one that puts back the size and mtime goes unnoticed until a
restart. A zip holds at most 65 536 files. Directory and dot entries inside the zip are
ignored, except under a top-level `.well-known/` (app and universal links, served when
`<base href>` is `/`), and so is anything in `versions/` that is not a `.zip`.

Deploy a new version as `dist/versions/.tmp-<v>.zip` then `mv` it to
`dist/versions/<v>.zip`: dot files are ignored and the rename is atomic. The
gateway checks every 5 seconds and reloads only the zips whose size or mtime
moved; a bundle that fails to load (a read error, a half-copied zip) is retried
with a backoff up to a minute, and its previous load keeps serving meanwhile
while its open file is unchanged, as after a replace by rename. `config.pb` is
written atomically by the admin API and by `gateway import`. With no bundle yet
the gateway still starts: app routes and `POST /__gateway/data` answer 503
until one is uploaded, while `/healthz` and the admin API work. A zip that
vanishes from a running gateway, deleted on disk or in the bucket, stops
serving at the next check; when it was the default, the newest bundle takes
over, with an issue in `GetStatus`.

The gateway compresses nothing itself: brotli comes from the build. Write
`x.br` next to every text file, store those entries and deflate the rest. A
`.br` goes out as is, never decoded to check it, so write it from the same
build; text files without one go out as gzip, which `GetStatus` reports.

```sh
cd build/browser   # the app's build output, before hashing and zipping
find . -type f -size +1k \( -name '*.js' -o -name '*.mjs' -o -name '*.css' -o -name '*.html' -o -name '*.svg' -o -name '*.json' -o -name '*.txt' \) \
  -print0 | xargs -0 -n 1 -P "$(getconf _NPROCESSORS_ONLN)" brotli -q 11 -f
zip -qr -9 -X -D ../4.80.0.zip . -x '*.br' && zip -qr -0 -X -D ../4.80.0.zip . -i '*.br'
```

## Config

The config is stored as `dist/config.pb`, a protobuf message whose contract is
`proto/bundlepilot/config/v1/config.proto`. Edit it through the admin API
(`UpdateConfig`, or a portal such as bundle-pilot-client), or write it as JSON
and import it:

```sh
gateway import config.json [DIST]   # validates, then writes DIST/config.pb
```

The JSON form, as `gateway import` reads it:

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

Every key is optional. `POST /__gateway/data` decides a bundle from the facts
the frontend sends:

1. the first rule, top to bottom, whose `when` entries all match;
2. else `backend`: the entry with the largest key `<=` the `backend` fact, or
   the oldest entry when the fact predates them all;
3. else `default`, or the newest bundle when none is set. A `default` that is
   not on disk keeps the bundle already serving as default (the newest on a
   fresh start).

A bundle that is blocked, not on disk, or needs a newer app than a posted
`native` fact (see Over-the-air updates) is passed over: its rules are skipped,
a backend step hands its backends to the closest older step that serves
(with none, the request is refused), and the default gives way to the closest
older bundle the app runs (`via: native`).

`when` values: a string, number or boolean matches exactly (numbers by their
literal text, so `2020210` equals `"2020210"`; `config.pb` keeps a rule's
numbers as doubles in their shortest form, so post numbers as `JSON.stringify`
writes them, `2020210` rather than `2020210.0`, and write ids of 2^53 or more as
strings: a rule holding such a number is inactive with an issue); a list
matches any element;
`<`, `<=`, `>`, `>=` compare against a version or a date, and
space-separated parts must all hold (`">=2.0.0 <3.0.0"`); `"*"` matches any
present value; `"10%"` matches a stable 10% of the values (hashed, so raising
the number only adds users, and every rule's 10% of a fact is the same users).
A fact of another shape, or a missing fact,
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

`gateway import` rejects unknown keys (`"bundel"`), duplicate keys and
trailing commas with a line and column; it and `UpdateConfig` reject invalid
rules by name. The admin API, like any Connect service, ignores fields it does
not know, so a newer client keeps working with an older gateway. A
`config.pb` that fails validation, cannot be read or goes missing stops
reloads: what serves keeps serving, and `lastError` in `GetStatus`
says why (uploads and deletes still reach the disk, but answer that error);
before any config loaded, a broken one stops the gateway at startup. While `config.pb`
cannot be read, saves are refused rather than written over it, which would
drop the admin account: put the file back, or remove it and restart to run
setup, then restore the rules from the history. Logs and `GetStatus` also
warn about a backend table that gets older as the
backend gets newer, dates far in the future, and rules that repeat an
earlier rule's conditions.

Facts are claimed by the browser, so rules route users, they do not grant
access: anyone can send `storeID` 2020210, just as anyone can use
`?bundle=`. Never rely on a bundle to hide features or data.

`blocked` and `tokens` are kept in `config.pb` but managed through their own
calls, `SetBlocked` and the token calls: `GetConfig` leaves them out, and
`UpdateConfig`, `gateway import` or a restore never changes them.

### Blocking a bundle

`SetBlocked` with `blocked: true` stops serving a bundle at once, without
deleting it: rules that point at it pause, backend steps pass its backends to
the closest older step, cookies and `?bundle=` naming it are ignored, and
`GET /__gateway/bundles/<version>…` answers 404, so over-the-air updaters
cannot install it. Pages already open on it still load its files:
blocking stops routing, not content, so delete a bundle that must disappear.
`blocked: false` lifts the block. The default cannot be blocked
(`failed_precondition`): set another one first.

### History

Every save that changes what `GetConfig` shows is kept in
`dist/history/` (the newest 100, mode 0600), with who saved it and the secrets
masked; the first save also keeps the config from before it. A save that only
changes a password, a token or a block adds nothing. `ListHistory` lists them
and `GetRevision` returns one; restoring is an `UpdateConfig` with that config,
which keeps the live admin account, secrets, tokens and blocked bundles.

### Admin auth and first run

`project_name` and `environment` label the gateway for whoever manages it.
`auth` is the admin account:

```json
{ "project_name": "POS web", "environment": "production",
  "auth": { "username": "admin", "password": "at least 8 characters" } }
```

`password` is write-only: the gateway hashes it (PBKDF2-SHA256, 600 000
rounds, random salt) and stores only `password_hash` in `config.pb`. Reads
return the hash masked as `***`. An `UpdateConfig` or `gateway import` that
sends `***` back, or leaves `auth` out, keeps the stored account; send a new
`password` to rotate it, or `"auth": {}` to remove it. A config without an
account leaves the admin API open, and `GetStatus` lists that as an issue.

`Login` trades the username and password for a token to send as
`Authorization: Bearer <token>`, valid for 12 hours. It is a PASETO v4.local
token whose key derives from the password hash and the username, so changing
either ends every login, while a restart keeps them. `RefreshSession`, sent
with an unexpired login token, returns a new one for another 12 hours, up to 30
days after the login, so an app keeps its session without storing the
password; after that, or once the password changes, it signs in again. At most two password
checks run at once (more get `resource_exhausted`), and failed logins are
logged. An address may send 5 wrong passwords in a row, then one more a minute
(`resource_exhausted` with `Retry-After`); a right one clears its count. Behind
a proxy on a private network, such as Traefik in Docker, the address is the
last `X-Forwarded-For` entry. Rootless Docker or a userland proxy leaves that
header to the client, and behind Cloudflare many users share an edge address.
Behind a proxy, do not also publish the gateway's port: a client reaching it
from a private address could write that header itself. Login limits and the
decision counts live in the process.

API tokens let CI and scripts call the admin API with
`Authorization: Bearer <token>` instead of logging in. Create one with
`CreateToken` `{"name": "ci", "scope": "upload"}`: the answer carries the token
this once, and only its SHA-256 is stored. Scopes: `read` (status, config,
history, decision tester), `upload` (also adds new versions; replacing one
takes `admin`), `admin` (everything). `DeleteToken` revokes one at once.
Tokens wait for an admin password.

Every save, upload, delete, block and token change is logged with who made it
(`by`: the admin's username, `token:<name>`, `setup`, `cli`, or `anonymous`
while no password is set) and the client address, and it lands in the activity
feed with reloads and the bucket sync's fetches and removals (see `ListEvents`).

A gateway that starts without `config.pb` is in setup: `GetInfo` and
`GetStatus` report `setupRequired`, reads stay open, and every call that
changes something needs the `X-Setup-Token` header carrying the token the
gateway prints to its log at startup. `WhoAmI` answers scope `read` until that
header carries the right token, then `admin`, so a setup wizard can check the
token before anything else. Setup ends with the first `UpdateConfig`
that sets `auth`; any other save, a block included, is refused until then, so
setup never leaves the admin API open.

Auth covers the admin API and bundle uploads and deletes;
`GET /__gateway/bundles/*` (for over-the-air clients) and
`POST /__gateway/data` stay open like `/healthz`. Credentials travel in a
header the client sets, never in a cookie, so another site's page cannot use
them; over plain HTTP they travel in clear, so terminate TLS in front of the
admin paths. `CORS_ORIGINS` lists the browser origins that may call the API:
by default the Bundle Pilot app (`capacitor://localhost` on iOS,
`https://localhost` on Android). A web build of the app, or another portal, on
its own host needs its origin added, and the list replaces the default, so keep
the app's two in it. Other origins get no CORS headers, so browsers keep their
pages out of the API.

## Bundle source

`versions/` is filled from wherever `"source"` in the config points. Without
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
`GetStatus` under `issues` (`source s3://bundles/prod/: operation
error S3: ListObjectsV2, ... api error AccessDenied: ...`) and in the log when
the state changes. Editing `"source"`
applies at the next reload without a restart; the first sync of a new bucket
runs before that load, so a fresh cache boots straight from the bucket (once
bundles are serving, that sync gets 20 s and the next poll fetches the rest).

To serve, the gateway only lists and reads: on R2 an API token with
**Object Read only** limited to the bucket; on AWS `s3:ListBucket` on the
bucket and `s3:GetObject` on `<prefix>/*`, granted to the instance role or to
a user whose keys go in the config. Uploads and deletes through the gateway
(the admin API, or CI with an API token) write to the bucket, so for those give
it **Object Read & Write** on R2, or `s3:PutObject` and `s3:DeleteObject` on
`<prefix>/*`. With a read-only token, CI writes to the bucket itself. Keys in
`config.pb` mean keeping that file `0600` (the gateway writes it so), out of
git and out of chat, and keeping `/__gateway/` behind the ingress. With a bucket source the container must be able to write
`versions/`: mount `dist/` read-write. Upload a new zip first and change
`default` or the rules at least one poll later, so every replica already has
the zip when the switch lands. The bucket carries zips only: each replica keeps
its own `config.pb` and history, so save the config to every one, or run one.
`gateway check` syncs the same way before checking.

## Configuration

| Env | Default | Purpose |
|---|---|---|
| `PORT` | `8080` | Listen port |
| `DIST` | `./dist` | Root holding `versions/` and `config.pb`; writable, the admin API writes the config and a bucket source writes `versions/` |
| `LOG_LEVEL` | `info` | `debug` also logs every bundle switch decided by `/data` |
| `BUNDLE_PUBKEY` | off | Ed25519 public key (hex or base64); every bundle must then be signed, see below |
| `CORS_ORIGINS` | `capacitor://localhost,https://localhost` | Comma-separated browser origins that may call `/__gateway/` from another origin. The default lets the Bundle Pilot app in; a web build on its own host, such as `https://portal.example.com`, must be added, and the list replaces the default |

The gateway serves bundles only; route API paths to their backend at the
ingress. On shutdown, requests get 10 seconds to finish before open
connections are closed.

## Admin API

`bundlepilot.gateway.v1.GatewayService`, a ConnectRPC service served under
`/__gateway/`, so a client's base URL is `https://host/__gateway`. Its contract,
every message and field, is `proto/bundlepilot/gateway/v1/gateway.proto`:
generate a client from it (connect-es, connect-go, or any gRPC toolchain), or
call it as JSON over HTTP, one `POST` per call:

```sh
curl https://host/__gateway/bundlepilot.gateway.v1.GatewayService/GetStatus \
  -H 'Content-Type: application/json' -H "Authorization: Bearer $TOKEN" -d '{}'
```

| Call | Scope | Does |
|---|---|---|
| `GetInfo` | none | `{version, setupRequired, passwordSet}`, so an app knows what answers before it signs in. |
| `Login` | none | `{username, password}` answers `{token, expiresAt}`, see Admin auth and first run. |
| `RefreshSession` | login token | A new `{token, expiresAt}` for the unexpired login token sent, see Admin auth and first run; `unauthenticated` for any other token. |
| `WhoAmI` | read | `{by, scope, expiresAt}`: who the token belongs to, as `by` names them in logs, its scope, and when a login token ends. During setup, scope `read` until `X-Setup-Token` is right. |
| `GetStatus` | read | Gateway `version` and `startedAt`, `projectName`, `environment`, `setupRequired` (no `config.pb` yet) and `passwordSet`, default and its source, base path, bundle `source` (`local` or `s3://bucket/prefix/`) with the last `sync` (`at`, `objects`, `error`) for a bucket, `signing` (whether `BUNDLE_PUBKEY` is set), `loadedAt`, reload count and last error, `errors` (a rejected config, refused bundles), memory the process holds from the OS (`residentBytes`; the zips sit in the page cache, which a cgroup limit counts too), bundles with file counts, `zipBytes`, `modTime`, `native`, `blocked` (on the blocked list), `served` (visitors can get it), the zip's `sha256` and `uploadedBy` (who uploaded it, or `s3`, while the feed remembers), decisions counted by `via` (and `refused`) and by bundle since start, `traffic` (decisions per minute over the last hour, oldest first, from `start`: `byBundle` and `byVia`, the latter keyed like the decision counts), load issues. `errors` and `issues` are `{code, text, rule, bundle, reason}`: `code` is stable for apps (`rule_expired`, `bundle_refused`, `no_password`, ...), `text` is for people, and `reason` says why the zip of a `bundle_refused` or `bundle_kept_previous` issue failed to load: `no_index`, `signature`, `damaged`, `too_large`, `too_many_files`, `unsupported` or `bad_native` (its `<meta name="bundle-pilot:native">`); empty for other causes, such as a read error. |
| `Watch` | read | A stream of `{status, events}`: one message at once, then one within a second of an event or a reload, and about every 10 seconds otherwise. `after` resumes events from the last id the app holds. It ends when the server drains, when the login token expires, and when a new config no longer lets the caller in. |
| `ListEvents` | read | The activity feed, oldest first: `{id, at, kind, by, bundle, token, revision}` with `kind` `upload`, `delete`, `block`, `unblock`, `config`, `token_create`, `token_delete`, `reload`, `fetch` or `remove` (the bucket sync, `by` `s3`). A `config` event's `revision` is the history id it saved, for `GetRevision`; it is empty when the save changed nothing history keeps, such as only the password. `after` lists those after an id, else the latest; `limit` is 100 by default, 500 at most, all the feed keeps. |
| `GetConfig` | read | The stored config and its `etag`; the S3 secret and the password hash come back masked as `***`, and `blocked` and `tokens` are left out. |
| `CheckConfig` | read | What `UpdateConfig` would answer, without saving: `error` when the config would be rejected, else its `issues`. |
| `UpdateConfig` | admin | Replace the config, validated against the loaded bundles: `invalid_argument` when rejected, else the new `etag` and `issues` (warnings); the gateway reloads before answering (a reload that fails then shows in `lastError` and is retried). A masked secret or password hash keeps the stored one; `auth.password` is hashed before the file is written. Send the `etag` of the config you edited: a save over a config that changed since answers `aborted` (without one the save goes through). |
| `Decide` | read | The decision tester: `{facts, config}` answers `{bundle, via, why, match, error}` as `/data` would under that config (the live one when `config` is left out). `why` has one `{rule, reason, fact, value, bucket, percent}` per earlier rule: `reason` is `inactive`, `expired`, `no_fact`, `no_match` or `needs_native`, and a percentage condition adds the value's `bucket` (0-99), which matches below `percent`. When a rule decided, `match` has one `{fact, value, bucket, percent}` per condition of it, the bucket again for a percentage (a value the condition lists matches whatever its bucket). No cookie, no counters. |
| `ListHistory`, `GetRevision` | read | The saved configs, newest first, as `{id, at, by}`, the newest `limit` only when set, each with its `config` when `configs` is set; one of them, as `GetConfig` returns a config. |
| `SetBlocked` | admin | Block a loaded bundle, or lift the block: `not_found` when not loaded, `failed_precondition` for the default. See Blocking a bundle. |
| `ListTokens`, `CreateToken`, `DeleteToken` | admin | List API tokens (name, scope, created), create one (its `token`, shown once; `failed_precondition` until an admin password is set) or revoke one. |
| `Reload` | admin | Reload config and bundles now instead of at the watcher's next tick; answers `reloadMs`, or `internal` when the previous snapshot was kept. |

Errors are Connect errors, `{"code": "...", "message": "..."}`, with the HTTP
status Connect gives the code: `invalid_argument` and `failed_precondition`
400, `unauthenticated` 401, `permission_denied` 403, `not_found` 404, `aborted`
409, `resource_exhausted` 429 (`Retry-After` says when to try again),
`internal` 500. Fields are lowerCamelCase in JSON; requests may also use the
proto's snake_case names.

## Endpoints

| Path | Behaviour |
|---|---|
| `<base href>**` | Bundle chosen by `?bundle=`, then cookie `bundle`, then default. When the request is for `index.html` and the browser carries the `bundle_facts` cookie the SDK writes whenever the facts change, the gateway decides from those facts right there (rules, backend table, default) and sets the `bundle` cookie, so a returning visitor gets the right build on the first byte without a `/data` call. `index.html` also carries a `Link` header preloading its module scripts and stylesheets (at most 12), which a CDN can turn into 103 Early Hints. Its relative URLs are relative to the page, climbing back to `<base href>`, so they still hold behind a proxy that mounts the app under a prefix and rewrites only `<base href>`. A file the bundle lacks comes from the newest bundle that has it, pages and `ngsw.json` excepted, so open sessions never 404 after a switch. Navigations (`Accept: text/html`) and paths without an extension fall back to `index.html`. |
| `POST /__gateway/data` | Body: a flat JSON object of facts (`Content-Type: application/json`, at most 4 KiB and 32 facts), e.g. `{"backend":"07.30.2026","storeID":2020210}`. Returns `{"bundle":"4.80.0","via":"rule:store-2020210"}`, where `via` is `rule:<id>`, `backend:<key>`, `default` or `native` (see Over-the-air updates). 400 when no bundle fits, counted as `refused` in the status. A rule or backend decision sets cookie `bundle` for 7 days (refreshed on every call); a default decision clears it. |
| `PUT /__gateway/bundles/<version>.zip` | Upload a bundle: the raw zip as the body (at most 256 MiB), named by its version, `4.81.0.zip` or a date in the configured format. Accepted only when it holds `index.html` and, with `BUNDLE_PUBKEY` set, a valid `bundle.sha256.sig`; 400 for a bad name, 413 when too large, 422 otherwise, with the refusal `reason` (as in `GetStatus` issues) in the problem. Takes the `upload` scope (401 or 403 without it). A zip of the same name is replaced, which an `upload` token may not do (409). With an S3 source the zip goes to the bucket first, then into `versions/`. The gateway reloads before answering `{"version","files","replaced","reload_ms"}`. |
| `GET /__gateway/bundles/<version>.zip` | The zip as uploaded, with ranges and no auth; 404 unless the gateway loaded it and serves it, so a blocked or refused zip cannot be installed. |
| `GET /__gateway/bundles/<version>/<path>` | One file of a bundle exactly as zipped (the raw `index.html`, not the stamped page), with ranges and no auth, no cookies and no `index.html` fallback; 404 while the bundle is blocked. Over-the-air updaters install from it file by file: fetch `bundle.sha256` and `bundle.sha256.sig`, verify the signature with the publisher's public key, and hand the listed files to the updater (Capgo's `download({ manifest })`), which checks each file's SHA-256 and only downloads what it does not already hold. Call `POST /__gateway/data` natively: a WebView's request is cross-origin, and the gateway sends CORS headers only to `CORS_ORIGINS`. |
| `DELETE /__gateway/bundles/<version>.zip` | Remove a bundle from `versions/` and from the bucket, then reload: 204, 404 when absent, 409 for the default bundle (set another default first). Rules that pointed at it turn inactive. Takes the `admin` scope. |
| `/healthz` | 200; 503 once the gateway is stopping, for about 6 s before it drains, so a load balancer or `HEALTHCHECK` stops sending traffic first. `gateway health` checks it from inside the image. |

Errors on these routes are RFC 9457 problems: `application/problem+json` with
`type` `about:blank`, `title` (the status text), `status`, `detail` (why) and
`instance` (the path). A refused upload adds `reason` (`too_large` for a 413).

Cache policy: hashed files are `public, max-age=31536000, immutable`;
everything else is `private, no-cache` with an ETag. Responses carry
`X-Bundle-Version` (the bundle that owns the file served). A CDN in front
caches the hashed files at the edge and passes everything else through; that
is the largest latency win available, the gateway itself spends about 10µs
of CPU per request.

## HTTP/3 and the edge

Terminate HTTP/3 (QUIC) where TLS already ends, at the edge: on Cloudflare,
Speed › Settings › Protocol Optimization › HTTP/3. On Traefik, Caddy or nginx,
enable it on the TLS entry point and publish `443/udp` next to `443/tcp`: with
UDP closed, `Alt-Svc: h3` still goes out and browsers quietly stay on HTTP/2.
The gateway keeps speaking
HTTP/1.1 to it; Cloudflare uses HTTP/2 to an origin only over TLS, so h2c would
not help. With 0-RTT on, Cloudflare sends only GET, HEAD and OPTIONS early,
marked `Early-Data: 1`, so a replay can at most count a decision twice.
Android's native HTTP (CapacitorHttp, the Capgo updater) does not use QUIC;
iOS's URLSession does. Never put Cache Everything or an edge TTL on
`/__gateway/*`, and purge `/__gateway/bundles/<version>/*` after re-uploading a
version.

The proxy's own timeouts still apply: Traefik's entry point `readTimeout` (60 s
by default) cuts uploads that take longer, and a `writeTimeout` cuts slow
downloads, although the gateway extends its deadlines by body size. Raise them
on the entry point that serves the gateway. With the docker provider, Traefik
stops routing to a container once its `HEALTHCHECK` turns unhealthy: on stop
the gateway answers 503 there for about 6 s, then drains for up to 10 s, so
give it `stop_grace_period: 20s` (on Kubernetes, a readiness probe on
`/healthz`, not a liveness probe, which would kill the pod mid-drain, and
`terminationGracePeriodSeconds` of at least 20).

## Signing bundles

With `BUNDLE_PUBKEY` set, every bundle must contain `bundle.sha256`
(`sha256sum` format, one line per file) and `bundle.sha256.sig` (its Ed25519
signature, raw or hex). A bundle whose manifest is missing, unsigned, or lists
a file that changed, is refused and its previous load keeps serving while its
open file is unchanged. A zip changed in place on disk is caught at the next
check, about 5 s, unless its size and mtime were put back; until then its
bytes go out unchecked, and hashed files served in that window are
cached as immutable, so purge the CDN after such an incident. Mobile
apps verify the same signature before installing, so one signature covers the
web and over-the-air updates; write it as hex, which every HTTP client reads
back unchanged. Files under `.well-known/` are served, so they must be listed:
re-sign a bundle whose manifest left them out. Gateways from before this
change kept `versions/*.zip.sig` files; they are unused now and can be deleted.

```sh
openssl genpkey -algorithm ed25519 -out bundle-key.pem           # once, keep private
openssl pkey -in bundle-key.pem -pubout -outform DER | tail -c 32 | xxd -p -c 64   # BUNDLE_PUBKEY
cd build/browser   # the app's build output, brotli sidecars already written
find . \( -name '.*' ! -path . ! -path ./.well-known \) -prune -o -type f ! -name 'bundle.sha256*' -exec sha256sum {} + > bundle.sha256
openssl pkeyutl -sign -inkey /path/to/bundle-key.pem -rawin -in bundle.sha256 | od -An -v -tx1 | tr -d ' \n' > bundle.sha256.sig
zip -qr -9 -X -D ../4.80.0.zip . -x '*.br' && zip -qr -0 -X -D ../4.80.0.zip . -i '*.br'
```

## Checking a deploy

`gateway check [DIST]` loads the dist directory exactly as the server would,
prints every issue and exits 1 when `config.pb` is rejected, a zip is
refused (unreadable, unsigned, or without `index.html` at its root) or there is no bundle. Run it in CI or before
copying a hand edit into place.

## Frontend integration

Use `@bundle-pilot/angular`, one provider for Angular 18 to 22, or
`@bundle-pilot/core` for any framework; see [libs/README.md](../libs/README.md).
By hand, the contract is:

- Every served `index.html` carries `<meta name="bundle-pilot:bundle" content="4.79.0">`
  right after its `<head>` (a page without one is reported as an issue),
  the bundle the page runs, stamped once at load so the page still goes out
  precompressed.
- Before showing a route that depends on the facts, post them to
  `/__gateway/data` and switch when `bundle` differs from the meta tag. Skip
  it when the URL pins a bundle with `?bundle=`, or the pin and the rules keep
  reloading each other.
- Write the facts into the `bundle_facts` cookie (`encodeURIComponent` of the
  JSON, `path=/`) as soon as they change: the gateway decides `index.html`
  from it on the first byte, so returning users land on their bundle without
  a switch.
- Switch with `location.replace` to a URL carrying `ngsw-bypass=<bundle>`. The
  Angular service worker leaves that request to the network, and the landing
  page can tell whether it got the bundle it asked for. When it did not, stay:
  the gateway disagrees (blocked cookies, a pin), and switching again loops.
- A decision kept on the client only holds while the page runs the bundle it
  names, since a service worker may serve an old `index.html`. Revalidate it
  in the background.

Send ids as strings when they can exceed 2^53.

### Over-the-air updates

A bundle that needs a newer native app says so in its `index.html`, which the
signature already covers:

```html
<meta name="bundle-pilot:native" content=">=4.102.0">
```

The content takes comparisons with `MAJOR.MINOR.PATCH` app versions that must
all hold (`">=4.102.0 <5.0.0"`); a pre-release such as `4.102.0-prod` sorts
below its release, so write plain versions. A zip whose tag does not parse is
refused. Apps post their version as the `native` fact (Capacitor:
`App.getInfo().version`). Rules then skip a bundle the app cannot run, backend
steps fall back to an older one, and the default gives way to the closest older
bundle that runs, with `via: native`; when none does, `/data` answers 400 and
the app keeps the bundle it has. Requests without `native`, such as the web's,
are unaffected. Runtime config files such as
`environments/env.js` are served from the selected bundle; keep one copy per
version, since a bundle without one gets another bundle's copy.

## Structure

```
cmd/gateway                    wiring: env -> bundlefs -> usecase -> handler -> http.Server; check, import
proto/                         the config and admin API contracts (buf.yaml, buf.gen.yaml at the root)
internal/gen                   Go generated from proto/ (configv1, gatewayv1 with its Connect handler), committed
internal/env                   environment variables: PORT, DIST, LOG_LEVEL, BUNDLE_PUBKEY, CORS_ORIGINS
internal/domain                Bundle, Snapshot, Config (rules, backend table): selection, no I/O
internal/usecase               Catalog: reload, watch, atomic snapshot swap (declares the Source port)
internal/infrastructure        bundlefs (zip loader, in-place serving, config history), mirror (bucket sync, s3 strategy)
internal/handler               HTTP routes, the admin API (Connect), auth and login limits, CORS, cookies, ngsw.json stamping
internal/pkg                   asset (serving from zips and memory: encodings, ranges, ETags), version (comparison)
```

## Run

```
make dev          # go run ./cmd/gateway
make build && bin/gateway check dist
make gen          # regenerate Go from proto/ after editing a contract
make lint         # golangci-lint, including layer rules
docker compose up --build
```
