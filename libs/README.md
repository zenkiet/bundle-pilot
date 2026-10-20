<p align="center">
  <img src="../frontend/src/lib/assets/favicon.svg" alt="Bundle Pilot" width="120">
</p>

<h1 align="center">Bundle Pilot SDK</h1>

<p align="center">
  Client packages that run each visitor on the frontend bundle the <a href="../README.md">Bundle Pilot</a> gateway picks,<br>
  on the web and in native apps.
</p>

<p align="center">
  <a href="https://www.npmjs.com/package/@bundle-pilot/core"><img src="https://img.shields.io/npm/v/@bundle-pilot/core?color=0f2a3a&label=%40bundle-pilot%2Fcore" alt="@bundle-pilot/core on npm"></a>
  <a href="https://www.npmjs.com/package/@bundle-pilot/angular"><img src="https://img.shields.io/npm/v/@bundle-pilot/angular?color=DD0031&label=%40bundle-pilot%2Fangular" alt="@bundle-pilot/angular on npm"></a>
  <a href="https://www.npmjs.com/package/@bundle-pilot/capacitor"><img src="https://img.shields.io/npm/v/@bundle-pilot/capacitor?color=119EFF&label=%40bundle-pilot%2Fcapacitor" alt="@bundle-pilot/capacitor on npm"></a>
  <img src="https://img.shields.io/badge/types-TypeScript-3178C6?logo=typescript&logoColor=white" alt="TypeScript">
</p>

---

## Packages

| Package | For | Size (min+gzip) |
|---|---|---|
| [`@bundle-pilot/angular`](angular/README.md) | Angular 18 to 22: one `provideBundlePilot()` guards every route | 0.5 kB + core |
| [`@bundle-pilot/core`](core/README.md) | Any framework or none: `createPilot().check(facts)` | 1.1 kB, no dependencies |
| [`@bundle-pilot/capacitor`](capacitor/README.md) | Capacitor 8 apps: signed over-the-air updates with Capgo | 1.9 kB + core |

### Which one do I need?

```mermaid
flowchart LR
    A{Where does the app run?} -->|browser| B{Angular?}
    A -->|Capacitor app| C["@bundle-pilot/capacitor<br>+ @bundle-pilot/angular, or core"]
    B -->|yes| D["@bundle-pilot/angular"]
    B -->|no| E["@bundle-pilot/core"]
```

- **An Angular web app:** `@bundle-pilot/angular`. That is one provider and nothing else.
- **React, Vue, Svelte or plain JavaScript:** `@bundle-pilot/core`. Call `check`
  from your router's guard or hook.
- **A Capacitor app:** `@bundle-pilot/capacitor`. With Angular, pass its pilot
  to `provideBundlePilot`, so the web build and the native app share the same code.

## In 30 seconds

```ts
// Angular, web
provideBundlePilot({
  facts: () => {
    const store = inject(StoreService);
    return store.id ? { storeID: store.id, backend: store.posVersion } : null;
  },
});
```

```ts
// Angular, Capacitor: the same provider, with the over-the-air pilot
provideBundlePilot({
  facts,
  pilot: () => createOtaPilot({ gateway: 'https://ota.example.com', publicKey, version }),
});
```

```ts
// Any framework
const pilot = createPilot();
router.beforeEach((to) => pilot.check(currentFacts(), router.resolve(to).href));
```

## How it fits together

```mermaid
sequenceDiagram
    participant App as App (any package)
    participant GW as Bundle Pilot gateway
    App->>GW: POST /__gateway/data { storeID, backend, native? }
    GW-->>App: { bundle: "4.80.0", via: "rule:pilot-stores" }
    alt bundle is the one running
        App->>App: go on (and answer at once next time)
    else web: another bundle
        App->>GW: location.replace(url?ngsw-bypass=4.80.0)
        GW-->>App: index.html of 4.80.0
    else native: another bundle
        App->>GW: GET bundle.sha256 + .sig, then the files
        App->>App: verify Ed25519, install with Capgo, restart
    end
```

Your app sends facts, and the gateway's rules decide. You pilot a build with
some stores, raise a percentage, or back out of a release from the admin UI,
with no new deploy of the app.

## Versioning

All packages are released together with the gateway, from one `vX.Y.Z` tag, so
`@bundle-pilot/*` 1.4.0 goes with gateway 1.4.0. The `0.0.0` in each
`package.json` is a placeholder that the release replaces: don't bump it by hand.

## Development

```sh
pnpm install
pnpm build   # tsc, every package (core first)
pnpm test    # node --test
```

Each package's README is the page npm shows. Keep its links and images
absolute.
