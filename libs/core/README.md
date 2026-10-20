<p align="center">
  <img src="https://raw.githubusercontent.com/zenkiet/bundle-pilot/main/frontend/src/lib/assets/favicon.svg" alt="Bundle Pilot" width="96">
</p>

<h1 align="center">@bundle-pilot/core</h1>

<p align="center">
  Run the frontend bundle <a href="https://github.com/zenkiet/bundle-pilot">Bundle Pilot</a> picks for each visitor, in any framework.
</p>

<p align="center">
  <a href="https://www.npmjs.com/package/@bundle-pilot/core"><img src="https://img.shields.io/npm/v/@bundle-pilot/core?color=0f2a3a&label=npm" alt="npm version"></a>
  <a href="https://www.npmjs.com/package/@bundle-pilot/core"><img src="https://img.shields.io/npm/dm/@bundle-pilot/core?color=f5b400" alt="npm downloads"></a>
  <a href="https://bundlephobia.com/package/@bundle-pilot/core"><img src="https://img.shields.io/badge/min%2Bgzip-1.1%20kB-3fb950" alt="1.1 kB min+gzip"></a>
  <img src="https://img.shields.io/badge/dependencies-0-3fb950" alt="zero dependencies">
  <img src="https://img.shields.io/badge/types-TypeScript-3178C6?logo=typescript&logoColor=white" alt="TypeScript">
  <a href="https://github.com/zenkiet/bundle-pilot/blob/main/libs/core/package.json"><img src="https://img.shields.io/npm/l/@bundle-pilot/core?color=0f2a3a" alt="license"></a>
</p>

<p align="center">
  <a href="#install">Install</a> ·
  <a href="#quick-start">Quick start</a> ·
  <a href="#use-cases">Use cases</a> ·
  <a href="#examples">Examples</a> ·
  <a href="#api">API</a> ·
  <a href="#how-it-decides">How it decides</a> ·
  <a href="#caveats">Caveats</a>
</p>

---

The Bundle Pilot gateway serves several builds of one app behind the same URL
and decides, from a few facts about the visitor, which build each one runs.
This package is the client half. It posts those facts, and when the gateway
picks another bundle, it moves the page there: once, without loops, and without
slowing down the navigations whose answer is already known.

- **Tiny:** about 1 kB minified and gzipped, no dependencies, no framework.
- **Fast:** one request per set of facts per page. A known answer comes back
  synchronously, and is revalidated in the background.
- **Lands right the first time:** the facts go into a cookie, so the next
  visit gets the right `index.html` on the first byte, with no switch at all.
- **Safe:** it checks that the page landed on the bundle it asked for, never
  loops, and lets every navigation through when the gateway cannot be reached.

> Using Angular? [`@bundle-pilot/angular`](https://www.npmjs.com/package/@bundle-pilot/angular)
> wires all of this into the router with one provider. Building a Capacitor app?
> See [`@bundle-pilot/capacitor`](https://www.npmjs.com/package/@bundle-pilot/capacitor).

## Install

```sh
npm install @bundle-pilot/core
# or
pnpm add @bundle-pilot/core
yarn add @bundle-pilot/core
```

The page must be served by a Bundle Pilot gateway, which stamps
`<meta name="bundle-pilot:bundle">` into every `index.html`. Without that tag,
on your dev server for example, the pilot does nothing and every check passes.

## Quick start

```ts
import { createPilot } from '@bundle-pilot/core';

const pilot = createPilot({
  beforeSwitch: () => document.body.classList.add('is-switching'), // optional: show a splash
});

// Before rendering a route that depends on who the visitor is:
const facts = { storeID: session.storeId, backend: session.apiVersion };
if (await pilot.check(facts, '/orders')) {
  render('/orders'); // false means the page is already leaving for the right bundle
}
```

`check` returns `true` to go on, or `false` while the page leaves for the
bundle the gateway picked. It answers synchronously whenever the decision is
already known, so `await` costs nothing on the common path.

## Use cases

Your app always sends the same facts. The gateway's rules decide what they
mean, and you change those rules without shipping code:

| Goal | Facts the app sends | Rule on the gateway |
|---|---|---|
| Pilot a new build with a few stores | `{ storeID }` | `{ "when": { "storeID": ["2020210", "2020215"] }, "bundle": "4.80.0" }` |
| Roll out to a share of users, stable per user | `{ user }` | `{ "when": { "user": "10%" }, "bundle": "4.80.0" }` |
| Give each backend the newest frontend it supports | `{ backend }` | backend table: `{ "4.78.0": "4.78.0", "4.80.0": "4.80.0" }` |
| Target a version range, until a date | `{ appVersion }` | `{ "when": { "appVersion": ">=2.0.0 <3.0.0" }, "bundle": "4.80.0", "until": "2026-12-31" }` |
| Back out at once | anything | block `4.80.0` in the admin UI: every rule pointing at it pauses |

Raising a percentage only adds people, because each user lands in a stable
bucket. The full rule syntax is in the
[gateway reference](https://github.com/zenkiet/bundle-pilot/blob/main/docs/reference.md#config).

## Examples

### Vue Router

```ts
import { createPilot } from '@bundle-pilot/core';

const pilot = createPilot({ beforeSwitch: () => splash.show() });

router.beforeEach((to) => pilot.check(currentFacts(), router.resolve(to).href));
```

Returning `false` cancels the navigation while the page reloads into the other
bundle.

### Any router, or none

```ts
async function navigate(path: string) {
  if (!(await pilot.check(currentFacts(), path))) return; // leaving for another bundle
  renderRoute(path);
}
```

### Signed out and public pages

Return `null`, or leave every fact empty, while the facts are unknown. The
check passes at once, and nothing is sent:

```ts
const currentFacts = () => (session.storeId ? { storeID: session.storeId } : null);
```

### Ignoring a stale navigation

Pass `alive` when navigations can be cancelled or replaced while the answer is
on its way. The pilot will not switch for a navigation that is no longer
current:

```ts
const id = ++navigationId;
await pilot.check(currentFacts(), path, () => id === navigationId);
```

## API

### `createPilot(options?)`

Returns a `Pilot`. Create it once, at startup.

| Option | Type | Default | |
|---|---|---|---|
| `endpoint` | `string` | `'/__gateway/data'` | where the facts are posted |
| `timeout` | `number` | `3000` | ms a waiting navigation gives the gateway; past it, the page stays |
| `beforeSwitch` | `() => unknown` | none | runs before the page leaves, and is awaited (its errors are ignored); then two frames to paint |

### `pilot.check(facts, url?, alive?)`

| Parameter | | |
|---|---|---|
| `facts` | `Record<string, string \| number \| boolean \| null \| undefined>` | what the gateway decides on; `null` and empty values are dropped, and no facts at all means "go on" |
| `url` | `string` | where to land in the other bundle; defaults to the current URL |
| `alive` | `() => boolean` | return `false` to veto a switch for a navigation that ended meanwhile |

Returns `true` to go on, or `false` while the page leaves. It is synchronous
unless the answer is still unknown.

### Also exported

| | |
|---|---|
| `pilot.version` | the bundle this page runs; `''` when the gateway did not serve it |
| `bundleVersion()` | the same, read from the meta tag |
| `factsKey(facts)` | the canonical form of a set of facts: empty values dropped, keys sorted |
| `Facts`, `Pilot`, `PilotOptions` | types |

## How it decides

1. **One question per set of facts per page.** Every `check` with the same
   facts shares one `POST /__gateway/data`. A failure lets the page go on, and
   is not retried on that page.
2. **Known answers are instant.** The last decision is kept in
   `localStorage`. While it names the running bundle, `check` returns `true`
   at once and asks again in the background. A different answer switches at
   the next navigation.
3. **The first byte is already right.** As soon as the facts change, they are
   written to the `bundle_facts` cookie. The gateway reads it, so a returning
   visitor gets their bundle's `index.html` without a switch.
4. **Switching never loops.** A switch is `location.replace(url?ngsw-bypass=<bundle>)`,
   which also skips the Angular service worker. The page that lands checks that
   it got the bundle it asked for. If not, the gateway disagreed (blocked
   cookies, a `?bundle=` pin), and the page stays instead of trying again.
5. **When it does nothing:**
   - without the gateway's meta tag;
   - with a `?bundle=` pin in the URL;
   - without facts;
   - on the server;
   - for a navigation that was cancelled;
   - when cookies are blocked.

## Caveats

- A navigation that bypasses the service worker also skips its update check,
  so files it caches without a hash in their name come from the old version
  until it updates. Trigger the update yourself in `beforeSwitch`.
- If a `beforeunload` prompt is cancelled during a switch, a splash shown in
  `beforeSwitch` stays up.
- Tabs that send different facts flip the cookie at every switch. Keep the
  facts per user, not per tab.

## Compatibility

Every package is released together with the gateway, at the gateway's version:
`@bundle-pilot/core` 1.4.0 goes with gateway 1.4.0. It runs in any browser with
`fetch` and `URL`. `localStorage` and `AbortSignal.timeout` are used when they
exist.

## License

[MIT](https://github.com/zenkiet/bundle-pilot/blob/main/libs/core/package.json).
Part of [Bundle Pilot](https://github.com/zenkiet/bundle-pilot).
