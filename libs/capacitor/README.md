<p align="center">
  <img src="https://raw.githubusercontent.com/zenkiet/bundle-pilot/main/frontend/src/lib/assets/favicon.svg" alt="Bundle Pilot" width="96">
</p>

<h1 align="center">@bundle-pilot/capacitor</h1>

<p align="center">
  Signed over-the-air updates for Capacitor apps, rolled out with <a href="https://github.com/zenkiet/bundle-pilot">Bundle Pilot</a>.
</p>

<p align="center">
  <a href="https://www.npmjs.com/package/@bundle-pilot/capacitor"><img src="https://img.shields.io/npm/v/@bundle-pilot/capacitor?color=0f2a3a&label=npm" alt="npm version"></a>
  <a href="https://www.npmjs.com/package/@bundle-pilot/capacitor"><img src="https://img.shields.io/npm/dm/@bundle-pilot/capacitor?color=f5b400" alt="npm downloads"></a>
  <img src="https://img.shields.io/badge/Capacitor-8-119EFF?logo=capacitor&logoColor=white" alt="Capacitor 8">
  <img src="https://img.shields.io/badge/updater-Capgo-7c3aed" alt="Capgo updater">
  <img src="https://img.shields.io/badge/signatures-Ed25519-3fb950" alt="Ed25519 signatures">
  <img src="https://img.shields.io/badge/min%2Bgzip-1.9%20kB%20%2B%20core-3fb950" alt="1.9 kB min+gzip plus core">
  <a href="https://github.com/zenkiet/bundle-pilot/blob/main/libs/capacitor/package.json"><img src="https://img.shields.io/npm/l/@bundle-pilot/capacitor?color=0f2a3a" alt="license"></a>
</p>

<p align="center">
  <a href="#install">Install</a> ·
  <a href="#quick-start">Quick start</a> ·
  <a href="#use-cases">Use cases</a> ·
  <a href="#publishing-a-bundle">Publishing a bundle</a> ·
  <a href="#api">API</a> ·
  <a href="#how-it-works">How it works</a>
</p>

---

A Capacitor app runs its web code from files on the device, so a page reload
cannot move it to a new build. This package gives native apps the same rollout
as the web. It asks the Bundle Pilot gateway which bundle the device should
run, using the same facts and rules as the web. When that is another bundle,
it checks the bundle's signature, downloads it with
[Capgo's updater](https://github.com/Cap-go/capacitor-updater), and restarts
into it.

- **Signed end to end.** The bundle's manifest must verify against your
  Ed25519 key before anything is downloaded, and Capgo then checks every file
  against that manifest. It is the same signature the gateway checks for the
  web.
- **Rolls back by itself.** A bundle that never starts is rolled back by Capgo,
  and one that fails to bootstrap is reset by `startOta().failed()`. A bundle
  that rolled back is not offered again.
- **Never slows a start.** The last answer is remembered. While it needs no
  update, the app starts at once and asks again in the background.
- **Knows the native app.** The app's native version goes along with every
  question, so a bundle that needs newer native plugins never reaches an app
  that cannot run it.
- **Fails safe.** No network, an unsigned bundle or a failed download all keep
  the bundle the device already has.

## Install

```sh
npm install @bundle-pilot/capacitor @capgo/capacitor-updater @capacitor/app
npx cap sync
```

The updater is driven by this package, so turn off Capgo's own update flow:

```ts
// capacitor.config.ts
plugins: {
  CapacitorUpdater: {
    autoUpdate: 'off',
    updateUrl: '',
    statsUrl: '',
    channelUrl: '',
    responseTimeout: 120, // seconds; on iOS it bounds the whole download
  },
},
```

Requires Capacitor 8, `@capgo/capacitor-updater` 8 and `@capacitor/app` 8, and
a Bundle Pilot gateway that has `BUNDLE_PUBKEY` set, so that every bundle it
holds is signed.

## Quick start

**1. Confirm each start, in `main.ts`.** This tells Capgo the new bundle works,
and rolls back one that cannot bootstrap:

```ts
import { startOta } from '@bundle-pilot/capacitor';

const ota = startOta(environment.version);

bootstrapApplication(AppComponent, appConfig).catch(async (err) => {
  console.error(err);
  await ota.failed();
});
```

**2. Guard your routes.** With Angular, give the over-the-air pilot to
[`@bundle-pilot/angular`](https://www.npmjs.com/package/@bundle-pilot/angular):

```ts
import { provideBundlePilot } from '@bundle-pilot/angular';
import { createOtaPilot } from '@bundle-pilot/capacitor';

provideBundlePilot({
  facts: () => {
    const store = inject(StoreService);
    return store.id ? { storeID: store.id, backend: store.posVersion } : null;
  },
  pilot: () =>
    createOtaPilot({
      gateway: 'https://ota.example.com',
      publicKey: environment.bundleKey, // base64 Ed25519 public key
      version: environment.version, // the bundle this build is
    }),
});
```

Without Angular, call the pilot before showing a screen that depends on the
facts:

```ts
const pilot = createOtaPilot({ gateway, publicKey, version });

if (await pilot.check({ storeID })) showHome(); // false: an update is installing
```

In a browser, or when `gateway` or `publicKey` is empty, the pilot lets every
navigation through. The same code therefore runs in `ng serve` and on the web.

## Use cases

### Staged rollout to the field

Upload `4.80.0` to the gateway, and add a rule such as
`{ "when": { "storeID": ["2020210"] }, "bundle": "4.80.0" }`. Devices of that
store pick it up at their next navigation, while everyone else stays put. To
widen the rollout, change the rule. To back out, block the bundle in the admin
UI.

### A bundle that needs a newer native app

A bundle that uses a new native plugin says so in its `index.html`:

```html
<meta name="bundle-pilot:native" content=">=4.102.0">
```

Apps older than 4.102.0 never receive it. They get the closest older bundle
they can run instead, so you can publish web code ahead of the store review.

### Progress and a restart screen

`onProgress` reports the download, and `beforeSwitch` gives you a moment before
the restart:

```ts
pilot: () => {
  const splash = inject(SplashScreenService);
  return createOtaPilot({
    gateway,
    publicKey,
    version,
    onProgress: (percent) =>
      percent === null ? splash.hide() : splash.progress('Downloading update', percent),
    beforeSwitch: async () => {
      splash.progress('Installing update', 100);
      await new Promise((r) => setTimeout(r, 600));
      splash.restarting();
    },
  });
},
```

`percent` runs from 1 to 100. It creeps forward on its own, so the bar never
looks stuck on a slow network, and only the real end of the download reaches
100. `null` means the update gave up and the app stays where it is.

## Publishing a bundle

Build the app, write a `sha256sum` manifest of its files, sign it with your
Ed25519 key, zip it, and upload it with an API token that has `upload` scope:

```sh
cd dist/browser
find . -type f ! -path '*/.*' ! -name 'bundle.sha256*' -exec sha256sum {} + > bundle.sha256
openssl pkeyutl -sign -inkey bundle-key.pem -rawin -in bundle.sha256 | od -An -v -tx1 | tr -d ' \n' > bundle.sha256.sig
zip -qr -9 -X ../4.80.0.zip .
curl --fail -T ../4.80.0.zip -H "Authorization: Bearer $BUNDLE_PILOT_TOKEN" https://ota.example.com/__gateway/bundles/4.80.0.zip
```

The app's `publicKey` is the same key as the gateway's `BUNDLE_PUBKEY`, written
in base64. Key generation and precompressed files are covered in the
[gateway reference](https://github.com/zenkiet/bundle-pilot/blob/main/docs/reference.md#signing-bundles).

## API

### `createOtaPilot(options)`

Returns a `Pilot` (see [`@bundle-pilot/core`](https://www.npmjs.com/package/@bundle-pilot/core#api)).

| Option | Type | |
|---|---|---|
| `gateway` | `string` | the OTA gateway's origin, such as `https://ota.example.com` |
| `publicKey` | `string` | base64 Ed25519 public key that signs each `bundle.sha256` |
| `version` | `string` | the bundle this app runs |
| `onProgress` | `(percent: number \| null) => void` | 1 to 100 while an update downloads; `null` when it gives up |
| `beforeSwitch` | `() => unknown` | awaited after the download, right before the app restarts into it |

`pilot.check(facts)` returns `true` to go on. While an update downloads and
installs, it returns a promise that never resolves, because the app restarts.

### `startOta(version)`

Call it once, before bootstrapping. It returns `{ failed() }`: call `failed()`
when bootstrapping throws, to mark the running bundle as bad and reset to the
built-in one. Off the native platform it does nothing.

## How it works

1. **Ask.** The first `check` for a set of facts posts them, along with the
   native app version, to `POST /__gateway/data`. This happens once per
   session, through Capacitor's native HTTP, so CORS does not apply.
2. **Answer from memory.** The last answer is kept. While it names the running
   bundle, or one that rolled back, `check` returns `true` at once and the
   question runs in the background. An update it finds installs at the next
   navigation.
3. **Verify.** The pilot fetches `bundle.sha256` and its signature, and checks
   them with WebCrypto Ed25519. An unsigned or tampered bundle is never
   downloaded.
4. **Download.** Capgo fetches the files listed in the manifest and checks each
   one's SHA-256. A bundle already downloaded with the same manifest is reused.
5. **Switch.** The pilot runs `beforeSwitch`, then `CapacitorUpdater.set()`
   restarts the app into the new bundle. On the next start, `startOta` confirms
   it, or records a rollback.

Requests time out after 5 s and the download after 60 s. Any failure keeps the
current bundle and is not retried until the next start.

<details>
<summary>Keys kept in <code>localStorage</code></summary>

| Key | |
|---|---|
| `ota.decision` | the gateway's last answer, so a start never waits on it |
| `ota.trying` | the bundle being started, to detect a rollback |
| `ota.failed` | the last bundle that rolled back; it is not offered again |
| `ota.manifest.<id>` | the digest of the manifest a downloaded bundle came from |

</details>

## Requirements and caveats

- Ed25519 in WebCrypto needs Android System WebView 137 or later, or iOS 17 or
  later. Older devices cannot verify a signature, so they keep their bundle.
- Keep `autoUpdate: 'off'`. Otherwise Capgo's own update flow competes with
  this one.
- Runtime config such as `env.js` ships inside each bundle, so keep it in every
  version you upload.

## Compatibility

| | |
|---|---|
| Capacitor | 8, Android and iOS |
| `@capgo/capacitor-updater` | 8 |
| Gateway | released together with it: `@bundle-pilot/capacitor` 1.4.0 goes with gateway 1.4.0 |

## License

[MIT](https://github.com/zenkiet/bundle-pilot/blob/main/libs/capacitor/package.json).
Part of [Bundle Pilot](https://github.com/zenkiet/bundle-pilot).
