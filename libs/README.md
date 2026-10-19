# Bundle Pilot SDK

Client packages for apps served by the gateway: `angular` is one provider,
`core` the same logic for any framework.

```
pnpm install && pnpm build && pnpm test   # in libs/
```

Both are published with the gateway, at the version of its release tag, so
SDK 1.4.0 goes with gateway 1.4.0. The `0.0.0` in each `package.json` is a
placeholder that the release replaces: don't bump it by hand.

## @bundle-pilot/angular

Angular 18 to 22, standalone or NgModule (put it in the root providers):

```ts
provideBundlePilot({
  facts: () => {
    const store = inject(StoreService);
    return store.id ? { storeID: store.id, backend: store.posVersion } : null; // null while signed out
  },
  beforeSwitch: () => inject(SplashService).show(), // optional
}),
```

Every route is guarded, lazy children included. A navigation whose facts call
for another bundle becomes a page load of that bundle at the same URL; any
other goes through, synchronously when the decision is already known.

| Option | Default | |
|---|---|---|
| `facts` | required | runs in the guard, so `inject()` works; null or empty skips |
| `beforeSwitch` | none | runs before the page leaves, in an injection context; awaited, then two frames to paint |
| `timeout` | 3000 ms | for a decision a navigation waits on; past it, the page stays |
| `endpoint` | `/__gateway/data` | |

## @bundle-pilot/core

```ts
import { createPilot } from '@bundle-pilot/core';

const pilot = createPilot({ beforeSwitch });
// before showing a route: true to go on, false while the page leaves for another bundle
const go = await pilot.check({ storeID, backend }, '/next');
```

`bundleVersion()` returns the version the gateway stamped into `index.html`.

## How it decides

- One `POST /__gateway/data` per set of facts per page, shared by every call.
  A failure fails open and is not retried on that page.
- The facts go into the `bundle_facts` cookie as soon as they change, so the
  gateway serves the right `index.html` on the first byte of the next visit.
- The last decision is kept in localStorage. While it names the running
  bundle, `check` returns `true` at once and revalidates in the background; a
  different answer switches at the next navigation.
- A switch is `location.replace(url?ngsw-bypass=<bundle>)`: it skips the
  service worker, and the page that lands checks it got that bundle. If not,
  the gateway disagreed (blocked cookies, a pin) and it stays rather than loop.
- Nothing happens without the gateway's meta tag (dev server), with a
  `?bundle=` pin, without facts, or on the server. A cancelled or redirected
  navigation never switches, nor does a page whose cookies are blocked.

## Caveats

- A bypassed navigation skips the service worker's update check, and files it
  caches without a hash in their name come from the old version until it
  updates. Call `SwUpdate.checkForUpdate()`, without awaiting, in
  `beforeSwitch` to update it sooner.
- `router.resetConfig` after startup drops the guards.
- Cancelling a `beforeunload` prompt during a switch leaves the splash up.
- Tabs with different facts flip the gateway's cookies at every switch.
