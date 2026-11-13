<p align="center">
  <img src="docs/logo.svg" alt="Bundle Pilot" width="200">
</p>

<h1 align="center">Bundle Pilot</h1>

<p align="center">
  Ship a new frontend to some of your users before all of them.<br>
  One URL, several builds side by side, and you decide who runs which.
</p>

<p align="center">
  <a href="LICENSE"><img src="https://img.shields.io/github/license/zenkiet/bundle-pilot?color=0f2a3a" alt="License"></a>
  <a href="https://github.com/zenkiet/bundle-pilot/releases"><img src="https://img.shields.io/github/v/release/zenkiet/bundle-pilot?color=f5b400" alt="Release"></a>
  <a href="https://go.dev"><img src="https://img.shields.io/badge/Go-1.27-00ADD8?logo=go&logoColor=white" alt="Go"></a>
  <a href="proto/bundlepilot/gateway/v1/gateway.proto"><img src="https://img.shields.io/badge/Admin%20API-ConnectRPC-5B5BD6" alt="ConnectRPC admin API"></a>
  <a href="libs/README.md"><img src="https://img.shields.io/badge/SDK-Angular%2018%2B-DD0031?logo=angular&logoColor=white" alt="Angular SDK"></a>
  <a href="https://github.com/zenkiet/bundle-pilot/pkgs/container/bundle-pilot"><img src="https://img.shields.io/badge/ghcr.io-zenkiet%2Fbundle--pilot-2496ED?logo=docker&logoColor=white" alt="Container image"></a>
  <a href="https://github.com/zenkiet/bundle-pilot/issues"><img src="https://img.shields.io/badge/PRs-welcome-3fb950" alt="PRs welcome"></a>
</p>

<p align="center">
  <a href="#why-bundle-pilot">Why</a> ·
  <a href="#how-it-works">How it works</a> ·
  <a href="#what-you-get">What you get</a> ·
  <a href="#quick-start">Quick start</a> ·
  <a href="#a-rollout-step-by-step">A rollout</a> ·
  <a href="#client-libraries">Client libraries</a> ·
  <a href="#admin-api">Admin API</a> ·
  <a href="#documentation">Documentation</a> ·
  <a href="#building-from-source">Building from source</a>
</p>

## Why Bundle Pilot

Many teams serve one web app to many customers: stores, tenants, branches,
partners. The app grows old, a big upgrade gets built, and then comes the
hard part. Shipping it means every user gets the new build at the same
moment, on the same day, whether their backend is ready or not. Not shipping
it means the upgrade waits, sometimes for years, because nobody wants to bet
the whole user base on one deploy.

Bundle Pilot removes that bet. It keeps every build you still need, old and
new, behind the same URL, and routes each visitor to one of them. A handful
of stores run the new build this week. If they are happy, more stores follow.
If something breaks, one edit sends them back to the old build, without a
redeploy and without touching the other users. The rollout becomes a dial
you turn, not a switch you flip.

The project was born from exactly that situation: a company whose clients all
ran one aging frontend, an upgrade ready to go, and a team that was not
willing to put 100% of the users on it on day one.

## How it works

<p align="center">
  <img src="docs/how-it-works.svg" alt="Users send a few facts to Bundle Pilot, which hands most of them the stable bundle and a chosen group the new one" width="920">
</p>

Every build is a zip in a folder or a bucket. Each time the app starts, it
sends Bundle Pilot a few facts about the visitor: which backend it talks to,
which store or user it is. Bundle Pilot answers with one build and the
browser reloads once if it is running a different one. The answer is
remembered in a cookie, so a user stays on the same build until you change
the rules.

```mermaid
flowchart LR
    U[Browser opens the app] --> F[App posts a few facts<br>backend, store, user]
    F --> P{Bundle Pilot}
    P -->|a rule matches| N[4.80.0<br>the pilot group]
    P -->|backend table| B[4.79.0<br>fits that backend version]
    P -->|otherwise| D[4.78.0<br>default]
    N --> C[Cookie remembers the choice<br>the app reloads once if it differs]
    B --> C
    D --> C
```

Three things decide, in this order:

| Priority | Decision | What it does |
|:---:|---|---|
| 1 | **Rules** | The first rule whose conditions all match. Exact values, lists, version ranges, a stable percentage of users, with an optional expiry date. |
| 2 | **Backend table** | The newest build that fits the backend version the app reported, so a frontend never runs ahead of its API; a backend older than every entry gets the oldest. |
| 3 | **Default** | What everyone else gets. |

## What you get

| Capability | What it means |
|---|---|
| **Builds side by side** | Old and new versions serve at once; files resolve across all of them, so a session that was open during a switch never breaks. |
| **Targeting without code** | Pick users by store, tenant, user id, app version, backend version, or a percentage that only grows as you raise it. |
| **A headless admin API** | ConnectRPC with a protobuf contract, so a portal, a script or CI can upload zips, edit rules, test a decision before saving, watch what is being served, block a bad build and restore any earlier config, on one gateway or many. |
| **Safe for a team** | Saves never overwrite a newer one, every save is kept with who made it, and CI gets scoped API tokens instead of the admin password. |
| **Over-the-air aware** | A build can require a minimum native app version; older apps keep the closest build they can run. |
| **A first run that sets itself up** | The gateway prints a setup token; the first config saved with it sets the admin password. Logins then get a 12-hour token, CI a scoped API token. |
| **Bundles from a folder or a bucket** | Point it at a directory, or at S3, Cloudflare R2, MinIO and the like, and it mirrors the zips by itself. |
| **Signed bundles** | Optionally, so only builds signed with your key can serve. |
| **One small binary** | No database: zips are served in place from the page cache, a few MB of RAM whatever the number of versions, roughly ten microseconds of CPU per request. Docker images for amd64 and arm64, binaries for macOS, Windows and Linux. |

## Quick start

### Run with Docker

Put each build in a zip named by its version, with `index.html` at the root
of the zip, and start the gateway on that folder:

```sh
mkdir -p dist/versions
cp 4.78.0.zip 4.80.0.zip dist/versions/
docker run -p 8080:8080 -v "$PWD/dist:/app/dist" ghcr.io/zenkiet/bundle-pilot
```

The app is served at `http://localhost:8080/` right away, the newest build as
the default until you say otherwise. The admin API waits for setup: the
gateway prints a setup token to its log (`docker logs`). Finish setup from a
portal such as bundle-pilot-client, or save a first config with that token:

```sh
curl http://localhost:8080/__gateway/bundlepilot.gateway.v1.GatewayService/UpdateConfig \
  -H 'Content-Type: application/json' -H 'X-Setup-Token: <token from the log>' \
  -d '{"config": {"project_name": "shop", "auth": {"username": "admin", "password": "a long password"}}}'
```

### Or run the binary

Prefer a binary? Download one from the
[releases](https://github.com/zenkiet/bundle-pilot/releases) and run
`DIST=./dist ./bundle-pilot`. Everything on disk is the `versions/` folder,
one `config.pb` written through the admin API, and the `history/` of its saves.

### Connect your app

Then teach the app to ask. For Angular, one provider does it; for any other
framework, `@bundle-pilot/core`. See [Client libraries](#client-libraries).

## A rollout, step by step

1. **Upload the new build.** Upload `4.80.0.zip` from the portal, or `PUT`
   it to the gateway from CI. Nobody sees it yet: the default is still `4.78.0`.
2. **Pick the pilot group.** Add a rule: store `2020210` gets `4.80.0`. Save.
   The next time that store opens the app, it reloads into the new build.
3. **Widen.** Change the rule to a list of stores, then to `10%` of users, then
   `50%`. The percentage is stable per user, so raising it only adds people.
4. **Finish or back out.** Make `4.80.0` the default and delete the rule, or,
   if something is wrong, block `4.80.0`: every rule pointing at
   it pauses at once and the pilot group is back on `4.78.0` at their next
   page. Unblock it once fixed, or restore an earlier config from the history.

The same config, written as JSON:

```json
{
  "default": "4.78.0",
  "rules": [
    { "id": "pilot-stores", "when": { "storeID": [2020210, 2020215] }, "bundle": "4.80.0" },
    { "id": "ten-percent",  "when": { "user": "10%" },                  "bundle": "4.80.0" }
  ]
}
```

## Client libraries

The app has to send its facts and reload when the answer changes. The
`libs/` folder ships that logic as packages:

| Package | For | Status |
|---|---|---|
| `@bundle-pilot/angular` | Angular 18 to 22, one `provideBundlePilot()` call | Ready |
| `@bundle-pilot/core` | Any framework or plain JavaScript, the same logic without Angular | Ready |
| `@bundle-pilot/capacitor` | Capacitor apps: signed over-the-air updates with Capgo | Ready |
| React and Vue adapters | Thin wrappers over `core` | Planned |

```ts
// app.config.ts: every route now follows the gateway's pick for these facts
provideBundlePilot({
  facts: () => {
    const store = inject(StoreService);
    return store.id ? { storeID: store.id, backend: store.posVersion } : null;
  },
});
```

Setup and options are in [libs/README.md](libs/README.md).

## Admin API

The gateway has no UI of its own: bundle-pilot-client, a separate project, is
the portal that manages one gateway or many. Everything it does goes through
`bundlepilot.gateway.v1.GatewayService`, a ConnectRPC service under
`/__gateway/` whose contract is
[gateway.proto](proto/bundlepilot/gateway/v1/gateway.proto). Generate a client
from it in any language, or call it as JSON over HTTP:

```sh
TOKEN=$(curl -s http://localhost:8080/__gateway/bundlepilot.gateway.v1.GatewayService/Login \
  -H 'Content-Type: application/json' -d '{"username": "admin", "password": "a long password"}' | jq -r .token)
curl http://localhost:8080/__gateway/bundlepilot.gateway.v1.GatewayService/GetStatus \
  -H 'Content-Type: application/json' -H "Authorization: Bearer $TOKEN" -d '{}'
```

The Bundle Pilot app's iOS and Android origins may call it by default; a web
build on its own host needs its origin in `CORS_ORIGINS`, next to those two.
`Watch` streams the status and the activity feed, so the app needs no polling.

## Documentation

| Resource | Covers |
|---|---|
| [docs/reference.md](docs/reference.md) | The config contract, rule syntax, bundle sources, every endpoint, environment variables, signing and deployment checks. |
| [libs/README.md](libs/README.md) | The client packages. |
| `proto/bundlepilot/config/v1/config.proto` | The config as a protobuf contract. |
| `proto/bundlepilot/gateway/v1/gateway.proto` | The admin API as a ConnectRPC contract. |

## Building from source

Requires Go 1.27; Node 26 and pnpm only for the libraries in `libs/`.

```sh
make build          # bin/gateway
make gen            # regenerate Go from proto/
make lint           # golangci-lint, including layer rules
docker compose up --build
```

Jenkins releases every `vX.Y.Z` tag at that one version: archives for macOS
(Intel and Apple silicon), Windows and Linux, a multi-arch image on
`ghcr.io/zenkiet/bundle-pilot`, and the npm packages; see the
[Jenkinsfile](Jenkinsfile).

## Contributing

Bug reports, feature requests and pull requests are welcome. Please open an issue before starting large changes so the design can be agreed first; every user-facing change in Bundle Pilot starts as a mockup.

## License

Bundle Pilot is free software, released under the [Apache-2.0](LICENSE).

<p align="center"><sub>© 2026 ZenSoftware. All rights reserved.</sub></p>
