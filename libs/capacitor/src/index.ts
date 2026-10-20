import { factsKey, type Facts, type Pilot } from '@bundle-pilot/core';
import { App } from '@capacitor/app';
import { Capacitor, CapacitorHttp } from '@capacitor/core';
import { CapacitorUpdater } from '@capgo/capacitor-updater';
import { entries, verifyManifest } from './manifest.js';

export interface OtaOptions {
	/** The OTA gateway's origin, such as https://ota.example.com. */
	gateway: string;
	/** Base64 Ed25519 public key that signs each bundle's bundle.sha256. */
	publicKey: string;
	/** The bundle this app runs. */
	version: string;
	/** 1 to 100 while an update downloads, smoothed so it never looks stuck; null when the update gives up. */
	onProgress?: (percent: number | null) => void;
	/** Awaited once the update is downloaded, right before the app reloads into it. */
	beforeSwitch?: () => unknown;
}

const TIMEOUT = 5000; // per request
const DOWNLOAD_TIMEOUT = 60_000;

// The first three are kept from the app this code came from, so devices in the field keep their state.
const TRYING = 'ota.trying'; // the bundle set() is about to start
const FAILED = 'ota.failed'; // a bundle Capgo rolled back: never offered again
const MANIFEST = 'ota.manifest.'; // + bundle id: digest of the manifest it was downloaded with
const DECISION = 'ota.decision'; // the gateway's last answer, { key, bundle }, so a start never waits on it

/**
 * A Pilot for native apps: asks the OTA gateway which bundle these facts get, verifies its signed
 * manifest, downloads it with Capgo and reloads into it. Off the native platform it always stays.
 *
 * One question per facts per session. While the last answer for these facts needs no update, check()
 * goes on at once and the question revalidates in the background; an update it finds installs at the
 * next navigation. A failure stays and is not retried that session.
 */
export function createOtaPilot(o: OtaOptions): Pilot {
	if (!Capacitor.isNativePlatform() || !o.gateway || !o.publicKey) return { version: o.version, check: () => true };
	const api = `${o.gateway.replace(/\/+$/, '')}/__gateway`;
	const stays = (v: string | null | undefined) => !v || v === o.version || v === localStorage.getItem(FAILED);

	let last: { key: string; bundle: string } | null = null;
	try {
		last = JSON.parse(localStorage.getItem(DECISION) ?? 'null');
	} catch {}
	const asked = new Map<string, Promise<string | null>>();
	let installing: Promise<boolean> | undefined;

	const ask = (k: string, facts: Facts) =>
		App.getInfo()
			.then(({ version: native }) =>
				CapacitorHttp.post({
					url: `${api}/data`,
					headers: { 'Content-Type': 'application/json' },
					data: { ...facts, native },
					connectTimeout: TIMEOUT,
					readTimeout: TIMEOUT
				})
			)
			.then((res) => {
				const v = res.status === 200 ? res.data?.bundle : undefined;
				if (typeof v !== 'string' || !v) return null;
				last = { key: k, bundle: v };
				localStorage.setItem(DECISION, JSON.stringify(last));
				return v;
			})
			.catch(() => null);

	return {
		version: o.version,
		check(facts) {
			const k = factsKey(facts);
			if (!k) return true;
			let answer = asked.get(k);
			if (!answer) asked.set(k, (answer = ask(k, facts!)));
			if (last?.key === k && stays(last.bundle)) return true;
			return answer.then(
				(v) =>
					stays(v) ||
					(installing ??= install(api, o, v!).catch((e) => {
						console.warn('[bundle-pilot] update failed, staying:', e);
						localStorage.removeItem(TRYING);
						o.onProgress?.(null);
						return true;
					}))
			);
		}
	};
}

/** Installs bundle v; resolves true to stay, and never resolves once the app reloads into it. */
async function install(api: string, o: OtaOptions, v: string): Promise<boolean> {
	const base = `${api}/bundles/${encodeURIComponent(v)}`;
	const [text, sig] = await Promise.all([fetchBytes(`${base}/bundle.sha256`), fetchBytes(`${base}/bundle.sha256.sig`)]);
	const manifest = text && sig && (await verifyManifest(bytes(o.publicKey), text, sig)) ? entries(text, base) : undefined;
	if (!text || !manifest) throw new Error(`${v} is not signed`);
	// Capgo keeps no checksum for manifest downloads: the manifest's digest names the bundle.
	const digest = btoa(String.fromCharCode(...new Uint8Array(await crypto.subtle.digest('SHA-256', text as BufferSource))));

	// Progress creeps toward 99 so the bar never looks stuck; real events pull it up, only the end reaches 100.
	let real = 0;
	let shown = 1;
	o.onProgress?.(shown);
	const onDownload = await CapacitorUpdater.addListener('download', ({ percent, bundle }) => {
		if (bundle?.version === v) real = Math.min(percent, 99);
	});
	const tick = setInterval(() => o.onProgress?.((shown += Math.max((99 - shown) * 0.03, (real - shown) * 0.25))), 250);
	let bundle;
	try {
		const { bundles } = await CapacitorUpdater.list();
		bundle = bundles.find(
			(b) => b.version === v && localStorage.getItem(MANIFEST + b.id) === digest && (b.status === 'pending' || b.status === 'success')
		);
		if (!bundle) {
			for (const b of bundles.filter((b) => b.version === v)) {
				localStorage.removeItem(MANIFEST + b.id);
				await CapacitorUpdater.delete({ id: b.id }).catch(() => undefined);
			}
			// Recorded even after the timeout, which stops waiting but not the native download.
			const done = CapacitorUpdater.download({ url: `${base}.zip`, version: v, manifest }).then((b) => {
				localStorage.setItem(MANIFEST + b.id, digest);
				return b;
			});
			bundle = await within(DOWNLOAD_TIMEOUT, done);
		}
	} finally {
		clearInterval(tick);
		void onDownload.remove();
	}

	o.onProgress?.(100);
	await o.beforeSwitch?.();
	localStorage.setItem(TRYING, v);
	await CapacitorUpdater.set({ id: bundle.id }); // reloads; never resolves on success
	return false;
}

/**
 * Call once before bootstrapping: tells Capgo this bundle started, and remembers a bundle it rolled back.
 * failed() is for a bootstrap error: a bundle that started but cannot run would otherwise never roll back.
 */
export function startOta(version: string): { failed(): Promise<void> } {
	if (!Capacitor.isNativePlatform()) return { failed: async () => {} };
	const trying = localStorage.getItem(TRYING);
	localStorage.removeItem(TRYING);
	if (trying && trying !== version) localStorage.setItem(FAILED, trying);
	void CapacitorUpdater.notifyAppReady();
	return {
		async failed() {
			const { bundle } = await CapacitorUpdater.current();
			if (bundle.id === 'builtin') return;
			localStorage.setItem(FAILED, bundle.version);
			await CapacitorUpdater.reset();
		}
	};
}

// Android's text mode drops the last newline, which breaks the signature: fetch bytes (base64).
async function fetchBytes(url: string): Promise<Uint8Array | undefined> {
	const res = await CapacitorHttp.get({ url, responseType: 'arraybuffer', connectTimeout: TIMEOUT, readTimeout: TIMEOUT });
	return res.status === 200 && typeof res.data === 'string' ? bytes(res.data) : undefined;
}

const bytes = (b64: string) => Uint8Array.from(atob(b64), (c) => c.charCodeAt(0));

const within = <T>(ms: number, p: Promise<T>) =>
	Promise.race([p, new Promise<never>((_, reject) => setTimeout(() => reject(new Error('download timeout')), ms))]);
