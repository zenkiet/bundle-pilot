export type Facts = Record<string, string | number | boolean | null | undefined>;

export interface PilotOptions {
	/** Default `/__gateway/data`. */
	endpoint?: string;
	/** Budget in ms for a decision the page waits on; past it the page stays. Default 3000. */
	timeout?: number;
	/** Runs before the page leaves for another bundle, e.g. to show a splash. */
	beforeSwitch?: () => unknown;
}

export interface Pilot {
	/** The bundle this page runs, from the gateway's meta tag; '' when the gateway did not serve it. */
	readonly version: string;
	/**
	 * true: stay. false: the page is leaving for the bundle the gateway picks for these facts.
	 * Synchronous unless the answer is unknown; url is where to land, alive() vetoes a stale switch.
	 */
	check(facts: Facts | null | undefined, url?: string, alive?: () => boolean): boolean | Promise<boolean>;
}

const BYPASS = 'ngsw-bypass'; // skips the service worker and names the bundle the switch asked for
const STORE = 'bundle-pilot';

export const bundleVersion = (): string =>
	globalThis.document?.querySelector('meta[name="bundle-pilot:bundle"]')?.getAttribute('content') ?? '';

// key drops what the gateway ignores and sorts, so equal facts give equal keys; '' when none are left.
function key(facts: Facts | null | undefined): string {
	const e = Object.entries(facts ?? {}).filter(([, v]) => v != null && v !== '');
	return e.length ? JSON.stringify(Object.fromEntries(e.sort(([a], [b]) => (a < b ? -1 : 1)))) : '';
}

const jar = (k: string) => `bundle_facts=${encodeURIComponent(k)}`;

const frames = () =>
	new Promise<void>((r) => {
		setTimeout(r, 250);
		requestAnimationFrame(() => requestAnimationFrame(() => r()));
	});

export function createPilot({ endpoint = '/__gateway/data', timeout = 3000, beforeSwitch }: PilotOptions = {}): Pilot {
	const version = bundleVersion();
	const stay = { version, check: () => true };
	if (!version) return stay;
	const here = new URL(location.href);
	const landed = here.searchParams.get(BYPASS);
	if (landed !== null) {
		here.searchParams.delete(BYPASS);
		history.replaceState(history.state, '', here.href);
		if (landed !== version) {
			console.warn(`[bundle-pilot] asked for ${landed}, the gateway served ${version}: staying`);
			return stay;
		}
	}
	if (here.searchParams.has('bundle')) return stay;

	let last: { key: string; bundle: string } | null = null;
	try {
		last = JSON.parse(localStorage.getItem(STORE) ?? 'null');
	} catch {}
	// One answer per facts per page: shared by every guard call, failures included.
	const asked = new Map<string, Promise<string | null>>();
	if (landed && last?.bundle === version) asked.set(last.key, Promise.resolve(version));
	const ask = (k: string) =>
		fetch(endpoint, {
			method: 'POST',
			headers: { 'Content-Type': 'application/json' },
			body: k,
			signal: AbortSignal.timeout?.(timeout)
		})
			.then((r) => (r.ok ? r.json() : null))
			.then((d) => {
				if (typeof d?.bundle !== 'string' || !d.bundle) return null;
				last = { key: k, bundle: d.bundle };
				try {
					localStorage.setItem(STORE, JSON.stringify(last));
				} catch {}
				return d.bundle as string;
			})
			.catch(() => null);
	let written = '';
	let switching = false;

	return {
		version,
		check(facts, url, alive = () => true) {
			if (switching) return false;
			const k = key(facts);
			if (!k) return true;
			if (k !== written) {
				written = k;
				document.cookie = `${jar(k)}; path=/; max-age=2592000; samesite=lax${location.protocol === 'https:' ? '; secure' : ''}`;
			}
			let answer = asked.get(k);
			if (!answer) asked.set(k, (answer = ask(k)));
			if (last?.key === k && last.bundle === version) return true;
			return answer.then(async (b) => {
				// The cookie both proves cookies work and holds the newest facts, which the gateway reads.
				if (!b || b === version || switching || !alive() || !document.cookie.includes(jar(k))) return !switching;
				switching = true;
				try {
					await beforeSwitch?.();
				} catch {}
				await frames();
				const next = new URL(url ?? location.href, document.baseURI);
				next.searchParams.set(BYPASS, b);
				location.replace(next.href);
				return false;
			});
		}
	};
}
