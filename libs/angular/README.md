<p align="center">
  <img src="https://raw.githubusercontent.com/zenkiet/bundle-pilot/main/frontend/src/lib/assets/favicon.svg" alt="Bundle Pilot" width="96">
</p>

<h1 align="center">@bundle-pilot/angular</h1>

<p align="center">
  One provider: every route of your Angular app runs the bundle <a href="https://github.com/zenkiet/bundle-pilot">Bundle Pilot</a> picks.
</p>

<p align="center">
  <a href="https://www.npmjs.com/package/@bundle-pilot/angular"><img src="https://img.shields.io/npm/v/@bundle-pilot/angular?color=0f2a3a&label=npm" alt="npm version"></a>
  <a href="https://www.npmjs.com/package/@bundle-pilot/angular"><img src="https://img.shields.io/npm/dm/@bundle-pilot/angular?color=f5b400" alt="npm downloads"></a>
  <img src="https://img.shields.io/badge/Angular-18%20to%2022-DD0031?logo=angular&logoColor=white" alt="Angular 18 to 22">
  <img src="https://img.shields.io/badge/min%2Bgzip-0.5%20kB%20%2B%20core-3fb950" alt="0.5 kB min+gzip plus core">
  <img src="https://img.shields.io/badge/SSR-safe-3fb950" alt="SSR safe">
  <a href="https://github.com/zenkiet/bundle-pilot/blob/main/libs/angular/package.json"><img src="https://img.shields.io/npm/l/@bundle-pilot/angular?color=0f2a3a" alt="license"></a>
</p>

<p align="center">
  <a href="#install">Install</a> ·
  <a href="#quick-start">Quick start</a> ·
  <a href="#use-cases">Use cases</a> ·
  <a href="#options">Options</a> ·
  <a href="#how-it-works">How it works</a> ·
  <a href="#caveats">Caveats</a>
</p>

---

Ship a new frontend to some of your users before all of them. The Bundle Pilot
gateway keeps every build you still need behind the same URL, and decides from
a few facts (a store, a tenant, a backend version) which one each visitor runs.
`provideBundlePilot()` connects your app to it:

- **Every route is guarded,** lazy children included, with no change to your
  route table.
- **Navigations stay instant.** A known decision is answered synchronously;
  only a navigation whose facts call for another bundle waits, and then becomes
  a page load of that bundle at the same URL.
- **Your services work as usual.** `facts` and `beforeSwitch` run in an
  injection context, so `inject()` just works.
- **Standalone or NgModule, browser or SSR.** On the server it does nothing.

Built on [`@bundle-pilot/core`](https://www.npmjs.com/package/@bundle-pilot/core),
which is installed with it.

## Install

```sh
npm install @bundle-pilot/angular
```

Angular 18 to 22 (`@angular/core`, `@angular/common` and `@angular/router`).

## Quick start

Add the provider to your root providers:

```ts
// app.config.ts
import { ApplicationConfig, inject } from '@angular/core';
import { provideRouter } from '@angular/router';
import { provideBundlePilot } from '@bundle-pilot/angular';

export const appConfig: ApplicationConfig = {
  providers: [
    provideRouter(routes),
    provideBundlePilot({
      facts: () => {
        const store = inject(StoreService);
        return store.id ? { storeID: store.id, backend: store.posVersion } : null; // null while signed out
      },
      beforeSwitch: () => inject(SplashService).show(),
    }),
  ],
};
```

With NgModules, put the same call in `AppModule`'s `providers`.

That is the whole integration. Now open the gateway's admin UI, add a rule
such as *store `2020210` gets `4.80.0`*, and that store moves to the new build
at its next navigation.

## Use cases

### Pilot a release with a few customers

Send whatever identifies the customer, then target them from the gateway: a
list of stores, a percentage of users, a version range, with an optional
expiry date. Widening the rollout, or backing out of it, needs no deploy.

```ts
provideBundlePilot({
  facts: () => {
    const { tenant, user } = inject(SessionService);
    return tenant ? { tenant: tenant.id, user: user.id } : null;
  },
});
```

### Keep the frontend in step with each backend

When customers run different backend versions, send the version as `backend`.
The gateway's backend table gives each one the newest frontend it supports,
so a frontend never runs ahead of its API.

```ts
facts: () => {
  const api = inject(ApiInfoService);
  return api.version ? { backend: api.version, storeID: api.storeId } : null;
},
```

### A splash, and the Angular service worker

`beforeSwitch` runs just before the page leaves, and is awaited. Use it to show
a loading screen, and to start the service worker's update check, which a
switch skips:

```ts
import { SwUpdate } from '@angular/service-worker';

provideBundlePilot({
  facts,
  beforeSwitch: () => {
    const sw = inject(SwUpdate);
    if (sw.isEnabled) void sw.checkForUpdate(); // don't await: the page is leaving
    inject(SplashService).show();
  },
});
```

### A gateway behind a path prefix

```ts
provideBundlePilot({ facts, endpoint: '/app/__gateway/data', timeout: 5000 });
```

### Native apps with over-the-air updates

In a Capacitor app the page runs from local files, so a reload cannot change
the bundle. Hand the provider the over-the-air pilot from
[`@bundle-pilot/capacitor`](https://www.npmjs.com/package/@bundle-pilot/capacitor),
which downloads, verifies and installs the bundle instead:

```ts
import { createOtaPilot } from '@bundle-pilot/capacitor';

provideBundlePilot({
  facts,
  pilot: () =>
    createOtaPilot({
      gateway: 'https://ota.example.com',
      publicKey: environment.bundleKey,
      version: environment.version,
    }),
});
```

## Options

| Option | Type | Default | |
|---|---|---|---|
| `facts` | `() => Facts \| null \| undefined` | required | what the gateway decides on. It runs in the guard, so `inject()` works. `null` or empty values skip the check |
| `beforeSwitch` | `() => unknown` | none | runs in an injection context before the page leaves, and is awaited; then two frames to paint |
| `timeout` | `number` | `3000` | ms a waiting navigation gives the gateway; past it, the page stays |
| `endpoint` | `string` | `'/__gateway/data'` | where the facts are posted |
| `pilot` | `() => Pilot` | the web pilot | another pilot to guard with, such as `createOtaPilot`. It runs once, in an injection context. `beforeSwitch`, `timeout` and `endpoint` then belong to that pilot |

Also exported: `bundleVersion()`, which returns the bundle the page runs, and
the `Facts` type.

## How it works

1. At startup, the provider adds one guard to `canActivate` and
   `canActivateChild` of every top-level route. Redirects are skipped, since
   Angular refuses guards on them. Lazy children inherit the guard from their
   parent.
2. On each navigation, the guard calls `facts()` and asks the pilot about it,
   passing the URL the navigation is heading to.
3. While the pilot already knows the answer, and it names the running bundle,
   the guard passes synchronously. Otherwise it waits for the gateway (at most
   `timeout`). If the gateway picks another bundle, `beforeSwitch` runs and the
   page reloads into that bundle at the target URL.
4. A navigation that was cancelled, redirected or superseded in the meantime
   never switches.

What the pilot does underneath (one request per set of facts, the cookie that
lets the next visit land right, loop protection) is described in
[`@bundle-pilot/core`](https://www.npmjs.com/package/@bundle-pilot/core#how-it-decides).

## Caveats

- Add the provider to the root providers. A `router.resetConfig` after startup
  replaces the routes and drops the guard.
- If a `beforeunload` prompt is cancelled during a switch, the splash shown in
  `beforeSwitch` stays up.
- Tabs that send different facts flip the gateway's cookie at every switch.

## Compatibility

| | |
|---|---|
| Angular | 18 to 22, standalone or NgModule |
| Rendering | browser; on the server (SSR, prerender) it does nothing |
| Gateway | released together with it: `@bundle-pilot/angular` 1.4.0 goes with gateway 1.4.0 |

## License

[MIT](https://github.com/zenkiet/bundle-pilot/blob/main/libs/angular/package.json).
Part of [Bundle Pilot](https://github.com/zenkiet/bundle-pilot).
